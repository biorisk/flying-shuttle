# Sub-chunk evaluation: surfacing the relevant span within a chunk

Grounding this in what you have: `EvidenceFinder.Find` (`internal/web/evidence.go`) takes top-N chunk IDs from `HybridIndex.Search`, loads each chunk, and shows `trimRunes(c.Content, 320)` — literally `content[:320]`. Chunks are ~160-word transcript greedy chunks, so you're showing roughly the first third, anchored at offset 0. Everything downstream (the transcript reader, `ReaderSegment.CharStart`, the excerpt form) is already offset-aware, so the missing piece is *locating* the relevant span and *threading offsets through*.

## Implementation steps

**1. search — Lexical locator (ship first).** Deterministic, no new infra: tokenize the chunk *with positions* using the existing `tokenize`, expose `BM25Index.IDF(term)`, then slide a window over the chunk and pick the span that maximizes summed-IDF of query-term hits (this is exactly what Elasticsearch's `unified` highlighter does). Center the snippet there instead of at 0, and return the individual hit offsets for bolding.

**2. ui — Match-centered snippet + `<mark>` on hits.** The single highest-value change: `content[bestStart-pad : bestEnd+pad]` with leading/trailing "…", query terms bolded in `evidence.templ`. Add `HighlightStart/End` (or a `[]span`) to `viewmodel.Candidate`.

**3. ui — Highlight stability.** This fires on every keystroke — relocate the span only when the result set changes, not on every query growth, so the highlight doesn't jitter.

**4. ui — Expand-in-place.** Card shows the focus span; a "show full chunk" toggle reveals the whole chunk with the span still highlighted and scrolled into view.

**5. ui — Carry the highlight into the transcript reader.** You already pass `CharStart` per `ReaderSegment` and have a `focus` KV. Extend it so opening the reader highlights the located sentences and auto-scrolls to them, not just the chunk boundary.

**6. ui — Prefill the excerpt form with the located span.** `#excerpt-form` already has hidden `char_start/char_end/text`. Populate them with the located span on reader open, so "Add as evidence" attaches the relevant sentence(s) by default instead of forcing a manual selection.

**7. search — Better query.** The bullet is prose being written, not a question — a noisy query. Strip to salient noun phrases, drop stopwords hard, optionally fold in the parent bullet (you already do this in `ContextChecker`). Tighter query → tighter localization.

**8. search — Passage/window re-scoring (retrieve → locate).** Keep chunk-level retrieval as-is. For the ~12 candidates you actually show, run a cheap second pass that splits the chunk into sentences (you already produce these in `ingest.ParseTranscript` — persist the boundary offsets) and scores each window against the query. Return `(charStart, charEnd)` of the best window, expanded ±1 sentence for context. Bounded cost because N is tiny.

**9. ui — Per-sentence relevance shading.** In the expanded card or the reader, shade each sentence light→dark by its window score. Turns "find the snippet" into a glance. Free once you have sentence scores.

**10. ui — Multi-span snippets.** If hits cluster in 2–3 places, show "… span A … span B …" rather than one contiguous window.

**11. search — Thread match provenance through `Result`.** Track each result's BM25 vs vector RRF contribution so the UI can label *why* something matched.

**12. ui — Match cues.** Small score bar/dot per card; a "keyword" vs "semantic" badge (needs step 11) so the writer trusts semantic hits that have no visible highlight.

**13. search — Semantic locator for vector-only matches.** When the hit came from the vector arm and no query term appears, lexical highlighting shows nothing. Options: embed the candidate chunks' sentences on the fly (small batch to the Python sidecar, only for shown candidates) and cosine against the query vector; or precompute per-sentence vectors at ingest via the backfiller. Highlight sentences above a similarity threshold.

**14. ui — Keyboard span cycling.** `n`/`N` to jump between highlighted spans across candidates, matching the outline editor's keyboard-first feel.

**15. search — Parent-document / dual-granularity indexing.** Root-cause fix: index small retrieval units (40–60 words, 1-sentence overlap) that point into larger "reading" chunks. "Chunk start" ≈ "relevant part" by construction, ranking gets more granular, and the transcript reader stitches context back. More index entries is the cost.
