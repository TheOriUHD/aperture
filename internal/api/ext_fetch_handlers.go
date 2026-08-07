package api

import (
	"context"
	"io"
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
	if verr != nil || len(d.NetHosts) == 0 {
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
	if !hostAllowed(u.Host, d.NetHosts) {
		writeErrorCode(w, http.StatusForbidden, "forbidden",
			"host "+u.Host+" was not granted to this extension (allowed: "+strings.Join(d.NetHosts, ", ")+")")
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
		writeErrorCode(w, http.StatusBadGateway, "upstream_error", "outbound request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, extFetchMaxRespBody))
	writeJSON(w, http.StatusOK, map[string]any{
		"status": resp.StatusCode,
		"body":   string(rb),
	})
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
