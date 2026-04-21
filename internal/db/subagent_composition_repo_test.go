package db

import (
	"context"
	"testing"
)

func TestSubagentCompositionRepository_UpsertCreateAndUpdate(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()
	repo := NewSubagentCompositionRepository(store)

	created, spec, err := repo.Upsert(ctx, SubagentSpec{
		Name:           "researcher",
		DisplayName:    "Researcher",
		Model:          "ollama/phi4-mini:latest",
		Role:           "subagent",
		Description:    "Finds evidence",
		FixedSkills:    []string{"search", "summarize"},
		FallbackModels: []string{"moonshot/kimi-k2-turbo-preview"},
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("Upsert create: %v", err)
	}
	if !created || spec == nil || spec.ID == "" {
		t.Fatalf("unexpected create result: created=%v spec=%+v", created, spec)
	}

	created, spec, err = repo.Upsert(ctx, SubagentSpec{
		Name:           "researcher",
		DisplayName:    "Research Lead",
		Model:          "moonshot/kimi-k2-turbo-preview",
		Role:           "subagent",
		Description:    "Updated",
		FixedSkills:    []string{"search"},
		FallbackModels: []string{"ollama/phi4-mini:latest"},
		Enabled:        false,
	})
	if err != nil {
		t.Fatalf("Upsert update: %v", err)
	}
	if created {
		t.Fatal("expected update to return created=false")
	}
	if spec == nil || spec.DisplayName != "Research Lead" || spec.Model != "moonshot/kimi-k2-turbo-preview" || spec.Enabled {
		t.Fatalf("unexpected updated spec: %+v", spec)
	}
}
