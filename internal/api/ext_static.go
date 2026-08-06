package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// extCSP restricts an extension UI document to a self-contained panel: inline
// script/style only (its own bundle), data/blob images, NO network of its own (it
// talks to the host via postMessage, never fetch), and it can only be framed by
// the app. Combined with the host rendering it in an iframe with
// sandbox="allow-scripts" (no allow-same-origin → opaque origin, no parent access,
// no cookies), a malicious panel is structurally unable to reach the session or
// the API except through the broker.
const extCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data: blob:; font-src data:; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

// handleExtAsset serves an enabled extension's UI files from disk (the ui/ subtree
// only). It is unauthenticated: the sandboxed iframe sends no cookie, so the assets
// (installed by an admin, not secret) are public while the extension is enabled;
// all DATA access is gated by the authed broker. Mounted at /ext/{id}/* below the
// API routes and before the SPA catch-all.
func (s *Server) handleExtAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ex, err := s.st.GetExtension(id)
	if err != nil || !ex.Enabled {
		http.NotFound(w, r)
		return
	}
	// Only the ui/ subtree is web-served (never backend code, the manifest, the
	// import map, or the token env). path.Clean collapses any traversal so a
	// ../-laden request cannot escape ui/.
	clean := path.Clean("/" + chi.URLParam(r, "*"))
	if !strings.HasPrefix(clean, "/ui/") {
		http.NotFound(w, r)
		return
	}
	base := filepath.Clean(s.extDir(id))
	full := filepath.Join(base, filepath.FromSlash(clean))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	if fi, err := os.Stat(full); err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", extCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, full)
}
