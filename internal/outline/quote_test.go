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
	svc := newSvc(t)
	root, err := svc.AddRoot("point")
	if err != nil {
		t.Fatal(err)
	}
	full := "ALPHA the quick brown fox jumps OMEGA"
	if err := svc.Store.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "f.txt", Content: full}); err != nil {
		t.Fatal(err)
	}
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
