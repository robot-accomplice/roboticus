package llm

import (
	"context"
	"math"
	"testing"
)

func TestNgramHash_Determinism(t *testing.T) {
	v1 := ngramHash("hello world", 128)
	v2 := ngramHash("hello world", 128)
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("ngramHash not deterministic at index %d: %f != %f", i, v1[i], v2[i])
		}
	}
}

func TestNgramHash_DifferentInputsDiffer(t *testing.T) {
	v1 := ngramHash("hello", 128)
	v2 := ngramHash("world", 128)
	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different inputs produced identical vectors")
	}
}

func TestNgramHash_L2Normalized(t *testing.T) {
	v := ngramHash("the quick brown fox jumps over the lazy dog", 128)
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 0.001 {
		t.Errorf("L2 norm = %f, want ~1.0", norm)
	}
}

func TestNgramHash_EmptyString(t *testing.T) {
	v := ngramHash("", 128)
	if len(v) != 128 {
		t.Fatalf("len = %d, want 128", len(v))
	}
	// Empty string has no trigrams — all zeros.
	for i, x := range v {
		if x != 0 {
			t.Errorf("empty string: v[%d] = %f, want 0", i, x)
			break
		}
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3, 4}
	sim := CosineSimilarity(a, a)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("identical vectors: sim = %f, want 1.0", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0, 0}
	b := []float32{0, 1, 0, 0}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim) > 0.001 {
		t.Errorf("orthogonal vectors: sim = %f, want 0.0", sim)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 1, 1}
	b := []float32{-1, -1, -1}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim+1.0) > 0.001 {
		t.Errorf("opposite vectors: sim = %f, want -1.0", sim)
	}
}

func TestCosineSimilarity_MismatchedLengths(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	sim := CosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("mismatched lengths: sim = %f, want 0", sim)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	sim := CosineSimilarity(nil, nil)
	if sim != 0 {
		t.Errorf("nil vectors: sim = %f, want 0", sim)
	}
}

func TestEmbeddingClient_FallbackNgram(t *testing.T) {
	ec := NewEmbeddingClient(nil) // no provider
	results, err := ec.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(results))
	}
	if len(results[0]) != ngramDim {
		t.Errorf("vector dim = %d, want %d", len(results[0]), ngramDim)
	}
}

func TestEmbeddingClient_EmbedSingle(t *testing.T) {
	ec := NewEmbeddingClient(nil)
	v, err := ec.EmbedSingle(context.Background(), "test text")
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != ngramDim {
		t.Errorf("vector dim = %d, want %d", len(v), ngramDim)
	}
}

func TestEmbeddingClient_Dimensions(t *testing.T) {
	ec := NewEmbeddingClient(nil)
	if d := ec.Dimensions(); d != ngramDim {
		t.Errorf("nil provider dimensions = %d, want %d", d, ngramDim)
	}

	ec2 := NewEmbeddingClient(&Provider{EmbeddingModel: "text-embedding-3-small"})
	if d := ec2.Dimensions(); d != 1536 {
		t.Errorf("text-embedding-3-small dimensions = %d, want 1536", d)
	}
}
