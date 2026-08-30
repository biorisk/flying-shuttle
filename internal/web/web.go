// Package web holds the server-rendered frontend: templ components and the
// static assets (Datastar runtime, CSS) served alongside them. It is being
// built out to replace the React app in web/; until the cutover both can be
// served by the same binary.
package web

import (
	"embed"
	"io/fs"
)

//go:generate go run github.com/a-h/templ/cmd/templ generate

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

// DatastarScriptPath is the URL path (under the static mount) of the pinned
// Datastar runtime. Bump this and the vendored file together.
const DatastarScriptPath = "/static/vendor/datastar-v1.0.3.js"
