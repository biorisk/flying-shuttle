package web

import (
	"sort"
	"strings"
	"sync"

	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// evidenceStability keeps the located snippet for each bullet steady while the
// writer keeps typing. The evidence pane refetches on every keystroke; as long
// as a query growth returns the same set of chunks, re-running the locator
// against the longer query would twitch the highlight around. So we freeze the
// per-chunk window and hit segments until the result set itself changes.
type evidenceStability struct {
	mu     sync.Mutex
	byNode map[string]stableEvidenceEntry
}

type stableEvidenceEntry struct {
	sig   string // sorted chunk IDs of the result set
	cands []viewmodel.Candidate
}

func newEvidenceStability() evidenceStability {
	return evidenceStability{byNode: make(map[string]stableEvidenceEntry)}
}

// stabilize returns fresh with each candidate's snippet/window/segments carried
// over from the previous render for the same bullet, when the two renders cover
// the same set of chunks. Ranking and scores always come from fresh.
func (s *evidenceStability) stabilize(node string, fresh []viewmodel.Candidate) []viewmodel.Candidate {
	if node == "" || len(fresh) == 0 {
		return fresh
	}
	sig := candidateSig(fresh)

	s.mu.Lock()
	defer s.mu.Unlock()

	if prev, ok := s.byNode[node]; ok && prev.sig == sig {
		prevByChunk := make(map[string]viewmodel.Candidate, len(prev.cands))
		for _, c := range prev.cands {
			prevByChunk[c.ChunkID] = c
		}
		out := make([]viewmodel.Candidate, len(fresh))
		for i, c := range fresh {
			if old, ok := prevByChunk[c.ChunkID]; ok {
				c.Snippet = old.Snippet
				c.Segments = old.Segments
				c.FocusStart = old.FocusStart
				c.FocusEnd = old.FocusEnd
			}
			out[i] = c
		}
		s.byNode[node] = stableEvidenceEntry{sig: sig, cands: out}
		return out
	}

	s.byNode[node] = stableEvidenceEntry{sig: sig, cands: fresh}
	return fresh
}

func candidateSig(cands []viewmodel.Candidate) string {
	ids := make([]string, len(cands))
	for i, c := range cands {
		ids[i] = c.ChunkID
	}
	sort.Strings(ids)
	return strings.Join(ids, "|")
}
