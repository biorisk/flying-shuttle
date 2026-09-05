package atlas

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// CorpusChunk is one embedded transcript chunk fed into a build.
type CorpusChunk struct {
	ID      string
	Content string
	Vec     []float32

	// SourceFile and StartOffset place this chunk within its transcript.
	// They're only used to reconstruct each file's full text, in order, for
	// its TranscriptDigest — clustering and linking never look at them.
	SourceFile  string
	StartOffset int
}

// ErrTooFewChunks means the corpus is too small to build a useful Atlas.
var ErrTooFewChunks = errors.New("atlas: too few embedded chunks to build")

// BuildParams bundles the knobs for one build.
type BuildParams struct {
	Cluster           ClusterParams
	Link              LinkParams
	MinChunks         int // refuse to build below this; default 2*MaxRegionSize
	KeywordsPerRegion int // default 6
	KeywordsPerChunk  int // default 4
}

// Builder assembles a Source Atlas build from the current embedded chunk
// corpus. It depends only on interfaces so it is testable with stubs.
type Builder struct {
	Store      Store
	Corpus     func() ([]CorpusChunk, error) // every chunk WITH an embedding
	Embedder   ingest.Embedder               // digest vectors; nil/not-ready tolerated
	Summariser Summariser                    // extractive or LLM
	Labeller   *ChunkLabeller                // per-chunk drill-down labels; nil skips Phase E
	Params     BuildParams
}

// Build runs Phases A–C and persists one new build, pruning the previous one.
// It returns the finished build (with regions, members, links) and a
// RegionIndex over whatever digest vectors were produced. A partial build on
// error is marked failed and deleted. ctx cancellation is checked between
// regions during the (potentially slow) digest phase.
func (b *Builder) Build(ctx context.Context) (*Build, *RegionIndex, error) {
	p := b.Params
	minChunks := p.MinChunks
	if minChunks <= 0 {
		// Enough to form a few regions; below this the Atlas is just a list.
		minChunks = 3 * ClusterParams{}.withDefaults().MinRegionSize
	}

	chunks, err := b.Corpus()
	if err != nil {
		return nil, nil, fmt.Errorf("atlas: load corpus: %w", err)
	}
	if len(chunks) < minChunks {
		return nil, nil, fmt.Errorf("%w (have %d, need %d)", ErrTooFewChunks, len(chunks), minChunks)
	}

	build := &Build{ChunkCount: len(chunks), Params: paramsJSON(p)}
	if err := b.Store.CreateBuild(build); err != nil {
		return nil, nil, fmt.Errorf("atlas: create build: %w", err)
	}

	result, ridx, err := b.assemble(ctx, build.ID, chunks, p)
	if err != nil {
		_ = b.Store.SetBuildStatus(build.ID, StatusFailed, len(chunks), err.Error())
		_ = b.Store.DeleteBuild(build.ID)
		return nil, nil, err
	}

	if err := b.Store.SetBuildStatus(build.ID, StatusReady, len(chunks), ""); err != nil {
		return nil, nil, fmt.Errorf("atlas: finalise build: %w", err)
	}
	if err := b.Store.PruneExcept(build.ID); err != nil {
		return nil, nil, fmt.Errorf("atlas: prune old builds: %w", err)
	}
	result.Status = StatusReady
	return result, ridx, nil
}

