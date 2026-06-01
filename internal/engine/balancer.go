package engine

import (
	"sync/atomic"

	"github.com/sony/gobreaker"
)

// loadBalancer performs round-robin across upstreams and skips hosts with an open circuit.
type loadBalancer struct {
	urls     []string
	next     uint64
	circuits map[string]*circuit
}

func newLoadBalancer(urls []string, settings *gobreaker.Settings, onStateChange func(name string, from, to gobreaker.State)) *loadBalancer {
	cp := make([]string, len(urls))
	copy(cp, urls)

	lb := &loadBalancer{urls: cp}
	if settings == nil {
		return lb
	}

	lb.circuits = make(map[string]*circuit, len(urls))
	for _, u := range cp {
		s := *settings
		s.Name = u
		lb.circuits[u] = newCircuit(&s, onStateChange)
	}
	return lb
}

// nextHealthy returns the next upstream whose circuit is not open.
func (lb *loadBalancer) nextHealthy() (string, bool) {
	if lb == nil || len(lb.urls) == 0 {
		return "", false
	}
	if len(lb.urls) == 1 {
		u := lb.urls[0]
		if lb.isAvailable(u) {
			return u, true
		}
		return "", false
	}

	n := len(lb.urls)
	start := atomic.AddUint64(&lb.next, 1) - 1
	for i := 0; i < n; i++ {
		u := lb.urls[(start+uint64(i))%uint64(n)]
		if lb.isAvailable(u) {
			return u, true
		}
	}
	return "", false
}

func (lb *loadBalancer) isAvailable(url string) bool {
	if lb.circuits == nil {
		return true
	}
	c := lb.circuits[url]
	return c == nil || !c.isOpen()
}

func (lb *loadBalancer) circuitFor(url string) *circuit {
	if lb == nil || lb.circuits == nil {
		return nil
	}
	return lb.circuits[url]
}

func (lb *loadBalancer) hostCount() int {
	if lb == nil {
		return 0
	}
	return len(lb.urls)
}
