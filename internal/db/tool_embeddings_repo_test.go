package db

import (
	"context"
	"math"
	"testing"
)

func TestToolEmbeddings_SaveAndGet(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolEmbeddingsRepository(store)
	ctx := context.Background()

	embedding := []float32{0.1, 0.2, 0.3, -0.5, 1.0}

	// Save.
	if err := repo.SaveToolEmbedding(ctx, "web_search", "abc123", embedding); err != nil {
		t.Fatalf("SaveToolEmbedding: %v", err)
	}

	// Get.
	got, err := repo.GetToolEmbedding(ctx, "web_search", "abc123")
	if err != nil {
		t.Fatalf("GetToolEmbedding: %v", err)
	}
	if got == nil {
		t.Fatal("expected embedding, got nil")
	}
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	for i, v := range embedding {
		if math.Abs(float64(got[i]-v)) > 1e-6 {
			t.Errorf("[%d] = %f, want %f", i, got[i], v)
		}
	}
}

func TestToolEmbeddings_GetMissing(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolEmbeddingsRepository(store)

	got, err := repo.GetToolEmbedding(context.Background(), "nonexistent", "xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing embedding")
	}
}

func TestToolEmbeddings_UpsertOnDescriptionChange(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolEmbeddingsRepository(store)
	ctx := context.Background()

	// Save with hash v1.
	_ = repo.SaveToolEmbedding(ctx, "calculator", "hash_v1", []float32{1.0, 2.0})

	// Save with hash v2 (description changed).
	_ = repo.SaveToolEmbedding(ctx, "calculator", "hash_v2", []float32{3.0, 4.0})

	// Old hash still works.
	old, _ := repo.GetToolEmbedding(ctx, "calculator", "hash_v1")
	if old == nil {
		t.Error("old hash should still exist")
	}

	// New hash works.
	new, _ := repo.GetToolEmbedding(ctx, "calculator", "hash_v2")
	if new == nil {
		t.Error("new hash should exist")
	}
	if new[0] != 3.0 {
		t.Errorf("new[0] = %f, want 3.0", new[0])
	}
}

func TestEmbeddingBlobRoundtrip(t *testing.T) {
	original := []float32{0.0, -1.5, 3.14159, math.MaxFloat32, math.SmallestNonzeroFloat32}
	blob := embeddingToBlob(original)
	if len(blob) != len(original)*4 {
		t.Fatalf("blob size = %d, want %d", len(blob), len(original)*4)
	}
	result := blobToEmbedding(blob)
	for i, v := range original {
		if result[i] != v {
			t.Errorf("[%d] = %f, want %f", i, result[i], v)
		}
	}
}
