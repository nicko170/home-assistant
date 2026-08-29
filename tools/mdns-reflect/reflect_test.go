package main

import (
	"net"
	"testing"
)

func TestPeer(t *testing.T) {
	const lan, vm = 4, 9

	tests := []struct {
		name    string
		ingress int
		want    int
		wantOK  bool
	}{
		{"lan forwards to vm", lan, vm, true},
		{"vm forwards to lan", vm, lan, true},
		{"unrelated interface is ignored", 77, 0, false},
		{"zero index is ignored", 0, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := peer(tc.ingress, lan, vm)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("peer(%d) = (%d, %v), want (%d, %v)",
					tc.ingress, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestParseGroups(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{name: "both groups", in: "mdns,ssdp", want: []string{"mdns", "ssdp"}},
		{name: "single group", in: "mdns", want: []string{"mdns"}},
		{name: "whitespace tolerated", in: " mdns , ssdp ", want: []string{"mdns", "ssdp"}},
		{name: "unknown group rejected", in: "mdns,bonjour", wantErr: true},
		{name: "empty rejected", in: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGroups(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseGroups(%q) succeeded; want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGroups(%q): %v", tc.in, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d groups, want %d", len(got), len(tc.want))
			}
			for i, g := range got {
				if g.name != tc.want[i] {
					t.Errorf("group %d = %q, want %q", i, g.name, tc.want[i])
				}
			}
		})
	}
}

// A packet sourced from one of our own addresses must be dropped, or the
// reflector feeds on its own output.
func TestLocalAddrsRecognisesOwnAddress(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skip("no interfaces available")
	}
	var withAddr *net.Interface
	var want string
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				withAddr, want = &ifaces[i], ipn.IP.String()
				break
			}
		}
		if withAddr != nil {
			break
		}
	}
	if withAddr == nil {
		t.Skip("no interface with an IPv4 address")
	}

	local, err := localAddrs(withAddr)
	if err != nil {
		t.Fatalf("localAddrs: %v", err)
	}
	if !local[want] {
		t.Fatalf("localAddrs did not include own address %s", want)
	}
	if local["203.0.113.1"] {
		t.Fatal("localAddrs claimed an unrelated address as local")
	}
}
