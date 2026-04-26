//go:build windows

package watchdog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockChecker scripte une suite de statuts retournés à chaque appel Check.
type mockChecker struct {
	mu     sync.Mutex
	calls  int
	script []Status
	// errAt : index d'appel auquel retourner une erreur (0 = jamais).
	errAt  int
	errVal error
}

func (m *mockChecker) Check(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.errAt > 0 && m.calls == m.errAt {
		return StatusMissing, m.errVal
	}
	if m.calls-1 < len(m.script) {
		return m.script[m.calls-1], nil
	}
	// Après épuisement du script, on reste sur le dernier état.
	if len(m.script) > 0 {
		return m.script[len(m.script)-1], nil
	}
	return StatusOK, nil
}

func (m *mockChecker) nCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// testLogger capture les logs pour vérification.
type testLogger struct {
	mu   sync.Mutex
	logs []string
}

func (l *testLogger) Infof(f string, a ...any)  { l.add("INFO", f, a) }
func (l *testLogger) Warnf(f string, a ...any)  { l.add("WARN", f, a) }
func (l *testLogger) Errorf(f string, a ...any) { l.add("ERROR", f, a) }
func (l *testLogger) add(level, f string, a []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, level+" "+f)
}
func (l *testLogger) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.logs))
	copy(out, l.logs)
	return out
}

