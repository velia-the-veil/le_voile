// R-T8 (2026-05-05) — QUIC Connection Migration (RFC 9000 §9).
//
// Use case : on Android over 4G LTE, a cell tower handoff or Wi-Fi <-> LTE
// network switch causes the public IP NAT to change. The QUIC connection's
// underlying UDP socket is bound to the old source IP, so packets stop
// reaching the relay. quic-go's MaxIdleTimeout (90s) eventually closes the
// connection, but the application sees several seconds of "zombie tunnel"
// where requests vanish silently before the timeout fires.
//
// MigrateToFD swaps the underlying socket without tearing down the QUIC
// session : the application-layer state (HTTP/3 streams, session token,
// /tunnel stream) is preserved across the migration. Path validation
// (PATH_CHALLENGE / PATH_RESPONSE, RFC 9000 §8.2) ensures the new socket
// actually reaches the relay before traffic is committed to it.
//
// The caller (typically Android NetworkCallback bridge → MigrateGomobile)
// supplies a file descriptor referencing a UDP socket that is :
//   - Already bound to the new underlying network
//     (ConnectivityManager.Network.bindSocket), AND
//   - Excluded from the VPN routing loop (VpnService.protect, otherwise the
//     QUIC packets would be aspirated back into the tunnel).
//
// On non-Android platforms (Linux/Windows desktop) MigrateToFD is unused —
// no NetworkCallback equivalent triggers it. The captured *quic.Conn /
// *quic.Transport are still set up by dialQUICCustom but the migration path
// stays dormant.

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// ErrMigrationNoActiveConn is returned by MigrateToFD when no QUIC
// connection has been captured yet (Connect not called, or transport reset).
var ErrMigrationNoActiveConn = errors.New("tunnel: migration: no active QUIC connection")

// migrationProbeTimeout bounds the PATH_CHALLENGE / PATH_RESPONSE round trip.
// 2s is generous on most cellular paths (typical RTT 30-100ms) and tight
// enough that a probe failure surfaces quickly if the new path is broken.
const migrationProbeTimeout = 2 * time.Second

// migrationOldTransportDrainDelay is how long we keep the old transport
// alive after switching paths. Any in-flight packets received on the old
// socket within this window are still delivered (avoids a hiccup at the
// switch boundary). After the delay, the old transport is closed and any
// remaining packets on that socket are dropped.
const migrationOldTransportDrainDelay = 2 * time.Second

