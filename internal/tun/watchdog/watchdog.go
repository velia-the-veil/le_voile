package watchdog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultInterval est la cadence de poll fixée par l'architecture
// (architecture.md §TUN/Firewall/Routing Patterns, NFR16 — détection < 5 s
// avec poll 3 s).
const DefaultInterval = 3 * time.Second

// Sentinelles exportées pour les usages courants.
var (
	ErrAlreadyRunning = errors.New("watchdog: already running")
	ErrMissingChecker = errors.New("watchdog: InterfaceChecker requis")
	ErrMissingOnLost  = errors.New("watchdog: OnLost requis")
)

// Logger abstrait l'émission de logs structurés. Laissé optionnel ; si nil,
// le watchdog reste silencieux. Le service fournit son propre logger.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Config paramètre un Watchdog.
type Config struct {
	// Checker inspecte l'interface à chaque cycle. Requis.
	Checker InterfaceChecker
	// OnLost est invoqué quand une perte ou altération est détectée
	// (StatusMissing ou StatusInvalid). DOIT être rapide ou lancer sa propre
	// goroutine. Le watchdog appelle OnLost de manière non-réentrante (un
	// seul OnLost en vol à la fois, garanti par flag atomic).
	OnLost func(ctx context.Context) error
	// Interval entre deux polls ; 0 → DefaultInterval (3 s).
	Interval time.Duration
	// Logger optionnel pour tracer détections et erreurs.
	Logger Logger
}

// Watchdog surveille une interface TUN/Wintun via un Checker et déclenche
// un callback OnLost quand la disparition ou l'altération est détectée.
//
// Cycle de vie : NewWatchdog → Start(ctx) (bloquant) → Stop() ou ctx.Done().
// Une seule instance Start simultanée par Watchdog.
type Watchdog struct {
	cfg Config

	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	done      chan struct{}
	parentCtx context.Context // ctx parent (service), non annulé par Stop()

	// onLostWg attend la fin de la goroutine OnLost en vol avant que Stop()
	// ne retourne — empêche le data race entre recoverTUN et shutdown().
	onLostWg sync.WaitGroup

	// recovering est armé avant l'invocation de OnLost et désarmé après son
	// retour. Empêche les triggers concurrents en cas de poll rapide sur une
	// interface durablement absente (AC6 — 1 reconnect max concurrent).
	recovering atomic.Bool
}

// NewWatchdog valide la configuration et construit un Watchdog prêt à
// démarrer.
func NewWatchdog(cfg Config) (*Watchdog, error) {
	if cfg.Checker == nil {
		return nil, ErrMissingChecker
	}
	if cfg.OnLost == nil {
		return nil, ErrMissingOnLost
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	return &Watchdog{cfg: cfg}, nil
}

// Start lance la boucle de poll. Bloque jusqu'à ctx.Done() ou Stop().
// Retourne nil en sortie normale. Retourne ErrAlreadyRunning si une
// instance est déjà active.
func (w *Watchdog) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ErrAlreadyRunning
	}
	w.parentCtx = ctx // conservé pour OnLost (M2 fix)
	ctx, w.cancel = context.WithCancel(ctx)
	w.done = make(chan struct{})
	w.running = true
	w.mu.Unlock()

	defer func() {
		// Attendre la fin de tout OnLost en vol avant de signaler done
		// (H1 fix — empêche le data race entre recoverTUN et shutdown).
		w.onLostWg.Wait()
		w.mu.Lock()
		w.running = false
		w.cancel = nil
		w.parentCtx = nil
		close(w.done)
		w.mu.Unlock()
	}()

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// Stop annule la boucle en cours et attend sa sortie. Idempotent :
// un second appel sur un Watchdog arrêté est no-op.
func (w *Watchdog) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// tick exécute un cycle : check + éventuel déclenchement OnLost.
func (w *Watchdog) tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	status, err := w.cfg.Checker.Check(ctx)
	if err != nil {
		w.warnf("tun.watchdog: check error: %v", err)
		// On continue la boucle ; une erreur transitoire ne déclenche pas
		// de reconnect (moins invasif que de paniquer à la première erreur
		// netlink).
		return
	}
	if status == StatusOK {
		return
	}
	// Anti-flapping : un seul OnLost en vol à la fois.
	if !w.recovering.CompareAndSwap(false, true) {
		w.infof("tun.watchdog: recovery already in progress (status=%s)", status)
		return
	}
	w.warnf("tun.watchdog: interface lost (status=%s) — triggering recovery", status)

	// OnLost reçoit le parentCtx (service ctx), pas le ctx interne du
	// watchdog — sinon Stop() annule le ctx et firewall.Activate /
	// tunnel.Connect échouent avec context.Canceled (fix M2).
	//
	// onLostWg empêche Stop()/done de se fermer avant que la goroutine ne
	// termine — élimine le data race entre recoverTUN et shutdown() (fix H1).
	recoveryCtx := w.parentCtx
	w.onLostWg.Add(1)
	go func() {
		defer w.onLostWg.Done()
		defer w.recovering.Store(false)
		if err := w.cfg.OnLost(recoveryCtx); err != nil {
			w.errorf("tun.watchdog: recovery failed: %v", err)
			return
		}
		w.infof("tun.watchdog: recovery completed")
	}()
}

// IsRecovering expose l'état du flag anti-flapping. Utilisé en test et pour
// l'UI de supervision.
func (w *Watchdog) IsRecovering() bool { return w.recovering.Load() }

func (w *Watchdog) infof(format string, args ...any) {
	if w.cfg.Logger != nil {
		w.cfg.Logger.Infof(format, args...)
	}
}
func (w *Watchdog) warnf(format string, args ...any) {
	if w.cfg.Logger != nil {
		w.cfg.Logger.Warnf(format, args...)
	}
}
func (w *Watchdog) errorf(format string, args ...any) {
	if w.cfg.Logger != nil {
		w.cfg.Logger.Errorf(format, args...)
	}
}
