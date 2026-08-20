// Command apdiscovery advertises this Aperture install on the local network over
// mDNS/DNS-SD, so it shows up as "Aperture NVR" in network scanners, Bonjour
// browsers and Finder/Safari instead of as an anonymous "ubuntu-server".
//
// Why this is a separate process rather than part of nvrd: mDNS is link-local
// MULTICAST (224.0.0.251), and multicast does not cross a Docker bridge NAT.
// nvrd runs on the bridge, so anything it broadcast would never leave the host.
// This runs in its own container with host networking, where it can. It holds no
// state, touches no database, and is entirely independent of recording — if it
// dies, nothing else notices.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/brutella/dnssd"
)

// lanInterfaces returns the interfaces worth advertising on: up, not loopback,
// carrying a real IPv4 address, and not a container bridge.
//
// This matters more than it looks. Left to itself the responder advertises every
// address it can see — including 172.17.0.1 and friends — and a client that picks
// one of those cannot reach anything. The first build published a service whose
// ONLY address was a Docker bridge IP, which is worse than not advertising.
func lanInterfaces() []string {
	if v := os.Getenv("APERTURE_ADVERTISE_IFACE"); v != "" {
		return strings.Split(v, ",")
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, in := range ifs {
		if in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 {
			continue
		}
		n := in.Name
		if strings.HasPrefix(n, "docker") || strings.HasPrefix(n, "br-") ||
			strings.HasPrefix(n, "veth") || strings.HasPrefix(n, "virbr") {
			continue
		}
		addrs, err := in.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	name := env("APERTURE_ADVERTISE_NAME", "Aperture NVR")
	// The hostname published as <host>.local. Without this the responder uses the
	// machine's own name ("ubuntu-server"), so scanners that show the mDNS
	// hostname reveal nothing about what the box actually is.
	host := env("APERTURE_ADVERTISE_HOST", "aperture")
	ifaces := lanInterfaces()
	portStr := env("APERTURE_ADVERTISE_PORT", "8971")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		log.Fatalf("apdiscovery: invalid APERTURE_ADVERTISE_PORT %q", portStr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, err := dnssd.NewResponder()
	if err != nil {
		log.Fatalf("apdiscovery: responder: %v", err)
	}

	// _http._tcp is what generic scanners and browsers look for, so the box shows
	// up as a web service they can open. The TXT record carries enough for a
	// client to build a URL and identify the product without guessing.
	svc, err := dnssd.NewService(dnssd.Config{
		Name:   name,
		Host:   host,
		Ifaces: ifaces,
		Type:   "_http._tcp",
		Port:   port,
		Text: map[string]string{
			"path":    "/",
			"product": "Aperture NVR",
			"vendor":  "Aperture",
		},
	})
	if err != nil {
		log.Fatalf("apdiscovery: service: %v", err)
	}
	if _, err := resp.Add(svc); err != nil {
		log.Fatalf("apdiscovery: add: %v", err)
	}

	// A product-specific type as well, so other Aperture-aware software can find
	// installs without sifting through every web server on the network.
	own, err := dnssd.NewService(dnssd.Config{
		Name:   name,
		Host:   host,
		Ifaces: ifaces,
		Type:   "_aperture._tcp",
		Port:   port,
		Text:   map[string]string{"path": "/", "product": "Aperture NVR"},
	})
	if err == nil {
		if _, err := resp.Add(own); err != nil {
			log.Printf("apdiscovery: _aperture._tcp not advertised: %v", err)
		}
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancel()
	}()

	log.Printf("apdiscovery: advertising %q as %s.local on port %d via %v (_http._tcp, _aperture._tcp)",
		name, host, port, ifaces)
	if err := resp.Respond(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("apdiscovery: respond: %v", err)
	}
	log.Printf("apdiscovery: stopped")
}