func (b *Builder) assemble(ctx context.Context, buildID string, chunks []CorpusChunk, p BuildParams) (*Build, *RegionIndex, error) {
	ids := make([]string, len(chunks))
	vecs := make([][]float32, len(chunks))
	textByID := make(map[string]string, len(chunks))
	for i, c := range chunks {
		ids[i] = c.ID
		vecs[i] = c.Vec
		textByID[c.ID] = c.Content
	}

	// Phase A — partition.
	regions := ClusterChunks(ids, vecs, p.Cluster)
	if len(regions) == 0 {
		return nil, nil, errors.New("atlas: clustering produced no regions")
	}

	kw := NewKeyworder(collectTexts(chunks))
	extractive := &ExtractiveSummariser{KW: kw}
	summ := b.Summariser
	switch s := summ.(type) {
	case nil:
		summ = extractive // ships without an LLM
	case *LLMSummariser:
		if s.Fallback == nil {
			s.Fallback = extractive // corpus-wide IDF for degraded fields
		}
	}

	// Phase B — digest + embed each region, reusing the content-addressed
	// cache (atlas_digest) so an unchanged cluster isn't re-summarised and a
	// crash loses at most one in-flight LLM call. See atlas_persistence_plan.md.
	if err := b.digestRegions(ctx, regions, textByID, summ); err != nil {
		return nil, nil, err
	}

	// Phase C — keywords, links.
	kw.TagRegions(regions, textByID, p.KeywordsPerRegion, p.KeywordsPerChunk)
	links := BuildLinks(regions, p.Link)

	if err := b.Store.InsertRegions(buildID, regions); err != nil {
		return nil, nil, fmt.Errorf("atlas: persist regions: %w", err)
	}
	if err := b.Store.InsertLinks(buildID, links); err != nil {
		return nil, nil, fmt.Errorf("atlas: persist links: %w", err)
	}

	// Phase D — one digest per transcript, from that file's own full text (in
	// document order), not a region-based sample. Same cache as Phase B.
	transcripts, err := buildTranscriptDigests(ctx, b.Store, chunks, summ)
	if err != nil {
		return nil, nil, fmt.Errorf("atlas: digest transcripts: %w", err)
	}
	if err := b.Store.InsertTranscriptDigests(buildID, transcripts); err != nil {
		return nil, nil, fmt.Errorf("atlas: persist transcript digests: %w", err)
	}

	// Phase E — per-chunk drill-down labels. Persisted per chunk (not per
	// build): a rebuild only sends chunks that have no "llm:<model>" label
	// yet (new chunks, plus any left with a "head" fallback from a build when
	// the LLM was down). Best-effort — a missing or flaky LLM logs and moves
	// on, never fails the build; only shutdown (ctx cancel) aborts.
	if err := b.labelNewChunks(ctx, ids, textByID); err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		log.Printf("atlas: chunk labelling incomplete: %v", err)
	}

	full, err := b.Store.GetBuild(buildID)
	if err != nil {
		return nil, nil, fmt.Errorf("atlas: reload build: %w", err)
	}
	return full, LoadRegionIndex(full), nil
}

// labelNewChunks fills in atlas_chunk_label for any corpus chunk that has no
// label yet. A nil Labeller (no LLM) is a no-op — the drill-down view then
// falls back to a text head. Labels persist per batch, so an interrupted
// build resumes where it left off.
func (b *Builder) labelNewChunks(ctx context.Context, ids []string, textByID map[string]string) error {
	if b.Labeller == nil {
		return nil
	}
	missing, err := b.Store.ChunkLabelsMissing(ids)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	in := make([]LabelInput, 0, len(missing))
	for _, id := range missing {
		in = append(in, LabelInput{ChunkID: id, Text: textByID[id]})
	}
	return b.Labeller.Label(ctx, in, b.Store.PutChunkLabels)
}

