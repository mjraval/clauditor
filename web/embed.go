// Package web embeds the clauditor WebUI (no CDN dependencies; everything
// ships inside the binary via go:embed).
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var content embed.FS

// FS returns the UI filesystem rooted at the static dir.
func FS() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err) // embed layout is fixed at build time
	}
	return sub
}
