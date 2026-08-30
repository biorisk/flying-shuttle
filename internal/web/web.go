// Package web holds the server-rendered frontend: templ components and the
// static assets (Datastar runtime, CSS) served alongside them. It is being
// built out to replace the React app in web/; until the cutover both can be
// served by the same binary.
package web

import (
	"embed"
	"io/fs"

	"github.com/biorisk/flying-shuttle/internal/web/components"
)

//go:generate go tool templ generate

// staticFS embeds everything under static/ (the vendored Datastar runtime and
// stylesheet). Served at /static/.
//
//go:embed static
var staticFS embed.FS

// StaticFS returns the embedded static asset tree rooted so that
// "vendor/datastar-v1.0.3.js" resolves.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed path is a compile-time constant; can't fail
	}
	return sub
}

// DatastarScriptPath re-exports the pinned Datastar runtime URL from the
// components package (defined there to keep components dependency-free).
const DatastarScriptPath = components.DatastarScriptPath
