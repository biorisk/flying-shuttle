package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// transcriptReader renders the #transcript-reader fragment: a continuous window
// of one source transcript centred on chunk, with prev/next scrubbing.
//
//	GET /evidence/transcript?chunk=<id>&node=<bullet id>
func (h *handlers) transcriptReader(w http.ResponseWriter, r *http.Request) {
	chunkID := r.URL.Query().Get("chunk")
	node := r.URL.Query().Get("node")
	// fs/fe: rune offsets of the located span within the focus chunk, passed
	// from the evidence card so the reader can highlight and scroll to it.
	fs, _ := strconv.Atoi(r.URL.Query().Get("fs"))
	fe, _ := strconv.Atoi(r.URL.Query().Get("fe"))

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
		seg := viewmodel.ReaderSegment{
			ChunkID:   s.ChunkID,
			Text:      s.Text,
			Focus:     s.Focus,
			CharStart: s.CharStart,
		}
		if s.Focus && fe > fs {
			seg.FocusStart, seg.FocusEnd = fs, fe
		}
		vm.Segments = append(vm.Segments, seg)
	}

	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.TranscriptReader(vm)); err != nil {
		log.Printf("transcript reader: patch: %v", err)
		return
	}
	_ = sse.MarshalAndPatchSignals(map[string]any{"readerChunk": win.FocusChunk})
}
