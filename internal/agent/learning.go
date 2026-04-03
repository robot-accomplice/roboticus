package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
)

// LearnedPattern captures a recurring behaviour pattern extracted from agent turns.
type LearnedPattern struct {
	ID           string
	Pattern      string
	Source       string
	SuccessCount int
	FailureCount int
}

// LearningExtractor extracts patterns and synthesizes procedures from tool sequences.
type LearningExtractor struct {
	patterns     map[string]*LearnedPattern
	minSeqLength int // minimum tool-call sequence length to consider (default 3)
}

// NewLearningExtractor creates a LearningExtractor.
func NewLearningExtractor() *LearningExtractor {
	return &LearningExtractor{
		patterns:     make(map[string]*LearnedPattern),
		minSeqLength: 3,
	}
}

// ExtractFromTurn analyses a turn and its tool results, returning discovered patterns.
func (le *LearningExtractor) ExtractFromTurn(turnContent string, toolResults []string, success bool) []LearnedPattern {
	var extracted []LearnedPattern
	for _, result := range toolResults {
		if success && len(result) > 0 {
			extracted = append(extracted, LearnedPattern{
				Pattern:      "successful_tool_use",
				Source:       truncateForLearning(turnContent, 100),
				SuccessCount: 1,
			})
		}
	}
	if strings.Contains(strings.ToLower(turnContent), "how to") {
		extracted = append(extracted, LearnedPattern{
			Pattern: "procedural_query",
			Source:  truncateForLearning(turnContent, 100),
		})
	}
	return extracted
}

// Register stores a pattern under its ID for outcome tracking.
func (le *LearningExtractor) Register(p LearnedPattern) {
	le.patterns[p.ID] = &p
}

// RecordOutcome increments success or failure counters for a registered pattern.
func (le *LearningExtractor) RecordOutcome(patternID string, success bool) {
	p, ok := le.patterns[patternID]
	if !ok {
		return
	}
	if success {
		p.SuccessCount++
	} else {
		p.FailureCount++
	}
}

// SuccessRate returns the fraction of successful outcomes for a registered pattern.
func (le *LearningExtractor) SuccessRate(patternID string) float64 {
	p, ok := le.patterns[patternID]
	if !ok {
		return 0
	}
	total := p.SuccessCount + p.FailureCount
	if total == 0 {
		return 0
	}
	return float64(p.SuccessCount) / float64(total)
}

// ToolCallRecord represents a single tool invocation for procedure detection.
type ToolCallRecord struct {
	ToolName string
	Success  bool
	Input    string
}

// Procedure is a candidate multi-step procedure detected from tool-call sequences.
type Procedure struct {
	Steps []string // ordered tool names
	Count int      // how many times this sequence appeared
}

// DetectCandidateProcedures finds recurring successful tool-call sequences.
// Uses a sliding window of minSeqLength over the chronological tool-call history.
func (le *LearningExtractor) DetectCandidateProcedures(calls []ToolCallRecord) []Procedure {
	if len(calls) < le.minSeqLength {
		return nil
	}

	// Extract only successful calls.
	var successful []string
	for _, c := range calls {
		if c.Success {
			successful = append(successful, c.ToolName)
		}
	}
	if len(successful) < le.minSeqLength {
		return nil
	}

	// Count sequences of length minSeqLength.
	seqCounts := make(map[string]int)
	for i := 0; i <= len(successful)-le.minSeqLength; i++ {
		key := strings.Join(successful[i:i+le.minSeqLength], "→")
		seqCounts[key]++
	}

	// Return sequences that appeared 2+ times.
	var procedures []Procedure
	for key, count := range seqCounts {
		if count >= 2 {
			procedures = append(procedures, Procedure{
				Steps: strings.Split(key, "→"),
				Count: count,
			})
		}
	}
	return procedures
}

// SynthesizeSkillMarkdown generates a SKILL.md file from a detected procedure.
func SynthesizeSkillMarkdown(proc Procedure) string {
	name := strings.Join(proc.Steps, "-")
	var b strings.Builder
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "type: instruction\n")
	fmt.Fprintf(&b, "priority: 50\n")
	fmt.Fprintf(&b, "triggers:\n")
	fmt.Fprintf(&b, "  keywords: [%s]\n", strings.Join(proc.Steps, ", "))
	fmt.Fprintf(&b, "---\n\n")
	fmt.Fprintf(&b, "# Learned Procedure: %s\n\n", name)
	fmt.Fprintf(&b, "This procedure was automatically learned from %d successful executions.\n\n", proc.Count)
	fmt.Fprintf(&b, "## Steps\n\n")
	for i, step := range proc.Steps {
		fmt.Fprintf(&b, "%d. Execute `%s`\n", i+1, step)
	}
	return b.String()
}

// PersistLearnedSkill writes a learned procedure to the learned_skills table.
func PersistLearnedSkill(ctx context.Context, store *db.Store, proc Procedure) {
	name := strings.Join(proc.Steps, "-")
	stepsJSON := `["` + strings.Join(proc.Steps, `","`) + `"]`

	_, err := store.ExecContext(ctx,
		`INSERT INTO learned_skills (id, name, steps_json, priority, success_count)
		 VALUES (?, ?, ?, 50, ?)
		 ON CONFLICT(name) DO UPDATE SET
		     success_count = success_count + excluded.success_count,
		     priority = min(100, priority + 5)`,
		db.NewID(), name, stepsJSON, proc.Count,
	)
	if err != nil {
		log.Warn().Err(err).Str("skill", name).Msg("failed to persist learned skill")
	}
}

// ReinforceLearning adjusts learned skill priorities based on outcomes.
func ReinforceLearning(ctx context.Context, store *db.Store, skillName string, success bool) {
	if success {
		_, _ = store.ExecContext(ctx,
			`UPDATE learned_skills SET success_count = success_count + 1,
			 priority = min(100, priority + 2) WHERE name = ?`, skillName)
	} else {
		_, _ = store.ExecContext(ctx,
			`UPDATE learned_skills SET failure_count = failure_count + 1,
			 priority = max(0, priority - 5) WHERE name = ?`, skillName)
	}
}

func truncateForLearning(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
