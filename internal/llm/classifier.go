package llm

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// TrustLevel indicates the quality of the embedding used for classification.
type TrustLevel int

const (
	// TrustNGram means a local n-gram fallback was used (lower confidence).
	TrustNGram TrustLevel = iota
	// TrustNeural means a real embedding provider was used (higher confidence).
	TrustNeural
)

// AbstainPolicy controls when the classifier should refuse to classify.
type AbstainPolicy struct {
	MinScore float64 // minimum top score to accept (default 0.3)
	MinGap   float64 // minimum gap between top two scores (default 0.1)
}

// ClassifierExample is a labeled example for intent classification.
type ClassifierExample struct {
	Text   string
	Intent string
	Vector []float32
}

// ClassificationResult holds a single classification outcome.
type ClassificationResult struct {
	Intent string
	Score  float64
	Trust  TrustLevel
}

// ExemplarSet groups examples by category with an optional per-set threshold.
type ExemplarSet struct {
	Name      string
	Examples  []ClassifierExample
	Threshold float64 // per-set override; 0 means use global
}

// SemanticClassifier classifies text using embedding similarity.
// Supports centroid-based classification (one vector per category) for
// efficiency, and abstain policy for low-confidence results.
type SemanticClassifier struct {
	embedder *EmbeddingClient
	corpus   []ClassifierExample
	abstain  AbstainPolicy

	// queryEmbedFn lets callers (typically tests) override the
	// per-query embedding strategy without supplying a full
	// EmbeddingClient. When nil, the standard path runs: embedder if
	// present, n-gram fallback otherwise.
	queryEmbedFn func(string) []float32

	mu        sync.RWMutex
	centroids map[string][]float32 // category → centroid vector
}

// NewSemanticClassifier creates a classifier. If embedder is nil, falls back
// to n-gram embeddings for classification.
func NewSemanticClassifier(embedder *EmbeddingClient, corpus []ClassifierExample) *SemanticClassifier {
	return &SemanticClassifier{
		embedder:  embedder,
		corpus:    corpus,
		abstain:   AbstainPolicy{MinScore: 0.3, MinGap: 0.1},
		centroids: make(map[string][]float32),
	}
}

// WithAbstainPolicy sets the abstain policy.
func (sc *SemanticClassifier) WithAbstainPolicy(p AbstainPolicy) *SemanticClassifier {
	sc.abstain = p
	return sc
}

// Classify returns the most likely intent and confidence for the given text.
// Returns ("abstain", 0, nil) if the abstain policy rejects the result.
func (sc *SemanticClassifier) Classify(ctx context.Context, text string) (string, float64, error) {
	if len(sc.corpus) == 0 {
		return "unknown", 0.0, nil
	}

	vec, trust, err := sc.embedText(ctx, text)
	if err != nil {
		return "unknown", 0.0, err
	}

	results := sc.classifyAgainstCentroids(vec, trust)
	if len(results) == 0 {
		return "unknown", 0.0, nil
	}

	top := results[0]

	// Apply abstain policy.
	if top.Score < sc.abstain.MinScore {
		return "abstain", top.Score, nil
	}
	if len(results) > 1 && (top.Score-results[1].Score) < sc.abstain.MinGap {
		return "abstain", top.Score, nil
	}

	return top.Intent, top.Score, nil
}

// ClassifyAll returns all classification results sorted by score descending.
func (sc *SemanticClassifier) ClassifyAll(ctx context.Context, text string) ([]ClassificationResult, error) {
	if len(sc.corpus) == 0 {
		return nil, nil
	}

	vec, trust, err := sc.embedText(ctx, text)
	if err != nil {
		return nil, err
	}

	return sc.classifyAgainstCentroids(vec, trust), nil
}

// ClassifyVector classifies a pre-embedded query vector against the corpus
// centroids without re-embedding. The trust level is treated as TrustNeural
// because the caller already chose the embedding source. Useful for tests
// that need deterministic behaviour without a real embedder, and for
// production paths that batch-embed many sentences and then classify each.
func (sc *SemanticClassifier) ClassifyVector(vec []float32) (string, float64, error) {
	if len(sc.corpus) == 0 || len(vec) == 0 {
		return "unknown", 0.0, nil
	}
	results := sc.classifyAgainstCentroids(vec, TrustNeural)
	if len(results) == 0 {
		return "unknown", 0.0, nil
	}
	top := results[0]
	if top.Score < sc.abstain.MinScore {
		return "abstain", top.Score, nil
	}
	if len(results) > 1 && (top.Score-results[1].Score) < sc.abstain.MinGap {
		return "abstain", top.Score, nil
	}
	return top.Intent, top.Score, nil
}

