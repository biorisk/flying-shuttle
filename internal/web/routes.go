package web

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/transcript"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/go-chi/chi/v5"
)

// Deps are the services the server-rendered UI needs. Fields are added as
// tasks require them.
type Deps struct {
	Store      store.Store
	Outline    *outline.Service
	Transcript *transcript.Service
	Index      *search.HybridIndex
}

// Mount attaches the server-rendered UI (templ + Datastar) under /app, plus
// the /static asset tree. It runs alongside the legacy React app until the
// cutover (bd flying-shuttle-6fv.6.1), at which point this moves to "/".
func Mount(r chi.Router, d Deps) {
	h := &handlers{d: d}

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(StaticFS()))))

	r.Route("/app", func(r chi.Router) {
		r.Get("/", h.shell)
	})
}

type handlers struct {
	d Deps
}

// shell renders the full two-pane application page.
func (h *handlers) shell(w http.ResponseWriter, r *http.Request) {
	// Outline/evidence fragments are wired into the initial render by later
	// tasks (.3.1 / .3.4); for now the shell renders with empty regions.
	Render(w, r, components.Page(nil, nil))
}
