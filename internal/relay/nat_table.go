package relay

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// randomHex returns 2n hex characters backed by crypto/rand.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// NAT TTL constants (NFR3).
//
// R-T8 BISECT round 6 (2026-05-10) — NATTTLUDP étendu 120s → 600s.
// Observation device : sur Free Mobile 4G LTE / NAT64, des sessions UDP
// (DNS, QUIC/HTTP3 sites) restaient inactives >120s pendant la navigation
// (page lue, user scroll lent), puis expiraient silencieusement côté
// relais. Quand un nouveau paquet user arrivait, le NAT n'avait plus de
// mapping → drop silencieux du retour → tunnel "zombie" perçu (TX continue,
// RX 0 strict observé pendant 20s+ sur device). 600s = 10 min, large
// fenêtre pour absorber les pages chargées et les transitions.
//
// Coût : entries NAT vivantes plus longtemps en mémoire. À 1000 sessions
// concurrentes × ~100 bytes/entry = ~100 KB. Négligeable.
const (
	NATTTLTCP = 300 * time.Second
	NATTTLUDP = 600 * time.Second
)

// NAT port range.
//
// R-T8 BISECT round 9 (2026-05-11) — réduit max 60000 → 32000 pour éviter
// le chevauchement avec `net.ipv4.ip_local_port_range` Linux (32768-60999
// par défaut). Symptôme observé sous charge (vidéos 9gag/TikTok, upload
// photo 4K WhatsApp) :
//
//   nat: dial: dial tcp4 217.160.59.54:56744->X:443: bind: address already in use
//
// Linux pre-alloue le port 56744 pour un de ses processes système (DNS,
// systemd-resolved, etc.) → notre NAT essaie de bind le même port (qu'il
// croit libre car alloué par son pool) → EADDRINUSE → drop du paquet.
// Avec range 10000-32000, on est entièrement hors de l'ephemeral Linux.
//
// Coût : 22000 ports au lieu de 50000. Largement suffisant pour 1-10
// users concurrents × 100-500 connexions actives. Si jamais on a >10000
// users, il faudra sysctl-ajuster ip_local_port_range côté serveur.
const (
	NATPortRangeMin uint16 = 10000
	NATPortRangeMax uint16 = 32000
)

const (
	natSweepInterval = 10 * time.Second
	// R-T8 BISECT round 6 (2026-05-10) — buffer reverse-channel étendu
	// 256 → 2048. Sous burst de trafic concurrent (Twitch live + Facebook
	// scroll = 50+ TCP streams en parallèle), le writer goroutine
	// HTTP/3 du tunnel ne consume pas assez vite, le buffer sature, et
	// `sendToSession` drop silencieusement les paquets retours (TCP ACK,
	// data, TLS handshake). Le client perçoit ça comme un tunnel zombie
	// (RX 0 sur 20s observé device).
	//
	// 2048 paquets × ~1.4 KB max = ~2.9 MB par session in-flight max.
	// Acceptable pour 1000 sessions × 2.9 MB = 2.9 GB worst-case (rare car
	// le buffer ne reste plein que pendant les bursts courts).
	//
	// R-T8 round 7 (2026-05-11) — bumped 2048 → 16384 pour le cas où la
	// session devient "zombie" : le writer HTTP/3 du tunnel peut bloquer
	// indéfiniment sur `w.Write` à cause du flow control quic-go (le client
	// n'envoie pas WINDOW_UPDATE assez vite pour le retour). Pendant ce
	// blocage, `tcpReverseLoop`/`udpReverseLoop` continuent à recevoir des
	// paquets d'Internet et appellent `sendToSession` qui drop dès que ch
	// est plein. Avec 16384 (~22 MB) on tolère plusieurs secondes de blocage
	// writer avant drop. Coût mémoire : 1000 sessions × 22 MB = 22 GB
	// worst-case — irréaliste, en pratique <50 sessions actives.
	natReverseBufSz  = 16384
	natMaxTCPPayload = 1380 // TunnelMaxFrameSize - IP header - TCP header
	natMaxUDPPayload = 1392 // TunnelMaxFrameSize - IP header - UDP header
	natTCPWindow     = 65535
)