// MigrateToFD switches the live QUIC connection's underlying UDP socket to
// the one wrapped by `fd`. The fd ownership is transferred to MigrateToFD :
// on success the fd belongs to the new *quic.Transport (closed when the
// transport is closed). On error the fd is closed before returning.
//
// The function blocks until path validation completes or fails ; bound by
// `ctx` deadline AND by migrationProbeTimeout (whichever is shorter). On
// success the application layer continues seamlessly on the new path.
//
// Concurrent migrations are NOT safe — the caller must serialize calls
// (typically via the single-threaded Android NetworkCallback dispatcher).
func (c *Client) MigrateToFD(ctx context.Context, fd int) error {
	c.quicMu.RLock()
	conn := c.quicConn
	oldTransport := c.quicTransport
	c.quicMu.RUnlock()

	if conn == nil {
		// fd was provided by the caller — they expect us to take ownership.
		// Close it so we don't leak the socket on this error path.
		_ = closeFD(fd)
		return ErrMigrationNoActiveConn
	}

	newTransport, err := buildTransportFromFD(fd)
	if err != nil {
		// closeFD is already done inside buildTransportFromFD on its error
		// paths ; on success the transport now owns the fd lifecycle.
		return err
	}

	path, err := conn.AddPath(newTransport)
	if err != nil {
		_ = newTransport.Close()
		return fmt.Errorf("tunnel: migration: add path: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, migrationProbeTimeout)
	defer cancel()

	if err := path.Probe(probeCtx); err != nil {
		_ = path.Close()
		_ = newTransport.Close()
		return fmt.Errorf("tunnel: migration: probe: %w", err)
	}

	if err := path.Switch(); err != nil {
		_ = path.Close()
		_ = newTransport.Close()
		return fmt.Errorf("tunnel: migration: switch: %w", err)
	}

	// Switch succeeded — committed to the new path. Update captured handles
	// so future migrations chain correctly and ResetTransport closes the
	// right transport.
	c.quicMu.Lock()
	c.quicTransport = newTransport
	c.quicMu.Unlock()

	// Drain the old socket : packets that arrived AFTER the switch but were
	// in flight at the OS level are still delivered to the connection. We
	// can't tell when "in flight" packets have all drained, so we use a
	// fixed window. Closing the old transport too early loses those packets
	// (TCP/QUIC retransmits would handle it but slower); too late wastes
	// kernel resources.
	if oldTransport != nil {
		time.AfterFunc(migrationOldTransportDrainDelay, func() {
			_ = oldTransport.Close()
		})
	}

	return nil
}

// buildTransportFromFD wraps the file descriptor in a *quic.Transport.
// Takes ownership of fd : closes it on error, otherwise transfers it to
// the returned *quic.Transport.
//
// We rely on net.FilePacketConn which dups the fd internally — we then
// close the original os.File. The dup'd fd lives inside the *net.UDPConn
// and is closed by transport.Close(). This avoids the ownership ambiguity
// of os.NewFile (which would close the original fd on file.Close()).
func buildTransportFromFD(fd int) (*quic.Transport, error) {
	if fd < 0 {
		return nil, fmt.Errorf("tunnel: migration: invalid fd %d", fd)
	}

	file := os.NewFile(uintptr(fd), "udp-migration")
	if file == nil {
		return nil, fmt.Errorf("tunnel: migration: os.NewFile returned nil for fd %d", fd)
	}

	pc, err := net.FilePacketConn(file)
	// FilePacketConn dups internally — close the original handle now ; the
	// dup'd fd lives inside `pc`. If FilePacketConn errored, file.Close()
	// also frees the original fd (no leak).
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("tunnel: migration: file packet conn: %w", err)
	}

	udpConn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("tunnel: migration: fd is not a UDP socket (got %T)", pc)
	}

	return &quic.Transport{Conn: udpConn}, nil
}

// closeFD closes a raw file descriptor without going through os.File ;
// used in the error path of MigrateToFD when buildTransportFromFD has not
// run yet (so we own the fd directly).
//
// On Windows, syscall.Close takes a Handle ; on Unix it takes an int.
// We narrow our concern to Unix-style fds (Android, Linux) by wrapping
// the fd in os.NewFile — its Close() returns it to the OS. If the fd is
// already invalid this is a no-op.
func closeFD(fd int) error {
	if fd < 0 {
		return nil
	}
	f := os.NewFile(uintptr(fd), "fd-closer")
	if f == nil {
		return errors.New("tunnel: closeFD: os.NewFile returned nil")
	}
	return f.Close()
}

// migrationCapture is a test seam : returns the currently captured *quic.Conn
// and *quic.Transport. Non-nil after a successful Connect ; nil after
// Disconnect or before any Connect. NOT exported — internal/tunnel tests use
// it via package-level access.
//
// Returned values are snapshots ; do not retain across migrations.
//
//nolint:unused // used by migration_test.go
func (c *Client) migrationCapture() (*quic.Conn, *quic.Transport) {
	c.quicMu.RLock()
	defer c.quicMu.RUnlock()
	return c.quicConn, c.quicTransport
}

// pathSwitchSync is a sync.Once-style guard reserved for callers that need
// to ensure only one migration runs at a time. The Android side already
// serializes via the NetworkCallback dispatcher (single thread), so this is
// belt-and-braces for desktop-test scenarios.
//
//nolint:unused // exposed for future test fixtures
type pathSwitchSync struct {
	mu sync.Mutex
}

func (p *pathSwitchSync) Do(fn func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return fn()
}
