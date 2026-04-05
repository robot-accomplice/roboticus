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
