package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// Render writes a templ component as a normal HTML response (full page or a
// one-off fragment fetched without SSE).
func Render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = c.Render(r.Context(), w)
}

// Patch opens a Datastar SSE response and morphs each component into the DOM
// (outer-merge by element id — the default). Use it from any handler that a
// Datastar action (@get/@post/...) calls. Returns the generator so callers can
// also patch signals or send several fragments.
func Patch(w http.ResponseWriter, r *http.Request, components ...templ.Component) (*datastar.ServerSentEventGenerator, error) {
	sse := datastar.NewSSE(w, r)
	for _, c := range components {
		if err := sse.PatchElementTempl(c); err != nil {
			return sse, err
		}
	}
	return sse, nil
}

// PatchInto is like Patch but targets a specific selector with an explicit
// merge mode (e.g. append the next page of results into a list).
func PatchInto(w http.ResponseWriter, r *http.Request, selector string, mode datastar.ElementPatchMode, c templ.Component) error {
	sse := datastar.NewSSE(w, r)
	return sse.PatchElementTempl(c, datastar.WithSelector(selector), datastar.WithMode(mode))
}

// RenderString renders a component to a string (handy for tests and logging).
func RenderString(c templ.Component) string {
	var b strings.Builder
	_ = c.Render(context.Background(), &b)
	return b.String()
}
