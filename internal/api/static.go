package api

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/TheOriUHD/aperture/internal/webassets"
)

// webFS returns the filesystem the SPA is served from: a dev directory on disk
// when APERTURE_WEB_DIR is set, otherwise the embedded production bundle.
func (s *Server) webFS() fs.FS {
	if s.cfg.WebDir != "" {
		return os.DirFS(s.cfg.WebDir)
	}
	f, err := webassets.FS()
	if err != nil {
		s.log.Error("embedded web assets unavailable", "err", err)
		return os.DirFS(".")
	}
	return f
}

// spaHandler serves static assets, falling back to index.html so client-side
// routing works on deep links. Content-hashed assets are cached immutably; the
// HTML shell is never cached, so a redeploy always reaches the browser (the
// embedded files share a zero modtime, which would otherwise yield stale 304s).
func (s *Server) spaHandler() http.HandlerFunc {
	fsys := s.webFS()
	fileServer := http.FileServerFS(fsys)
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if f, err := fsys.Open(p); err == nil {
				info, statErr := f.Stat()
				f.Close()
				if statErr == nil && !info.IsDir() {
					if strings.HasPrefix(p, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}
		// index.html and unknown SPA routes: always serve fresh.
		b, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		_, _ = w.Write(b)
	}
}
