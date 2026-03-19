package httpproxy

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/publicsuffix"
)

// Volume bypass constants.
const (
	VolumeThreshold    int64         = 500 * 1024 * 1024 // 500 MB
	WindowDuration     time.Duration = 1 * time.Hour
	BypassCooldown     time.Duration = 24 * time.Hour
	BypassDialTimeout  time.Duration = 3 * time.Second
	MaxDirectFailures  int32         = 3
	CleanupInterval    time.Duration = 60 * time.Second
	CleanupTTL         time.Duration = 24 * time.Hour
)

// sharedCDNs lists CDN domains where the full FQDN is used as key
// instead of the registrable domain, to avoid bypassing all sites
// hosted on the same CDN.
var sharedCDNs = map[string]bool{
	"akamaized.net":  true,
	"cloudfront.net": true,
	"fastly.net":     true,
	"edgecast.net":   true,
	"azureedge.net":  true,
	"cdn77.org":      true,
	"jsdelivr.net":   true,
	"googleapis.com": true,
	"gstatic.com":    true,
	"cdninstagram.com": true,
	"fbcdn.net":      true,
	"akamai.net":     true,
	"llnwd.net":      true,
	"edgesuite.net":  true,
}

// domainState tracks per-domain volume and bypass state using atomics.
type domainState struct {
	bytesUsed         atomic.Int64
	windowStart       atomic.Int64 // unix timestamp
	bypassed          atomic.Bool
	bypassedAt        atomic.Int64 // unix timestamp
	directFailures    atomic.Int32
	lastSeen          atomic.Int64 // unix timestamp
	markedForDeletion atomic.Bool
}

// connSet holds active relay connections for a domain.
type connSet struct {
	mu    sync.Mutex
	conns map[int64]net.Conn
}

// VolumeTracker monitors per-domain download volume and triggers bypass
// when a domain exceeds the threshold within a time window.
type VolumeTracker struct {
	domains   sync.Map // map[string]*domainState
	conns     sync.Map // map[string]*connSet
	threshold int64
	connIDGen atomic.Int64
}

// NewVolumeTracker creates a tracker with the given byte threshold.
func NewVolumeTracker(threshold int64) *VolumeTracker {
	return &VolumeTracker{threshold: threshold}
}

// domainKey extracts the grouping key for a host:port target.
func domainKey(host string) string {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}

	// Raw IP — use as-is.
	if net.ParseIP(hostname) != nil {
		return hostname
	}

	registered, err := publicsuffix.EffectiveTLDPlusOne(hostname)
	if err != nil {
		return hostname
	}

	// Shared CDN — use full FQDN to avoid bypassing all sites on the CDN.
	if sharedCDNs[registered] {
		return hostname
	}

	return registered
}

// IsBypassed returns true if the domain should be connected directly.
func (vt *VolumeTracker) IsBypassed(target string) bool {
	key := domainKey(target)
	val, ok := vt.domains.Load(key)
	if !ok {
		return false
	}
	st := val.(*domainState)

	if !st.bypassed.Load() {
		return false
	}

	// Check cooldown expiry — CAS ensures only one goroutine resets.
	now := time.Now().Unix()
	if now-st.bypassedAt.Load() > int64(BypassCooldown/time.Second) {
		if st.bypassed.CompareAndSwap(true, false) {
			st.directFailures.Store(0)
			st.bytesUsed.Store(0)
			st.windowStart.Store(0)
		}
		return false
	}

	// If direct dial keeps failing, fall back to relay.
	if st.directFailures.Load() >= MaxDirectFailures {
		return false
	}

	return true
}

// AddBytes adds n bytes to the domain's counter. Returns true if this call
// triggered the bypass (threshold just exceeded). The caller's relay loop
// should exit when true is returned.
func (vt *VolumeTracker) AddBytes(target string, n int) bool {
	key := domainKey(target)
	now := time.Now().Unix()

	val, _ := vt.domains.LoadOrStore(key, &domainState{})
	st := val.(*domainState)

	// Rescue from pending deletion.
	st.markedForDeletion.Store(false)

	// Initialize window on first use.
	st.windowStart.CompareAndSwap(0, now)

	// Check window expiry — CAS reset. The winner subtracts the old value
	// instead of Store(0) so concurrent Add() calls are not lost.
	ws := st.windowStart.Load()
	if now-ws > int64(WindowDuration.Seconds()) {
		if st.windowStart.CompareAndSwap(ws, now) {
			old := st.bytesUsed.Swap(0)
			// If anyone added between Swap and our Add below, they see
			// a clean counter. The Swap is atomic — no bytes are lost.
			_ = old
		}
	}

	total := st.bytesUsed.Add(int64(n))
	st.lastSeen.Store(now)

	// Check threshold — CAS on bypassed to ensure only one goroutine triggers.
	if total > vt.threshold && st.bypassed.CompareAndSwap(false, true) {
		st.bypassedAt.Store(now)
		vt.closeAll(key)
		return true
	}

	return false
}

