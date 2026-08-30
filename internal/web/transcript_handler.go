package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// transcriptReader renders the #transcript-reader fragment: a continuous window
// of one source transcript centred on chunk, with prev/next scrubbing.
//
//	GET /app/evidence/transcript?chunk=<id>&node=<bullet id>
func (h *handlers) transcriptReader(w http.ResponseWriter, r *http.Request) {
	chunkID := r.URL.Query().Get("chunk")
	node := r.URL.Query().Get("node")

	win, err := h.d.Transcript.WindowAround(chunkID, 0)
	if err != nil {
		log.Printf("transcript reader: %v", err)
		http.Error(w, "transcript not found", http.StatusNotFound)
		return
	}

	vm := viewmodel.TranscriptReader{
		NodeID:     node,
		SourceFile: win.SourceFile,
		FocusChunk: win.FocusChunk,
		HasPrev:    win.HasPrev,
		HasNext:    win.HasNext,
		PrevChunk:  win.PrevChunk,
		NextChunk:  win.NextChunk,
	}
	for _, s := range win.Segments {
		vm.Segments = append(vm.Segments, viewmodel.ReaderSegment{
			ChunkID:   s.ChunkID,
			Text:      s.Text,
			Focus:     s.Focus,
			CharStart: s.CharStart,
		})
	}

	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.TranscriptReader(vm)); err != nil {
		log.Printf("transcript reader: patch: %v", err)
		return
	}
	_ = sse.MarshalAndPatchSignals(map[string]any{"readerChunk": win.FocusChunk})
}
