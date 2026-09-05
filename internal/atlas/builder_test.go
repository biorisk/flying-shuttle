package atlas_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/doc"
)

// buildCorpus makes N chunks split across `groups` topics, each with a
// deterministic embedding pointing along one axis, and persists them.
func buildCorpus(t *testing.T, s *doc.SQLiteStore, groups, per, dim int) func() ([]atlas.CorpusChunk, error) {
	t.Helper()
	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	for g := 0; g < groups; g++ {
		for i := 0; i < per; i++ {
			id := string(rune('A'+g)) + string(rune('0'+i%10)) + string(rune('a'+i/10))
			vec := make([]float32, dim)
			vec[g%dim] = 1
			body := topics[g%len(topics)]
			chunks = append(chunks, model.Chunk{
				ID: id, SourceFile: "t.txt", Content: body,
				EmbeddingVec: ingest.Float32sToBytes(vec),
			})
			corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: body, Vec: vec})
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return func() ([]atlas.CorpusChunk, error) { return corpus, nil }
}

var topics = []string{
	"The mainsail luffed as the skipper tacked the sloop hard through the wind.",
	"She folded cold butter into the flour until the pastry dough just held together.",
	"The compiler rejected the borrow because the mutable reference outlived its scope.",
}

func TestBuilder_Build(t *testing.T) {
	s, as := newTestStore(t)
	corpus := buildCorpus(t, s, 3, 14, 6)

	b := &atlas.Builder{
		Store:      as,
		Corpus:     corpus,
		Embedder:   &ingest.StubEmbedder{Dim: 16},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 6},
	}

	build, idx, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if build.Status != atlas.StatusReady {
		t.Fatalf("status = %s", build.Status)
	}
	if len(build.Regions) < 3 {
		t.Fatalf("want >=3 regions, got %d", len(build.Regions))
	}
	if build.ChunkCount != 42 {
		t.Fatalf("chunk count = %d", build.ChunkCount)
	}

	total := 0
	for _, r := range build.Regions {
		total += len(r.Members)
		if r.Digest.Title == "" || len(r.Digest.Keywords) == 0 {
			t.Fatalf("region digest not filled: %+v", r.Digest)
		}
		if r.Digest.Source != "extractive" {
			t.Fatalf("digest source = %q", r.Digest.Source)
		}
		for _, m := range r.Members {
			if len(m.Keywords) == 0 {
				t.Fatalf("member %s missing keywords", m.ChunkID)
			}
		}
		if len(r.DigestVec) != 16 {
			t.Fatalf("digest vec not embedded: %d", len(r.DigestVec))
		}
	}
	if total != 42 {
		t.Fatalf("partition lost chunks: %d", total)
	}
	if idx.Len() != len(build.Regions) {
		t.Fatalf("region index size %d != regions %d", idx.Len(), len(build.Regions))
	}

	// Persisted as the current build.
	got, err := as.CurrentBuild()
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}
	if got.ID != build.ID || len(got.Regions) != len(build.Regions) {
		t.Fatalf("current build mismatch")
	}

	// A second build prunes the first.
	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	builds, _ := as.ListBuilds()
	if len(builds) != 1 {
		t.Fatalf("old build not pruned: %d remain", len(builds))
	}
}

func TestBuilder_Build_TranscriptDigests(t *testing.T) {
	s, as := newTestStore(t)

	// Two source files, one topic each, chunks in reverse insertion order so
	// a correct digest requires sorting by StartOffset rather than trusting
	// slice/insertion order.
	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	files := []string{"sailing.txt", "baking.txt"}
	for g, file := range files {
		for i := 6; i >= 0; i-- { // descending: out of order on purpose
			id := file[:1] + string(rune('0'+i))
			vec := make([]float32, 4)
			vec[g] = 1
			body := topics[g]
			chunks = append(chunks, model.Chunk{
				ID: id, SourceFile: file, Content: body, StartOffset: i,
				EmbeddingVec: ingest.Float32sToBytes(vec),
			})
			corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: body, Vec: vec, SourceFile: file, StartOffset: i})
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed: %v", err)
	}

	b := &atlas.Builder{
		Store:      as,
		Corpus:     func() ([]atlas.CorpusChunk, error) { return corpus, nil },
		Embedder:   &ingest.StubEmbedder{Dim: 8},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 6},
	}

	build, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(build.Transcripts) != 2 {
		t.Fatalf("want 2 transcript digests, got %d: %+v", len(build.Transcripts), build.Transcripts)
	}
	for _, td := range build.Transcripts {
		if td.ChunkCount != 7 {
			t.Fatalf("transcript %s: chunk count = %d, want 7", td.SourceFile, td.ChunkCount)
		}
		if len(td.Digest.Keywords) == 0 || td.Digest.Title == "" {
			t.Fatalf("transcript %s: digest not filled: %+v", td.SourceFile, td.Digest)
		}
		// Extractive abstract = opening sentence(s) of the FIRST chunk in
		// document order (StartOffset 0), proving the reconstruction sorted
		// rather than trusting corpus order.
		if !strings.Contains(td.Digest.Abstract, "The mainsail") && !strings.Contains(td.Digest.Abstract, "She folded") {
			t.Fatalf("transcript %s: abstract %q doesn't look like the topic's opening line", td.SourceFile, td.Digest.Abstract)
		}
	}
}

