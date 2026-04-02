package llm

import (
	"context"
)

// ClassifierExample is a labeled example for intent classification.
type ClassifierExample struct {
	Text   string
	Intent string
	Vector []float32
}

// SemanticClassifier classifies text using embedding similarity.
type SemanticClassifier struct {
	embedder *EmbeddingClient
	corpus   []ClassifierExample
}

// NewSemanticClassifier creates a classifier. If embedder is nil, falls back to keyword matching.
func NewSemanticClassifier(embedder *EmbeddingClient, corpus []ClassifierExample) *SemanticClassifier {
	return &SemanticClassifier{embedder: embedder, corpus: corpus}
}

// Classify returns the most likely intent and confidence for the given text.
func (sc *SemanticClassifier) Classify(ctx context.Context, text string) (string, float64, error) {
	if sc.embedder == nil || len(sc.corpus) == 0 {
		return "unknown", 0.0, nil
	}

	vec, err := sc.embedder.EmbedSingle(ctx, text)
	if err != nil {
		return "unknown", 0.0, err
	}

	// Find highest cosine similarity.
	var bestIntent string
	var bestSim float64
	for _, ex := range sc.corpus {
		sim := CosineSimilarity(vec, ex.Vector)
		if sim > bestSim {
			bestSim = sim
			bestIntent = ex.Intent
		}
	}

	return bestIntent, bestSim, nil
}
