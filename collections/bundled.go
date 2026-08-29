// Package bundled holds the book collections shipped with Polka
// (the JSON files in this directory are embedded into the binary and loaded
// at server start; see README.md alongside).
package bundled

import (
	"embed"
	"io/fs"
)

//go:embed *.json
var files embed.FS

// FS holds the collection files (*.json) at the root.
func FS() fs.FS { return files }
