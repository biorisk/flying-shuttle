// Package transcript provides continuous, scrubbable reads of a source
// transcript. Chunk boundaries exist only to bound embeddings — they carry no
// meaning — so the reader stitches adjacent chunks of the same source file
// into one flowing passage and lets the caller page earlier / later across
// those boundaries seamlessly.
package transcript

import (
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
)

// DefaultRadius is how many chunks on each side of the focus a window includes.
const DefaultRadius = 2

// Segment is one chunk's worth of transcript text within a window.
type Segment struct {
	ChunkID    string `json:"chunk_id"`
	SourceFile string `json:"source_file"`
	CharStart  int    `json:"char_start"` // rune offset of this segment within the whole source
	CharEnd    int    `json:"char_end"`
	Text       string `json:"text"`
	Speaker    string `json:"speaker,omitempty"`
	Focus      bool   `json:"focus"` // true for the chunk the window is centered on
}

// Window is a continuous slice of a transcript centered on one chunk.
type Window struct {
	SourceFile string    `json:"source_file"`
	FocusChunk string    `json:"focus_chunk"`
	Segments   []Segment `json:"segments"`
	// Text is every segment concatenated — the continuous reading view.
	Text    string `json:"text"`
	HasPrev bool   `json:"has_prev"` // earlier chunks exist before the first segment
	HasNext bool   `json:"has_next"` // later chunks exist after the last segment
	// PrevChunk / NextChunk are the focus chunk to request to scroll the
	// window one step earlier / later ("" when at an edge).
	PrevChunk string `json:"prev_chunk,omitempty"`
	NextChunk string `json:"next_chunk,omitempty"`
}

// Service reads transcript windows from the store.
type Service struct {
	Store store.Store
}

// WindowAround returns a window centered on chunkID with `radius` chunks of
// context on each side. radius <= 0 uses DefaultRadius.
func (s *Service) WindowAround(chunkID string, radius int) (*Window, error) {
	focus, err := s.Store.GetChunk(chunkID)
	if err != nil {
		return nil, err
	}
	return s.window(focus.SourceFile, chunkID, radius)
}

// WindowFrom returns a window centered on whichever chunk of sourceFile
// contains (or is nearest after) charOffset.
func (s *Service) WindowFrom(sourceFile string, charOffset, radius int) (*Window, error) {
	chunks, err := s.Store.ListChunksBySourceFile(sourceFile)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return &Window{SourceFile: sourceFile}, nil
	}
	center := chunks[0].ID
	for _, c := range chunks {
		if c.StartOffset <= charOffset && charOffset < c.EndOffset {
			center = c.ID
			break
		}
		if c.StartOffset > charOffset {
			center = c.ID
			break
		}
		center = c.ID
	}
	return s.window(sourceFile, center, radius)
}

func (s *Service) window(sourceFile, focusID string, radius int) (*Window, error) {
	if radius <= 0 {
		radius = DefaultRadius
	}
	chunks, err := s.Store.ListChunksBySourceFile(sourceFile)
	if err != nil {
		return nil, err
	}

	fi := -1
	for i, c := range chunks {
		if c.ID == focusID {
			fi = i
			break
		}
	}
	if fi < 0 {
		return nil, store.ErrNotFound
	}

	lo := fi - radius
	if lo < 0 {
		lo = 0
	}
	hi := fi + radius + 1
	if hi > len(chunks) {
		hi = len(chunks)
	}

	w := &Window{
		SourceFile: sourceFile,
		FocusChunk: focusID,
		HasPrev:    lo > 0,
		HasNext:    hi < len(chunks),
	}
	if w.HasPrev {
		w.PrevChunk = chunks[lo-1].ID
	}
	if w.HasNext {
		w.NextChunk = chunks[hi].ID
	}

	for i := lo; i < hi; i++ {
		c := chunks[i]
		seg := Segment{
			ChunkID:    c.ID,
			SourceFile: c.SourceFile,
			CharStart:  c.StartOffset,
			CharEnd:    c.EndOffset,
			Text:       c.Content,
			Focus:      c.ID == focusID,
		}
		if c.Speaker != nil {
			seg.Speaker = *c.Speaker
		}
		w.Segments = append(w.Segments, seg)
		if w.Text != "" {
			w.Text += " "
		}
		w.Text += c.Content
	}
	return w, nil
}

// SourceFiles returns the distinct transcript names present in the store,
// with a chunk count for each.
func SourceFiles(chunks []model.Chunk) []FileSummary {
	order := []string{}
	count := map[string]int{}
	for _, c := range chunks {
		if _, seen := count[c.SourceFile]; !seen {
			order = append(order, c.SourceFile)
		}
		count[c.SourceFile]++
	}
	out := make([]FileSummary, 0, len(order))
	for _, f := range order {
		out = append(out, FileSummary{SourceFile: f, ChunkCount: count[f]})
	}
	return out
}

// FileSummary is a transcript name plus how many chunks it produced.
type FileSummary struct {
	SourceFile string `json:"source_file"`
	ChunkCount int    `json:"chunk_count"`
}
