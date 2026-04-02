package db

import (
	"context"
	"testing"
)

func TestMetricsRepository_RecordAndListCosts(t *testing.T) {
	store := testTempStore(t)
	repo := NewMetricsRepository(store)
	ctx := context.Background()

	costs := []InferenceCostRow{
		{ID: "ic-1", Model: "gpt-4", Provider: "openai", TokensIn: 100, TokensOut: 50, Cost: 0.01},
		{ID: "ic-2", Model: "claude-3", Provider: "anthropic", TokensIn: 200, TokensOut: 80, Cost: 0.02},
		{ID: "ic-3", Model: "gpt-4", Provider: "openai", TokensIn: 150, TokensOut: 60, Cost: 0.015, Cached: true},
	}
	for _, c := range costs {
		if err := repo.RecordCost(ctx, c); err != nil {
			t.Fatalf("RecordCost(%s): %v", c.ID, err)
		}
	}

	rows, err := repo.ListRecentCosts(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentCosts: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("got %d rows, want 3", len(rows))
	}
}

func TestMetricsRepository_TotalCostByModel(t *testing.T) {
	store := testTempStore(t)
	repo := NewMetricsRepository(store)
	ctx := context.Background()

	_ = repo.RecordCost(ctx, InferenceCostRow{ID: "ic-1", Model: "gpt-4", Provider: "openai", Cost: 0.01})
	_ = repo.RecordCost(ctx, InferenceCostRow{ID: "ic-2", Model: "gpt-4", Provider: "openai", Cost: 0.02})
	_ = repo.RecordCost(ctx, InferenceCostRow{ID: "ic-3", Model: "claude-3", Provider: "anthropic", Cost: 0.05})

	totals, err := repo.TotalCostByModel(ctx)
	if err != nil {
		t.Fatalf("TotalCostByModel: %v", err)
	}
	if len(totals) != 2 {
		t.Errorf("got %d models, want 2", len(totals))
	}
	if totals["gpt-4"] != 0.03 {
		t.Errorf("gpt-4 total = %f, want 0.03", totals["gpt-4"])
	}
}

func TestMetricsRepository_Snapshots(t *testing.T) {
	store := testTempStore(t)
	repo := NewMetricsRepository(store)
	ctx := context.Background()

	// No snapshot yet — should not error.
	snap, err := repo.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot (empty): %v", err)
	}
	_ = snap // may be nil

	if err := repo.SaveSnapshot(ctx, MetricSnapshotRow{
		ID:          "snap-1",
		MetricsJSON: `{"cpu":0.5}`,
		AlertsJSON:  `[]`,
	}); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	snap, err = repo.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.MetricsJSON != `{"cpu":0.5}` {
		t.Errorf("MetricsJSON = %q, want {\"cpu\":0.5}", snap.MetricsJSON)
	}
}