// Exported errors.
var (
	ErrNATPortExhausted = errors.New("nat: port pool exhausted")
	ErrSSRFBlocked      = errors.New("nat: SSRF destination blocked")
	ErrUnsupportedProto = errors.New("nat: unsupported protocol")
	ErrInvalidPacket    = errors.New("nat: invalid packet")
)

// SessionID identifies a tunnel session.
type SessionID = string

// NATStats holds NAT table statistics for /health.
type NATStats struct {
	Entries   int64
	PortsUsed int64
}

// NATOption configures a NAT instance.
type NATOption func(*NAT)

// WithClock injects a time source for testing.
func WithClock(clock func() time.Time) NATOption {
	return func(n *NAT) { n.clock = clock }
}

// WithContext sets the parent context for the sweeper.
func WithContext(ctx context.Context) NATOption {
	return func(n *NAT) { n.parentCtx = ctx }
}

// WithDialTCP replaces net.DialTCP for testing.
func WithDialTCP(fn func(laddr, raddr *net.TCPAddr) (net.Conn, error)) NATOption {
	return func(n *NAT) { n.dialTCP = fn }
}

// WithDialUDP replaces net.DialUDP for testing.
func WithDialUDP(fn func(laddr, raddr *net.UDPAddr) (net.Conn, error)) NATOption {
	return func(n *NAT) { n.dialUDP = fn }
}

// WithDNSResolver attaches a DNS resolver to intercept DNS packets (port 53)
// before NAT translation (story 3.5, AC1).
func WithDNSResolver(r *DNSResolver) NATOption {
	return func(n *NAT) { n.dnsResolver = r }
}

// TCP connection states.
const (
	tcpStateSynSent     = 1
	tcpStateEstablished = 2
	tcpStateFINWait     = 3
	tcpStateClosed      = 4
)

// natEntry represents a single NAT mapping.
type natEntry struct {
	session SessionID
	srcIP   net.IP // original client source IP
	dstIP   net.IP // original destination IP
	srcPort uint16
	dstPort uint16
	natPort uint16
	proto   uint8

	lastSeen atomic.Int64 // unix nano
	conn     io.Closer    // underlying network connection

	// TCP state (protected by mu).
	mu            sync.Mutex
	tcpState      uint8
	clientISN     uint32
	relayISN      uint32
	clientSeqNext uint32 // next expected byte from client
	relaySeqNext  uint32 // next sequence number for data to client
}

func (e *natEntry) ttl() time.Duration {
	if e.proto == ipv4ProtoTCP {
		return NATTTLTCP
	}
	return NATTTLUDP
}

// NAT manages the in-memory NAT table. Implements PacketForwarder.
type NAT struct {
	relayIP net.IP
	pool    *portPool

	entriesByTuple   sync.Map // tupleKey string → *natEntry
	entriesByNATPort sync.Map // uint16 → *natEntry

	entriesCount atomic.Int64
	portsUsed    atomic.Int64

	clock     func() time.Time
	parentCtx context.Context
	cancel    context.CancelFunc

	dialTCP func(laddr, raddr *net.TCPAddr) (net.Conn, error)
	dialUDP func(laddr, raddr *net.UDPAddr) (net.Conn, error)

	dnsResolver *DNSResolver // optional: intercepts port-53 packets before NAT (story 3.5)

	stopped atomic.Bool

	sessionsMu     sync.RWMutex
	sessions       map[SessionID]chan []byte
	sessionCounter atomic.Int64
}

// Compile-time check that NAT implements PacketForwarder.
var _ PacketForwarder = (*NAT)(nil)

// natDialTimeout bounds new outbound TCP/UDP dial attempts by the NAT.
//
// R-T8 BISECT round 8 (2026-05-11) — CAUSE ROOT du bug "Twitch+FB KO" :
// `net.DialTCP` sans context blocque sur le TCP RTO par défaut (60-90s) si
// la destination ne répond pas (firewall, dest morte). Pendant ce blocage,
// le reader goroutine du tunnel_handler bloque sur `Forward → Translate →
// dialTCP` → le serveur ne consume plus le stream client → flow control
// HTTP/3 sature → côté client le pump-out continue mais le pump-in ne
// reçoit plus rien (RX=0 strict). Symptôme observable : "stream zombie"
// détecté par watchdog client.
//
// 5s est un compromis : assez court pour ne pas bloquer le reader sur des
// dest mortes, assez long pour absorber les RTT légitimes (<200ms en
// Europe-DE, <300ms vers US). Une dest qui ne répond pas en 5s est de
// toute façon non-fonctionnelle pour l'usage utilisateur.
const natDialTimeout = 5 * time.Second

