package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// pumpFrameHeaderSize must equal relay.TunnelFrameHeaderSize (2 bytes BE).
const pumpFrameHeaderSize = 2

// pumpMaxFrameSize must equal relay.TunnelMaxFrameSize. Any IP packet larger
// than this is dropped — the kernel sees an MTU-sized link (config default
// 1420) so this never triggers in practice.
const pumpMaxFrameSize = 1420

// R-T8 BISECT round 5 (2026-05-10) — watchdog d'activité du pump.
//
// Use case : le stream HTTP/3 POST /tunnel peut mourir silencieusement côté
// serveur (RST stream, middlebox reset, NAT64 relay drop, congestion radio
// LTE qui freeze le retour) SANS que la connexion QUIC top-level meure. Le
// heartbeat /health passe alors car il utilise un autre stream sur la même
// connexion saine. Le pump bloque indéfiniment sur `read hdr` car le QUIC
// reader attend du data qui n'arrivera jamais. Symptôme observé sur Free
// Mobile 4G LTE / NAT64 : tunnel apparaît zombie, RX presque nul mais TX
// continue à pousser des paquets (qui se perdent dans le vide), curl ipify
// FAIL.
//
// Le watchdog mesure RX/TX bytes via compteurs atomiques. Critère de trip :
// sur la fenêtre `pumpWatchdogStallWindow`, si TX dépasse
// `pumpWatchdogMinTxBytes` (preuve d'activité utilisateur réelle) MAIS RX
// reste inférieur à `pumpWatchdogMinRxBytes` (pas de retour significatif),
// trip une erreur → le pump exit → runGomobilePump émet "disconnected" →
// scheduleAutoReconnect Kotlin re-établit le tunnel.
//
// Pourquoi pas un check binaire RX==0 : observé en prod que le stream
// zombie laisse passer quelques paquets résiduels (TCP ACK différés, DNS
// cache hit) — RX reste > 0 mais reste catastrophiquement bas. Critère de
// ratio (RX << TX) plus robuste qu'un check binaire.
const (
	pumpWatchdogTickInterval = 5 * time.Second
	pumpWatchdogStallWindow  = 15 * time.Second
	pumpWatchdogMinTxBytes   = 10 * 1024 // 10 KB TX sur 15s = preuve activité utilisateur (Chrome page = 50 KB+ en quelques s)
	pumpWatchdogMinRxBytes   = 1 * 1024  // 1 KB RX minimum attendu (10% du TX)
)

// PacketWriter delivers an inbound IP packet to the local TUN device.
// The pump calls it from a single goroutine.
type PacketWriter func(pkt []byte) (int, error)

// RunPump opens a POST /tunnel stream to the relay and bridges IP packets
// between the caller-provided channel/writer and the relay's NAT in both
// directions. Each frame is encoded as `[2-byte BE length][packet bytes]`
// (matches internal/relay/tunnel_handler.go). Authenticates with the current
// session token (Bearer); the caller must call EnsureSessionToken first.
//
// Why a channel + writer rather than a PacketDevice: tun.Device.Read has no
// context awareness and cannot be interrupted, so reading directly inside
// RunPump would leak a goroutine every time the pump returns due to a
// stream error (next pump iteration would then race on a second concurrent
// dev.Read, which tun.Device explicitly forbids). The caller must run a
// long-lived reader goroutine that survives pump restarts and writes
// packets into outbound; closing outbound signals that input is permanently
// gone.
//
// Blocks until the first of: ctx cancellation, outbound channel close,
// inbound writer error, stream EOF, or stream error. Returns the first
// non-nil error observed (nil if ctx cancelled cleanly or outbound closed).
func (c *Client) RunPump(ctx context.Context, outbound <-chan []byte, inbound PacketWriter) error {
	if c.state.Get() != StateConnected {
		return ErrNotConnected
	}
	token := c.SessionToken()
	if token == "" {
		return ErrTokenExpired
	}

	pr, pw := io.Pipe()
	url := c.relayURL("/tunnel")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		_ = pw.Close()
		return fmt.Errorf("tunnel: pump: build request: %w", err)
	}
	// ContentLength=-1 disables length negotiation — request body streams as
	// long as the pipe is open.
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		_ = pw.Close()
		return fmt.Errorf("tunnel: pump: open stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = pw.Close()
		return fmt.Errorf("tunnel: pump: relay returned status %d", resp.StatusCode)
	}

	return runPumpLoops(ctx, outbound, inbound, pw, resp.Body)
}

