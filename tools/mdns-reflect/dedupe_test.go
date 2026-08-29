package main

import (
	"testing"
	"time"
)

func TestDedupe(t *testing.T) {
	pkt := []byte("announcement")

	tests := []struct {
		name string
		run  func(*testing.T, *dedupe, *time.Time)
	}{
		{
			name: "first sighting is forwarded",
			run: func(t *testing.T, d *dedupe, _ *time.Time) {
				if d.suppress(pkt, 1) {
					t.Fatal("first sighting was suppressed; it should be forwarded")
				}
			},
		},
		{
			name: "immediate repeat on same interface is suppressed",
			run: func(t *testing.T, d *dedupe, _ *time.Time) {
				d.suppress(pkt, 1)
				if !d.suppress(pkt, 1) {
					t.Fatal("repeat within the window was forwarded; this is the loop we must break")
				}
			},
		},
		{
			// The whole point of a bidirectional reflector: the same bytes
			// must be allowed out of the other interface.
			name: "same payload out of a different interface is forwarded",
			run: func(t *testing.T, d *dedupe, _ *time.Time) {
				d.suppress(pkt, 1)
				if d.suppress(pkt, 2) {
					t.Fatal("payload was suppressed on the opposite interface; reflection would be one-way")
				}
			},
		},
		{
			name: "repeat after the window expires is forwarded again",
			run: func(t *testing.T, d *dedupe, now *time.Time) {
				d.suppress(pkt, 1)
				*now = now.Add(3 * time.Second)
				if d.suppress(pkt, 1) {
					t.Fatal("legitimate re-announcement after the window was swallowed")
				}
			},
		},
		{
			name: "distinct payloads do not collide",
			run: func(t *testing.T, d *dedupe, _ *time.Time) {
				d.suppress(pkt, 1)
				if d.suppress([]byte("a different announcement"), 1) {
					t.Fatal("unrelated payload was suppressed")
				}
			},
		},
		{
			name: "sweep does not evict entries still inside the window",
			run: func(t *testing.T, d *dedupe, now *time.Time) {
				// Push the table past the sweep threshold with stale entries,
				// then confirm a fresh entry still suppresses.
				for i := range 600 {
					d.suppress([]byte{byte(i), byte(i >> 8)}, 9)
				}
				d.suppress(pkt, 1)
				*now = now.Add(100 * time.Millisecond)
				for i := range 600 {
					d.suppress([]byte{byte(i), byte(i >> 8), 0xff}, 9)
				}
				if !d.suppress(pkt, 1) {
					t.Fatal("sweep evicted an entry that was still inside the window")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			d := newDedupe(2 * time.Second)
			d.now = func() time.Time { return now }
			tc.run(t, d, &now)
		})
	}
}
