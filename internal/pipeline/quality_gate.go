package pipeline

import (
	"fmt"
	"strings"
)

// QualityGate performs basic quality checks on generated content (Wave 8, #86).
// It enforces minimum length and maximum repetition ratio before the content
// is sent through the full guard chain.
type QualityGate struct {
	minLength     int     // minimum acceptable content length in characters
	maxRepetition float64 // maximum ratio of repeated words (0.0–1.0)
}

// NewQualityGate creates a quality gate with reasonable defaults.
func NewQualityGate() *QualityGate {
	return &QualityGate{
		minLength:     10,
		maxRepetition: 0.6,
	}
}

// NewQualityGateCustom creates a quality gate with custom thresholds.
func NewQualityGateCustom(minLen int, maxRep float64) *QualityGate {
	return &QualityGate{
		minLength:     minLen,
		maxRepetition: maxRep,
	}
}

// Check validates content against the quality gate thresholds.
// Returns nil if the content passes, or an error describing the failure.
func (qg *QualityGate) Check(content string) error {
	trimmed := strings.TrimSpace(content)

	// Length check.
	if len(trimmed) < qg.minLength {
		return fmt.Errorf("content too short: %d chars (minimum %d)", len(trimmed), qg.minLength)
	}

	// Repetition check: ratio of unique words to total words.
	words := strings.Fields(strings.ToLower(trimmed))
	if len(words) < 5 {
		return nil // too few words to measure repetition meaningfully
	}
	unique := make(map[string]bool, len(words))
	for _, w := range words {
		unique[w] = true
	}
	repetitionRatio := 1.0 - float64(len(unique))/float64(len(words))
	if repetitionRatio > qg.maxRepetition {
		return fmt.Errorf("content too repetitive: %.1f%% repeated words (maximum %.1f%%)",
			repetitionRatio*100, qg.maxRepetition*100)
	}

	return nil
}