// runPumpLoops is the transport-agnostic core of RunPump. Extracted so tests
// can drive the framing logic with in-memory io.Pipes instead of an HTTP
// roundtripper (Go's HTTP/1.1 test server can't do full-duplex streaming
// with a chunked request body — production uses HTTP/3 which can).
func runPumpLoops(ctx context.Context, outbound <-chan []byte, inbound PacketWriter, txOut io.WriteCloser, rxIn io.ReadCloser) error {
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	// Watcher: when the pump context cancels (either side errored or the
	// caller cancelled), close both ends so blocked I/O unblocks. Idempotent
	// closes are fine — io.Pipe and http.Response.Body tolerate them.
	go func() {
		<-pumpCtx.Done()
		_ = rxIn.Close()
		_ = txOut.Close()
	}()

	var (
		errOnce  sync.Once
		firstErr error
	)
	setErr := func(e error) {
		if e == nil {
			return
		}
		errOnce.Do(func() { firstErr = e })
		pumpCancel()
	}

	// R-T8 BISECT round 5 — watchdog activity counters. Updated lock-free
	// by both goroutines, sampled by the watchdog goroutine.
	var (
		txBytesTotal atomic.Uint64
		rxBytesTotal atomic.Uint64
	)

	// Watchdog goroutine : detects "stream zombie" (TX active mais RX
	// catastrophiquement faible sur la stall window). Cf. constantes
	// ci-dessus pour la justification du critère ratio (RX << TX).
	//
	// Algorithme : maintient une fenêtre glissante de la durée
	// pumpWatchdogStallWindow. À chaque tick (10s), additionne deltaTx et
	// deltaRx au compteur de fenêtre. Si la fenêtre atteint son âge complet,
	// vérifie si TX > minTx ET RX < minRx → trip. Sinon (fenêtre récente, ou
	// ratio sain), reset la fenêtre.
	go func() {
		ticker := time.NewTicker(pumpWatchdogTickInterval)
		defer ticker.Stop()

		var (
			lastTx, lastRx        uint64
			windowTx, windowRx    uint64
			windowStart           = time.Now()
			stallSignaled         bool
		)

		for {
			select {
			case <-pumpCtx.Done():
				return
			case now := <-ticker.C:
				curTx := txBytesTotal.Load()
				curRx := rxBytesTotal.Load()
				windowTx += curTx - lastTx
				windowRx += curRx - lastRx
				lastTx, lastRx = curTx, curRx

				// Tant que la fenêtre n'a pas atteint la durée requise, on
				// continue d'accumuler. Le check se fait quand on a assez
				// de signal pour conclure.
				if now.Sub(windowStart) < pumpWatchdogStallWindow {
					continue
				}

				// Fenêtre complète. Évaluation :
				//  - TX < minTx → utilisateur idle, pas de signal pour conclure.
				//    Reset fenêtre.
				//  - TX >= minTx ET RX >= minRx → trafic sain, ratio acceptable.
				//    Reset fenêtre.
				//  - TX >= minTx ET RX < minRx → stream zombie, trip.
				switch {
				case windowTx < pumpWatchdogMinTxBytes:
					// Idle légitime. Pas de signal pour stall.
					if stallSignaled {
						slog.Info("tunnel: pump watchdog: idle (no recent TX)",
							"window_tx", windowTx, "window_rx", windowRx)
						stallSignaled = false
					}
				case windowRx >= pumpWatchdogMinRxBytes:
					// Sain. RX confirme que le retour fonctionne.
					if stallSignaled {
						slog.Info("tunnel: pump watchdog: recovered",
							"window_tx", windowTx, "window_rx", windowRx)
						stallSignaled = false
					}
				default:
					// TX actif mais RX catastrophiquement faible. Stream zombie.
					slog.Error("tunnel: pump watchdog: stream zombie — tripping reconnect",
						"window_duration", now.Sub(windowStart).Round(time.Second),
						"window_tx_bytes", windowTx,
						"window_rx_bytes", windowRx,
						"min_tx", pumpWatchdogMinTxBytes,
						"min_rx", pumpWatchdogMinRxBytes,
						"tx_total", curTx,
						"rx_total", curRx)
					setErr(fmt.Errorf("tunnel: pump: stream zombie (window TX=%d RX=%d, total TX=%d RX=%d)",
						windowTx, windowRx, curTx, curRx))
					return
				}

				// Reset fenêtre pour la prochaine évaluation.
				windowStart = now
				windowTx, windowRx = 0, 0
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// Outbound goroutine: outbound channel → relay.
	go func() {
		defer wg.Done()
		// Cancelling pumpCtx on exit wakes the inbound goroutine and the
		// watcher even when this side terminates cleanly (e.g. outbound
		// channel closed). Without this, an EOF on one side would leave the
		// other blocked on its read forever.
		defer pumpCancel()
		// Closing the writer signals EOF to the relay reader; the response
		// stream will then close cleanly.
		defer txOut.Close()
		// Frame buffer reusable — concat header + payload en un seul Write
		// pour diviser le nombre de syscalls/I/O ops par 2 (perf : à
		// 8000 paquets/s, 16000 Writes/s → 8000 Writes/s). Capacité
		// allouée à pumpMaxFrameSize+pumpFrameHeaderSize, slice réinitialisé
		// à chaque paquet pour éviter de re-allouer.
		frame := make([]byte, 0, pumpFrameHeaderSize+pumpMaxFrameSize)
		for {
			select {
			case <-pumpCtx.Done():
				return
			case pkt, ok := <-outbound:
				if !ok {
					return // outbound source permanently gone
				}
				n := len(pkt)
				if n == 0 {
					continue
				}
				if n > pumpMaxFrameSize {
					// Defensive: should never happen with a properly configured MTU.
					continue
				}
				// Concat header + payload : 1 seul Write au lieu de 2.
				frame = frame[:pumpFrameHeaderSize+n]
				binary.BigEndian.PutUint16(frame[:pumpFrameHeaderSize], uint16(n))
				copy(frame[pumpFrameHeaderSize:], pkt)
				if _, werr := txOut.Write(frame); werr != nil {
					if pumpCtx.Err() == nil {
						setErr(fmt.Errorf("tunnel: pump: write frame: %w", werr))
					}
					return
				}
				// R-T8 round 5 — feed le watchdog. Compté uniquement après
				// Write réussi : un échec Write ne reflète pas un TX réel.
				txBytesTotal.Add(uint64(pumpFrameHeaderSize + n))
			}
		}
	}()

	// Inbound goroutine: relay → inbound writer.
	go func() {
		defer wg.Done()
		// See outbound goroutine — pumpCancel on exit unblocks the peer.
		defer pumpCancel()
		hdr := make([]byte, pumpFrameHeaderSize)
		buf := make([]byte, pumpMaxFrameSize)
		for {
			if pumpCtx.Err() != nil {
				return
			}
			if _, err := io.ReadFull(rxIn, hdr); err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && pumpCtx.Err() == nil {
					setErr(fmt.Errorf("tunnel: pump: read hdr: %w", err))
				}
				return
			}
			n := binary.BigEndian.Uint16(hdr)
			if n == 0 || n > pumpMaxFrameSize {
				setErr(fmt.Errorf("tunnel: pump: invalid frame size %d", n))
				return
			}
			if _, err := io.ReadFull(rxIn, buf[:n]); err != nil {
				if pumpCtx.Err() == nil {
					setErr(fmt.Errorf("tunnel: pump: read payload: %w", err))
				}
				return
			}
			// R-T8 round 5 — feed le watchdog AVANT inbound() : un échec
			// inbound (TUN write) ne change pas le fait que le serveur a
			// envoyé du data, le watchdog doit en être informé.
			rxBytesTotal.Add(uint64(pumpFrameHeaderSize + int(n)))
			if _, err := inbound(buf[:n]); err != nil {
				setErr(fmt.Errorf("tunnel: pump: tun write: %w", err))
				return
			}
		}
	}()

	wg.Wait()
	return firstErr
}
