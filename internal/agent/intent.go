package agent

import (
	"sort"
	"strings"
	"sync"
)

// IntentResult represents a classified intent with confidence.
type IntentResult struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

// IntentClassifier uses centroid-based cosine similarity to classify user
// messages into intent categories. Centroids are computed once from exemplar
// banks and cached for the process lifetime.
type IntentClassifier struct {
	mu        sync.RWMutex
	centroids map[string][]float32 // label → centroid vector
	dims      int
	threshold float64
}

// IntentClassifierConfig holds configuration for the classifier.
type IntentClassifierConfig struct {
	Enabled             bool    `json:"enabled" mapstructure:"enabled"`
	ConfidenceThreshold float64 `json:"confidence_threshold" mapstructure:"confidence_threshold"`
}

// NewIntentClassifier creates a classifier with n-gram embeddings.
// Centroids are computed immediately from the built-in exemplar bank.
func NewIntentClassifier(cfg IntentClassifierConfig) *IntentClassifier {
	dims := 128
	threshold := cfg.ConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.3
	}

	ic := &IntentClassifier{
		centroids: make(map[string][]float32),
		dims:      dims,
		threshold: threshold,
	}

	// Compute centroids from exemplar bank.
	for label, exemplars := range builtinExemplarBank {
		vecs := make([][]float32, len(exemplars))
		for i, ex := range exemplars {
			vecs[i] = NgramEmbedding(strings.ToLower(ex), dims)
		}
		ic.centroids[label] = CentroidOf(vecs)
	}

	return ic
}

// Classify returns all matching intents above the confidence threshold,
// sorted by confidence descending.
func (ic *IntentClassifier) Classify(text string) []IntentResult {
	if ic == nil || len(ic.centroids) == 0 {
		return nil
	}

	ic.mu.RLock()
	defer ic.mu.RUnlock()

	inputVec := NgramEmbedding(strings.ToLower(text), ic.dims)
	var results []IntentResult

	for label, centroid := range ic.centroids {
		score := CosineSimilarity(inputVec, centroid)
		if score >= ic.threshold {
			results = append(results, IntentResult{Label: label, Confidence: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})
	return results
}

// TopIntent returns the highest-confidence intent, or empty string if none.
func (ic *IntentClassifier) TopIntent(text string) string {
	results := ic.Classify(text)
	if len(results) == 0 {
		return ""
	}
	return results[0].Label
}

// IntentLabels returns all classified intent labels (convenience for GuardContext).
func (ic *IntentClassifier) IntentLabels(text string) []string {
	results := ic.Classify(text)
	labels := make([]string, len(results))
	for i, r := range results {
		labels[i] = r.Label
	}
	return labels
}

// builtinExemplarBank defines exemplar phrases for each intent category.
// These are used to compute centroids for n-gram classification.
var builtinExemplarBank = map[string][]string{
	"conversation": {
		"hello", "how are you", "tell me about yourself",
		"what do you think", "that's interesting", "thanks for the help",
		"good morning", "can we chat", "tell me a joke",
	},
	"execution": {
		"run the command", "execute this script", "build the project",
		"deploy to production", "start the server", "compile the code",
		"run the tests", "install the package",
	},
	"delegation": {
		"ask the specialist", "delegate this task", "have the expert handle it",
		"route this to the team", "assign this to someone",
	},
	"cron": {
		"schedule a job", "set up a cron", "run this every hour",
		"create a recurring task", "schedule at midnight",
	},
	"memory_query": {
		"what do you remember", "recall our conversation",
		"search your memory", "what did we discuss", "remember when",
	},
	"tool_request": {
		"use the search tool", "look this up", "search the web for",
		"read the file", "write to the file", "list the directory",
	},
	"creative": {
		"write a poem", "compose a story", "create a haiku",
		"generate a song", "write fiction", "imagine a world",
	},
	"current_events": {
		"what's happening today", "latest news about",
		"current weather in", "what happened recently",
		"today's headlines", "recent developments",
	},
	"model_identity": {
		"what model are you", "what are you running on",
		"which AI are you", "who made you", "what's your name",
	},
}
