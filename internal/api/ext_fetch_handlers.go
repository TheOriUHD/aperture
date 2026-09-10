package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/ext"
)

// Host-mediated outbound fetch — the panel side of the `network` permission. A
// sandboxed UI panel cannot fetch anything itself (opaque origin + CSP connect-src
// 'none'); when it needs to reach a declared host (e.g. Home Assistant), it asks
// the host, which performs the request server-side, bounded to the extension's
// DECLARED hosts. {{secret:key}} placeholders are expanded from the extension's own
// secret settings server-side, so a secret token authenticates the call without the
// plaintext ever entering the browser. This is the same host allow-list a backend's
// Deno --allow-net enforces, applied to the panel path.
//
// Residual, stated plainly (and documented for authors): anyone who can use the
// panel can trigger requests to the declared host with the configured secret — this
// is what a `network` grant is. It is bounded to the declared host + the extension's
// own declared secrets, and never widens Aperture authority. Grant network only to
// extensions and hosts you trust.

const (
	extFetchTimeout     = 12 * time.Second
	extFetchMaxRespBody = 256 << 10 // 256 KiB of response returned to the panel
)

var (
	extFetchMethods = map[string]bool{
		http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
		http.MethodPatch: true, http.MethodDelete: true,
	}
	// reSecretRef matches {{secret:key}} placeholders the host expands server-side.
	reSecretRef = regexp.MustCompile(`\{\{\s*secret:([a-z0-9._-]+)\s*\}\}`)
	// extFetchLimiter caps outbound throughput so a chatty panel can't be used to
	// hammer a third-party host through the server.
	extFetchLimiter = newTokenBucket(10, 20)
)

type extFetchRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

func (s *Server) handleExtFetch(w http.ResponseWriter, r *http.Request) {
	if !s.extEnabled() {
		writeError(w, http.StatusNotFound, "extensions are disabled")
		return
	}
	id := chi.URLParam(r, "id")
	ex, err := s.st.GetExtension(id)
	if err != nil || !ex.Enabled {
		writeError(w, http.StatusNotFound, "extension not found or disabled")
		return
	}
	m, perr := ext.Parse([]byte(ex.ManifestJSON))
	if perr != nil {
		writeError(w, http.StatusInternalServerError, "manifest unreadable")
		return
	}
	d, verr := m.Validate()
	if verr != nil || (len(d.NetHosts) == 0 && len(d.NetSettings) == 0) {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "this extension did not request network access")
		return
	}

	var req extFetchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !extFetchMethods[method] {
		writeErrorCode(w, http.StatusBadRequest, "validation", "unsupported method")
		return
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation", "url must be an absolute http(s) URL")
		return
	}
	// The allow-list is the static manifest hosts PLUS the current host of every url
	// setting the manifest referenced — so it follows the URL the user configured.
	allowed, unconfigured := s.resolveExtHosts(id, d)
	if !hostAllowed(u.Host, allowed) {
		if unconfigured {
			writeErrorCode(w, http.StatusBadRequest, "not_configured",
				"the server address is not set (or is not a valid URL) — configure it in this extension's settings")
			return
		}
		writeErrorCode(w, http.StatusForbidden, "forbidden", "host "+u.Host+" is not one this extension may reach")
		return
	}
	if !extFetchLimiter.allow() {
		writeError(w, http.StatusTooManyRequests, "too many outbound requests; slow down")
		return
	}

	secretKeys := map[string]bool{}
	for _, k := range d.SecretKeys {
		secretKeys[k] = true
	}
	// Expand {{secret:key}} in the body and header values from the extension's own
	// secret settings. Referencing anything that is not a DECLARED secret fails —
	// the mechanism can only inject the extension's own secrets, nothing else.
	body, code, msg := s.expandSecrets(id, req.Body, secretKeys)
	if code != 0 {
		writeErrorCode(w, code, "validation", msg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), extFetchTimeout)
	defer cancel()
	out, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(body))
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid request")
		return
	}
	for k, v := range req.Headers {
		if strings.EqualFold(k, "host") {
			continue // never let the panel override Host
		}
		ev, ecode, emsg := s.expandSecrets(id, v, secretKeys)
		if ecode != 0 {
			writeErrorCode(w, ecode, "validation", emsg)
			return
		}
		out.Header.Set(k, ev)
	}

	// Do NOT follow redirects: a 3xx to a non-declared (possibly internal) host would
	// escape the allow-list. Return the redirect response as-is instead.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(out)
	if err != nil {
		// A connection failure is a legible result, not a server fault: resolve with
		// status 0 + a plain sentence the panel can show, keeping the raw error as a
		// detail the panel can reveal but not lead with.
		friendly, detail := classifyEgressError(err, u.Host)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": 0, "ok": false, "error": friendly, "detail": detail,
		})
		return
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, extFetchMaxRespBody))
	writeJSON(w, http.StatusOK, map[string]any{
		"status": resp.StatusCode,
		"ok":     resp.StatusCode >= 200 && resp.StatusCode < 300,
		"body":   string(rb),
	})
}

