// Package webassets embeds the built React single-page app so the daemon can
// serve the UI from a single binary. During `make build` / Docker build, the
// real Vite output is copied into ./dist, replacing the placeholder.
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded web bundle rooted at the dist directory.
func FS() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}