// SetCorpusVectors replaces the corpus with explicit pre-embedded examples.
// Centroids are reset so the next Classify recomputes them from the new
// corpus. Useful for tests that need deterministic, in-process classifier
// behaviour without depending on a remote embedder.
func (sc *SemanticClassifier) SetCorpusVectors(examples []ClassifierExample) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.corpus = append([]ClassifierExample(nil), examples...)
	sc.centroids = make(map[string][]float32)
}

// embedText returns the embedding vector and trust level for the given text.
func (sc *SemanticClassifier) embedText(ctx context.Context, text string) ([]float32, TrustLevel, error) {
	if sc.queryEmbedFn != nil {
		return sc.queryEmbedFn(text), TrustNeural, nil
	}
	if sc.embedder != nil {
		vec, err := sc.embedder.EmbedSingle(ctx, text)
		if err == nil {
			return vec, TrustNeural, nil
		}
		// Fall through to n-gram on error.
	}
	return ngramEmbed(text, 128), TrustNGram, nil
}

// SetQueryEmbedFn installs a deterministic per-query embedding function.
// Primarily useful for tests that need predictable centroid distances; in
// production the field stays nil and the standard embedder / n-gram path
// is used.
func (sc *SemanticClassifier) SetQueryEmbedFn(fn func(string) []float32) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.queryEmbedFn = fn
}

// PrepareCorpus pre-embeds every corpus example whose Vector is nil. It uses
// the same embedText path as Classify, so corpus and query vectors share an
// embedding space (neural when an embedder is configured, n-gram otherwise).
// Callers should invoke this once at construction time so the first
// Classify call doesn't pay the corpus-embedding cost on the hot path.
//
// Returns the count of examples whose vectors were populated. An error is
// returned only when the embedder fails for every example AND the n-gram
// fallback was not reached for some structural reason — in normal use this
// always succeeds because n-gram embedding cannot fail.
func (sc *SemanticClassifier) PrepareCorpus(ctx context.Context) (int, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	populated := 0
	for i := range sc.corpus {
		if len(sc.corpus[i].Vector) > 0 {
			continue
		}
		vec, _, err := sc.embedText(ctx, sc.corpus[i].Text)
		if err != nil {
			return populated, err
		}
		sc.corpus[i].Vector = vec
		populated++
	}
	// Reset any cached centroids so they're recomputed from the freshly-
	// embedded corpus the next time Classify runs.
	sc.centroids = make(map[string][]float32)
	return populated, nil
}

// classifyAgainstCentroids computes similarity against category centroids.
func (sc *SemanticClassifier) classifyAgainstCentroids(vec []float32, trust TrustLevel) []ClassificationResult {
	sc.ensureCentroids()

	sc.mu.RLock()
	defer sc.mu.RUnlock()

	var results []ClassificationResult
	for cat, centroid := range sc.centroids {
		sim := CosineSimilarity(vec, centroid)
		results = append(results, ClassificationResult{
			Intent: cat,
			Score:  sim,
			Trust:  trust,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// ensureCentroids lazily computes centroids from corpus examples.
func (sc *SemanticClassifier) ensureCentroids() {
	sc.mu.RLock()
	if len(sc.centroids) > 0 {
		sc.mu.RUnlock()
		return
	}
	sc.mu.RUnlock()

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Double-check after acquiring write lock.
	if len(sc.centroids) > 0 {
		return
	}

	groups := make(map[string][][]float32)
	for _, ex := range sc.corpus {
		groups[ex.Intent] = append(groups[ex.Intent], ex.Vector)
	}

	for cat, vecs := range groups {
		sc.centroids[cat] = centroidOf(vecs)
	}
}

// centroidOf computes the mean vector of a set of vectors.
func centroidOf(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dims := len(vecs[0])
	result := make([]float32, dims)
	for _, v := range vecs {
		for i := range result {
			if i < len(v) {
				result[i] += v[i]
			}
		}
	}
	n := float32(len(vecs))
	for i := range result {
		result[i] /= n
	}
	return result
}

// ngramEmbed produces a simple character n-gram hash embedding for fallback.
func ngramEmbed(text string, dims int) []float32 {
	vec := make([]float32, dims)
	text = strings.ToLower(text)
	for i := 0; i+3 <= len(text); i++ {
		h := uint32(0)
		for j := 0; j < 3; j++ {
			h = h*31 + uint32(text[i+j])
		}
		vec[h%uint32(dims)] += 1.0
	}
	// Normalize.
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		norm = float32(1.0 / float64(norm))
		for i := range vec {
			vec[i] *= norm
		}
	}
	return vec
}
