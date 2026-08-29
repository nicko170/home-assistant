// mdns-reflect bridges multicast discovery between the house LAN and the
// OrbStack container subnet on the mac mini.
//
// WHY THIS EXISTS
// Home Assistant runs in an OrbStack container. docker-compose declares
// network_mode: host, which on Linux would put HA on the LAN - but on macOS
// "host" is the Linux VM's network stack, not the mac's. HA lands on
// 192.168.138.0/23 behind a NAT. Unicast out still works, so anything
// configured by IP is fine; multicast never crosses, so every discovery-based
// integration is blind.
//
// The mac sits on both segments (en0 on the LAN, bridge100 on the OrbStack
// bridge), so it is the one place that can relay between them. This reflects
// mDNS and SSDP in both directions.
//
// It replaces hap-reflect.sh, which solved one hardcoded slice of the same
// problem: republishing HA's single _hap._tcp record outbound via dns-sd.
//
// NOT SOLVED HERE: UPnP event callbacks. Sonos, Reolink and SamsungTV
// subscribe by handing HA's address to the device, which then connects back
// by unicast. That is a routing problem, fixed by the static route on the
// Juniper plus IP forwarding on this host - not by anything in this file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const dedupeWindow = 2 * time.Second

func main() {
	var (
		lanName  = flag.String("lan", "en0", "interface on the house LAN")
		vmName   = flag.String("vm", "bridge100", "interface on the OrbStack container bridge")
		names    = flag.String("groups", "mdns,ssdp", "comma-separated multicast groups to reflect")
		verbose  = flag.Bool("v", false, "log every forwarded packet")
		statsInt = flag.Duration("stats", 15*time.Minute, "how often to log counters (0 disables)")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if err := run(log, *lanName, *vmName, *names, *statsInt); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, lanName, vmName, names string, statsInt time.Duration) error {
	lan, err := net.InterfaceByName(lanName)
	if err != nil {
		return fmt.Errorf("lan interface %q: %w", lanName, err)
	}
	vm, err := net.InterfaceByName(vmName)
	if err != nil {
		return fmt.Errorf("vm interface %q: %w", vmName, err)
	}
	if lan.Index == vm.Index {
		return fmt.Errorf("lan and vm are the same interface (%s); reflection would loop", lanName)
	}

	selected, err := parseGroups(names)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st := &stats{}
	errs := make(chan error, len(selected))

	// A group we cannot bind is a degradation, not a fatal error. On this host
	// OrbStack holds a wildcard *:1900 without SO_REUSEPORT, so SSDP cannot be
	// shared - but mDNS still binds, and mDNS is what Cast and AirPlay
	// discovery need. Losing SSDP costs us discovery of NEW UPnP devices only;
	// the already-configured ones (Sonos, Reolink, SamsungTV) are reached by
	// IP and fixed by the static route, not by this process.
	started := 0
	for _, g := range selected {
		r, err := newReflector(g, lan, vm, log, st)
		if err != nil {
			log.Warn("group unavailable, continuing without it",
				"group", g.name, "port", g.port, "err", err)
			continue
		}
		log.Info("reflecting", "group", g.name, "port", g.port,
			"lan", lan.Name, "vm", vm.Name)
		started++
		go func() { errs <- r.run(ctx) }()
	}
	if started == 0 {
		return fmt.Errorf("no multicast groups could be bound; nothing to reflect")
	}

	if statsInt > 0 {
		go func() {
			t := time.NewTicker(statsInt)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					log.Info("counters",
						"forwarded", st.forwarded.Load(),
						"suppressed_loops", st.suppress.Load(),
						"dropped_self", st.selfDrop.Load())
				}
			}
		}()
	}

	select {
	case <-ctx.Done():
		log.Info("shutting down",
			"forwarded", st.forwarded.Load(),
			"suppressed_loops", st.suppress.Load(),
			"dropped_self", st.selfDrop.Load())
		return nil
	case err := <-errs:
		return err
	}
}

func parseGroups(names string) ([]group, error) {
	var out []group
	for _, n := range strings.Split(names, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		g, ok := groups[n]
		if !ok {
			return nil, fmt.Errorf("unknown group %q (known: mdns, ssdp)", n)
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no groups selected")
	}
	return out, nil
}
