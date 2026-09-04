package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// evidence renders the #evidence fragment for the current bullet text.
//
//	GET /evidence?q=<bullet text>&node=<bullet id>&mode=<hybrid|keyword|semantic>&page=<n>&page_size=<n>
//
// It always responds as a Datastar SSE patch (the caller is the debounced
// bullet input wired in .3.4, the mode toggle, or the pager re-issuing the
// last query). A blank q yields the idle placeholder. mode defaults to
// hybrid; page defaults to 1; page_size defaults to (and is clamped onto)
// EvidenceFinder.PageSizeOptions.
func (h *handlers) evidence(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	node := r.URL.Query().Get("node")
	mode := search.Mode(r.URL.Query().Get("mode"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

	res, err := h.evidenceFinder().FindPage(r.Context(), q, mode, page, pageSize)
	if err != nil {
		// HybridIndex.Search already degrades to BM25 on embedder trouble, so
		// an error here is unexpected — surface an empty pane rather than 500.
		log.Printf("evidence: find %q: %v", q, err)
	}

	res.Candidates = h.evStab.stabilize(node, res.Candidates)

	totalPages := (res.Total + res.PageSize - 1) / res.PageSize
	if totalPages < 1 {
		totalPages = 1
	}
	vm := viewmodel.EvidencePane{
		Query:               q,
		NodeID:              node,
		Candidates:          res.Candidates,
		Mode:                string(mode),
		SemanticUnavailable: mode == search.ModeSemantic && h.evidenceFinder().Index != nil && h.evidenceFinder().Index.Embedder == nil,
		Page:                res.Page,
		PageSize:            res.PageSize,
		TotalMatches:        res.Total,
		TotalPages:          totalPages,
		PageSizeOptions:     PageSizeOptions,
	}
	if _, err := Patch(w, r, components.Evidence(vm)); err != nil {
		log.Printf("evidence: patch: %v", err)
	}
}
