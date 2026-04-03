package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogisticRouter_ScoreRange(t *testing.T) {
	lr := DefaultLogisticRouter()

	tests := []QueryFeatures{
		{CharCount: 0, MessageCount: 0, ToolCount: 0},
		{CharCount: 100, MessageCount: 2, ToolCount: 0},
		{CharCount: 5000, MessageCount: 10, ToolCount: 3, HasCode: true},
		{CharCount: 15000, MessageCount: 30, ToolCount: 10, HasCode: true, HasMath: true},
	}

	for _, feat := range tests {
		score := lr.Route(feat)
		if score < 0.0 || score > 1.0 {
			t.Errorf("score %f out of [0,1] for features %+v", score, feat)
		}
	}
}

func TestLogisticRouter_SimpleVsComplex(t *testing.T) {
	lr := DefaultLogisticRouter()

	simple := QueryFeatures{CharCount: 50, MessageCount: 1, ToolCount: 0}
	complex := QueryFeatures{CharCount: 12000, MessageCount: 25, ToolCount: 8, HasCode: true, HasMath: true}

	simpleScore := lr.Route(simple)
	complexScore := lr.Route(complex)

	if simpleScore >= complexScore {
		t.Errorf("expected simple (%f) < complex (%f)", simpleScore, complexScore)
	}
}

func TestLogisticRouter_CustomWeights(t *testing.T) {
	// All-zero weights + positive bias should yield sigmoid(bias) > 0.5.
	lr := NewLogisticRouter([]float64{0, 0, 0, 0, 0}, 1.0)
	score := lr.Route(QueryFeatures{})
	expected := sigmoid(1.0)
	if score != expected {
		t.Errorf("expected %f, got %f", expected, score)
	}
}

func TestLogisticRouter_MismatchedWeights(t *testing.T) {
	// Wrong number of weights → should return 0.5 default.
	lr := NewLogisticRouter([]float64{1.0, 2.0}, 0.0)
	score := lr.Route(QueryFeatures{CharCount: 1000})
	if score != 0.5 {
		t.Errorf("expected 0.5 for mismatched weights, got %f", score)
	}
}

func TestLogisticRouter_Training(t *testing.T) {
	lr := DefaultLogisticRouter()

	// Simple examples: all inputs should produce low scores.
	dataset := []RoutingExample{
		{Features: QueryFeatures{CharCount: 10, MessageCount: 1}, Outcome: 0.0},
		{Features: QueryFeatures{CharCount: 20, MessageCount: 1}, Outcome: 0.0},
		{Features: QueryFeatures{CharCount: 30, MessageCount: 1}, Outcome: 0.0},
	}

	before := lr.Route(QueryFeatures{CharCount: 20, MessageCount: 1})
	lr.Train(dataset, 100, 0.1)
	after := lr.Route(QueryFeatures{CharCount: 20, MessageCount: 1})

	// After training toward 0, prediction should move toward 0 (i.e. decrease).
	if after >= before {
		t.Logf("before=%f after=%f", before, after)
		// Acceptable if already very low.
		if before > 0.1 {
			t.Errorf("training should have reduced score: before=%f after=%f", before, after)
		}
	}
}

func TestLogisticRouter_TrainEmptyDataset(t *testing.T) {
	lr := DefaultLogisticRouter()
	before := lr.Route(QueryFeatures{CharCount: 100})
	lr.Train(nil, 100, 0.1)
	after := lr.Route(QueryFeatures{CharCount: 100})
	if before != after {
		t.Errorf("empty training should not change weights: before=%f after=%f", before, after)
	}
}

func TestLogisticRouter_SaveLoad(t *testing.T) {
	lr := DefaultLogisticRouter()
	lr.Train([]RoutingExample{
		{Features: QueryFeatures{CharCount: 5000, HasCode: true}, Outcome: 1.0},
	}, 50, 0.1)

	path := filepath.Join(t.TempDir(), "model.json")
	if err := lr.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}

	loaded, err := LoadLogisticRouter(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	feat := QueryFeatures{CharCount: 3000, MessageCount: 5, HasCode: true}
	if lr.Route(feat) != loaded.Route(feat) {
		t.Errorf("loaded model diverges: %f vs %f", lr.Route(feat), loaded.Route(feat))
	}
}

func TestPreferenceCollector_Basic(t *testing.T) {
	pc := NewPreferenceCollector(100)
	pc.Record(QueryFeatures{CharCount: 100}, "gpt-4", 0.8)
	pc.Record(QueryFeatures{CharCount: 200}, "claude", 0.9)

	if pc.Len() != 2 {
		t.Errorf("len = %d, want 2", pc.Len())
	}

	examples := pc.AsTrainingSet()
	if len(examples) != 2 || examples[0].Outcome != 0.8 {
		t.Errorf("unexpected training set: %+v", examples)
	}
}

func TestPreferenceCollector_Overflow(t *testing.T) {
	pc := NewPreferenceCollector(10)
	for i := 0; i < 15; i++ {
		pc.Record(QueryFeatures{CharCount: i * 100}, "model", float64(i)/15)
	}
	if pc.Len() > 10 {
		t.Errorf("overflow not handled: len = %d", pc.Len())
	}
}
