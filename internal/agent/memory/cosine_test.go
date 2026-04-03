package memory

import (
	"math"
	"testing"
)

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	got := CosineSimilarity(a, a)
	if math.Abs(got-1.0) > 0.001 {
		t.Errorf("identical vectors: got %f, want 1.0", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	got := CosineSimilarity(a, b)
	if math.Abs(got) > 0.001 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{0, 0, 0}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("zero vector: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	got := CosineSimilarity([]float32{1, 2}, []float32{1})
	if got != 0 {
		t.Errorf("different lengths: got %f, want 0.0", got)
	}
}

func TestCentroidOf(t *testing.T) {
	vecs := [][]float32{
		{2, 4, 6},
		{4, 6, 8},
	}
	c := CentroidOf(vecs)
	if len(c) != 3 {
		t.Fatalf("centroid dims = %d, want 3", len(c))
	}
	if c[0] != 3 || c[1] != 5 || c[2] != 7 {
		t.Errorf("centroid = %v, want [3 5 7]", c)
	}
}

func TestCentroidOf_Empty(t *testing.T) {
	c := CentroidOf(nil)
	if c != nil {
		t.Errorf("empty centroid should be nil, got %v", c)
	}
}

func TestNgramEmbedding_Normalized(t *testing.T) {
	v := NgramEmbedding("hello world", 64)
	if len(v) != 64 {
		t.Fatalf("dims = %d, want 64", len(v))
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 0.01 {
		t.Errorf("L2 norm = %f, want 1.0", norm)
	}
}

func TestNgramEmbedding_Deterministic(t *testing.T) {
	a := NgramEmbedding("test input", 64)
	b := NgramEmbedding("test input", 64)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("not deterministic at index %d", i)
		}
	}
}
