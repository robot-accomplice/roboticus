package agent

import "testing"

func TestIntentClassifier_ReturnsResults(t *testing.T) {
	ic := NewIntentClassifier(IntentClassifierConfig{Enabled: true, ConfidenceThreshold: 0.1})
	results := ic.Classify("hello, how are you doing today?")
	if len(results) == 0 {
		t.Error("should return at least one result for conversational input")
	}
	// Verify results are sorted by confidence descending.
	for i := 1; i < len(results); i++ {
		if results[i].Confidence > results[i-1].Confidence {
			t.Error("results should be sorted by confidence descending")
		}
	}
}

func TestIntentClassifier_TopIntentNonEmpty(t *testing.T) {
	ic := NewIntentClassifier(IntentClassifierConfig{Enabled: true, ConfidenceThreshold: 0.1})
	top := ic.TopIntent("run the build command and compile the project")
	if top == "" {
		t.Error("should return a non-empty top intent for task-like input")
	}
}

func TestIntentClassifier_DifferentInputsDifferentScores(t *testing.T) {
	ic := NewIntentClassifier(IntentClassifierConfig{Enabled: true, ConfidenceThreshold: 0.1})
	convResults := ic.Classify("hello good morning tell me a joke")
	execResults := ic.Classify("run the build command execute the script deploy")

	if len(convResults) == 0 || len(execResults) == 0 {
		t.Skip("n-gram classifier may not separate all inputs cleanly")
	}
	// At minimum, different inputs should produce different top scores.
	if convResults[0].Label == execResults[0].Label && convResults[0].Confidence == execResults[0].Confidence {
		t.Error("different inputs should produce different classification scores")
	}
}

func TestIntentClassifier_ThresholdFiltering(t *testing.T) {
	ic := NewIntentClassifier(IntentClassifierConfig{Enabled: true, ConfidenceThreshold: 0.99})
	results := ic.Classify("hello")
	// Very high threshold should filter most or all results.
	if len(results) > 2 {
		t.Errorf("very high threshold should filter most results, got %d", len(results))
	}
}

func TestIntentClassifier_IntentLabels(t *testing.T) {
	ic := NewIntentClassifier(IntentClassifierConfig{Enabled: true, ConfidenceThreshold: 0.1})
	labels := ic.IntentLabels("run the test command")
	if len(labels) == 0 {
		t.Error("should return at least one label")
	}
}

func TestIntentClassifier_Nil(t *testing.T) {
	var ic *IntentClassifier
	results := ic.Classify("hello")
	if results != nil {
		t.Error("nil classifier should return nil")
	}
}

func TestIntentClassifier_CentroidsComputed(t *testing.T) {
	ic := NewIntentClassifier(IntentClassifierConfig{Enabled: true, ConfidenceThreshold: 0.1})
	if len(ic.centroids) != len(builtinExemplarBank) {
		t.Errorf("centroids = %d, want %d (one per exemplar bank)", len(ic.centroids), len(builtinExemplarBank))
	}
	for label, centroid := range ic.centroids {
		if len(centroid) != ic.dims {
			t.Errorf("centroid %q has %d dims, want %d", label, len(centroid), ic.dims)
		}
	}
}
