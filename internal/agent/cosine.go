package agent

import "math"

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns 0.0 if either vector is zero-length or all-zeros.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (normA * normB)
}

// CentroidOf computes the mean vector of a set of vectors.
// All vectors must have the same dimensionality.
func CentroidOf(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	centroid := make([]float32, dim)
	for _, v := range vecs {
		for i := range v {
			centroid[i] += v[i]
		}
	}
	n := float32(len(vecs))
	for i := range centroid {
		centroid[i] /= n
	}
	return centroid
}

// NgramEmbedding creates a simple character n-gram embedding as a fallback
// when no external embedding provider is available. Uses 3-grams hashed
// into a fixed-size vector. Not as good as neural embeddings but sufficient
// for intent classification with well-separated exemplar banks.
func NgramEmbedding(text string, dims int) []float32 {
	if dims < 1 {
		dims = 128
	}
	vec := make([]float32, dims)
	runes := []rune(text)
	for i := 0; i+2 < len(runes); i++ {
		h := uint32(runes[i])*31*31 + uint32(runes[i+1])*31 + uint32(runes[i+2])
		idx := h % uint32(dims)
		vec[idx] += 1.0
	}
	// L2 normalize.
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}
	return vec
}
