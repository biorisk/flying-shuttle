package outline

import (
	"errors"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/model"
)

func newSvc(t *testing.T) *Service {
	svc, _ := newSvcStore(t)
	return svc
}

// newSvcStore returns the outline service plus a corpus store over the same
// connection, for tests that need to seed chunks before attaching evidence.
func newSvcStore(t *testing.T) (*Service, corpus.Store) {
	t.Helper()
	s, err := doc.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cs := corpus.New(s.DB())
	return &Service{Store: s, Corpus: cs}, cs
}

func seedChunk(t *testing.T, cs corpus.Store, c *model.Chunk) {
	t.Helper()
	if err := cs.CreateChunk(c); err != nil {
		t.Fatal(err)
	}
}

// layout renders the forest as "id" with 2-space indent per depth, one per line.
func layout(forest []*TreeNode) []string {
	var out []string
	var walk func(ns []*TreeNode, d int)
	walk = func(ns []*TreeNode, d int) {
		for _, tn := range ns {
			pad := ""
			for i := 0; i < d; i++ {
				pad += "  "
			}
			out = append(out, pad+tn.Node.Title)
			walk(tn.Children, d+1)
		}
	}
	walk(forest, 0)
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("layout mismatch\n got: %v\nwant: %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("layout mismatch at %d\n got: %v\nwant: %v", i, got, want)
		}
	}
}

func TestService_AddSiblingAndChild(t *testing.T) {
	svc := newSvc(t)

	a, _ := svc.AddRoot("a")
	b, _ := svc.AddSibling(a.ID, "b")
	if _, err := svc.AddChild(a.ID, "a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSibling(b.ID, "b0-inserted"); err != nil { // sibling of root b -> another root
		t.Fatal(err)
	}

	forest, _ := svc.Tree()
	// a and b are roots (creation order a, b, b0-inserted); a1 is child of a.
	eq(t, layout(forest), []string{"a", "  a1", "b", "b0-inserted"})
}

func TestService_ChildOrderingOnInsert(t *testing.T) {
	svc := newSvc(t)
	root, _ := svc.AddRoot("root")
	c1, _ := svc.AddChild(root.ID, "c1")
	svc.AddChild(root.ID, "c3")
	// insert c2 right after c1
	if _, err := svc.AddSibling(c1.ID, "c2"); err != nil {
		t.Fatal(err)
	}
	forest, _ := svc.Tree()
	eq(t, layout(forest), []string{"root", "  c1", "  c2", "  c3"})
}

func TestService_IndentUnindent(t *testing.T) {
	svc := newSvc(t)
	a, _ := svc.AddRoot("a")
	b, _ := svc.AddSibling(a.ID, "b")
	c, _ := svc.AddSibling(b.ID, "c")
	_ = c

	// indent b under a
	if _, err := svc.Indent(b.ID); err != nil {
		t.Fatal(err)
	}
	forest, _ := svc.Tree()
	eq(t, layout(forest), []string{"a", "  b", "c"})

	// indent c under a (c is now index 1 among roots [a, c])
	if _, err := svc.Indent(c.ID); err != nil {
		t.Fatal(err)
	}
	forest, _ = svc.Tree()
	eq(t, layout(forest), []string{"a", "  b", "  c"})

	// unindent c back to root, after a
	if _, err := svc.Unindent(c.ID); err != nil {
		t.Fatal(err)
	}
	forest, _ = svc.Tree()
	eq(t, layout(forest), []string{"a", "  b", "c"})

	// indenting the first sibling is a no-op
	if _, err := svc.Indent(a.ID); !errors.Is(err, ErrNoop) {
		t.Fatalf("expected ErrNoop, got %v", err)
	}
	// unindenting a root is a no-op
	if _, err := svc.Unindent(a.ID); !errors.Is(err, ErrNoop) {
		t.Fatalf("expected ErrNoop, got %v", err)
	}
}

func TestService_UnindentKeepsDescendants(t *testing.T) {
	svc := newSvc(t)
	a, _ := svc.AddRoot("a")
	b, _ := svc.AddChild(a.ID, "b")
	svc.AddChild(b.ID, "b1")

	// unindent b -> becomes root sibling after a, keeping b1
	if _, err := svc.Unindent(b.ID); err != nil {
		t.Fatal(err)
	}
	forest, _ := svc.Tree()
	eq(t, layout(forest), []string{"a", "b", "  b1"})
}