func TestNewWatchdog_ValidatesConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{"missing-checker", Config{OnLost: func(context.Context) error { return nil }}, ErrMissingChecker},
		{"missing-onlost", Config{Checker: &mockChecker{}}, ErrMissingOnLost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWatchdog(tc.cfg)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewWatchdog err=%v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewWatchdog_DefaultInterval(t *testing.T) {
	w, err := NewWatchdog(Config{
		Checker: &mockChecker{},
		OnLost:  func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewWatchdog: %v", err)
	}
	if w.cfg.Interval != DefaultInterval {
		t.Errorf("Interval = %v, want %v (default)", w.cfg.Interval, DefaultInterval)
	}
}

func TestWatchdog_StatusOK_NoTrigger(t *testing.T) {
	chk := &mockChecker{script: []Status{StatusOK, StatusOK, StatusOK}}
	var triggered atomic.Int32
	w, _ := NewWatchdog(Config{
		Checker:  chk,
		OnLost:   func(context.Context) error { triggered.Add(1); return nil },
		Interval: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.Start(ctx) }()

	time.Sleep(40 * time.Millisecond)
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := triggered.Load(); got != 0 {
		t.Errorf("OnLost appelé %d fois alors que StatusOK", got)
	}
	if chk.nCalls() < 3 {
		t.Errorf("Checker.Check appelé %d fois, attendu ≥ 3", chk.nCalls())
	}
}

func TestWatchdog_Missing_TriggersOnLost(t *testing.T) {
	chk := &mockChecker{script: []Status{StatusOK, StatusMissing, StatusOK, StatusOK}}
	triggered := make(chan struct{}, 4)
	w, _ := NewWatchdog(Config{
		Checker: chk,
		OnLost: func(context.Context) error {
			triggered <- struct{}{}
			return nil
		},
		Interval: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- w.Start(ctx) }()

	select {
	case <-triggered:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OnLost non déclenché sur StatusMissing")
	}
	cancel()
	<-errCh
}

func TestWatchdog_Invalid_TriggersOnLost(t *testing.T) {
	chk := &mockChecker{script: []Status{StatusInvalid}}
	triggered := make(chan struct{}, 1)
	w, _ := NewWatchdog(Config{
		Checker: chk,
		OnLost: func(context.Context) error {
			triggered <- struct{}{}
			return nil
		},
		Interval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	select {
	case <-triggered:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OnLost non déclenché sur StatusInvalid (AC4)")
	}
}

func TestWatchdog_AntiFlapping_SingleRecoveryConcurrent(t *testing.T) {
	chk := &mockChecker{script: []Status{
		StatusMissing, StatusMissing, StatusMissing, StatusMissing, StatusMissing,
	}}
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	onLostStarted := make(chan struct{}, 10)
	onLostRelease := make(chan struct{})

	w, _ := NewWatchdog(Config{
		Checker: chk,
		OnLost: func(ctx context.Context) error {
			n := concurrent.Add(1)
			for {
				m := maxConcurrent.Load()
				if n <= m || maxConcurrent.CompareAndSwap(m, n) {
					break
				}
			}
			onLostStarted <- struct{}{}
			<-onLostRelease
			concurrent.Add(-1)
			return nil
		},
		Interval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	<-onLostStarted
	// Laisser plusieurs cycles s'écouler pendant que OnLost est "en cours".
	time.Sleep(50 * time.Millisecond)
	if got := concurrent.Load(); got != 1 {
		t.Errorf("concurrent OnLost = %d, attendu 1 (AC6)", got)
	}
	if !w.IsRecovering() {
		t.Error("IsRecovering() = false pendant OnLost, attendu true")
	}
	close(onLostRelease)
	// Laisser le premier OnLost terminer.
	time.Sleep(20 * time.Millisecond)
	if max := maxConcurrent.Load(); max > 1 {
		t.Errorf("max concurrent OnLost = %d, attendu ≤ 1 (AC6)", max)
	}
}

func TestWatchdog_CheckError_NoTrigger(t *testing.T) {
	chk := &mockChecker{
		script: []Status{StatusOK, StatusOK, StatusOK},
		errAt:  2, // 2e appel retourne une erreur
		errVal: errors.New("netlink transient"),
	}
	var triggered atomic.Int32
	logger := &testLogger{}
	w, _ := NewWatchdog(Config{
		Checker:  chk,
		OnLost:   func(context.Context) error { triggered.Add(1); return nil },
		Interval: 5 * time.Millisecond,
		Logger:   logger,
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Start(ctx) }()
	time.Sleep(40 * time.Millisecond)
	cancel()

	if got := triggered.Load(); got != 0 {
		t.Errorf("OnLost appelé %d fois sur erreur transitoire, attendu 0", got)
	}
	// Vérifier qu'on a émis au moins un WARN.
	found := false
	for _, l := range logger.snapshot() {
		if len(l) >= 4 && l[:4] == "WARN" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Aucun log WARN émis sur erreur de check")
	}
}

func TestWatchdog_StopIdempotent(t *testing.T) {
	w, _ := NewWatchdog(Config{
		Checker:  &mockChecker{},
		OnLost:   func(context.Context) error { return nil },
		Interval: 5 * time.Millisecond,
	})
	// Stop sans Start : no-op.
	w.Stop()

	ctx := context.Background()
	go func() { _ = w.Start(ctx) }()
	time.Sleep(10 * time.Millisecond)

	w.Stop()
	w.Stop() // idempotent
}

func TestWatchdog_ContextCancel_StopsPromptly(t *testing.T) {
	w, _ := NewWatchdog(Config{
		Checker:  &mockChecker{},
		OnLost:   func(context.Context) error { return nil },
		Interval: 3 * time.Second, // gros interval — on teste que ctx.Done() réveille
	})
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.Start(ctx) }()
	time.Sleep(5 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start n'a pas retourné après ctx.Cancel() (AC5)")
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("Start a mis %v à sortir après cancel, attendu < 200ms", d)
	}
}

func TestWatchdog_AlreadyRunning(t *testing.T) {
	w, _ := NewWatchdog(Config{
		Checker:  &mockChecker{},
		OnLost:   func(context.Context) error { return nil },
		Interval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()
	time.Sleep(10 * time.Millisecond)

	err := w.Start(ctx)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("deuxième Start err=%v, want %v", err, ErrAlreadyRunning)
	}
}

func TestWatchdog_OnLost_ErrorLogged(t *testing.T) {
	chk := &mockChecker{script: []Status{StatusMissing}}
	logger := &testLogger{}
	done := make(chan struct{}, 1)
	w, _ := NewWatchdog(Config{
		Checker: chk,
		OnLost: func(context.Context) error {
			done <- struct{}{}
			return errors.New("recovery failed: routing.Setup")
		},
		Interval: 5 * time.Millisecond,
		Logger:   logger,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	<-done
	time.Sleep(20 * time.Millisecond) // laisser le log ERROR s'écrire

	foundErr := false
	for _, l := range logger.snapshot() {
		if len(l) >= 5 && l[:5] == "ERROR" {
			foundErr = true
			break
		}
	}
	if !foundErr {
		t.Error("Aucun log ERROR émis quand OnLost retourne une erreur")
	}
}

func TestStatus_String(t *testing.T) {
	cases := map[Status]string{
		StatusOK: "ok", StatusMissing: "missing", StatusInvalid: "invalid",
		Status(99): "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}

// TestWatchdog_Stop_WaitsForOnLost vérifie que Stop() bloque jusqu'à la
// fin de la goroutine OnLost en vol (fix H1 — empêche data race avec shutdown).
func TestWatchdog_Stop_WaitsForOnLost(t *testing.T) {
	chk := &mockChecker{script: []Status{StatusMissing}}
	onLostStarted := make(chan struct{})
	onLostRelease := make(chan struct{})
	var onLostDone atomic.Bool

	w, _ := NewWatchdog(Config{
		Checker: chk,
		OnLost: func(ctx context.Context) error {
			onLostStarted <- struct{}{}
			<-onLostRelease
			onLostDone.Store(true)
			return nil
		},
		Interval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	// Attendre que OnLost démarre.
	<-onLostStarted

	// Lancer Stop() en background — il doit bloquer tant que OnLost n'a pas fini.
	stopDone := make(chan struct{})
	go func() {
		w.Stop()
		close(stopDone)
	}()

	// Stop ne doit PAS encore avoir retourné.
	time.Sleep(30 * time.Millisecond)
	select {
	case <-stopDone:
		t.Fatal("Stop() a retourné avant la fin de OnLost — data race possible (H1)")
	default:
	}

	// Libérer OnLost.
	close(onLostRelease)

	// Stop doit retourner rapidement après.
	select {
	case <-stopDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop() bloqué indéfiniment après fin de OnLost")
	}

	if !onLostDone.Load() {
		t.Error("OnLost n'a pas terminé avant Stop()")
	}
}

// TestWatchdog_OnLost_ReceivesParentCtx vérifie que OnLost reçoit le parent
// ctx (service ctx) et non le ctx annulé du watchdog (fix M2).
func TestWatchdog_OnLost_ReceivesParentCtx(t *testing.T) {
	chk := &mockChecker{script: []Status{StatusMissing}}
	onLostCtxCh := make(chan context.Context, 1)

	w, _ := NewWatchdog(Config{
		Checker: chk,
		OnLost: func(ctx context.Context) error {
			onLostCtxCh <- ctx
			return nil
		},
		Interval: 5 * time.Millisecond,
	})

	parentCtx := context.Background()
	go func() { _ = w.Start(parentCtx) }()

	select {
	case got := <-onLostCtxCh:
		// Le ctx reçu par OnLost ne doit PAS être annulé (c'est le parent).
		if got.Err() != nil {
			t.Errorf("OnLost a reçu un ctx annulé: %v (devrait être le parent ctx)", got.Err())
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OnLost non déclenché")
	}
	w.Stop()
}

func TestCheckerConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     CheckerConfig
		wantErr bool
	}{
		{"ok", CheckerConfig{Name: "levoile0", ExpectedMTU: 1420}, false},
		{"empty-name", CheckerConfig{Name: "", ExpectedMTU: 1420}, true},
		{"zero-mtu", CheckerConfig{Name: "levoile0", ExpectedMTU: 0}, true},
		{"negative-mtu", CheckerConfig{Name: "levoile0", ExpectedMTU: -1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
