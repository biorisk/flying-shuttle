package outline

import (
	"errors"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
)

// attachQuote sets up a root bullet with one evidence child citing the middle
// of a chunk, and returns the evidence node id plus the service.
func attachQuote(t *testing.T) (*Service, string) {
	t.Helper()
	svc, cs := newSvcStore(t)
	root, err := svc.AddRoot("point")
	if err != nil {
		t.Fatal(err)
	}
	full := "ALPHA the quick brown fox jumps OMEGA"
	seedChunk(t, cs, &model.Chunk{ID: "c1", SourceFile: "f.txt", Content: full})
	start := len([]rune("ALPHA "))
	end := len([]rune("ALPHA the quick brown fox jumps"))
	ev, err := svc.AttachEvidence(root.ID, "c1", start, end, "the quick brown fox jumps")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Locked {
		t.Fatalf("attached quote should not be locked: %+v", ev)
	}
	return svc, ev.ID
}

func quoteText(t *testing.T, svc *Service, nodeID string) (string, model.Evidence) {
	t.Helper()
	evs, err := svc.Store.ListNodeEvidence(nodeID)
	if err != nil || len(evs) != 1 {
		t.Fatalf("ListNodeEvidence: %v / %d", err, len(evs))
	}
	n, err := svc.Store.GetNode(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != evs[0].Text {
		t.Errorf("node Body %q out of sync with evidence Text %q", n.Body, evs[0].Text)
	}
	return evs[0].Text, evs[0]
}

func TestLocateSelection(t *testing.T) {
	content := "The auditor said the numbers   were\nwrong from the start."
	cr := []rune(content)
	cases := []struct {
		sel  string
		want string // expected content[s:e] (rune slice), "" = not found
	}{
		{"the numbers   were", "the numbers   were"},                                  // exact
		{"  the numbers   were  ", "the numbers   were"},                              // outer whitespace trimmed
		{"numbers were wrong from the start", "numbers   were\nwrong from the start"}, // normalized: newline + doubled spaces
		{"not present anywhere", ""},
	}
	for _, c := range cases {
		s, e, ok := locateSelection(content, c.sel)
		if c.want == "" {
			if ok {
				t.Errorf("locateSelection(%q) unexpectedly matched", c.sel)
			}
			continue
		}
		if !ok || string(cr[s:e]) != c.want {
			t.Errorf("locateSelection(%q) = %d,%d,%v slice=%q want %q",
				c.sel, s, e, ok, func() string {
					if ok {
						return string(cr[s:e])
					}
					return ""
				}(), c.want)
		}
	}
}

func TestAttachEvidence_realignsFromText(t *testing.T) {
	svc, cs := newSvcStore(t)
	root, _ := svc.AddRoot("point")
	full := "PREFIX the chosen words END"
	seedChunk(t, cs, &model.Chunk{ID: "c1", SourceFile: "f.txt", Content: full})
	// Client sent trimmed text but stale/too-long offsets (the classic bug).
	ev, err := svc.AttachEvidence(root.ID, "c1", 0, 999, "the chosen words")
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := svc.Store.ListNodeEvidence(ev.ID)
	if len(evs) != 1 {
		t.Fatalf("evidence rows: %d", len(evs))
	}
	got := evs[0]
	if got.Text != "the chosen words" || got.CharStart != 7 || got.CharEnd != 23 {
		t.Fatalf("attach did not realign: %+v", got)
	}
	if string([]rune(full)[got.CharStart:got.CharEnd]) != got.Text {
		t.Fatalf("stored offsets and text disagree")
	}
}

func TestEditQuote_trimToSelection(t *testing.T) {
	svc, id := attachQuote(t)
	// current text: "the quick brown fox jumps"; keep "quick brown fox"
	if _, err := svc.EditQuote(id, QuoteTrim, 4, 19); err != nil {
		t.Fatal(err)
	}
	got, ev := quoteText(t, svc, id)
	if got != "quick brown fox" {
		t.Fatalf("trim got %q", got)
	}
	// offsets track the chunk: "ALPHA the " = 10 runes
	if ev.CharStart != 10 || ev.CharEnd != 25 {
		t.Errorf("trim offsets: got [%d,%d) want [10,25)", ev.CharStart, ev.CharEnd)
	}
}

func TestEditQuote_spliceInteriorAndSuffix(t *testing.T) {
	svc, id := attachQuote(t)
	// remove interior "quick " -> "the brown fox jumps"
	if _, err := svc.EditQuote(id, QuoteSplice, 4, 10); err != nil {
		t.Fatal(err)
	}
	got, _ := quoteText(t, svc, id)
	if got != "the brown fox jumps" {
		t.Fatalf("splice interior got %q", got)
	}
	// remove suffix " jumps"
	if _, err := svc.EditQuote(id, QuoteSplice, 13, 19); err != nil {
		t.Fatal(err)
	}
	got, _ = quoteText(t, svc, id)
	if got != "the brown fox" {
		t.Fatalf("splice suffix got %q", got)
	}
}

func TestEditQuote_noopGuards(t *testing.T) {
	svc, id := attachQuote(t)
	if _, err := svc.EditQuote(id, QuoteTrim, 5, 5); !errors.Is(err, ErrNoop) {
		t.Errorf("empty range: want ErrNoop, got %v", err)
	}
	if _, err := svc.EditQuote(id, QuoteSplice, 0, 999); !errors.Is(err, ErrNoop) {
		t.Errorf("splice whole quote: want ErrNoop (would be empty), got %v", err)
	}
	if _, err := svc.EditQuote(id, "bogus", 0, 3); !errors.Is(err, ErrNoop) {
		t.Errorf("bad op: want ErrNoop, got %v", err)
	}
}