// NewNAT creates a new in-memory NAT table with sweeper.
func NewNAT(relayIP net.IP, opts ...NATOption) *NAT {
	n := &NAT{
		relayIP:   relayIP.To4(),
		pool:      newPortPool(NATPortRangeMin, NATPortRangeMax),
		clock:     time.Now,
		parentCtx: context.Background(),
		dialTCP: func(laddr, raddr *net.TCPAddr) (net.Conn, error) {
			d := net.Dialer{Timeout: natDialTimeout, LocalAddr: laddr}
			c, err := d.Dial("tcp4", raddr.String())
			if err != nil {
				return nil, err
			}
			return c, nil
		},
		dialUDP: func(laddr, raddr *net.UDPAddr) (net.Conn, error) {
			// net.DialUDP est non-bloquant (pas de handshake) mais on garde
			// le ListenUDP+Connect via Dialer pour cohérence et pouvoir
			// poser un Deadline si besoin futur.
			d := net.Dialer{Timeout: natDialTimeout, LocalAddr: laddr}
			c, err := d.Dial("udp4", raddr.String())
			if err != nil {
				return nil, err
			}
			return c, nil
		},
		sessions: make(map[SessionID]chan []byte),
	}
	for _, opt := range opts {
		opt(n)
	}
	ctx, cancel := context.WithCancel(n.parentCtx)
	n.cancel = cancel
	go n.sweepLoop(ctx)
	return n
}

// sessionKey returns the authoritative key for a TunnelSession. Prefers the
// cryptographically random ID populated by tunnel_handler (fix H5) and falls
// back to the legacy IPHash@UnixNano format only when ID is empty — kept
// for test code that builds TunnelSession literals and does not care about
// hijack resistance.
func sessionKey(s TunnelSession) SessionID {
	if s.ID != "" {
		return s.ID
	}
	return fmt.Sprintf("%s@%d", s.ClientIPHash, s.OpenedAt.UnixNano())
}

// newSessionID mints a fresh 32-hex session identifier backed by
// crypto/rand. Callers MUST treat a non-nil error as fatal for the
// current tunnel setup — a weak ID trivially re-opens H5.
func newSessionID() (string, error) {
	return randomHex(16) // 16 bytes = 32 hex chars, 128 bits entropy
}

// tupleKey generates a canonical key for the NAT entry lookup.
func tupleKey(session SessionID, srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, proto uint8) string {
	s := srcIP.To4()
	d := dstIP.To4()
	return fmt.Sprintf("%s/%02x%02x%02x%02x:%d>%02x%02x%02x%02x:%d/%d",
		session,
		s[0], s[1], s[2], s[3], srcPort,
		d[0], d[1], d[2], d[3], dstPort,
		proto)
}

// OpenSession creates a session channel for reverse-path packets.
func (n *NAT) OpenSession(_ context.Context, session TunnelSession) (<-chan []byte, func()) {
	sid := sessionKey(session)
	ch := make(chan []byte, natReverseBufSz)

	n.sessionsMu.Lock()
	n.sessions[sid] = ch
	n.sessionsMu.Unlock()

	cleanup := func() {
		n.sessionsMu.Lock()
		delete(n.sessions, sid)
		n.sessionsMu.Unlock()
		n.closeSessionEntries(sid)
	}
	return ch, cleanup
}

// Forward delivers an inbound IP packet from a tunnel client.
// DNS packets (UDP/TCP dstPort=53) are intercepted and resolved internally
// when a DNSResolver is configured (story 3.5, AC1). The response is sent
// back to the session channel with source = original DNS server IP.
func (n *NAT) Forward(ctx context.Context, session TunnelSession, pkt []byte) error {
	// DNS interception — before NAT.
	if n.dnsResolver != nil {
		if intercepted := n.tryDNSIntercept(ctx, session, pkt); intercepted {
			return nil
		}
	}

	sid := sessionKey(session)
	_, err := n.Translate(sid, pkt)
	return err
}

