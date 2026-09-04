package atlas

import (
	"context"
	"errors"
	"fmt"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// CorpusChunk is one embedded transcript chunk fed into a build.
type CorpusChunk struct {
	ID      string
	Content string
	Vec     []float32
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
		minChunks = 2 * ClusterParams{}.withDefaults().MaxRegionSize
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
	summ := b.Summariser
	if summ == nil {
		summ = &ExtractiveSummariser{KW: kw} // ships without an LLM
	}

	// Phase B — digest each region independently.
	for i := range regions {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		texts := make([]string, 0, len(regions[i].Members))
		for _, m := range regions[i].Members { // already nearest-centroid first
			texts = append(texts, textByID[m.ChunkID])
		}
		d, err := summ.Summarise(ctx, SummariseInput{Texts: texts})
		if err != nil {
			return nil, nil, fmt.Errorf("atlas: digest region %d: %w", i, err)
		}
		regions[i].Digest = d
	}

	// Phase C — keywords, links, digest embeddings.
	kw.TagRegions(regions, textByID, p.KeywordsPerRegion, p.KeywordsPerChunk)
	links := BuildLinks(regions, p.Link)

	if err := EmbedDigests(ctx, b.Embedder, regions); err != nil && !errors.Is(err, ingest.ErrEmbedderNotReady) {
		return nil, nil, fmt.Errorf("atlas: embed digests: %w", err)
	}

	if err := b.Store.InsertRegions(buildID, regions); err != nil {
		return nil, nil, fmt.Errorf("atlas: persist regions: %w", err)
	}
	if err := b.Store.InsertLinks(buildID, links); err != nil {
		return nil, nil, fmt.Errorf("atlas: persist links: %w", err)
	}

	full, err := b.Store.GetBuild(buildID)
	if err != nil {
		return nil, nil, fmt.Errorf("atlas: reload build: %w", err)
	}
	return full, LoadRegionIndex(full), nil
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
