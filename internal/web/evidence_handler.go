package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// evidence renders the #evidence fragment for the current bullet text.
//
//	GET /evidence?q=<bullet text>&node=<bullet id>&mode=<hybrid|keyword|semantic>
//
// It always responds as a Datastar SSE patch (the caller is the debounced
// bullet input wired in .3.4, or the mode toggle re-issuing the last query).
// A blank q yields the idle placeholder. mode defaults to hybrid.
func (h *handlers) evidence(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	node := r.URL.Query().Get("node")
	mode := search.Mode(r.URL.Query().Get("mode"))

	cands, err := h.evidenceFinder().Find(r.Context(), q, 0, mode)
	if err != nil {
		// HybridIndex.Search already degrades to BM25 on embedder trouble, so
		// an error here is unexpected — surface an empty pane rather than 500.
		log.Printf("evidence: find %q: %v", q, err)
	}

	cands = h.evStab.stabilize(node, cands)

	vm := viewmodel.EvidencePane{
		Query:               q,
		NodeID:              node,
		Candidates:          cands,
		Mode:                string(mode),
		SemanticUnavailable: mode == search.ModeSemantic && h.evidenceFinder().Index != nil && h.evidenceFinder().Index.Embedder == nil,
	}
	if _, err := Patch(w, r, components.Evidence(vm)); err != nil {
		log.Printf("evidence: patch: %v", err)
	}
}