// countingCompleter records how many passages it's been asked to label.
type countingCompleter struct{ seen int }

func (c *countingCompleter) Complete(_ context.Context, _, userPrompt string) (string, error) {
	n := strings.Count(userPrompt, "\n\n")
	c.seen += n
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(itoa(i) + ". label " + itoa(i) + "\n")
	}
	return b.String(), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func TestBuilder_ChunkLabels_Incremental(t *testing.T) {
	s, as := newTestStore(t)

	mk := func(id, file string, off, axis int) (model.Chunk, atlas.CorpusChunk) {
		vec := make([]float32, 4)
		vec[axis] = 1
		body := topics[axis%len(topics)]
		return model.Chunk{ID: id, SourceFile: file, Content: body, StartOffset: off, EmbeddingVec: ingest.Float32sToBytes(vec)},
			atlas.CorpusChunk{ID: id, Content: body, Vec: vec, SourceFile: file, StartOffset: off}
	}

	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	for i := 0; i < 8; i++ {
		c, cc := mk("s"+itoa(i), "sailing.txt", i, 0)
		chunks = append(chunks, c)
		corpus = append(corpus, cc)
	}
	for i := 0; i < 8; i++ {
		c, cc := mk("b"+itoa(i), "baking.txt", i, 1)
		chunks = append(chunks, c)
		corpus = append(corpus, cc)
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmp := &countingCompleter{}
	b := &atlas.Builder{
		Store:      as,
		Corpus:     func() ([]atlas.CorpusChunk, error) { return corpus, nil },
		Embedder:   &ingest.StubEmbedder{Dim: 8},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Labeller:   &atlas.ChunkLabeller{Complete: cmp, ModelName: "stub"},
		Params:     atlas.BuildParams{MinChunks: 6},
	}

	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build 1: %v", err)
	}
	if cmp.seen != 16 {
		t.Fatalf("build 1 should label all 16 chunks, labelled %d", cmp.seen)
	}

	// Add a third transcript and rebuild.
	for i := 0; i < 5; i++ {
		c, cc := mk("r"+itoa(i), "rust.txt", i, 2)
		if err := s.CreateChunks([]model.Chunk{c}); err != nil {
			t.Fatalf("seed rust: %v", err)
		}
		corpus = append(corpus, cc)
	}
	cmp.seen = 0
	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build 2: %v", err)
	}
	if cmp.seen != 5 {
		t.Fatalf("build 2 should label only the 5 new chunks, labelled %d", cmp.seen)
	}

	// The originals kept their build-1 labels.
	got, err := as.GetChunkLabels([]string{"s0", "b0", "r0"})
	if err != nil {
		t.Fatalf("GetChunkLabels: %v", err)
	}
	if got["s0"] == "" || got["b0"] == "" || got["r0"] == "" {
		t.Fatalf("every chunk should have a label after two builds: %v", got)
	}
}

// flakyCompleter errors until .up is set, then echoes labels.
type flakyCompleter struct {
	up   bool
	seen int
}

func (c *flakyCompleter) Complete(_ context.Context, _, userPrompt string) (string, error) {
	if !c.up {
		return "", errors.New("llm down")
	}
	n := strings.Count(userPrompt, "\n\n")
	c.seen += n
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(itoa(i) + ". label " + itoa(i) + "\n")
	}
	return b.String(), nil
}

