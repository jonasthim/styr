// Package web embeds the built frontend (web/dist) and serves it as an SPA.
// Run `make web` first; the tracked dist/.gitkeep keeps the embed pattern
// valid in a fresh checkout (the SPA then 404s until a real build exists).
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded frontend with SPA index-fallback behavior.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return spaHandler(http.FS(sub))
}
