package main

import (
	"hash/fnv"
	"sync"
	"time"
)

// dedupe suppresses packets we have very recently forwarded.
//
// Two distinct loops need suppressing. The obvious one is a packet we emit on
// interface B arriving straight back at us on B. Disabling multicast loopback
// handles most of that, but not all: some macOS interface configurations still
// deliver a copy, and bridge100 has OrbStack's own bridging in the path.
//
// The subtler one is a genuine duplicate from the network - mDNS responders
// legitimately repeat announcements. Those are safe to forward, so the window
// is deliberately short. Too long and we swallow real repeat announcements
// that a client is waiting on; too short and a loop gets through. 2s is well
// under the 1s-plus-jitter that responders use for repeats.
type dedupe struct {
	mu     sync.Mutex
	seen   map[uint64]time.Time
	window time.Duration
	now    func() time.Time // injectable for tests
}

func newDedupe(window time.Duration) *dedupe {
	return &dedupe{seen: make(map[uint64]time.Time), window: window, now: time.Now}
}

// key hashes the payload together with the egress interface index, so the same
// packet legitimately reflected in both directions is not mistaken for a loop.
func key(payload []byte, egress int) uint64 {
	h := fnv.New64a()
	h.Write(payload)
	var b [4]byte
	b[0], b[1], b[2], b[3] = byte(egress), byte(egress>>8), byte(egress>>16), byte(egress>>24)
	h.Write(b[:])
	return h.Sum64()
}

// suppress reports whether this payload was already forwarded out of egress
// within the window. It records the packet as a side effect when it returns
// false, so callers must only call it once per forwarding decision.
func (d *dedupe) suppress(payload []byte, egress int) bool {
	k := key(payload, egress)
	now := d.now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.seen[k]; ok && now.Sub(t) < d.window {
		return true
	}

	// Amortised sweep. The table only ever holds a couple of seconds of
	// traffic, so a full scan here is cheaper than a background timer.
	if len(d.seen) > 512 {
		for k, t := range d.seen {
			if now.Sub(t) >= d.window {
				delete(d.seen, k)
			}
		}
	}

	d.seen[k] = now
	return false
}
