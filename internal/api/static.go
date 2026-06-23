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
// routing works on deep links.
func (s *Server) spaHandler() http.HandlerFunc {
	fsys := s.webFS()
	fileServer := http.FileServerFS(fsys)
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			http.ServeFileFS(w, r, fsys, "index.html")
			return
		}
		if f, err := fsys.Open(p); err == nil {
			info, statErr := f.Stat()
			f.Close()
			if statErr == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// Unknown path: hand it to the SPA router.
		http.ServeFileFS(w, r, fsys, "index.html")
	}
}
