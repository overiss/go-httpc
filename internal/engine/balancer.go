package engine

import "sync/atomic"

type roundRobin struct {
	urls []string
	next uint64
}

func newRoundRobin(urls []string) *roundRobin {
	cp := make([]string, len(urls))
	copy(cp, urls)
	return &roundRobin{urls: cp}
}

func (r *roundRobin) nextURL() string {
	if len(r.urls) == 1 {
		return r.urls[0]
	}
	i := atomic.AddUint64(&r.next, 1) - 1
	return r.urls[i%uint64(len(r.urls))]
}
