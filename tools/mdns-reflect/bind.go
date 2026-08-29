package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// bindGroup binds a UDP socket to a multicast GROUP address.
//
// This deliberately bypasses net.ListenConfig.ListenPacket. Go resolves a
// multicast listen address to the wildcard internally, so asking it for
// 239.255.255.250:1900 actually attempts 0.0.0.0:1900 - which collides with
// OrbStack's existing wildcard holder and fails with EADDRINUSE. The kernel
// itself is perfectly willing to bind the group address (verified with the
// equivalent Python calls, which succeed on both 5353 and 1900); only Go's
// convenience layer is in the way.
//
// Binding the group rather than the wildcard is what lets us coexist with
// mDNSResponder on 5353 and OrbStack on 1900, neither of which sets
// SO_REUSEPORT, so a wildcard bind can never be shared with them.
//
// Receiving multicast REQUIRES this. A socket bound to an interface's unicast
// address binds and joins fine but receives nothing - measured, 0 packets over
// 8s on a busy segment. That is why send and receive are separate sockets.
func bindGroup(ip net.IP, port int) (net.PacketConn, error) {
	v4 := ip.To4()
	if v4 == nil {
		return nil, fmt.Errorf("%s is not an IPv4 address", ip)
	}

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	// From here on every error path must close the fd; it is not yet owned by
	// an os.File that would finalise it.
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
	// FilePacketConn dups the descriptor, so the os.File is ours to close
	// regardless of outcome - leaving it open would leak one fd per group.
	defer f.Close()

	pc, err := net.FilePacketConn(f)
	if err != nil {
		return nil, fmt.Errorf("FilePacketConn: %w", err)
	}
	return pc, nil
}
