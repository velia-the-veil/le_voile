package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// inboxFunc returns a PacketWriter that pushes received packets into a slice
// (under mutex) so tests can assert on them.
type inbox struct {
	mu      sync.Mutex
	packets [][]byte
	got     chan []byte
}

func newInbox() *inbox {
	return &inbox{got: make(chan []byte, 16)}
}

func (i *inbox) write(pkt []byte) (int, error) {
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	i.mu.Lock()
	i.packets = append(i.packets, cp)
	i.mu.Unlock()
	i.got <- cp
	return len(pkt), nil
}

func (i *inbox) recv(t *testing.T, dt time.Duration) []byte {
	t.Helper()
	select {
	case p := <-i.got:
		return p
	case <-time.After(dt):
		t.Fatalf("timed out waiting for inbound packet (%s)", dt)
		return nil
	}
}

// TestRunPump_RejectsWhenDisconnected verifies the auth pre-check before any
// I/O is attempted.
func TestRunPump_RejectsWhenDisconnected(t *testing.T) {
	c := &Client{state: NewStateManager(), sessionToken: "test"}
	out := make(chan []byte)
	err := c.RunPump(context.Background(), out, func([]byte) (int, error) { return 0, nil })
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("got %v, want %v", err, ErrNotConnected)
	}
}

// TestRunPump_RejectsWhenNoSessionToken verifies the bearer-token pre-check.
func TestRunPump_RejectsWhenNoSessionToken(t *testing.T) {
	c := &Client{state: NewStateManager()}
	c.state.Set(StateConnected)
	out := make(chan []byte)
	err := c.RunPump(context.Background(), out, func([]byte) (int, error) { return 0, nil })
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("got %v, want %v", err, ErrTokenExpired)
	}
}

// TestPumpLoops_Roundtrip drives runPumpLoops with two in-memory pipes that
// simulate the bidirectional /tunnel stream. We loop frames from the
// outbound side back to the inbound side (via an external echo goroutine)
// and verify the inbound writer receives the frames after the framing
// roundtrip.
func TestPumpLoops_Roundtrip(t *testing.T) {
	out := make(chan []byte, 8)
	in := newInbox()
	txReader, txWriter := io.Pipe()
	rxReader, rxWriter := io.Pipe()

	// Echo: forward every frame written by the pump back to the pump.
	echoDone := make(chan struct{})
	go func() {
		defer close(echoDone)
		hdr := make([]byte, 2)
		buf := make([]byte, pumpMaxFrameSize)
		for {
			if _, err := io.ReadFull(txReader, hdr); err != nil {
				rxWriter.Close()
				return
			}
			n := binary.BigEndian.Uint16(hdr)
			if n == 0 || n > pumpMaxFrameSize {
				rxWriter.Close()
				return
			}
			if _, err := io.ReadFull(txReader, buf[:n]); err != nil {
				rxWriter.Close()
				return
			}
			if _, err := rxWriter.Write(hdr); err != nil {
				return
			}
			if _, err := rxWriter.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pumpDone := make(chan error, 1)
	go func() { pumpDone <- runPumpLoops(ctx, out, in.write, txWriter, rxReader) }()

	for _, p := range [][]byte{[]byte("hello"), []byte("world!"), {0x00, 0x01, 0x02, 0x03}} {
		out <- p
		got := in.recv(t, 2*time.Second)
		if string(got) != string(p) {
			t.Errorf("inbound = %q, want %q", got, p)
		}
	}

	cancel()
	close(out)
	select {
	case <-pumpDone:
	case <-time.After(2 * time.Second):
		t.Fatal("runPumpLoops did not return after ctx cancel")
	}
	<-echoDone
}

// TestPumpLoops_OutboundCloseExitsCleanly: closing the outbound channel
// signals EOF to the relay; the pump should return without error.
func TestPumpLoops_OutboundCloseExitsCleanly(t *testing.T) {
	out := make(chan []byte)
	in := newInbox()
	_, txWriter := io.Pipe()
	rxReader, _ := io.Pipe()
	close(out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runPumpLoops(ctx, out, in.write, txWriter, rxReader) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("got %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not return")
	}
}

// TestPumpLoops_InvalidFrameSize rejects a zero-length frame from the relay.
func TestPumpLoops_InvalidFrameSize(t *testing.T) {
	out := make(chan []byte, 4)
	in := newInbox()
	_, txWriter := io.Pipe()
	rxReader, rxWriter := io.Pipe()

	go func() {
		hdr := []byte{0x00, 0x00}
		rxWriter.Write(hdr)
		rxWriter.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runPumpLoops(ctx, out, in.write, txWriter, rxReader)
	if err == nil {
		t.Fatal("expected error for zero-length frame")
	}
}

// TestPumpLoops_InboundEOFExitsCleanly: server closing the response stream
// terminates the pump without reporting an error (clean EOF).
func TestPumpLoops_InboundEOFExitsCleanly(t *testing.T) {
	out := make(chan []byte)
	in := newInbox()
	_, txWriter := io.Pipe()
	rxReader, rxWriter := io.Pipe()
	rxWriter.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runPumpLoops(ctx, out, in.write, txWriter, rxReader)
	if err != nil {
		t.Errorf("inbound EOF path returned %v, want nil", err)
	}
}

// TestPumpLoops_InboundWriterError: if the device write fails the pump
// surfaces the error.
func TestPumpLoops_InboundWriterError(t *testing.T) {
	out := make(chan []byte)
	_, txWriter := io.Pipe()
	rxReader, rxWriter := io.Pipe()

	// Inject one valid frame then close.
	go func() {
		hdr := []byte{0x00, 0x05}
		rxWriter.Write(hdr)
		rxWriter.Write([]byte("hello"))
		// keep writer open so reader doesn't EOF before our writer error
	}()

	failingWriter := func(_ []byte) (int, error) {
		return 0, errors.New("device gone")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runPumpLoops(ctx, out, failingWriter, txWriter, rxReader)
	if err == nil {
		t.Fatal("expected error from inbound writer failure")
	}
	rxWriter.Close()
}