// digestRegions fills Digest + DigestVec for every region, consulting the
// content-addressed cache (atlas_digest) keyed by the region's member set:
// an unchanged cluster is a cache hit (no LLM call), a freshly summarised one
// is persisted to the cache immediately (crash loses at most one call), and a
// provisional "extractive" cached digest is upgraded once an LLM is available.
func (b *Builder) digestRegions(ctx context.Context, regions []Region, textByID map[string]string, summ Summariser) error {
	hashes := make([]string, len(regions))
	for i := range regions {
		hashes[i] = regionDigestHash(&regions[i])
	}
	cache, err := newDigestCache(ctx, b.Store, hashes)
	if err != nil {
		return fmt.Errorf("atlas: load digest cache: %w", err)
	}

	for i := range regions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c, ok := cache.get(hashes[i]); ok && c.reusable(summ) {
			regions[i].Digest, regions[i].DigestVec = c.Digest, c.Vec
			continue
		}
		texts := make([]string, 0, len(regions[i].Members))
		for _, m := range regions[i].Members { // already nearest-centroid first
			texts = append(texts, textByID[m.ChunkID])
		}
		d, err := summ.Summarise(ctx, SummariseInput{Texts: texts})
		if err != nil {
			return fmt.Errorf("atlas: digest region %d: %w", i, err)
		}
		regions[i].Digest, regions[i].DigestVec = d, nil
		if err := cache.put(CachedDigest{InputHash: hashes[i], Kind: "region", Digest: d, Source: d.Source}); err != nil {
			return fmt.Errorf("atlas: persist region digest: %w", err)
		}
	}

	// Embed any digest still missing a vector (freshly computed, or a cache
	// hit whose vector was never stored) in one batch, and persist each back
	// to its cache row.
	if b.Embedder == nil {
		return nil
	}
	var need []int
	for i := range regions {
		if len(regions[i].DigestVec) == 0 && digestText(regions[i].Digest) != "" {
			need = append(need, i)
		}
	}
	if len(need) == 0 {
		return nil
	}
	sub := make([]Region, len(need))
	for k, i := range need {
		sub[k] = regions[i]
	}
	if err := EmbedDigests(ctx, b.Embedder, sub); err != nil {
		if errors.Is(err, ingest.ErrEmbedderNotReady) {
			return nil // Atlas stays usable; digest search just unavailable
		}
		return fmt.Errorf("atlas: embed digests: %w", err)
	}
	for k, i := range need {
		regions[i].DigestVec = sub[k].DigestVec
		if len(sub[k].DigestVec) > 0 {
			if err := b.Store.SetDigestVec(hashes[i], sub[k].DigestVec); err != nil {
				return fmt.Errorf("atlas: persist digest vec: %w", err)
			}
		}
	}
	return nil
}

// buildTranscriptDigests digests every source file represented in chunks,
// using that file's own chunks — reconstructed in document order by
// StartOffset — as the summariser input. Consults the same content-addressed
// cache as digestRegions, keyed by (source_file, member chunk-id set): a
// transcript whose chunks are unchanged is a cache hit. Chunks with no
// SourceFile are skipped; each fresh digest is persisted before the next.
func buildTranscriptDigests(ctx context.Context, s Store, chunks []CorpusChunk, summ Summariser) ([]TranscriptDigest, error) {
	byFile := map[string][]CorpusChunk{}
	for _, c := range chunks {
		if c.SourceFile == "" {
			continue
		}
		byFile[c.SourceFile] = append(byFile[c.SourceFile], c)
	}

	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	hashByFile := make(map[string]string, len(files))
	allHashes := make([]string, 0, len(files))
	for _, f := range files {
		group := byFile[f]
		ids := make([]string, len(group))
		for i, c := range group {
			ids[i] = c.ID
		}
		h := transcriptDigestHash(f, ids)
		hashByFile[f] = h
		allHashes = append(allHashes, h)
	}
	cache, err := newDigestCache(ctx, s, allHashes)
	if err != nil {
		return nil, fmt.Errorf("load digest cache: %w", err)
	}

	out := make([]TranscriptDigest, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		group := byFile[f]
		sort.Slice(group, func(a, b int) bool { return group[a].StartOffset < group[b].StartOffset })

		if c, ok := cache.get(hashByFile[f]); ok && c.reusable(summ) {
			out = append(out, TranscriptDigest{SourceFile: f, ChunkCount: len(group), Digest: c.Digest})
			continue
		}
		texts := make([]string, len(group))
		for i, c := range group {
			texts[i] = c.Content
		}
		d, err := summ.Summarise(ctx, SummariseInput{Texts: texts})
		if err != nil {
			return nil, fmt.Errorf("transcript %q: %w", f, err)
		}
		if err := cache.put(CachedDigest{InputHash: hashByFile[f], Kind: "transcript", Digest: d, Source: d.Source}); err != nil {
			return nil, fmt.Errorf("persist transcript digest %q: %w", f, err)
		}
		out = append(out, TranscriptDigest{SourceFile: f, ChunkCount: len(group), Digest: d})
	}
	return out, nil
}

func collectTexts(chunks []CorpusChunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Content
	}
	return out
}

func paramsJSON(p BuildParams) string {
	c := p.Cluster.withDefaults()
	l := p.Link.withDefaults()
	return fmt.Sprintf(
		`{"maxRegionSize":%d,"minRegionSize":%d,"maxRegions":%d,"seed":%d,"linkK":%d,"linkMinWeight":%g}`,
		c.MaxRegionSize, c.MinRegionSize, c.MaxRegions, c.Seed, l.K, l.MinWeight,
	)
}