// Register adds a relay connection to the domain's connection set.
// Returns a unique connection ID for later Unregister.
func (vt *VolumeTracker) Register(target string, conn net.Conn) int64 {
	key := domainKey(target)
	connID := vt.connIDGen.Add(1)

	val, _ := vt.conns.LoadOrStore(key, &connSet{conns: make(map[int64]net.Conn)})
	cs := val.(*connSet)

	cs.mu.Lock()
	cs.conns[connID] = conn
	cs.mu.Unlock()

	return connID
}

// Unregister removes a connection from the domain's set.
// Empty connSets are left in the sync.Map to avoid races with concurrent
// Register/closeAll calls; the cleanup goroutine handles removal.
func (vt *VolumeTracker) Unregister(target string, connID int64) {
	key := domainKey(target)
	val, ok := vt.conns.Load(key)
	if !ok {
		return
	}
	cs := val.(*connSet)

	cs.mu.Lock()
	delete(cs.conns, connID)
	cs.mu.Unlock()
}

// closeAll force-closes all relay connections for a domain.
func (vt *VolumeTracker) closeAll(key string) {
	val, ok := vt.conns.Load(key)
	if !ok {
		return
	}
	cs := val.(*connSet)

	cs.mu.Lock()
	toClose := make([]net.Conn, 0, len(cs.conns))
	for _, c := range cs.conns {
		toClose = append(toClose, c)
	}
	// Clear the map so Unregister sees it empty.
	for id := range cs.conns {
		delete(cs.conns, id)
	}
	cs.mu.Unlock()

	for _, c := range toClose {
		c.Close()
	}
}

// RecordDirectFailure increments the direct-dial failure counter.
func (vt *VolumeTracker) RecordDirectFailure(target string) {
	key := domainKey(target)
	val, ok := vt.domains.Load(key)
	if !ok {
		return
	}
	st := val.(*domainState)
	st.directFailures.Add(1)
}

// StartCleanup runs periodic cleanup until ctx is cancelled.
func (vt *VolumeTracker) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			vt.cleanup()
		}
	}
}

// cleanup removes stale domain entries using two-phase CAS,
// and cleans up empty connSets.
func (vt *VolumeTracker) cleanup() {
	cutoff := time.Now().Add(-CleanupTTL).Unix()
	vt.domains.Range(func(key, val any) bool {
		st := val.(*domainState)
		if st.lastSeen.Load() < cutoff {
			if st.markedForDeletion.Load() {
				// Second cycle — still marked, safe to delete.
				vt.domains.Delete(key)
			} else {
				// First cycle — mark for deletion.
				st.markedForDeletion.Store(true)
			}
		}
		return true
	})

	// Clean up empty connSets left by Unregister.
	vt.conns.Range(func(key, val any) bool {
		cs := val.(*connSet)
		cs.mu.Lock()
		empty := len(cs.conns) == 0
		cs.mu.Unlock()
		if empty {
			vt.conns.Delete(key)
		}
		return true
	})
}

// countingReader wraps a reader to count bytes and detect bypass triggers.
type countingReader struct {
	reader  io.Reader
	tracker *VolumeTracker
	target  string
	stopped atomic.Bool
}

// WrapReader creates a countingReader that tracks bytes for the given target.
func (vt *VolumeTracker) WrapReader(target string, reader io.Reader) *countingReader {
	return &countingReader{
		reader:  reader,
		tracker: vt,
		target:  target,
	}
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.reader.Read(p)
	if n > 0 && !cr.stopped.Load() {
		// Stop counting if bypass was already triggered (by another connection
		// or a prior Read), avoiding overshoot on stale data in kernel buffers.
		if cr.tracker.IsBypassed(cr.target) {
			cr.stopped.Store(true)
		} else if cr.tracker.AddBytes(cr.target, n) {
			cr.stopped.Store(true)
		}
	}
	return n, err
}

// Stopped returns true if the bypass was triggered during reading.
func (cr *countingReader) Stopped() bool {
	return cr.stopped.Load()
}
