package db

import (
	"context"
	"testing"
)

func seedInferenceCost(t *testing.T, store *Store, model string, tokensIn, tokensOut int, cost float64, cached bool) {
	t.Helper()
	cachedInt := 0
	if cached {
		cachedInt = 1
	}
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost, cached)
		 VALUES (?, ?, 'test', ?, ?, ?, ?)`,
		NewID(), model, tokensIn, tokensOut, cost, cachedInt)
	if err != nil {
		t.Fatalf("seedInferenceCost: %v", err)
	}
}

func TestEfficiency_ComputeEmpty(t *testing.T) {
	store := testTempStore(t)
	repo := NewEfficiencyRepository(store)

	report, err := repo.ComputeEfficiency(context.Background(), "all", "")
	if err != nil {
		t.Fatalf("ComputeEfficiency: %v", err)
	}
	if len(report.Models) != 0 {
		t.Errorf("expected 0 models, got %d", len(report.Models))
	}
	if report.Totals.TotalTurns != 0 {
		t.Errorf("expected 0 turns, got %d", report.Totals.TotalTurns)
	}
}

func TestEfficiency_ComputeWithData(t *testing.T) {
	store := testTempStore(t)
	repo := NewEfficiencyRepository(store)
	ctx := context.Background()

	// Seed some inference costs.
	seedInferenceCost(t, store, "gpt-4", 1000, 200, 0.05, false)
	seedInferenceCost(t, store, "gpt-4", 800, 150, 0.04, true)
	seedInferenceCost(t, store, "claude-3", 500, 300, 0.03, false)

	report, err := repo.ComputeEfficiency(ctx, "all", "")
	if err != nil {
		t.Fatalf("ComputeEfficiency: %v", err)
	}
	if len(report.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(report.Models))
	}

	gpt4 := report.Models["gpt-4"]
	if gpt4.TotalTurns != 2 {
		t.Errorf("gpt-4 turns = %d, want 2", gpt4.TotalTurns)
	}
	if gpt4.CacheHitRate != 0.5 {
		t.Errorf("cache_hit_rate = %f, want 0.5", gpt4.CacheHitRate)
	}

	if report.Totals.TotalTurns != 3 {
		t.Errorf("total turns = %d, want 3", report.Totals.TotalTurns)
	}
}

func TestEfficiency_ModelFilter(t *testing.T) {
	store := testTempStore(t)
	repo := NewEfficiencyRepository(store)
	ctx := context.Background()

	seedInferenceCost(t, store, "gpt-4", 1000, 200, 0.05, false)
	seedInferenceCost(t, store, "claude-3", 500, 300, 0.03, false)

	report, err := repo.ComputeEfficiency(ctx, "all", "claude-3")
	if err != nil {
		t.Fatalf("ComputeEfficiency filtered: %v", err)
	}
	if len(report.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(report.Models))
	}
	if _, ok := report.Models["claude-3"]; !ok {
		t.Error("expected claude-3 in results")
	}
}

func TestEfficiency_BuildUserProfileEmpty(t *testing.T) {
	store := testTempStore(t)
	repo := NewEfficiencyRepository(store)

	profile, err := repo.BuildUserProfile(context.Background(), "all")
	if err != nil {
		t.Fatalf("BuildUserProfile: %v", err)
	}
	if profile.TotalTurns != 0 {
		t.Errorf("expected 0 turns, got %d", profile.TotalTurns)
	}
	if len(profile.ModelsUsed) != 0 {
		t.Errorf("expected 0 models, got %d", len(profile.ModelsUsed))
	}
}

func TestEfficiency_BuildUserProfileWithData(t *testing.T) {
	store := testTempStore(t)
	repo := NewEfficiencyRepository(store)
	ctx := context.Background()

	seedInferenceCost(t, store, "gpt-4", 1000, 200, 0.05, false)
	seedInferenceCost(t, store, "gpt-4", 800, 150, 0.04, true)

	profile, err := repo.BuildUserProfile(ctx, "all")
	if err != nil {
		t.Fatalf("BuildUserProfile: %v", err)
	}
	if profile.TotalTurns != 2 {
		t.Errorf("total turns = %d, want 2", profile.TotalTurns)
	}
	if len(profile.ModelsUsed) != 1 || profile.ModelsUsed[0] != "gpt-4" {
		t.Errorf("models_used = %v", profile.ModelsUsed)
	}
	if profile.CacheHitRate != 0.5 {
		t.Errorf("cache_hit_rate = %f, want 0.5", profile.CacheHitRate)
	}
}

func TestTrendLabel(t *testing.T) {
	tests := []struct {
		first, second float64
		want          string
	}{
		{0, 0, "stable"},
		{1.0, 1.0, "stable"},
		{1.0, 1.2, "increasing"},
		{1.0, 0.8, "decreasing"},
		{1.0, 1.04, "stable"},
	}
	for _, tt := range tests {
		got := trendLabel(tt.first, tt.second)
		if got != tt.want {
			t.Errorf("trendLabel(%f, %f) = %q, want %q", tt.first, tt.second, got, tt.want)
		}
	}
}