// resolveExtHosts is the effective egress allow-list: the static manifest hosts plus
// the current host of each url-setting the manifest referenced. It reports whether
// any referenced setting is unset or malformed, so a miss can be explained as a
// configuration gap ("set the address in settings") rather than a flat refusal.
func (s *Server) resolveExtHosts(id string, d ext.Derived) (allowed []string, unconfigured bool) {
	allowed = append(allowed, d.NetHosts...)
	for _, key := range d.NetSettings {
		// A dotted key addresses one url column of a group setting: every configured
		// row contributes a host, so an extension that talks to a user-named list of
		// devices gets exactly the egress the user configured and nothing more.
		if group, col, dotted := strings.Cut(key, "."); dotted {
			hosts, ok := s.groupHosts(id, group, col)
			if !ok {
				unconfigured = true
			}
			allowed = append(allowed, hosts...)
			continue
		}
		v, found, _ := s.st.ExtKVGet(id, key)
		v = strings.TrimSpace(v)
		if !found || v == "" {
			unconfigured = true
			continue
		}
		if u, err := url.Parse(v); err == nil && u.Host != "" {
			allowed = append(allowed, u.Host)
		} else {
			unconfigured = true
		}
	}
	return allowed, unconfigured
}

// groupHosts reads one url column out of a group setting (a JSON array of row
// objects) and returns the host[:port] of every row that has one. ok is false when
// the group is unset or any row is missing/malformed, so the caller can report the
// extension as not fully configured rather than silently narrowing its egress.
func (s *Server) groupHosts(id, group, col string) (hosts []string, ok bool) {
	raw, found, _ := s.st.ExtKVGet(id, group)
	if !found || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var rows []map[string]string
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, false
	}
	if len(rows) == 0 {
		return nil, false
	}
	ok = true
	for _, r := range rows {
		v := strings.TrimSpace(r[col])
		if v == "" {
			ok = false
			continue
		}
		u, err := url.Parse(v)
		if err != nil || u.Host == "" {
			ok = false
			continue
		}
		hosts = append(hosts, u.Host)
	}
	return hosts, ok
}

// classifyEgressError maps a Go outbound error to a plain sentence naming the host and
// what to check, returning (friendly, rawDetail). The most common self-hoster trap —
// a .local/mDNS name that does not resolve inside the container — gets a specific hint.
func classifyEgressError(err error, host string) (string, string) {
	detail := err.Error()
	low := strings.ToLower(detail)
	var dns *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(low, "timeout") || strings.Contains(low, "deadline exceeded"):
		return host + " did not respond in time. Check the address and that the server is reachable from Aperture.", detail
	case errors.As(err, &dns) || strings.Contains(low, "no such host") || strings.Contains(low, "server misbehaving"):
		return "Could not find " + host + ". A .local/mDNS name does not resolve inside the container — use a LAN IP or a real DNS name.", detail
	case strings.Contains(low, "connection refused"):
		return host + " refused the connection. Check the server is running and the port is correct.", detail
	case strings.Contains(low, "x509") || strings.Contains(low, "tls") || strings.Contains(low, "certificate"):
		return "Could not establish a secure connection to " + host + ". Check its certificate, or use http:// for a local server.", detail
	default:
		return "Could not reach " + host + ".", detail
	}
}

// expandSecrets replaces {{secret:key}} in s with the extension's stored secret
// values. It returns (expanded, 0, "") on success, or (_, code, msg) if a referenced
// key is not a declared secret or is not configured.
func (s *Server) expandSecrets(id, in string, secretKeys map[string]bool) (string, int, string) {
	var failCode int
	var failMsg string
	out := reSecretRef.ReplaceAllStringFunc(in, func(match string) string {
		key := reSecretRef.FindStringSubmatch(match)[1]
		if !secretKeys[key] {
			failCode, failMsg = http.StatusBadRequest, "{{secret:"+key+"}} is not a declared secret setting"
			return ""
		}
		v, found, err := s.st.ExtKVGet(id, key)
		if err != nil || !found || v == "" {
			failCode, failMsg = http.StatusBadRequest, "secret "+key+" is not configured yet"
			return ""
		}
		return v
	})
	if failCode != 0 {
		return "", failCode, failMsg
	}
	return out, 0, ""
}

// hostAllowed reports whether host (authority, host[:port]) matches one of the
// declared allow-list entries exactly — the same shape Deno --allow-net uses.
func hostAllowed(host string, allowed []string) bool {
	for _, a := range allowed {
		if host == a {
			return true
		}
	}
	return false
}
