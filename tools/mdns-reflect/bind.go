package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// bindReceive opens the receive socket for a group, preferring a WILDCARD bind
// and falling back to the group address.
//
// The wildcard is strongly preferred, and the reason is not obvious.
// Discovery protocols answer queries by UNICAST as often as by multicast -
// mDNS has the QU bit, SSDP answers M-SEARCH unicast to the querier. A socket
// bound to 224.0.0.251 only ever sees traffic addressed to the group, so every
// unicast reply is invisible to it. Measured on this host over 12s: wildcard
// received 278 mDNS packets, the group bind 27. Reflecting only the multicast
// half means queries go out and answers never come back, which looks exactly
// like a reflector that is running fine and discovering nothing.
//
// The fallback exists because the wildcard is not always available. 5353 is
// held by mDNSResponder and 1900 by OrbStack; SO_REUSEPORT only shares a port
// when every holder set it, and OrbStack's did not. So mDNS gets the wildcard
// and full two-way reflection, while SSDP falls back to the group address and
// carries multicast NOTIFY announcements only. That is enough for devices that
// announce themselves periodically, which includes Sonos.
//
// Both paths use direct syscalls rather than net.ListenConfig.ListenPacket,
// because Go resolves a multicast listen address to the wildcard internally -
// asking it for 239.255.255.250:1900 really attempts 0.0.0.0:1900. That makes
// the two cases indistinguishable from Go's API and silently collides with
// OrbStack.
func bindReceive(group net.IP, port int) (pc net.PacketConn, wildcard bool, err error) {
	if pc, err = rawBind(net.IPv4zero, port); err == nil {
		return pc, true, nil
	}
	wildErr := err
	if pc, err = rawBind(group, port); err == nil {
		return pc, false, nil
	}
	return nil, false, fmt.Errorf("wildcard: %v; group: %w", wildErr, err)
}

// rawBind creates a UDP socket bound to exactly the address given, with the
// reuse options set before bind so it can coexist with an existing holder.
func rawBind(ip net.IP, port int) (net.PacketConn, error) {
	v4 := ip.To4()
	if v4 == nil {
		return nil, fmt.Errorf("%s is not an IPv4 address", ip)
	}

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	// Until an os.File owns this fd, every error path has to close it by hand.
	fail := func(format string, args ...any) (net.PacketConn, error) {
		syscall.Close(fd)
		return nil, fmt.Errorf(format, args...)
	}

	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return fail("SO_REUSEADDR: %w", err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
		return fail("SO_REUSEPORT: %w", err)
	}

	sa := &syscall.SockaddrInet4{Port: port}
	copy(sa.Addr[:], v4)
	if err := syscall.Bind(fd, sa); err != nil {
		return fail("bind %s:%d: %w", ip, port, err)
	}

	f := os.NewFile(uintptr(fd), fmt.Sprintf("mcast:%s:%d", ip, port))
	// FilePacketConn dups the descriptor, so this os.File is ours to close
	// either way - not closing it leaks one fd per group.
	defer f.Close()

	pc, err := net.FilePacketConn(f)
	if err != nil {
		return nil, fmt.Errorf("FilePacketConn: %w", err)
	}
	return pc, nil
}
