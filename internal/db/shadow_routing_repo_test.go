package db

import (
	"context"
	"testing"
)

func TestShadowRoutingRepository_AgreementRate(t *testing.T) {
	store := testTempStore(t)
	repo := NewShadowRoutingRepository(store)
	ctx := context.Background()

	cases := []bool{true, true, true, false}
	for i, agreed := range cases {
		if err := repo.SavePrediction(ctx, NewID(), "prod", "shadow", 0.2+float64(i)*0.1, 0.3, agreed); err != nil {
			t.Fatalf("SavePrediction(%d): %v", i, err)
		}
	}

	rate, err := repo.AgreementRate(ctx, 10)
	if err != nil {
		t.Fatalf("AgreementRate: %v", err)
	}
	if rate != 75.0 {
		t.Fatalf("agreement rate = %.2f, want 75.00", rate)
	}
}

func TestRoutingDatasetRepo_SaveAndListRoutingExamples(t *testing.T) {
	store := testTempStore(t)
	repo := NewRoutingDatasetRepo(store)
	ctx := context.Background()

	if err := repo.SaveRoutingExample(ctx, "turn-1", "prod-a", 0.2, 0.8, "shadow-b", false, `{"winner":"shadow-b"}`); err != nil {
		t.Fatalf("SaveRoutingExample: %v", err)
	}
	if err := repo.SaveRoutingExample(ctx, "turn-2", "prod-b", 0.7, 0.7, "shadow-b", true, `{"winner":"prod-b"}`); err != nil {
		t.Fatalf("SaveRoutingExample: %v", err)
	}

	examples, err := repo.ListRoutingExamples(ctx, 10)
	if err != nil {
		t.Fatalf("ListRoutingExamples: %v", err)
	}
	if len(examples) != 2 {
		t.Fatalf("example count = %d, want 2", len(examples))
	}

	agreed := 0
	for _, ex := range examples {
		if ex.ShadowModel == nil || *ex.ShadowModel == "" {
			t.Fatalf("shadow model missing in %+v", ex)
		}
		if ex.DetailJSON == nil || *ex.DetailJSON == "" {
			t.Fatalf("detail json missing in %+v", ex)
		}
		if ex.Agreed {
			agreed++
		}
	}
	if agreed != 1 {
		t.Fatalf("agreed count = %d, want 1", agreed)
	}
}

func TestRoutingDatasetRepo_ExtractAndSummarizeRoutingDataset(t *testing.T) {
	store := testTempStore(t)
	repo := NewRoutingDatasetRepo(store)
	ctx := context.Background()

	_, err := store.ExecContext(ctx,
		`INSERT INTO model_selection_events
		 (id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model, user_excerpt, candidates_json, schema_version, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		"mse-1", "turn-1", "session-1", "agent-1", "api", "cloud-model", "metascore", "cloud-model", "sensitive excerpt", "[]", 2)
	if err != nil {
		t.Fatalf("seed model_selection_events: %v", err)
	}
	_, err = store.ExecContext(ctx,
		`INSERT INTO inference_costs
		 (id, turn_id, model, provider, cost, tokens_in, tokens_out, cached, latency_ms, quality_score, escalation, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		"cost-1", "turn-1", "cloud-model", "openai", 0.03, 100, 200, 1, 250, 0.88, 1)
	if err != nil {
		t.Fatalf("seed inference_costs: %v", err)
	}

	rows, err := repo.ExtractRoutingDataset(ctx, DatasetFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ExtractRoutingDataset: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.SelectedModel != "cloud-model" {
		t.Fatalf("selected_model = %q, want cloud-model", row.SelectedModel)
	}
	if !row.AnyCached || !row.AnyEscalation {
		t.Fatalf("expected cached and escalation flags to be true: %+v", row)
	}
	if row.TotalTokensOut != 200 {
		t.Fatalf("total_tokens_out = %d, want 200", row.TotalTokensOut)
	}

	summary, err := repo.SummarizeRoutingDataset(ctx, DatasetFilter{Limit: 10})
	if err != nil {
		t.Fatalf("SummarizeRoutingDataset: %v", err)
	}
	if summary.TotalRows != 1 {
		t.Fatalf("summary total_rows = %d, want 1", summary.TotalRows)
	}
	if summary.DistinctModels != 1 || summary.DistinctStrategies != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
