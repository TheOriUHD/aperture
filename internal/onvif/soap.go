package onvif

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/version"
)

// maxSOAPBody caps request bodies to avoid unbounded reads from a hostile client.
const maxSOAPBody = 1 << 20 // 1 MiB

// handleSOAP parses the incoming SOAP request, extracts the operation name from
// the Body, dispatches to the matching handler, and writes a SOAP 1.2 response.
func (s *Server) handleSOAP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// Some clients issue a GET to probe the endpoint; answer with a hint.
		w.Header().Set("Allow", "POST")
		http.Error(w, "ONVIF SOAP endpoint: POST only", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxSOAPBody))
	if err != nil {
		s.writeFault(w, "reading request body")
		return
	}

	op := operationName(body)
	if op == "" {
		s.writeFault(w, "could not determine ONVIF operation")
		return
	}

	s.log.Debug("onvif request", "op", op, "remote", r.RemoteAddr)

	switch op {
	// --- Device service ---
	case "GetDeviceInformation":
		s.respond(w, s.deviceInformation())
	case "GetCapabilities":
		s.respond(w, s.capabilities())
	case "GetServices":
		s.respond(w, s.services())
	case "GetSystemDateAndTime":
		s.respond(w, s.systemDateAndTime())
	case "GetScopes":
		s.respond(w, s.scopes())
	case "GetServiceCapabilities":
		// Shared name across services; a minimal device capabilities doc is a
		// safe answer for lenient clients.
		s.respond(w, s.deviceServiceCapabilities())

	// --- Media service ---
	case "GetProfiles":
		s.respond(w, s.profiles())
	case "GetProfile":
		s.respond(w, s.profile(tokenArg(body)))
	case "GetVideoSources":
		s.respond(w, s.videoSources())
	case "GetVideoSourceConfigurations":
		s.respond(w, s.videoSourceConfigurations())
	case "GetStreamUri":
		s.respond(w, s.streamURIResponse(tokenArg(body)))
	case "GetSnapshotUri":
		s.respond(w, s.snapshotURIResponse(tokenArg(body)))

	default:
		s.writeFault(w, "unsupported operation: "+op)
	}
}

// respond writes an inner XML payload wrapped in a SOAP 1.2 envelope.
func (s *Server) respond(w http.ResponseWriter, inner string) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(envelopeOpen)
	buf.WriteString(inner)
	buf.WriteString(envelopeClose)

	w.Header().Set("Content-Type", `application/soap+xml; charset=utf-8`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// writeFault emits a SOAP 1.2 Fault with an explanatory reason.
func (s *Server) writeFault(w http.ResponseWriter, reason string) {
	inner := `<s:Fault><s:Code><s:Value>s:Sender</s:Value></s:Code>` +
		`<s:Reason><s:Text xml:lang="en">` + xmlEscape(reason) + `</s:Text></s:Reason></s:Fault>`
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(envelopeOpen)
	buf.WriteString(inner)
	buf.WriteString(envelopeClose)

	w.Header().Set("Content-Type", `application/soap+xml; charset=utf-8`)
	// SOAP 1.2 faults are returned with HTTP 500 by convention.
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write(buf.Bytes())
}

// operationName walks the SOAP XML and returns the local name of the first
// element inside the Body, which is the ONVIF operation being invoked. It is
// namespace-agnostic (matches on local name) so it tolerates any client prefix.
func operationName(body []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	inBody := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !inBody {
			if strings.EqualFold(se.Name.Local, "Body") {
				inBody = true
			}
			continue
		}
		// First start element inside Body is the operation.
		return se.Name.Local
	}
}

// tokenArg extracts a ProfileToken / VideoSourceToken-style argument from the
// request Body, if present. ONVIF passes the token as a child element (e.g.
// <ProfileToken>...</ProfileToken>) or occasionally as an attribute; we match on
// any element whose local name ends in "Token".
func tokenArg(body []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.HasSuffix(se.Name.Local, "Token") {
			// Read the char data that follows.
			var val string
			for {
				t2, err := dec.Token()
				if err != nil {
					break
				}
				if cd, ok := t2.(xml.CharData); ok {
					val += string(cd)
				}
				if _, ok := t2.(xml.EndElement); ok {
					break
				}
			}
			if v := strings.TrimSpace(val); v != "" {
				return v
			}
		}
	}
}

// --- helpers to derive stable ONVIF tokens from camera IDs ---

func profileToken(camID string) string     { return "profile_" + camID }
func videoSourceToken(camID string) string { return "vsource_" + camID }
func videoSourceCfgToken(camID string) string {
	return "vsource_cfg_" + camID
}
func videoEncoderCfgToken(camID string) string { return "vencoder_" + camID }

// camIDFromToken maps any of our issued tokens back to the underlying camera. It
// strips the known prefix and validates the camera exists + is enabled.
func (s *Server) camIDFromToken(token string) (*store.Camera, bool) {
	id := token
	for _, p := range []string{"profile_", "vsource_cfg_", "vsource_", "vencoder_"} {
		if strings.HasPrefix(id, p) {
			id = strings.TrimPrefix(id, p)
			break
		}
	}
	for _, c := range s.enabledCameras() {
		if c.ID == id {
			return c, true
		}
	}
	return nil, false
}

// nowUTC returns the current time in UTC for GetSystemDateAndTime.
func nowUTC() time.Time { return time.Now().UTC() }

// firmwareVersion reports Aperture's version string as the device firmware.
func firmwareVersion() string { return version.Version }
