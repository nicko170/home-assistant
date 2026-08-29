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

// ipv4Addr returns an interface's first IPv4 address. Reflected packets are
// sent from this, so it must be a real unicast address.
func ipv4Addr(ifi *net.Interface) (net.IP, error) {
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, fmt.Errorf("addrs for %s: %w", ifi.Name, err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if v4 := ipn.IP.To4(); v4 != nil {
				return v4, nil
			}
		}
	}
	return nil, fmt.Errorf("%s has no IPv4 address", ifi.Name)
}

type stats struct {
	forwarded atomic.Uint64
	suppress  atomic.Uint64
	selfDrop  atomic.Uint64
}

type reflector struct {
	g       group
	rx      *ipv4.PacketConn
	tx      map[int]*ipv4.PacketConn // egress interface index -> send socket
	lan, vm *net.Interface
	dedupe  *dedupe
	local   map[string]bool
	stats   *stats
	log     *slog.Logger
}

func reusePorts(_, _ string, c syscall.RawConn) error {
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
}

// listenGroup opens the RECEIVE socket, bound to the multicast group address.
//
// Binding the group rather than the wildcard is deliberate. Both ports are
// already held on this host - mDNSResponder has 5353 and OrbStack has *:1900 -
// and SO_REUSEPORT only lets sockets share a port when every holder set it,
// which OrbStack's does not. The group address is a different, narrower
// address that BSD allows alongside the existing wildcard, and it delivers
// exactly the traffic we want.
func listenGroup(g group) (net.PacketConn, error) {
	return bindGroup(g.ip, g.port)
}

// listenSend opens a per-interface SEND socket, bound to that interface's own
// address on the group's port.
//
// This must be a separate socket from the receive one. A socket bound to the
// group address sends with a source of 224.0.0.251, which is not a valid
// source address - the packets leave, and every receiver discards them. That
// bug made the reflector look like it was working while delivering nothing.
//
// Binding the group's port (rather than an ephemeral one) matters too: mDNS
// requires responses to originate from 5353, and responders ignore packets
// that do not.
func listenSend(ifi *net.Interface, g group) (*ipv4.PacketConn, error) {
	addr, err := ipv4Addr(ifi)
	if err != nil {
		return nil, err
	}
	lc := net.ListenConfig{Control: reusePorts}
	pc, err := lc.ListenPacket(context.Background(), "udp4",
		net.JoinHostPort(addr.String(), strconv.Itoa(g.port)))
	if err != nil {
		return nil, fmt.Errorf("send socket on %s: %w", ifi.Name, err)
	}
	conn := ipv4.NewPacketConn(pc)
	if err := conn.SetMulticastInterface(ifi); err != nil {
		pc.Close()
		return nil, fmt.Errorf("multicast interface %s: %w", ifi.Name, err)
	}
	// Discovery is link-local by design; 1 hop keeps reflected traffic on the
	// two segments we bridge and off the rest of the network.
	if err := conn.SetMulticastTTL(1); err != nil {
		pc.Close()
		return nil, fmt.Errorf("ttl on %s: %w", ifi.Name, err)
	}
	// Our own reflections must not be delivered back to us.
	if err := conn.SetMulticastLoopback(false); err != nil {
		pc.Close()
		return nil, fmt.Errorf("loopback on %s: %w", ifi.Name, err)
	}
	return conn, nil
}

func newReflector(g group, lan, vm *net.Interface, log *slog.Logger, st *stats) (*reflector, error) {
	pc, err := listenGroup(g)
	if err != nil {
		return nil, fmt.Errorf("listen %s/%d: %w", g.name, g.port, err)
	}

	rx := ipv4.NewPacketConn(pc)
	gaddr := &net.UDPAddr{IP: g.ip}
	for _, ifi := range []*net.Interface{lan, vm} {
		if err := rx.JoinGroup(ifi, gaddr); err != nil {
			pc.Close()
			return nil, fmt.Errorf("join %s on %s: %w", g.name, ifi.Name, err)
		}
	}
	// We need the ingress interface on every read to know which way to forward.
	if err := rx.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		pc.Close()
		return nil, fmt.Errorf("control message: %w", err)
	}

	tx := make(map[int]*ipv4.PacketConn, 2)
	for _, ifi := range []*net.Interface{lan, vm} {
		conn, err := listenSend(ifi, g)
		if err != nil {
			pc.Close()
			for _, c := range tx {
				c.Close()
			}
			return nil, err
		}
		tx[ifi.Index] = conn
	}

	local, err := localAddrs(lan, vm)
	if err != nil {
		pc.Close()
		for _, c := range tx {
			c.Close()
		}
		return nil, err
	}

	return &reflector{
		g: g, rx: rx, tx: tx, lan: lan, vm: vm,
		dedupe: newDedupe(dedupeWindow), local: local, stats: st, log: log,
	}, nil
}

func (r *reflector) run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		r.rx.Close()
		for _, c := range r.tx {
			c.Close()
		}
	}()

	dst := &net.UDPAddr{IP: r.g.ip, Port: r.g.port}
	buf := make([]byte, 9000) // jumbo-safe; mDNS and SSDP are far smaller

	for {
		n, cm, src, err := r.rx.ReadFrom(buf)
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

		out, ok := r.tx[egress]
		if !ok {
			continue
		}
		if _, err := out.WriteTo(buf[:n], nil, dst); err != nil {
			// A single write failure is not fatal - the interface may be
			// mid-reconfigure. Log and keep serving the other direction.
			r.log.Warn("forward failed", "group", r.g.name, "egress", egress, "err", err)
			continue
		}
		r.stats.forwarded.Add(1)
		r.log.Debug("forwarded", "group", r.g.name, "src", src.String(), "bytes", n, "egress", egress)
	}
}
