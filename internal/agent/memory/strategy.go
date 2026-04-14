package memory

import "time"

// RetrievalMode represents how memory retrieval is performed.
type RetrievalMode int

const (
	RetrievalHybrid   RetrievalMode = iota // FTS5 + cosine (default)
	RetrievalSemantic                      // Cosine-only
	RetrievalKeyword                       // FTS5-only
	RetrievalANN                           // approximate nearest neighbor
	RetrievalRecency                       // Time-sorted only
)

func (m RetrievalMode) String() string {
	switch m {
	case RetrievalHybrid:
		return "hybrid"
	case RetrievalSemantic:
		return "semantic"
	case RetrievalKeyword:
		return "keyword"
	case RetrievalANN:
		return "ann"
	case RetrievalRecency:
		return "recency"
	default:
		return "unknown"
	}
}

// RetrievalStrategy selects the optimal retrieval mode based on context.
type RetrievalStrategy struct {
	EmbeddingAvailable bool
	CorpusSize         int
	ANNThreshold       int // default 1000
}

// NewRetrievalStrategy creates a strategy with the given context.
func NewRetrievalStrategy(embeddingAvailable bool, corpusSize int) *RetrievalStrategy {
	return &RetrievalStrategy{
		EmbeddingAvailable: embeddingAvailable,
		CorpusSize:         corpusSize,
		ANNThreshold:       1000,
	}
}

// AdaptiveHybridWeight returns a hybrid weight tuned to corpus size.
// At small scale, dense vectors are reliable so we weight them heavily.
// As corpus grows past ~10K entries, embeddings crowd in high-dimensional
// space (semantic collapse) and lexical matching becomes more discriminative.
func AdaptiveHybridWeight(corpusSize int) float64 {
	switch {
	case corpusSize < 1000:
		return 0.7
	case corpusSize < 5000:
		return 0.5
	case corpusSize < 10000:
		return 0.4
	default:
		return 0.3
	}
}

// SelectMode chooses the optimal retrieval mode for a query.
//
// Decision logic:
//   - No embeddings -> Keyword (FTS5 only)
//   - Session < 5 min -> Recency (recent context is most relevant)
//   - Corpus >= ANNThreshold -> ANN (approximate nearest neighbor)
//   - Default -> Hybrid (FTS5 + cosine blend)
func (rs *RetrievalStrategy) SelectMode(_ string, sessionAge time.Duration) RetrievalMode {
	if !rs.EmbeddingAvailable {
		return RetrievalKeyword
	}

	if sessionAge <= 5*time.Minute {
		return RetrievalRecency
	}

	if rs.CorpusSize >= rs.ANNThreshold {
		return RetrievalANN
	}

	return RetrievalHybrid
}
