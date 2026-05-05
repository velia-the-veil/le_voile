//go:build linux

package ipc

import (
	"fmt"
	"net"
	"os/user"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

// PeerCred captures the SO_PEERCRED triple of a Unix socket peer.
type PeerCred struct {
	PID int32
	UID uint32
	GID uint32
}

// getPeerCred reads SO_PEERCRED for a Unix socket connection. Returns an
// error if the underlying syscall fails or if the connection is not a
// Unix socket. Audit fix R-T1.1 (2026-05-04): identifies the calling
// process so the strict-auth gate can decide on more than just a token
// (which is group-readable and therefore stealable by any same-group
// process).
func getPeerCred(conn net.Conn) (PeerCred, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return PeerCred{}, fmt.Errorf("ipc: peercred: connection is not *net.UnixConn (got %T)", conn)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return PeerCred{}, fmt.Errorf("ipc: peercred: syscall conn: %w", err)
	}
	var ucred *unix.Ucred
	var sockErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if ctrlErr != nil {
		return PeerCred{}, fmt.Errorf("ipc: peercred: control: %w", ctrlErr)
	}
	if sockErr != nil {
		return PeerCred{}, fmt.Errorf("ipc: peercred: getsockopt: %w", sockErr)
	}
	return PeerCred{PID: ucred.Pid, UID: ucred.Uid, GID: ucred.Gid}, nil
}

// allowedGroupName is the Unix group whose members may speak to the IPC
// service. Mirrors the socket mode 0660 levoile:levoile DAC and the
// systemd unit's User= field.
const allowedGroupName = "levoile"

// peerAuthCache memoises the levoile group GID so each accept() doesn't
// pay an extra /etc/group lookup. Member UID resolution is done per-peer
// via user.LookupId + user.GroupIds, which is a cheap nss call and means
// we never cache a stale snapshot of the user database — a freshly added
// user is honoured on the very next connection without restart.
type peerAuthCache struct {
	mu         sync.RWMutex
	resolved   bool
	allowedGID uint32
	hasGroup   bool
}

var globalPeerAuth peerAuthCache

func (c *peerAuthCache) groupGID() (uint32, bool) {
	c.mu.RLock()
	if c.resolved {
		gid, ok := c.allowedGID, c.hasGroup
		c.mu.RUnlock()
		return gid, ok
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resolved {
		return c.allowedGID, c.hasGroup
	}
	c.resolved = true
	g, err := user.LookupGroup(allowedGroupName)
	if err != nil {
		return 0, false
	}
	gid64, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		return 0, false
	}
	c.allowedGID = uint32(gid64)
	c.hasGroup = true
	return c.allowedGID, true
}

// authorizePeer validates that the peer's UID is allowed to drive the
// service IPC. Authorised when:
//   - the peer is root (UID 0); the service can always be administered
//     out-of-band by root via systemctl + raw socket access — refusing
//     root would not add any security and would break legitimate
//     troubleshooting,
//   - the peer's primary GID matches the levoile group, OR
//   - the peer's supplementary groups include the levoile group, OR
//   - the levoile group cannot be resolved (test / non-postinstalled env)
//     in which case we fall back to the legacy "any caller" behaviour
//     so that running the binary in a CI container never hangs on
//     missing nss data.
func authorizePeer(cred PeerCred) bool {
	if cred.UID == 0 {
		return true
	}
	gid, ok := globalPeerAuth.groupGID()
	if !ok {
		return true
	}
	if cred.GID == gid {
		return true
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(cred.UID), 10))
	if err != nil {
		return false
	}
	gids, err := u.GroupIds()
	if err != nil {
		return false
	}
	target := strconv.FormatUint(uint64(gid), 10)
	for _, candidate := range gids {
		if candidate == target {
			return true
		}
	}
	return false
}
