package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// pumpFrameHeaderSize must equal relay.TunnelFrameHeaderSize (2 bytes BE).
const pumpFrameHeaderSize = 2

// pumpMaxFrameSize must equal relay.TunnelMaxFrameSize. Any IP packet larger
// than this is dropped — the kernel sees an MTU-sized link (config default
// 1420) so this never triggers in practice.
const pumpMaxFrameSize = 1420

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
			if _, err := inbound(buf[:n]); err != nil {
				setErr(fmt.Errorf("tunnel: pump: tun write: %w", err))
				return
			}
		}
	}()

	wg.Wait()
	return firstErr
}