func TestBuilder_ChunkLabels_RetriesAfterLLMRecovers(t *testing.T) {
	s, as := newTestStore(t)

	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	for i := 0; i < 12; i++ {
		vec := make([]float32, 4)
		vec[i%2] = 1
		body := topics[i%2]
		id := "c" + itoa(i)
		chunks = append(chunks, model.Chunk{ID: id, SourceFile: "t.txt", Content: body, StartOffset: i, EmbeddingVec: ingest.Float32sToBytes(vec)})
		corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: body, Vec: vec, SourceFile: "t.txt", StartOffset: i})
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmp := &flakyCompleter{up: false}
	b := &atlas.Builder{
		Store:      as,
		Corpus:     func() ([]atlas.CorpusChunk, error) { return corpus, nil },
		Embedder:   &ingest.StubEmbedder{Dim: 8},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Labeller:   &atlas.ChunkLabeller{Complete: cmp, ModelName: "stub"},
		Params:     atlas.BuildParams{MinChunks: 6},
	}

	// Build 1: LLM down. The batch call errors, nothing is persisted, but the
	// build still succeeds (labels are best-effort).
	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build 1 (llm down) should still succeed: %v", err)
	}
	ids := make([]string, len(corpus))
	for i, c := range corpus {
		ids[i] = c.ID
	}
	missing, _ := as.ChunkLabelsMissing(ids)
	if len(missing) != 12 {
		t.Fatalf("all 12 chunks should still be pending after an LLM-down build, got %d", len(missing))
	}

	// Build 2: LLM back. Every chunk gets re-attempted and upgraded.
	cmp.up = true
	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build 2: %v", err)
	}
	if cmp.seen != 12 {
		t.Fatalf("build 2 should re-attempt all 12 head chunks, labelled %d", cmp.seen)
	}
	if m, _ := as.ChunkLabelsMissing(ids); len(m) != 0 {
		t.Fatalf("nothing should be pending after the LLM recovered, got %v", m)
	}
}

// digestCompleter returns a well-formed digest and counts calls.
type digestCompleter struct{ calls int }

func (c *digestCompleter) Complete(_ context.Context, _, _ string) (string, error) {
	c.calls++
	return "TITLE: a topic\nABSTRACT: some passages about a topic.\nKEYWORDS: alpha, beta, gamma", nil
}

func TestBuilder_DigestCache_ReusesUnchanged(t *testing.T) {
	s, as := newTestStore(t)

	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	for g := 0; g < 3; g++ {
		for i := 0; i < 10; i++ {
			id := string(rune('A'+g)) + itoa(i)
			vec := make([]float32, 4)
			vec[g] = 1
			body := topics[g]
			chunks = append(chunks, model.Chunk{ID: id, SourceFile: "f" + itoa(g) + ".txt", Content: body, StartOffset: i, EmbeddingVec: ingest.Float32sToBytes(vec)})
			corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: body, Vec: vec, SourceFile: "f" + itoa(g) + ".txt", StartOffset: i})
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmp := &digestCompleter{}
	b := &atlas.Builder{
		Store:      as,
		Corpus:     func() ([]atlas.CorpusChunk, error) { return corpus, nil },
		Embedder:   &ingest.StubEmbedder{Dim: 8},
		Summariser: &atlas.LLMSummariser{Complete: cmp, ModelName: "stub"},
		Params:     atlas.BuildParams{MinChunks: 6},
	}

	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build 1: %v", err)
	}
	build1Calls := cmp.calls
	if build1Calls == 0 {
		t.Fatal("build 1 made no LLM calls")
	}

	// Rebuild with the identical corpus: every region + transcript digest is
	// a cache hit, so zero new LLM calls.
	cmp.calls = 0
	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build 2: %v", err)
	}
	if cmp.calls != 0 {
		t.Fatalf("rebuild of an unchanged corpus should hit the digest cache, made %d LLM calls", cmp.calls)
	}

	// Add one transcript; only its regions/transcript digest are recomputed.
	extra := make([]model.Chunk, 10)
	for i := 0; i < 10; i++ {
		id := "Z" + itoa(i)
		vec := make([]float32, 4)
		vec[3] = 1
		extra[i] = model.Chunk{ID: id, SourceFile: "fZ.txt", Content: topics[0], StartOffset: i, EmbeddingVec: ingest.Float32sToBytes(vec)}
		corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: topics[0], Vec: vec, SourceFile: "fZ.txt", StartOffset: i})
	}
	if err := s.CreateChunks(extra); err != nil {
		t.Fatalf("seed extra: %v", err)
	}
	cmp.calls = 0
	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build 3: %v", err)
	}
	if cmp.calls == 0 {
		t.Fatal("build 3 added a transcript but recomputed nothing")
	}
	if cmp.calls >= build1Calls {
		t.Fatalf("build 3 recomputed everything (%d calls vs build-1's %d) — cache not helping", cmp.calls, build1Calls)
	}
}