// Translate processes an outbound IP packet: parses, applies NAT, forwards
// via userspace socket. Returns the rewritten packet or an error.
func (n *NAT) Translate(session SessionID, pkt []byte) ([]byte, error) {
	if n.stopped.Load() {
		return nil, errors.New("nat: shutting down")
	}

	p, err := parseIPv4(pkt)
	if err != nil {
		return nil, err
	}

	// SSRF check (AC6).
	if isBlockedIP(p.dstIP) {
		return nil, ErrSSRFBlocked
	}

	key := tupleKey(session, p.srcIP, p.srcPort, p.dstIP, p.dstPort, p.proto)

	// Existing entry — fast path.
	if val, ok := n.entriesByTuple.Load(key); ok {
		entry := val.(*natEntry)
		entry.lastSeen.Store(n.clock().UnixNano())
		rewriteSource(pkt, p, n.relayIP, entry.natPort)
		n.forwardExisting(entry, p, pkt)
		return pkt, nil
	}

	// New entry — allocate port.
	port, err := n.pool.allocate()
	if err != nil {
		// AC5: try synchronous sweep before giving up.
		n.sweepOnce(n.clock())
		port, err = n.pool.allocate()
		if err != nil {
			return nil, ErrNATPortExhausted
		}
	}

	entry := &natEntry{
		session: session,
		srcIP:   cloneIP(p.srcIP),
		dstIP:   cloneIP(p.dstIP),
		srcPort: p.srcPort,
		dstPort: p.dstPort,
		natPort: port,
		proto:   p.proto,
	}
	entry.lastSeen.Store(n.clock().UnixNano())

	if err := n.openConnection(entry, p); err != nil {
		n.pool.release(port)
		return nil, fmt.Errorf("nat: dial: %w", err)
	}

	n.entriesByTuple.Store(key, entry)
	n.entriesByNATPort.Store(port, entry)
	n.entriesCount.Add(1)
	n.portsUsed.Add(1)

	rewriteSource(pkt, p, n.relayIP, port)
	n.forwardNew(entry, p, pkt)
	return pkt, nil
}

// Reverse performs reverse NAT lookup on a packet destined to relayIP:natPort.
// Returns the rewritten packet and the session it belongs to.
func (n *NAT) Reverse(pkt []byte) ([]byte, SessionID, error) {
	p, err := parseIPv4(pkt)
	if err != nil {
		return nil, "", err
	}

	val, ok := n.entriesByNATPort.Load(p.dstPort)
	if !ok {
		return nil, "", ErrInvalidPacket
	}
	entry := val.(*natEntry)

	rewriteDest(pkt, p, entry.srcIP, entry.srcPort)
	entry.lastSeen.Store(n.clock().UnixNano())
	return pkt, entry.session, nil
}

// Stats returns current NAT table statistics (O(1)).
func (n *NAT) Stats() NATStats {
	return NATStats{
		Entries:   n.entriesCount.Load(),
		PortsUsed: n.portsUsed.Load(),
	}
}

// Shutdown gracefully closes all entries and stops the sweeper.
func (n *NAT) Shutdown(_ context.Context) error {
	n.stopped.Store(true)
	n.cancel()
	n.entriesByTuple.Range(func(key, val any) bool {
		entry := val.(*natEntry)
		if entry.conn != nil {
			entry.conn.Close()
		}
		n.entriesByTuple.Delete(key)
		n.entriesByNATPort.Delete(entry.natPort)
		n.pool.release(entry.natPort)
		n.entriesCount.Add(-1)
		n.portsUsed.Add(-1)
		return true
	})
	return nil
}

// --- Connection management ---

func (n *NAT) openConnection(entry *natEntry, p *parsedPacket) error {
	switch entry.proto {
	case ipv4ProtoTCP:
		return n.openTCP(entry, p)
	case ipv4ProtoUDP:
		return n.openUDP(entry)
	default:
		return ErrUnsupportedProto
	}
}

