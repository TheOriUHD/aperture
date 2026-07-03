package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"log/slog"
	"net"
	"strings"

	"github.com/TheOriUHD/aperture/internal/store"
)

// WS-Discovery multicast group and port per the ONVIF / WS-Discovery spec.
const (
	discoveryGroup = "239.255.255.250"
	discoveryPort  = 3702
)

// discovery answers WS-Discovery Probe requests with a ProbeMatch pointing at the
// ONVIF device service URL, letting VMS clients auto-find Aperture on the LAN.
type discovery struct {
	deviceXAddr string
	log         *slog.Logger
}

func newDiscovery(deviceXAddr string, log *slog.Logger) *discovery {
	return &discovery{deviceXAddr: deviceXAddr, log: log}
}

// run binds the WS-Discovery multicast socket and serves ProbeMatch replies until
// ctx is cancelled. A bind failure (e.g. containers without multicast/host
// networking) is logged as a warning and returns without killing the SOAP server.
func (d *discovery) run(ctx context.Context) {
	group := &net.UDPAddr{IP: net.ParseIP(discoveryGroup), Port: discoveryPort}

	// Listen on the multicast group across all interfaces. iface=nil lets the OS
	// choose; this is best-effort and may fail without host networking.
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		d.log.Warn("onvif ws-discovery disabled (multicast bind failed; SOAP server still running)", "err", err)
		return
	}
	defer conn.Close()

	_ = conn.SetReadBuffer(1 << 16)
	d.log.Info("onvif ws-discovery listening", "group", discoveryGroup, "port", discoveryPort)

	// Close the socket on cancellation to unblock ReadFromUDP.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	buf := make([]byte, 65536)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				d.log.Debug("onvif ws-discovery read", "err", err)
				return
			}
		}
		msgID, ok := parseProbe(buf[:n])
		if !ok {
			continue
		}
		reply := d.probeMatch(msgID)
		if _, err := conn.WriteToUDP([]byte(reply), src); err != nil {
			d.log.Debug("onvif ws-discovery reply", "err", err, "to", src.String())
		}
	}
}

// parseProbe returns (relatesToMessageID, true) if the datagram is a WS-Discovery
// Probe. It matches on the "Probe" local element name and pulls the WS-Addressing
// MessageID so the reply can carry a RelatesTo header.
func parseProbe(data []byte) (string, bool) {
	if !bytes.Contains(data, []byte("Probe")) {
		return "", false
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		isProbe bool
		msgID   string
		inMsgID bool
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Probe":
				isProbe = true
			case "MessageID":
				inMsgID = true
			}
		case xml.CharData:
			if inMsgID {
				msgID += string(t)
			}
		case xml.EndElement:
			if t.Name.Local == "MessageID" {
				inMsgID = false
			}
		}
	}
	if !isProbe {
		return "", false
	}
	return strings.TrimSpace(msgID), true
}

// probeMatch builds a SOAP 1.2 / WS-Discovery ProbeMatches envelope advertising
// this device and its Device service XAddr.
func (d *discovery) probeMatch(relatesTo string) string {
	// A stable urn:uuid endpoint reference for this device.
	epr := "urn:uuid:" + deviceUUID()
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"` +
		` xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"` +
		` xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"` +
		` xmlns:dn="http://www.onvif.org/ver10/network/wsdl">`)
	b.WriteString(`<s:Header>`)
	b.WriteString(`<a:MessageID>urn:uuid:` + newMsgUUID() + `</a:MessageID>`)
	if relatesTo != "" {
		b.WriteString(`<a:RelatesTo>` + xmlEscape(relatesTo) + `</a:RelatesTo>`)
	}
	b.WriteString(`<a:To>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:To>`)
	b.WriteString(`<a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</a:Action>`)
	b.WriteString(`</s:Header>`)
	b.WriteString(`<s:Body><d:ProbeMatches><d:ProbeMatch>`)
	b.WriteString(`<a:EndpointReference><a:Address>` + xmlEscape(epr) + `</a:Address></a:EndpointReference>`)
	b.WriteString(`<d:Types>dn:NetworkVideoTransmitter</d:Types>`)
	b.WriteString(`<d:Scopes>` +
		`onvif://www.onvif.org/type/video_encoder ` +
		`onvif://www.onvif.org/Profile/Streaming ` +
		`onvif://www.onvif.org/name/Aperture ` +
		`onvif://www.onvif.org/hardware/ApertureNVR` +
		`</d:Scopes>`)
	b.WriteString(`<d:XAddrs>` + xmlEscape(d.deviceXAddr) + `</d:XAddrs>`)
	b.WriteString(`<d:MetadataVersion>1</d:MetadataVersion>`)
	b.WriteString(`</d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	return b.String()
}

// deviceUUID returns a stable UUID string for this device's endpoint reference.
// We derive it from a fixed namespace so the same NVR reports the same EPR across
// restarts; store.NewID is only used as a fallback source of randomness.
func deviceUUID() string {
	// Fixed, deterministic UUID for the Aperture ONVIF device. This keeps VMS
	// device registrations stable across restarts.
	return "b7f3a1e2-9c4d-4a6b-8e10-aperture0nvr1"
}

// newMsgUUID returns a fresh random UUID-ish string for per-message MessageIDs.
func newMsgUUID() string {
	id := store.NewID() // 32 hex chars
	if len(id) < 32 {
		return id
	}
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:32]
}