func TestBuilder_DigestCache_UpgradesExtractiveWhenLLMArrives(t *testing.T) {
	s, as := newTestStore(t)

	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	for g := 0; g < 2; g++ {
		for i := 0; i < 10; i++ {
			id := string(rune('A'+g)) + itoa(i)
			vec := make([]float32, 4)
			vec[g] = 1
			chunks = append(chunks, model.Chunk{ID: id, SourceFile: "f" + itoa(g) + ".txt", Content: topics[g], StartOffset: i, EmbeddingVec: ingest.Float32sToBytes(vec)})
			corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: topics[g], Vec: vec, SourceFile: "f" + itoa(g) + ".txt", StartOffset: i})
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mk := func(summ atlas.Summariser) *atlas.Builder {
		return &atlas.Builder{
			Store: as, Corpus: func() ([]atlas.CorpusChunk, error) { return corpus, nil },
			Embedder: &ingest.StubEmbedder{Dim: 8}, Summariser: summ,
			Params: atlas.BuildParams{MinChunks: 6},
		}
	}

	// Build 1: no LLM -> extractive (provisional) digests cached.
	if _, _, err := mk(&atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)}).Build(context.Background()); err != nil {
		t.Fatalf("build 1: %v", err)
	}

	// Build 2: LLM available -> the provisional rows are recomputed.
	cmp := &digestCompleter{}
	build, _, err := mk(&atlas.LLMSummariser{Complete: cmp, ModelName: "stub"}).Build(context.Background())
	if err != nil {
		t.Fatalf("build 2: %v", err)
	}
	if cmp.calls == 0 {
		t.Fatal("an LLM build after extractive-only builds should recompute the provisional digests")
	}
	for _, r := range build.Regions {
		if !strings.HasPrefix(r.Digest.Source, "llm:") {
			t.Fatalf("region digest not upgraded to llm: %q", r.Digest.Source)
		}
	}

	// Build 3: LLM still available -> now all cache hits, no calls.
	cmp2 := &digestCompleter{}
	if _, _, err := mk(&atlas.LLMSummariser{Complete: cmp2, ModelName: "stub"}).Build(context.Background()); err != nil {
		t.Fatalf("build 3: %v", err)
	}
	if cmp2.calls != 0 {
		t.Fatalf("build 3 should be all cache hits, made %d calls", cmp2.calls)
	}
}

func TestBuilder_TooFewChunks(t *testing.T) {
	s, as := newTestStore(t)
	corpus := buildCorpus(t, s, 1, 3, 4)
	b := &atlas.Builder{
		Store: as, Corpus: corpus,
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 10},
	}
	if _, _, err := b.Build(context.Background()); err == nil {
		t.Fatal("expected ErrTooFewChunks")
	}
	builds, _ := as.ListBuilds()
	if len(builds) != 0 {
		t.Fatalf("failed precondition should not leave a build row: %d", len(builds))
	}
}

func TestService_RebuildSingleFlight(t *testing.T) {
	s, as := newTestStore(t)
	corpus := buildCorpus(t, s, 2, 12, 5)
	svc := &atlas.Service{Builder: &atlas.Builder{
		Store: as, Corpus: corpus, Embedder: &ingest.StubEmbedder{Dim: 8},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 6},
	}}

	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	st := svc.Status()
	if st.Building || st.Regions == 0 || st.LastError != "" {
		t.Fatalf("post-build status: %+v", st)
	}
	cur, idx := svc.Current()
	if cur == nil || idx == nil {
		t.Fatal("Current() nil after build")
	}

	// LoadCurrent on a fresh service recovers the same build.
	svc2 := &atlas.Service{Builder: svc.Builder}
	if err := svc2.LoadCurrent(); err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if c2, _ := svc2.Current(); c2 == nil || c2.ID != cur.ID {
		t.Fatal("LoadCurrent did not restore the build")
	}
}
