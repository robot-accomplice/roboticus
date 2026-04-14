// reflection.go implements post-turn episode summarization (Layer 16).
//
// After each run, the agent should summarize what it tried, what worked,
// what failed, and what mattered. This turns one-off execution into learning.
//
// v1.0.5: heuristic reflection (no LLM needed).
// v1.1.0+: LLM-based reflection for richer episode summaries.

package memory

import (
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ToolEvent captures a single tool invocation from a turn.
type ToolEvent struct {
	ToolName string
	Success  bool
	Duration time.Duration
}

// EpisodeSummary is a structured reflection on a completed turn.
type EpisodeSummary struct {
	Goal      string        // what was being attempted
	Actions   []string      // tool names in order
	Outcome   string        // "success" / "partial" / "failure"
	Learnings []string      // insights extracted
	Duration  time.Duration // total turn duration
}

// Reflect produces a structured episode summary from turn data.
// Uses heuristic extraction — no LLM call needed.
func Reflect(userContent string, toolEvents []ToolEvent, turnDuration time.Duration) *EpisodeSummary {
	if userContent == "" && len(toolEvents) == 0 {
		return nil
	}

	summary := &EpisodeSummary{
		Duration: turnDuration,
	}

	// Goal: first sentence of user content, capped at 200 chars.
	summary.Goal = extractGoal(userContent)

	// Actions: tool names in execution order.
	for _, te := range toolEvents {
		summary.Actions = append(summary.Actions, te.ToolName)
	}

	// Outcome: based on tool success/failure ratio.
	successCount := 0
	failureCount := 0
	for _, te := range toolEvents {
		if te.Success {
			successCount++
		} else {
			failureCount++
		}
	}

	switch {
	case len(toolEvents) == 0:
		summary.Outcome = "conversation" // no tools used — pure dialogue
	case failureCount == 0:
		summary.Outcome = "success"
	case successCount == 0:
		summary.Outcome = "failure"
	default:
		summary.Outcome = "partial"
	}

	// Learnings: detect patterns worth noting.
	summary.Learnings = extractLearnings(toolEvents)

	log.Debug().
		Str("goal", summary.Goal).
		Str("outcome", summary.Outcome).
		Int("actions", len(summary.Actions)).
		Int("learnings", len(summary.Learnings)).
		Msg("reflection: episode summary generated")

	return summary
}

// FormatForStorage produces a compact text representation for episodic memory.
func (es *EpisodeSummary) FormatForStorage() string {
	var b strings.Builder
	b.WriteString("Goal: ")
	b.WriteString(es.Goal)
	b.WriteString(" | Outcome: ")
	b.WriteString(es.Outcome)

	if len(es.Actions) > 0 {
		b.WriteString(" | Actions: ")
		b.WriteString(strings.Join(es.Actions, " → "))
	}
	if len(es.Learnings) > 0 {
		b.WriteString(" | Learnings: ")
		b.WriteString(strings.Join(es.Learnings, "; "))
	}
	if es.Duration > 0 {
		b.WriteString(" | Duration: ")
		b.WriteString(es.Duration.Round(time.Second).String())
	}
	return b.String()
}

// extractGoal gets the first sentence of user content as the goal.
func extractGoal(content string) string {
	if content == "" {
		return "(no goal detected)"
	}

	// Take first sentence.
	for i, r := range content {
		if (r == '.' || r == '!' || r == '?') && i > 10 {
			goal := strings.TrimSpace(content[:i+1])
			if len(goal) > 200 {
				return goal[:200] + "..."
			}
			return goal
		}
	}

	// No sentence terminator — take first 200 chars.
	if len(content) > 200 {
		return content[:200] + "..."
	}
	return content
}

// extractLearnings detects notable patterns from tool events.
func extractLearnings(events []ToolEvent) []string {
	if len(events) == 0 {
		return nil
	}

	var learnings []string

	// Detect retry patterns: same tool called 2+ times.
	toolCounts := make(map[string]int)
	toolFails := make(map[string]int)
	for _, te := range events {
		toolCounts[te.ToolName]++
		if !te.Success {
			toolFails[te.ToolName]++
		}
	}

	for tool, count := range toolCounts {
		if count >= 2 && toolFails[tool] >= 1 {
			learnings = append(learnings,
				tool+": retry pattern detected ("+
					strings.Repeat("fail→", toolFails[tool])+"success)")
		}
	}

	// Detect all-fail turns.
	allFailed := true
	for _, te := range events {
		if te.Success {
			allFailed = false
			break
		}
	}
	if allFailed && len(events) > 0 {
		learnings = append(learnings, "all tool calls failed — may need different approach")
	}

	return learnings
}
