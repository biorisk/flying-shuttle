package atlas

import (
	"regexp"
	"sort"
	"strings"
)

// Keyworder extracts salient terms from text with TF-IDF scored against a
// fixed corpus. It is self-contained (no dependency on the BM25 index) and
// deterministic — the labels it produces feed the network view.
type Keyworder struct {
	df    map[string]int // document frequency across the corpus
	nDocs int
}

var wordRE = regexp.MustCompile(`[\p{L}\p{N}]+`)

// NewKeyworder builds a Keyworder from the full corpus of chunk texts. Pass
// every chunk that went into the build so IDF reflects the whole transcript
// set.
func NewKeyworder(corpus []string) *Keyworder {
	k := &Keyworder{df: make(map[string]int), nDocs: len(corpus)}
	for _, doc := range corpus {
		for term := range termSet(doc) {
			k.df[term]++
		}
	}
	return k
}

// Top returns the n highest TF-IDF terms in text (descending), tie-broken
// alphabetically for determinism.
func (k *Keyworder) Top(text string, n int) []string {
	return k.topScored(termCounts(text), n)
}

// TopFromDocs aggregates term frequencies across several texts (a region's
// member chunks) and returns the n highest TF-IDF terms.
func (k *Keyworder) TopFromDocs(texts []string, n int) []string {
	agg := make(map[string]int)
	for _, t := range texts {
		for term, c := range termCounts(t) {
			agg[term] += c
		}
	}
	return k.topScored(agg, n)
}

func (k *Keyworder) topScored(tf map[string]int, n int) []string {
	type scored struct {
		term  string
		score float64
	}
	var xs []scored
	for term, c := range tf {
		// idf = log-ish: corpus with df==nDocs contributes ~0; rare terms high.
		df := k.df[term]
		if df == 0 {
			df = 1
		}
		idf := float64(k.nDocs+1) / float64(df+1)
		xs = append(xs, scored{term, float64(c) * idf})
	}
	sort.Slice(xs, func(i, j int) bool {
		if xs[i].score != xs[j].score {
			return xs[i].score > xs[j].score
		}
		return xs[i].term < xs[j].term
	})
	if n > len(xs) {
		n = len(xs)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = xs[i].term
	}
	return out
}

// TagRegions fills in extractive keyword labels across a build: each region's
// Digest.Keywords (when still empty — an LLM digest may already have set them)
// and every member chunk's Keywords. texts maps chunk id -> chunk content.
// perRegion / perChunk cap the tag counts (defaults 6 / 4).
func (k *Keyworder) TagRegions(regions []Region, texts map[string]string, perRegion, perChunk int) {
	perRegion = orTagDefault(perRegion, 6)
	perChunk = orTagDefault(perChunk, 4)
	for ri := range regions {
		r := &regions[ri]
		memTexts := make([]string, 0, len(r.Members))
		for mi := range r.Members {
			m := &r.Members[mi]
			body := texts[m.ChunkID]
			memTexts = append(memTexts, body)
			m.Keywords = k.Top(body, perChunk)
		}
		if len(r.Digest.Keywords) == 0 {
			r.Digest.Keywords = k.TopFromDocs(memTexts, perRegion)
		}
	}
}

func orTagDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// termCounts tokenises text into scoreable terms with their frequency.
func termCounts(text string) map[string]int {
	out := make(map[string]int)
	for _, tok := range wordRE.FindAllString(strings.ToLower(text), -1) {
		if !scoreable(tok) {
			continue
		}
		out[tok]++
	}
	return out
}

func termSet(text string) map[string]struct{} {
	out := make(map[string]struct{})
	for tok := range termCounts(text) {
		out[tok] = struct{}{}
	}
	return out
}

func scoreable(tok string) bool {
	if len(tok) < 3 || len(tok) > 30 {
		return false
	}
	if stopwords[tok] {
		return false
	}
	// Drop pure numbers.
	allDigits := true
	for _, r := range tok {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	return !allDigits
}

var stopwords = func() map[string]bool {
	list := strings.Fields(`
		the and that have for not with you this but his from they she her been
		than its who what when where which will would there their them then
		these those your are was were has had did does doing done being
		about above after again against all any because before below between
		both down during each few more most other some such only own same
		too very can just should now also into out off over under here how why
		our ours out yourself yourselves himself herself itself themselves
		would could might must shall may
		yeah okay like really kind sort thing things going know think said say
		says gonna wanna got get getting one two three lot bit way`)
	m := make(map[string]bool, len(list))
	for _, w := range list {
		m[w] = true
	}
	return m
}()
