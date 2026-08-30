package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// stitchView renders the Preview fragment: the outline (or a thread) linearized
// and stitched.
//
//	GET /app/stitch?thread=<id>&glue=<0-100>
func (h *handlers) stitchView(w http.ResponseWriter, r *http.Request) {
	threadID := r.URL.Query().Get("thread")
	glue := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("glue")); err == nil {
		glue = v
	}

	mode := dag.ModeManuscript
	if threadID != "" {
		mode = dag.ModeThread
	}

	vm := viewmodel.StitchView{ThreadID: threadID, Glue: glue}
	res, err := dag.LinearizeAndStitch(r.Context(), h.d.Store, h.d.Stitcher, dag.LinearizeRequest{
		Mode: mode, ThreadID: threadID, GlueLevel: glue,
	})
	if err != nil {
		vm.Err = err.Error()
	} else {
		vm.NodeCount = len(res.Nodes)
		if res.Stitch != nil {
			vm.TotalChars = res.Stitch.Stats.TotalChars
			vm.GlueRatioPct = int(res.Stitch.Stats.GlueRatio*100 + 0.5)
			for _, sp := range res.Stitch.Spans {
				vm.Spans = append(vm.Spans, viewmodel.StitchSpan{
					Glue: sp.Type == stitch.SpanGlue,
					Text: sp.Text,
				})
			}
		}
	}

	if _, err := Patch(w, r, components.Stitch(vm)); err != nil {
		log.Printf("stitch: patch: %v", err)
	}
}
