// Package web provides the embedded SPA assets.
// In production, web/dist/ contains the built SvelteKit output.
// During development, the Go server proxies to the Vite dev server.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded SPA filesystem rooted at "dist".
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
