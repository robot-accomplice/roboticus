package pipeline

import "strings"

// Intent represents a classified user intent.
type Intent string

const (
	IntentQuestion Intent = "question"
	IntentCommand  Intent = "command"
	IntentCreative Intent = "creative"
	IntentAnalysis Intent = "analysis"
	IntentChat     Intent = "chat"
)

// IntentClassifier classifies user input into an intent with confidence.
type IntentClassifier interface {
	Classify(content string) (Intent, float64)
}

// IntentClassifierFunc adapts a function to the IntentClassifier interface.
type IntentClassifierFunc func(string) (Intent, float64)

func (f IntentClassifierFunc) Classify(content string) (Intent, float64) {
	return f(content)
}

// IntentRegistry holds classifiers and picks the highest-confidence result.
type IntentRegistry struct {
	classifiers []IntentClassifier
}

// NewIntentRegistry creates a registry with the default keyword classifier.
func NewIntentRegistry() *IntentRegistry {
	return &IntentRegistry{
		classifiers: []IntentClassifier{&keywordClassifier{}},
	}
}

// AddClassifier adds a custom classifier. Custom classifiers are checked
// before the default keyword classifier.
func (ir *IntentRegistry) AddClassifier(c IntentClassifier) {
	// Prepend so custom classifiers take priority.
	ir.classifiers = append([]IntentClassifier{c}, ir.classifiers...)
}

// Classify returns the highest-confidence intent across all classifiers.
func (ir *IntentRegistry) Classify(content string) (Intent, float64) {
	var bestIntent Intent = IntentChat
	var bestConf float64

	for _, c := range ir.classifiers {
		intent, conf := c.Classify(content)
		if conf > bestConf {
			bestIntent = intent
			bestConf = conf
		}
	}
	return bestIntent, bestConf
}

// keywordClassifier uses simple keyword heuristics for intent classification.
type keywordClassifier struct{}

func (k *keywordClassifier) Classify(content string) (Intent, float64) {
	if content == "" {
		return IntentChat, 0.0
	}

	lower := strings.ToLower(strings.TrimSpace(content))

	// Commands: starts with / or imperative verbs
	if strings.HasPrefix(lower, "/") {
		return IntentCommand, 0.9
	}
	for _, verb := range []string{"run ", "execute ", "start ", "stop ", "restart ", "deploy ", "install "} {
		if strings.HasPrefix(lower, verb) {
			return IntentCommand, 0.7
		}
	}

	// Questions: ends with ? or starts with question words
	if strings.HasSuffix(lower, "?") {
		return IntentQuestion, 0.8
	}
	for _, qw := range []string{"what ", "how ", "why ", "when ", "where ", "who ", "which ", "can ", "could ", "is ", "are ", "do ", "does "} {
		if strings.HasPrefix(lower, qw) {
			return IntentQuestion, 0.7
		}
	}

	// Creative: generation keywords
	for _, cw := range []string{"write ", "compose ", "create ", "generate ", "draft ", "poem", "story", "song"} {
		if strings.Contains(lower, cw) {
			return IntentCreative, 0.7
		}
	}

	// Analysis: data/analysis keywords
	for _, aw := range []string{"analyze ", "analyse ", "compare ", "evaluate ", "assess ", "review ", "summarize ", "breakdown"} {
		if strings.Contains(lower, aw) {
			return IntentAnalysis, 0.7
		}
	}

	// Default: chat — non-empty inputs that match nothing specific are informal chat
	return IntentChat, 0.5
}
