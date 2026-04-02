package pipeline

import "testing"

func TestIntentRegistry_Classify(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantIntent Intent
		wantMin    float64 // minimum confidence
	}{
		{"question with ?", "What is the weather today?", IntentQuestion, 0.7},
		{"how question", "How do I reset my password", IntentQuestion, 0.7},
		{"command with /", "/status check", IntentCommand, 0.8},
		{"run command", "run the test suite", IntentCommand, 0.6},
		{"creative request", "write me a poem about the sea", IntentCreative, 0.6},
		{"analysis request", "analyze the sales data from Q4", IntentAnalysis, 0.6},
		{"casual chat", "hey how are you doing", IntentChat, 0.5},
		{"empty input", "", IntentChat, 0.0},
		{"ambiguous", "hello", IntentChat, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewIntentRegistry()
			intent, conf := reg.Classify(tt.content)
			if intent != tt.wantIntent {
				t.Errorf("Classify(%q) intent = %q, want %q", tt.content, intent, tt.wantIntent)
			}
			if conf < tt.wantMin {
				t.Errorf("Classify(%q) confidence = %.2f, want >= %.2f", tt.content, conf, tt.wantMin)
			}
		})
	}
}

func TestIntentRegistry_AddClassifier(t *testing.T) {
	reg := NewIntentRegistry()

	// Add a custom classifier that always returns "custom" intent
	reg.AddClassifier(IntentClassifierFunc(func(content string) (Intent, float64) {
		if content == "magic word" {
			return Intent("custom"), 1.0
		}
		return "", 0
	}))

	intent, conf := reg.Classify("magic word")
	if intent != Intent("custom") {
		t.Errorf("intent = %q, want %q", intent, "custom")
	}
	if conf != 1.0 {
		t.Errorf("confidence = %.2f, want 1.0", conf)
	}
}
