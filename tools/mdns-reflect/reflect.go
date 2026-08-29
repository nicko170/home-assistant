package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync/atomic"
	"syscall"

	"golang.org/x/net/ipv4"
)

// group is a multicast destination we reflect between the two interfaces.
type group struct {
	name string
	ip   net.IP
	port int
}

var groups = map[string]group{
	// Bonjour/mDNS. Cast and AirPlay discovery ride on this.
	"mdns": {name: "mdns", ip: net.IPv4(224, 0, 0, 251), port: 5353},
	// SSDP. Sonos and other UPnP devices announce here.
	"ssdp": {name: "ssdp", ip: net.IPv4(239, 255, 255, 250), port: 1900},
}

// peer returns the interface a packet should be forwarded out of, given the
// interface it arrived on. Packets from anywhere else are not ours to reflect.
func peer(ingress, lan, vm int) (int, bool) {
	switch ingress {
	case lan:
		return vm, true
	case vm:
		return lan, true
	default:
		return 0, false
	}
}

// localAddrs collects every unicast address on the two interfaces we bridge.
// A packet sourced from one of these is either our own reflection coming back
// or something the host itself emitted, and must never be forwarded - that is
// how a reflector turns into a packet storm.
func localAddrs(ifaces ...*net.Interface) (map[string]bool, error) {
	out := make(map[string]bool)
	for _, ifi := range ifaces {
		addrs, err := ifi.Addrs()
		if err != nil {
			return nil, fmt.Errorf("addrs for %s: %w", ifi.Name, err)
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				out[ipn.IP.String()] = true
			}
		}
	}
	return out, nil
}

type stats struct {
	forwarded atomic.Uint64
	suppress  atomic.Uint64
	selfDrop  atomic.Uint64
}

type reflector struct {
	g       group
	conn    *ipv4.PacketConn
	lan, vm *net.Interface
	dedupe  *dedupe
	local   map[string]bool
	stats   *stats
	log     *slog.Logger
}

// listen binds the multicast group with SO_REUSEADDR|SO_REUSEPORT.
//
// Two details are load-bearing on macOS. First, both socket options are
// required: mDNSResponder already holds 5353 and OrbStack holds 1900, and
// without them the bind fails outright rather than sharing.
//
// Second, we bind the GROUP address rather than the wildcard. SO_REUSEPORT
// only lets sockets share a port when every holder set it, and OrbStack's
// wildcard *:1900 does not - so a wildcard bind loses the race and returns
// EADDRINUSE. Binding 239.255.255.250:1900 is a different, narrower address,
// which BSD permits alongside the existing wildcard and which delivers
// exactly the group traffic we want.
func listen(g group) (net.PacketConn, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			err := c.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
					serr = err
					return
				}
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
			})
			if err != nil {
				return err
			}
			return serr
		},
	}
	return lc.ListenPacket(context.Background(), "udp4", net.JoinHostPort(g.ip.String(), strconv.Itoa(g.port)))
}

func newReflector(g group, lan, vm *net.Interface, log *slog.Logger, st *stats) (*reflector, error) {
	pc, err := listen(g)
	if err != nil {
		return nil, fmt.Errorf("listen %s/%d: %w", g.name, g.port, err)
	}

	conn := ipv4.NewPacketConn(pc)
	gaddr := &net.UDPAddr{IP: g.ip}
	for _, ifi := range []*net.Interface{lan, vm} {
		if err := conn.JoinGroup(ifi, gaddr); err != nil {
			pc.Close()
			return nil, fmt.Errorf("join %s on %s: %w", g.name, ifi.Name, err)
		}
	}

	// We need the ingress interface on every read to know which way to
	// forward, and we set the egress interface per-write via the same struct.
	if err := conn.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		pc.Close()
		return nil, fmt.Errorf("control message: %w", err)
	}
	// Our own reflections must not be delivered back to us.
	if err := conn.SetMulticastLoopback(false); err != nil {
		pc.Close()
		return nil, fmt.Errorf("disable loopback: %w", err)
	}
	// Discovery is link-local by design; 1 hop keeps reflected traffic on the
	// two segments we bridge and off the rest of the network.
	if err := conn.SetMulticastTTL(1); err != nil {
		pc.Close()
		return nil, fmt.Errorf("set ttl: %w", err)
	}

	local, err := localAddrs(lan, vm)
	if err != nil {
		pc.Close()
		return nil, err
	}

	return &reflector{
		g: g, conn: conn, lan: lan, vm: vm,
		dedupe: newDedupe(dedupeWindow), local: local, stats: st, log: log,
	}, nil
}

func (r *reflector) run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		r.conn.Close()
	}()

	dst := &net.UDPAddr{IP: r.g.ip, Port: r.g.port}
	buf := make([]byte, 9000) // jumbo-safe; mDNS and SSDP are far smaller

	for {
		n, cm, src, err := r.conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return fmt.Errorf("read %s: %w", r.g.name, err)
		}
		if cm == nil {
			continue
		}

		egress, ok := peer(cm.IfIndex, r.lan.Index, r.vm.Index)
		if !ok {
			continue // arrived on an interface we do not bridge
		}

		if ua, ok := src.(*net.UDPAddr); ok && r.local[ua.IP.String()] {
			r.stats.selfDrop.Add(1)
			continue
		}

		if r.dedupe.suppress(buf[:n], egress) {
			r.stats.suppress.Add(1)
			continue
		}

		out := &ipv4.ControlMessage{IfIndex: egress}
		if _, err := r.conn.WriteTo(buf[:n], out, dst); err != nil {
			// A single write failure is not fatal - the interface may be
			// mid-reconfigure. Log and keep serving the other direction.
			r.log.Warn("forward failed", "group", r.g.name, "egress", egress, "err", err)
			continue
		}
		r.stats.forwarded.Add(1)
		r.log.Debug("forwarded", "group", r.g.name, "src", src.String(), "bytes", n, "egress", egress)
	}
}
