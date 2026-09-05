package indexer

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/search"
)

// Backfiller embeds chunks that have no vector yet. It runs on an interval and
// on demand (Trigger), so a chunk uploaded while the embedder is up is
// vectorised within seconds, and one uploaded while it's down is picked up as
// soon as the embedder becomes ready.
type Backfiller struct {
	store    corpus.Store
	embedder ingest.Embedder
	idx      *search.HybridIndex

	batch    int
	interval time.Duration
	trigger  chan struct{}
}

// NewBackfiller wires a Backfiller. batch <= 0 defaults to 16; interval <= 0
// defaults to 30s.
func NewBackfiller(s corpus.Store, e ingest.Embedder, idx *search.HybridIndex, batch int, interval time.Duration) *Backfiller {
	if batch <= 0 {
		batch = 16
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Backfiller{
		store:    s,
		embedder: e,
		idx:      idx,
		batch:    batch,
		interval: interval,
		trigger:  make(chan struct{}, 1),
	}
}

// Trigger asks the backfiller to run a pass as soon as possible. Non-blocking.
func (b *Backfiller) Trigger() {
	select {
	case b.trigger <- struct{}{}:
	default:
	}
}

// Run drains the backlog whenever triggered or the interval elapses, until ctx
// is cancelled.
func (b *Backfiller) Run(ctx context.Context) {
	if b.embedder == nil {
		return
	}
	t := time.NewTicker(b.interval)
	defer t.Stop()

	// Attempt once at startup.
	b.drain(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.drain(ctx)
		case <-b.trigger:
			b.drain(ctx)
		}
	}
}

// drain runs batches until the backlog is empty, the embedder is unavailable,
// or ctx is cancelled.
func (b *Backfiller) drain(ctx context.Context) {
	total := 0
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := b.pass(ctx)
		if err != nil {
			if errors.Is(err, ingest.ErrEmbedderNotReady) {
				if total > 0 {
					log.Printf("indexer: backfill paused (embedder not ready) after %d chunk(s)", total)
				}
				return
			}
			log.Printf("indexer: backfill error: %v", err)
			return
		}
		if n == 0 {
			if total > 0 {
				remaining, _ := b.store.CountChunksMissingEmbedding()
				log.Printf("indexer: backfilled %d chunk(s); %d remaining", total, remaining)
			}
			return
		}
		total += n
	}
}

// pass embeds one batch of vector-less chunks and returns how many it did.
func (b *Backfiller) pass(ctx context.Context) (int, error) {
	chunks, err := b.store.ListChunksMissingEmbedding(b.batch)
	if err != nil {
		return 0, err
	}
	if len(chunks) == 0 {
		return 0, nil
	}

	texts := make([]string, len(chunks))
	for i := range chunks {
		texts[i] = chunks[i].Content
	}

	vecs, err := b.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, err
	}
	if len(vecs) != len(chunks) {
		return 0, errors.New("backfill: embedder returned wrong vector count")
	}

	for i := range chunks {
		vec := vecs[i]
		if len(vec) == 0 {
			continue
		}
		raw := ingest.Float32sToBytes(vec)
		if err := b.store.SetChunkEmbedding(chunks[i].ID, raw); err != nil {
			return 0, err
		}
		b.idx.SetChunkVector(chunks[i].ID, vec)
	}
	return len(chunks), nil
}
