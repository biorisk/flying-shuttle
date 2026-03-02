package api

import (
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/search"
)

const maxSearchLimit = 100

type searchHandler struct {
	index *search.HybridIndex
}

func (h *searchHandler) query(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing q parameter")
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "hybrid"
	}

	var results []search.Result

	switch mode {
	case "bm25":
		results = h.index.BM25.Search(q, limit)

	case "hybrid":
		var err error
		results, err = h.index.Search(r.Context(), q, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

	case "vector":
		// Vector-only mode requires a live embedder to embed the query string.
		// The current architecture uses offline Qwen3 embeddings (pre-computed);
		// no live embedder is wired at query time.
		writeError(w, http.StatusNotImplemented,
			"vector mode requires a live embedder; use mode=hybrid or mode=bm25")
		return

	default:
		writeError(w, http.StatusBadRequest, "mode must be hybrid, bm25, or vector")
		return
	}

	writeJSON(w, http.StatusOK, results)
}