func (n *NAT) openTCP(entry *natEntry, p *parsedPacket) error {
	laddr := &net.TCPAddr{IP: n.relayIP, Port: int(entry.natPort)}
	raddr := &net.TCPAddr{IP: entry.dstIP, Port: int(entry.dstPort)}

	conn, err := n.dialTCP(laddr, raddr)
	if err != nil {
		// Send RST to client on dial failure.
		if p.tcpFlags&tcpFlagSYN != 0 {
			rst := buildTCPPacket(entry.dstIP, entry.srcIP, entry.dstPort, entry.srcPort,
				nil, tcpFlagRST|tcpFlagACK, 0, p.tcpSeq+1, 0)
			n.sendToSession(entry.session, rst)
		}
		return err
	}
	entry.conn = conn

	// Initialize TCP state.
	entry.mu.Lock()
	entry.clientISN = p.tcpSeq
	entry.relayISN = rand.Uint32()
	entry.clientSeqNext = p.tcpSeq + 1 // SYN consumes one sequence number
	entry.relaySeqNext = entry.relayISN + 1
	entry.tcpState = tcpStateEstablished
	entry.mu.Unlock()

	// Send SYN-ACK to client.
	synack := buildTCPPacket(entry.dstIP, entry.srcIP, entry.dstPort, entry.srcPort,
		nil, tcpFlagSYN|tcpFlagACK, entry.relayISN, entry.clientSeqNext, natTCPWindow)
	n.sendToSession(entry.session, synack)

	// Start reverse-path reader.
	go n.tcpReverseLoop(entry)
	return nil
}

func (n *NAT) openUDP(entry *natEntry) error {
	laddr := &net.UDPAddr{IP: n.relayIP, Port: int(entry.natPort)}
	raddr := &net.UDPAddr{IP: entry.dstIP, Port: int(entry.dstPort)}

	conn, err := n.dialUDP(laddr, raddr)
	if err != nil {
		return err
	}
	entry.conn = conn

	go n.udpReverseLoop(entry)
	return nil
}

// --- Forward path ---

func (n *NAT) forwardNew(entry *natEntry, p *parsedPacket, pkt []byte) {
	switch entry.proto {
	case ipv4ProtoTCP:
		// First packet is SYN — connection already established in openTCP.
		// If it's data (non-SYN), forward payload.
		if p.tcpFlags&tcpFlagSYN == 0 {
			n.forwardTCPData(entry, p, pkt)
		}
	case ipv4ProtoUDP:
		n.forwardUDPData(entry, p, pkt)
	}
}

func (n *NAT) forwardExisting(entry *natEntry, p *parsedPacket, pkt []byte) {
	switch entry.proto {
	case ipv4ProtoTCP:
		n.handleTCPPacket(entry, p, pkt)
	case ipv4ProtoUDP:
		n.forwardUDPData(entry, p, pkt)
	}
}

func (n *NAT) handleTCPPacket(entry *natEntry, p *parsedPacket, pkt []byte) {
	entry.mu.Lock()
	state := entry.tcpState
	entry.mu.Unlock()

	if state == tcpStateClosed {
		return
	}

	// RST from client — close connection.
	if p.tcpFlags&tcpFlagRST != 0 {
		entry.mu.Lock()
		entry.tcpState = tcpStateClosed
		entry.mu.Unlock()
		if entry.conn != nil {
			entry.conn.Close()
		}
		return
	}

	// FIN from client.
	if p.tcpFlags&tcpFlagFIN != 0 {
		entry.mu.Lock()
		entry.clientSeqNext = p.tcpSeq + 1
		entry.tcpState = tcpStateFINWait
		seq := entry.relaySeqNext
		ack := entry.clientSeqNext
		entry.mu.Unlock()

		// Send FIN-ACK.
		finack := buildTCPPacket(entry.dstIP, entry.srcIP, entry.dstPort, entry.srcPort,
			nil, tcpFlagFIN|tcpFlagACK, seq, ack, natTCPWindow)
		n.sendToSession(entry.session, finack)
		if entry.conn != nil {
			entry.conn.Close()
		}
		return
	}

	// Data packet.
	payload := p.payload(pkt)
	if len(payload) > 0 {
		n.forwardTCPData(entry, p, pkt)
	}
	// Pure ACK — just update lastSeen (already done in caller).
}

