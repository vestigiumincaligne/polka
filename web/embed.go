// Package web embeds the built frontend (web/dist) into the binary.
// Run `npm run build` in this directory (or `make web`) before `go build`.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built SPA rooted at the dist directory.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
