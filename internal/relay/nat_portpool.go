package relay

import (
	"math/rand"
	"sync"
)

// portPool manages NAT port allocation with mutex-protected stack.
type portPool struct {
	mu        sync.Mutex
	free      []uint16
	allocated map[uint16]bool
}

func newPortPool(min, max uint16) *portPool {
	n := int(max) - int(min) + 1
	ports := make([]uint16, n)
	for i := range ports {
		ports[i] = min + uint16(i)
	}
	rand.Shuffle(n, func(i, j int) { ports[i], ports[j] = ports[j], ports[i] })
	return &portPool{free: ports, allocated: make(map[uint16]bool, n)}
}

func (p *portPool) allocate() (uint16, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) == 0 {
		return 0, ErrNATPortExhausted
	}
	port := p.free[len(p.free)-1]
	p.free = p.free[:len(p.free)-1]
	p.allocated[port] = true
	return port, nil
}

func (p *portPool) release(port uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.allocated[port] {
		return // guard against double-release
	}
	delete(p.allocated, port)
	p.free = append(p.free, port)
}

func (p *portPool) available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.free)
}