func (n *NAT) forwardTCPData(entry *natEntry, p *parsedPacket, pkt []byte) {
	payload := p.payload(pkt)
	if len(payload) == 0 {
		return
	}

	conn, ok := entry.conn.(net.Conn)
	if !ok || conn == nil {
		return
	}

	entry.mu.Lock()
	expected := entry.clientSeqNext
	entry.mu.Unlock()

	// Drop out-of-order or retransmitted segments (M1 review fix).
	// The TUN→QUIC path is FIFO so this is defensive only.
	if p.tcpSeq != expected {
		return
	}

	_, err := conn.Write(payload)
	if err != nil {
		return
	}

	entry.mu.Lock()
	entry.clientSeqNext = p.tcpSeq + uint32(len(payload))
	seq := entry.relaySeqNext
	ack := entry.clientSeqNext
	entry.mu.Unlock()

	// ACK the data.
	ackPkt := buildTCPPacket(entry.dstIP, entry.srcIP, entry.dstPort, entry.srcPort,
		nil, tcpFlagACK, seq, ack, natTCPWindow)
	n.sendToSession(entry.session, ackPkt)
}

func (n *NAT) forwardUDPData(entry *natEntry, p *parsedPacket, pkt []byte) {
	payload := p.payload(pkt)
	if len(payload) == 0 {
		return
	}
	conn, ok := entry.conn.(net.Conn)
	if !ok || conn == nil {
		return
	}
	conn.Write(payload)
}

// --- Reverse path (socket read goroutines) ---

func (n *NAT) tcpReverseLoop(entry *natEntry) {
	conn, ok := entry.conn.(net.Conn)
	if !ok {
		return
	}
	buf := make([]byte, natMaxTCPPayload)
	for {
		nr, err := conn.Read(buf)
		if nr > 0 {
			data := make([]byte, nr)
			copy(data, buf[:nr])

			entry.mu.Lock()
			seq := entry.relaySeqNext
			ack := entry.clientSeqNext
			entry.relaySeqNext += uint32(nr)
			entry.mu.Unlock()

			pkt := buildTCPPacket(entry.dstIP, entry.srcIP, entry.dstPort, entry.srcPort,
				data, tcpFlagPSH|tcpFlagACK, seq, ack, natTCPWindow)
			n.sendToSession(entry.session, pkt)
			entry.lastSeen.Store(n.clock().UnixNano())
		}
		if err != nil {
			// Send FIN on EOF, then mark closed.
			entry.mu.Lock()
			if entry.tcpState == tcpStateEstablished || entry.tcpState == tcpStateFINWait {
				seq := entry.relaySeqNext
				ack := entry.clientSeqNext
				entry.tcpState = tcpStateClosed
				entry.mu.Unlock()
				fin := buildTCPPacket(entry.dstIP, entry.srcIP, entry.dstPort, entry.srcPort,
					nil, tcpFlagFIN|tcpFlagACK, seq, ack, natTCPWindow)
				n.sendToSession(entry.session, fin)
			} else {
				entry.tcpState = tcpStateClosed
				entry.mu.Unlock()
			}
			return
		}
	}
}

func (n *NAT) udpReverseLoop(entry *natEntry) {
	conn, ok := entry.conn.(net.Conn)
	if !ok {
		return
	}
	buf := make([]byte, natMaxUDPPayload)
	for {
		nr, err := conn.Read(buf)
		if nr > 0 {
			data := make([]byte, nr)
			copy(data, buf[:nr])
			pkt := buildUDPPacket(entry.dstIP, entry.srcIP, entry.dstPort, entry.srcPort, data)
			n.sendToSession(entry.session, pkt)
			entry.lastSeen.Store(n.clock().UnixNano())
		}
		if err != nil {
			return
		}
	}
}

// --- Session channel helpers ---

// reverseDropsBySession compte les drops de paquets retour par session
// (round 7 instrumentation). Log toutes les 100 drops par session pour
// diagnostiquer si le `natReverseBufSz` (2048) suffit en burst.
var (
	reverseDropsMu sync.Mutex
	reverseDrops   = make(map[SessionID]int64)
)

