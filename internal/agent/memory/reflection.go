// reflection.go implements post-turn episode summarization (Layer 16).
//
// After each run, the agent should summarize what it tried, what worked,
// what failed, and what mattered. This turns one-off execution into learning.
//
// v1.0.5: heuristic reflection (no LLM needed).
// v1.1.0+: LLM-based reflection for richer episode summaries.

package memory

import (
	"encoding/json"
	"strconv"
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
//
// Milestone 8 enriches the summary beyond goal/actions/outcome so
// consolidation can promote reusable learnings into longer-term memory
// instead of only archiving episodes.
type EpisodeSummary struct {
	Goal                string        // what was being attempted
	Actions             []string      // tool names in order
	Outcome             string        // "success" / "partial" / "failure"
	Learnings           []string      // insights extracted
	Duration            time.Duration // total turn duration
	ModelUsed           string        // selected model for the turn, when known
	ReactTurns          int           // number of react turns for the final answer
	EvidenceRefs        []string      // content previews of evidence items that shaped the answer
	FailedHypotheses    []string      // hypotheses the agent walked back
	FixPatterns         []string      // tool sequences that succeeded after prior failures
	ErrorsSeen          []string      // error messages from failed tool calls
	GuardViolations     []string      // final guard violations applied to the answer
	GuardRetried        bool          // whether the turn required a guard-triggered retry
	ResultQuality       float64       // 0-1 blended signal: verifier pass + tool success rate
	VerifierPassed      bool          // whether the verifier passed the final answer
	VerifiedRecorded    int           // verified conclusions written into executive state
	QuestionsOpened     int           // unresolved questions opened for uncovered subgoals
	QuestionsResolved   int           // prior unresolved questions closed by this turn
	AssumptionsRecorded int           // assumptions written into executive state

	// Relations captures canonical (subject, relation, object) triples
	// extracted from the episode's text — assistant answer, learnings, and
	// evidence-ref content. These are the candidates M8 tallies across
	// successful, high-quality episodes for promotion into knowledge_facts.
	//
	// The triples are extracted by the same canonical-relation extractor
	// (extractKnowledgeFacts) the per-document semantic ingestion path uses,
	// so the relation vocabulary is identical across both paths and
	// `db.IsCanonicalGraphRelation` is the single write gate.
	Relations []EpisodeRelation
}

// EpisodeRelation is a single canonical relation triple extracted from an
// episode's textual evidence. The shape mirrors `extractedFact` but is an
// exported type so the round-trip through episode_summary serialisation can
// be tested directly.
type EpisodeRelation struct {
	Subject  string
	Relation string
	Object   string
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
	if len(es.FailedHypotheses) > 0 {
		b.WriteString(" | FailedHypotheses: ")
		b.WriteString(strings.Join(es.FailedHypotheses, "; "))
	}
	if len(es.FixPatterns) > 0 {
		b.WriteString(" | FixPatterns: ")
		b.WriteString(strings.Join(es.FixPatterns, "; "))
	}
	if len(es.EvidenceRefs) > 0 {
		b.WriteString(" | EvidenceRefs: ")
		b.WriteString(strings.Join(es.EvidenceRefs, " | "))
	}
	if len(es.ErrorsSeen) > 0 {
		b.WriteString(" | Errors: ")
		b.WriteString(strings.Join(es.ErrorsSeen, "; "))
	}
	if es.ModelUsed != "" {
		b.WriteString(" | Model: ")
		b.WriteString(es.ModelUsed)
	}
	if es.ReactTurns > 0 {
		b.WriteString(" | ReactTurns: ")
		b.WriteString(strconv.Itoa(es.ReactTurns))
	}
	if len(es.GuardViolations) > 0 {
		b.WriteString(" | GuardViolations: ")
		b.WriteString(strings.Join(es.GuardViolations, "; "))
	}
	if es.GuardRetried {
		b.WriteString(" | GuardRetried: yes")
	}
	if es.VerifiedRecorded > 0 {
		b.WriteString(" | ExecutiveVerified: ")
		b.WriteString(strconv.Itoa(es.VerifiedRecorded))
	}
	if es.QuestionsOpened > 0 {
		b.WriteString(" | ExecutiveQuestionsOpened: ")
		b.WriteString(strconv.Itoa(es.QuestionsOpened))
	}
	if es.QuestionsResolved > 0 {
		b.WriteString(" | ExecutiveQuestionsResolved: ")
		b.WriteString(strconv.Itoa(es.QuestionsResolved))
	}
	if es.AssumptionsRecorded > 0 {
		b.WriteString(" | ExecutiveAssumptions: ")
		b.WriteString(strconv.Itoa(es.AssumptionsRecorded))
	}
	if len(es.Relations) > 0 {
		// Wire format is `subject||relation||object` per triple, joined by
		// "; " between triples. The "||" separator avoids collisions with
		// the existing "; " and " | " separators used elsewhere in this
		// summary line so parsing stays unambiguous.
		var triples []string
		for _, rel := range es.Relations {
			triples = append(triples, rel.Subject+"||"+rel.Relation+"||"+rel.Object)
		}
		b.WriteString(" | Relations: ")
		b.WriteString(strings.Join(triples, "; "))
	}
	if es.ResultQuality > 0 {
		b.WriteString(" | Quality: ")
		b.WriteString(formatQuality(es.ResultQuality))
	}
	if es.Duration > 0 {
		b.WriteString(" | Duration: ")
		b.WriteString(es.Duration.Round(time.Second).String())
	}
	return b.String()
}

// JSON returns the structured episode summary for machine-consumable storage.
func (es *EpisodeSummary) JSON() string {
	if es == nil {
		return ""
	}
	b, err := json.Marshal(es)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseEpisodeSummaryJSON reconstructs an EpisodeSummary from stored JSON.
func ParseEpisodeSummaryJSON(raw string) (*EpisodeSummary, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var summary EpisodeSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// EpisodeInput carries the extra context the enriched reflection needs
// beyond the original user content and tool events.
type EpisodeInput struct {
	UserContent         string
	AssistantAnswer     string
	ToolEvents          []ToolEvent
	EvidenceItems       []string // retrieved-evidence items that reached the model
	VerifierPassed      bool
	ErrorMessages       []string // stderr / failure outputs captured from tool calls
	Duration            time.Duration
	ModelUsed           string
	ReactTurns          int
	GuardViolations     []string
	GuardRetried        bool
	VerifiedRecorded    int
	QuestionsOpened     int
	QuestionsResolved   int
	AssumptionsRecorded int
}

// AnalyzeEpisode is the enriched reflection entry point. It extends Reflect
// with evidence tracking, failed-hypothesis detection, fix-pattern
// extraction, and a blended result-quality score that combines verifier
// pass with tool-success rate. The base Reflect() remains as a backward-
// compatible shim for callers that do not have evidence/verifier data.
func AnalyzeEpisode(input EpisodeInput) *EpisodeSummary {
	summary := Reflect(input.UserContent, input.ToolEvents, input.Duration)
	if summary == nil {
		if input.AssistantAnswer == "" && len(input.EvidenceItems) == 0 {
			return nil
		}
		summary = &EpisodeSummary{
			Goal:     extractGoal(input.UserContent),
			Outcome:  "conversation",
			Duration: input.Duration,
		}
	}

	summary.VerifierPassed = input.VerifierPassed
	summary.ModelUsed = strings.TrimSpace(input.ModelUsed)
	summary.ReactTurns = input.ReactTurns
	summary.EvidenceRefs = evidencePreviews(input.EvidenceItems, 3)
	summary.FixPatterns = extractFixPatterns(input.ToolEvents)
	summary.FailedHypotheses = extractFailedHypotheses(input.AssistantAnswer)
	summary.ErrorsSeen = dedupeAndTrim(input.ErrorMessages, 3, 200)
	summary.GuardViolations = dedupeAndTrim(input.GuardViolations, 3, 120)
	summary.GuardRetried = input.GuardRetried
	summary.VerifiedRecorded = input.VerifiedRecorded
	summary.QuestionsOpened = input.QuestionsOpened
	summary.QuestionsResolved = input.QuestionsResolved
	summary.AssumptionsRecorded = input.AssumptionsRecorded
	summary.ResultQuality = computeResultQuality(input)
	summary.Relations = extractEpisodeRelations(input)
	summary.Learnings = mergeLearnings(summary.Learnings, structuredLearnings(input))

	return summary
}

// extractEpisodeRelations runs the canonical relation extractor over the
// episode's textual surface (assistant answer, evidence refs, learnings)
// and returns deduped triples. The same extractor that powers per-document
// semantic ingestion is used here so the relation vocabulary stays
// identical and `db.IsCanonicalGraphRelation` is the single write gate.
//
// A relation appearing more than once in the same episode is collapsed to
// one occurrence here; the per-episode count is always 0 or 1 in the
// distillation tally so a single chatty episode can't drive promotion on
// its own. The promotion threshold cares about the number of distinct
// episodes that observed the relation, not the per-episode hit count.
func extractEpisodeRelations(input EpisodeInput) []EpisodeRelation {
	var sources []string
	if strings.TrimSpace(input.AssistantAnswer) != "" {
		sources = append(sources, input.AssistantAnswer)
	}
	for _, item := range input.EvidenceItems {
		if strings.TrimSpace(item) != "" {
			sources = append(sources, item)
		}
	}
	if len(sources) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var out []EpisodeRelation
	for _, text := range sources {
		// extractKnowledgeFacts expects (key, value); the empty key
		// signals the extractor to use the text-derived subject only —
		// no auto-substitution from a document key.
		for _, fact := range extractKnowledgeFacts("", text) {
			signature := strings.ToLower(fact.Subject) + "|" + fact.Relation + "|" + strings.ToLower(fact.Object)
			if _, dup := seen[signature]; dup {
				continue
			}
			seen[signature] = struct{}{}
			out = append(out, EpisodeRelation(fact))
		}
	}
	return out
}

func computeResultQuality(input EpisodeInput) float64 {
	// Tool component: success rate across all tools, default 1.0 when no tools ran.
	toolComponent := 1.0
	if len(input.ToolEvents) > 0 {
		successes := 0
		for _, ev := range input.ToolEvents {
			if ev.Success {
				successes++
			}
		}
		toolComponent = float64(successes) / float64(len(input.ToolEvents))
	}
	// Verifier component: binary pass/fail.
	verifierComponent := 0.5
	if input.VerifierPassed {
		verifierComponent = 1.0
	}
	// Evidence component: rewards answers that had evidence to draw from.
	evidenceComponent := 0.5
	if len(input.EvidenceItems) >= 3 {
		evidenceComponent = 1.0
	} else if len(input.EvidenceItems) >= 1 {
		evidenceComponent = 0.75
	}
	blended := (toolComponent*0.5 + verifierComponent*0.3 + evidenceComponent*0.2)
	if len(input.GuardViolations) > 0 {
		blended -= 0.1
	}
	if blended < 0 {
		return 0
	}
	if blended > 1 {
		return 1
	}
	return blended
}

func structuredLearnings(input EpisodeInput) []string {
	var out []string
	if input.GuardRetried {
		out = append(out, "guard-triggered revision required before final answer")
	}
	if input.ReactTurns >= 2 {
		out = append(out, "multi-step react loop used")
	}
	return out
}

func mergeLearnings(base []string, extras []string) []string {
	if len(extras) == 0 {
		return base
	}
	return dedupeStrings(append(base, extras...))
}

// extractFixPatterns detects tool sequences where a failure was followed by
// a success on the same tool, typically indicating a successful retry or
// correction. Returns a short descriptor per detected pattern.
func extractFixPatterns(events []ToolEvent) []string {
	if len(events) < 2 {
		return nil
	}
	var patterns []string
	seen := make(map[string]bool)
	for i := 1; i < len(events); i++ {
		prev := events[i-1]
		curr := events[i]
		if prev.ToolName == curr.ToolName && !prev.Success && curr.Success {
			key := curr.ToolName
			if seen[key] {
				continue
			}
			seen[key] = true
			patterns = append(patterns, key+": fail→success on retry")
		}
	}
	return patterns
}

// extractFailedHypotheses detects explicit self-corrections in the assistant
// response — phrases like "actually, I was wrong about X" or "on second
// thought, Y is incorrect". These are hypotheses the agent walked back and
// are worth remembering so the same mistake is not made twice.
var hypothesisWalkbackMarkers = []string{
	"actually, i was wrong",
	"actually, i was incorrect",
	"on second thought",
	"correction:",
	"my earlier statement",
	"revising my answer",
	"that was incorrect",
	"i was mistaken",
	"i need to correct",
}

func extractFailedHypotheses(answer string) []string {
	if answer == "" {
		return nil
	}
	lower := strings.ToLower(answer)
	var out []string
	for _, marker := range hypothesisWalkbackMarkers {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		// Capture the sentence that contains the marker.
		sentenceEnd := len(answer)
		for j := idx + len(marker); j < len(answer); j++ {
			r := answer[j]
			if r == '.' || r == '!' || r == '?' || r == '\n' {
				sentenceEnd = j
				break
			}
		}
		start := idx
		for start > 0 {
			r := answer[start-1]
			if r == '.' || r == '!' || r == '?' || r == '\n' {
				break
			}
			start--
		}
		clause := strings.TrimSpace(answer[start:sentenceEnd])
		if clause == "" {
			continue
		}
		if len(clause) > 200 {
			clause = clause[:200]
		}
		out = append(out, clause)
	}
	return dedupeStrings(out)
}

func evidencePreviews(items []string, max int) []string {
	if len(items) == 0 {
		return nil
	}
	var out []string
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 120 {
			trimmed = trimmed[:120] + "…"
		}
		out = append(out, trimmed)
		if len(out) >= max {
			break
		}
	}
	return out
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	var out []string
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func dedupeAndTrim(items []string, max, maxChars int) []string {
	trimmed := dedupeStrings(items)
	if max > 0 && len(trimmed) > max {
		trimmed = trimmed[:max]
	}
	for i, item := range trimmed {
		if maxChars > 0 && len(item) > maxChars {
			trimmed[i] = item[:maxChars]
		}
	}
	return trimmed
}

func formatQuality(q float64) string {
	switch {
	case q >= 0.85:
		return "high"
	case q >= 0.60:
		return "medium"
	case q > 0:
		return "low"
	}
	return "unknown"
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
