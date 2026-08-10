// Package onvif implements a pragmatic ONVIF Profile S provider so external
// NVRs/VMSes (e.g. Milestone, Frigate, Synology Surveillance Station, Blue Iris)
// can discover and consume Aperture's cameras as if Aperture were a multi-channel
// ONVIF encoder.
//
// It exposes a minimal SOAP 1.2 endpoint covering the ONVIF Device and Media
// services (enough of Profile S for stream + snapshot consumption) plus optional
// WS-Discovery so clients can auto-find the device on the LAN.
//
// The provider is OFF BY DEFAULT and, when enabled, accepts unauthenticated
// requests on the LAN (see the package README/caveats). It is intended to run on
// a trusted management network only.
package onvif

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Server hosts the ONVIF SOAP endpoint and (optionally) a WS-Discovery responder.
type Server struct {
	cfg *config.Config
	st  *store.Store
	log *slog.Logger
}

// New constructs an ONVIF Server. It does not bind any sockets until Run is
// called. When cfg.ONVIFEnabled is false, Run is a no-op.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Server {
	return &Server{
		cfg: cfg,
		st:  st,
		log: log.With("subsystem", "onvif"),
	}
}

// Run starts the ONVIF HTTP SOAP server and the WS-Discovery listener, blocking
// until ctx is cancelled. When ONVIF is disabled it logs and returns immediately.
func (s *Server) Run(ctx context.Context) {
	if !s.cfg.ONVIFEnabled {
		s.log.Info("onvif disabled")
		return
	}

	addr := s.cfg.ONVIFAddr
	if addr == "" {
		addr = ":10101"
	}

	mux := http.NewServeMux()
	// ONVIF clients POST SOAP to the service paths advertised in GetServices /
	// GetCapabilities. We serve Device and Media from distinct paths and also
	// accept the bare "/onvif/" prefix for lenient clients.
	mux.HandleFunc("/onvif/device_service", s.handleSOAP)
	mux.HandleFunc("/onvif/media_service", s.handleSOAP)
	mux.HandleFunc("/onvif/", s.handleSOAP)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start WS-Discovery; failure to bind multicast (common in containers without
	// host networking) must not stop the SOAP server.
	disc := newDiscovery(s.deviceServiceURL(), s.log)
	go disc.run(ctx)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	s.log.Info("onvif listening",
		"addr", addr,
		"device_service", s.deviceServiceURL(),
		"media_service", s.mediaServiceURL(),
	)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.log.Error("onvif http server", "err", err)
	}
}

// deviceServiceURL is the externally-reachable URL of the Device service. ONVIF
// clients expect an absolute http:// URL they can reach. We derive the host from
// the local outbound IP (best-effort) and the configured ONVIF port so the URL
// is valid on the LAN even when PublicURL points at a reverse proxy that does not
// forward the ONVIF port.
func (s *Server) deviceServiceURL() string {
	return "http://" + s.advertiseHostPort() + "/onvif/device_service"
}

func (s *Server) mediaServiceURL() string {
	return "http://" + s.advertiseHostPort() + "/onvif/media_service"
}

// snapshotURL builds the per-camera JPEG snapshot URL served by go2rtc, advertised
// at an address an external ONVIF client can actually reach (not the internal
// how-NVRD-reaches-go2rtc host, e.g. host.docker.internal).
func (s *Server) snapshotURL(camID string) string {
	return "http://" + s.streamAdvertiseHost(s.cfg.Go2RTCSnapPort()) + "/api/frame.jpeg?src=cam_" + camID
}

// streamURL builds the per-camera RTSP restream URL served by go2rtc, at an
// externally-reachable address (see streamAdvertiseHost).
func (s *Server) streamURL(camID string) string {
	return "rtsp://" + s.streamAdvertiseHost(s.cfg.Go2RTCRTSPPort()) + "/cam_" + camID
}

// streamAdvertiseHost returns host:port an external client should use to reach
// go2rtc's restream/snapshot: the configured StreamHost, or the best-effort primary
// LAN IP. The Go2RTC* bases name how NVRD reaches go2rtc (e.g. host.docker.internal)
// and are unroutable for anyone else, so they must not be advertised.
func (s *Server) streamAdvertiseHost(port string) string {
	host := strings.TrimSpace(s.cfg.StreamHost)
	if host == "" {
		host = primaryIP()
	}
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, port)
	}
	return host
}

// advertiseHostPort returns host:port that ONVIF clients should use to reach the
// SOAP endpoint. Host is the best-effort primary LAN IP; port is taken from
// ONVIFAddr (defaulting to 10101).
func (s *Server) advertiseHostPort() string {
	host := primaryIP()
	_, port, err := net.SplitHostPort(s.cfg.ONVIFAddr)
	if err != nil || port == "" {
		port = "10101"
	}
	return net.JoinHostPort(host, port)
}

// primaryIP returns the host's primary outbound IPv4 address, falling back to
// 127.0.0.1 if it cannot be determined. It uses a UDP dial (no packets sent) to
// let the kernel pick the source address for the default route.
func primaryIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok && ua.IP != nil {
			return ua.IP.String()
		}
	}
	// Fallback: first non-loopback IPv4 on any interface.
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if v4 := ipnet.IP.To4(); v4 != nil {
					return v4.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

// enabledCameras returns the enabled cameras, sorted by SortOrder then ID so the
// ONVIF profile/source token order is stable across requests.
func (s *Server) enabledCameras() []*store.Camera {
	cams, err := s.st.ListCameras()
	if err != nil {
		s.log.Warn("onvif: list cameras", "err", err)
		return nil
	}
	out := make([]*store.Camera, 0, len(cams))
	for _, c := range cams {
		if c.Enabled {
			out = append(out, c)
		}
	}
	return out
}