func (n *NAT) sendToSession(session SessionID, pkt []byte) {
	n.sessionsMu.RLock()
	ch, ok := n.sessions[session]
	n.sessionsMu.RUnlock()
	if !ok {
		return
	}
	select {
	case ch <- pkt:
	default:
		// Channel full — drop packet (back-pressure).
		// R-T8 round 7 — log les drops toutes les 100 par session pour
		// quantifier le problème de buffer reverse.
		reverseDropsMu.Lock()
		reverseDrops[session]++
		count := reverseDrops[session]
		reverseDropsMu.Unlock()
		if count%100 == 0 {
			sidShort := session
			if len(session) > 8 {
				sidShort = session[:8]
			}
			log.Printf("nat: reverse buffer drop session=%s totalDrops=%d (buf=%d full)",
				sidShort, count, natReverseBufSz)
		}
	}
}

func (n *NAT) closeSessionEntries(session SessionID) {
	n.entriesByTuple.Range(func(key, val any) bool {
		entry := val.(*natEntry)
		if entry.session == session {
			n.evictEntry(key, entry)
		}
		return true
	})
}

// --- Sweeper ---

func (n *NAT) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(natSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.sweepOnce(n.clock())
		}
	}
}

func (n *NAT) sweepOnce(now time.Time) {
	nowNano := now.UnixNano()
	n.entriesByTuple.Range(func(key, val any) bool {
		entry := val.(*natEntry)
		age := time.Duration(nowNano - entry.lastSeen.Load())
		if age > entry.ttl() {
			n.evictEntry(key, entry)
		}
		return true
	})
}

func (n *NAT) evictEntry(key any, entry *natEntry) {
	n.entriesByTuple.Delete(key)
	n.entriesByNATPort.Delete(entry.natPort)
	if entry.conn != nil {
		entry.conn.Close()
	}
	n.pool.release(entry.natPort)
	n.entriesCount.Add(-1)
	n.portsUsed.Add(-1)
}

// --- DNS interception (story 3.5) ---

const dnsPort = 53

// tryDNSIntercept checks if pkt is a DNS query (UDP/TCP dstPort=53) and,
// if so, resolves it via the attached DNSResolver. The response packet is
// built with source IP = original destination (the DNS server the client
// intended to reach) so the client's stub resolver recognizes the answer.
// Returns true if the packet was intercepted (caller should NOT NAT-forward).
func (n *NAT) tryDNSIntercept(ctx context.Context, session TunnelSession, pkt []byte) bool {
	p, err := parseIPv4(pkt)
	if err != nil {
		return false
	}
	if p.dstPort != dnsPort {
		return false
	}
	if p.proto != ipv4ProtoUDP && p.proto != ipv4ProtoTCP {
		return false
	}

	payload := p.payload(pkt)
	if len(payload) == 0 {
		return false
	}

	// TCP DNS uses a 2-byte length prefix before the wire message.
	dnsPayload := payload
	if p.proto == ipv4ProtoTCP {
		if len(payload) < 2 {
			return false
		}
		msgLen := int(payload[0])<<8 | int(payload[1])
		if len(payload) < 2+msgLen {
			return false
		}
		dnsPayload = payload[2 : 2+msgLen]
	}

	resp, err := n.dnsResolver.Resolve(ctx, dnsPayload)
	if err != nil || len(resp) == 0 {
		return false
	}

	sid := sessionKey(session)

	if p.proto == ipv4ProtoUDP {
		respPkt := buildUDPPacket(p.dstIP, p.srcIP, p.dstPort, p.srcPort, resp)
		n.sendToSession(sid, respPkt)
	} else {
		// TCP DNS: prepend 2-byte length prefix to the response.
		tcpPayload := make([]byte, 2+len(resp))
		tcpPayload[0] = byte(len(resp) >> 8)
		tcpPayload[1] = byte(len(resp))
		copy(tcpPayload[2:], resp)
		respPkt := buildTCPPacket(p.dstIP, p.srcIP, p.dstPort, p.srcPort,
			tcpPayload, tcpFlagPSH|tcpFlagACK, 0, 0, natTCPWindow)
		n.sendToSession(sid, respPkt)
	}
	return true
}

// --- Helpers ---

func cloneIP(ip net.IP) net.IP {
	out := make(net.IP, len(ip))
	copy(out, ip)
	return out
}
