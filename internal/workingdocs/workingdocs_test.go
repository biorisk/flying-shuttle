package workingdocs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/storetest"
)

func newStore(t *testing.T) doc.Store { return storetest.New(t).Doc }

func TestRenderOutline(t *testing.T) {
	st := storetest.New(t)
	s := st.Doc
	svc := &outline.Service{Store: s, Corpus: st.Corpus}
	root, _ := svc.AddRoot("Chapter one")
	pt, _ := svc.AddChild(root.ID, "A point")
	svc.SetLocked(pt.ID, true)
	c := &model.Chunk{ID: "c1", SourceFile: "iv.txt", Content: "the exact words chosen"}
	if err := st.Corpus.CreateChunk(c); err != nil {
		t.Fatal(err)
	}
	svc.AttachEvidence(root.ID, "c1", 0, 0, "")

	data, _ := s.ExportState()
	md := RenderOutline("my-book", data, nil)

	for _, want := range []string{
		"# my-book",
		"- Chapter one",
		"  - A point  `[locked]`",
		"> the exact words chosen — _iv.txt_",
		"1 with evidence",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("outline.md missing %q:\n%s", want, md)
		}
	}
}

func TestFlusher_writesAndRecovers(t *testing.T) {
	dir := t.TempDir()
	omd := filepath.Join(dir, "outline.md")
	sj := filepath.Join(dir, "state.json")

	s := newStore(t)
	svc := &outline.Service{Store: s}
	a, _ := svc.AddRoot("Kept bullet")
	svc.AddSibling(a.ID, "Second bullet")

	f := &Flusher{Store: s, Project: "p", OutlineMD: omd, StateJSON: sj, Interval: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()

	time.Sleep(80 * time.Millisecond)
	svc.AddRoot("Third bullet")
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	md, _ := os.ReadFile(omd)
	for _, w := range []string{"Kept bullet", "Second bullet", "Third bullet"} {
		if !strings.Contains(string(md), w) {
			t.Fatalf("outline.md missing %q after flush:\n%s", w, md)
		}
	}

	// Recover into a fresh doc.
	st, err := LoadState(sj)
	if err != nil {
		t.Fatal(err)
	}
	s2 := newStore(t)
	if err := s2.ImportState(st.Data); err != nil {
		t.Fatal(err)
	}
	nodes, _ := s2.ListNodes()
	if len(nodes) != 3 {
		t.Fatalf("recovered %d nodes, want 3", len(nodes))
	}
}
