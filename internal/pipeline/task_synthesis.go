package pipeline

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/rs/zerolog/log"
)

// TaskSynthesis holds the result of task state analysis, matching Rust's
// synthesize_task_state output. Used by the pipeline to make routing and
// delegation decisions.
type TaskSynthesis struct {
	Intent          string   // Classified intent (e.g., "question", "task", "creative", "code")
	Complexity      string   // "simple", "moderate", "complex", "specialist"
	PlannedAction   string   // "execute_directly", "delegate_to_specialist", "compose_subagent"
	Confidence      float64  // Planner confidence 0–1
	RetrievalNeeded bool     // Whether memory retrieval is beneficial
	RetrievalReason string   // Why memory retrieval was selected
	ProceduralUncertainty bool // Whether the framework is uncertain how to carry out the task
	MissingSkills   []string // Capabilities not covered by registered skills
	CapabilityFit   float64  // 0–1 ratio of available capability coverage
}

// SynthesizeTaskState performs intent classification, complexity analysis, and
// planner action selection. Matches Rust's synthesize_task_state + plan().
//
// This is a heuristic implementation — Rust uses a semantic classifier backed
// by an LLM call, but the Go version uses keyword-based classification to avoid
// adding inference latency to every turn. The classification accuracy is
// comparable for the common cases.
func SynthesizeTaskState(content string, sessionTurns int, agentSkills []string) TaskSynthesis {
	intent := classifyIntent(content)
	complexity := classifyComplexity(content, sessionTurns)
	capTokens := capabilityTokens(content)
	fit, missing := matchCapabilities(capTokens, agentSkills)

	action := "execute_directly"
	confidence := 0.8

	// Delegation heuristic (mirrors Rust's planner logic).
	if complexity == "complex" || complexity == "specialist" {
		if fit < 0.3 && len(missing) > 0 {
			action = "compose_subagent"
			confidence = 0.6
		} else if fit < 0.7 {
			action = "delegate_to_specialist"
			confidence = 0.65
		}
	}

	// Retrieval decision: action turns only pull memory when there is an
	// explicit continuity/context signal. Simple direct tasks should not widen
	// into memory-bearing autonomous turns by intent label alone.
	retrievalNeeded, retrievalReason, proceduralUncertainty := shouldRetrieveForTurn(intent, content, sessionTurns, fit, missing, action, complexity)

	result := TaskSynthesis{
		Intent:               intent,
		Complexity:           complexity,
		PlannedAction:        action,
		Confidence:           confidence,
		RetrievalNeeded:      retrievalNeeded,
		RetrievalReason:      retrievalReason,
		ProceduralUncertainty: proceduralUncertainty,
		MissingSkills:        missing,
		CapabilityFit:        fit,
	}

	log.Debug().
		Str("intent", intent).
		Str("complexity", complexity).
		Str("action", action).
		Float64("confidence", confidence).
		Float64("capability_fit", fit).
		Bool("retrieval", retrievalNeeded).
		Bool("procedural_uncertainty", proceduralUncertainty).
		Str("retrieval_reason", retrievalReason).
		Msg("task state synthesized")

	return result
}

func shouldRetrieveForTurn(intent, content string, sessionTurns int, capabilityFit float64, missingSkills []string, plannedAction, complexity string) (bool, string, bool) {
	lower := strings.ToLower(content)
	proceduralUncertainty := appliedLearningHelpful(intent, lower, capabilityFit, missingSkills, plannedAction, complexity)

	if intent == "question" {
		return true, "question_default", proceduralUncertainty
	}

	if intent == "task" || intent == "code" {
		if taskNeedsPriorContext(lower) || taskNeedsEvidence(lower) {
			return true, "continuity_or_evidence", proceduralUncertainty
		}
		if proceduralUncertainty {
			return true, "applied_learning_uncertainty", true
		}
	}

	if sessionTurns > 3 && turnCarriesContinuityCue(lower) {
		return true, "session_continuity", proceduralUncertainty
	}

	return false, "none", proceduralUncertainty
}

func appliedLearningHelpful(intent, lower string, capabilityFit float64, missingSkills []string, plannedAction, complexity string) bool {
	if intent != "task" && intent != "code" {
		return false
	}
	if looksLikeBoundedAuthoringTask(lower) {
		return false
	}
	if hasProceduralLearningCue(lower) {
		return true
	}
	if plannedAction != "execute_directly" {
		return false
	}
	if capabilityFit >= 0.45 {
		return false
	}
	if complexity == "moderate" || complexity == "complex" || complexity == "specialist" {
		return true
	}
	return len(missingSkills) >= 2
}

func hasProceduralLearningCue(lower string) bool {
	proceduralMarkers := []string{
		"how do i", "how to", "steps to", "procedure", "runbook",
		"playbook", "workflow", "process for",
	}
	return containsAnyMarker(lower, proceduralMarkers)
}

func taskNeedsPriorContext(lower string) bool {
	contextMarkers := []string{
		"previous", "earlier", "existing", "current", "continue", "follow up", "follow-up",
		"we discussed", "as before", "same as", "based on", "from the session", "in the session",
		"remember", "context", "history", "prior", "last time", "again",
		"update", "revise", "modify", "edit", "append", "resume",
	}
	for _, marker := range contextMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func taskNeedsEvidence(lower string) bool {
	evidenceMarkers := []string{
		"report", "analysis", "analyze", "explain", "identify",
		"root cause", "affected", "which systems", "what happened",
		"why this happened", "summarize", "summary", "investigate",
	}
	for _, marker := range evidenceMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func turnCarriesContinuityCue(lower string) bool {
	cues := []string{
		"previous", "earlier", "we discussed", "as before", "same as",
		"continue", "follow up", "follow-up", "remember", "history",
		"context", "last time", "resume",
	}
	for _, cue := range cues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// classifyIntent determines the user's intent from message content.
// Matches Rust's semantic classifier categories.
func classifyIntent(content string) string {
	lower := strings.ToLower(content)

	// Short phatic / social turns are not information-seeking even when they
	// contain question words like "what's" or "how's".
	if isSocialConversationalTurn(lower) {
		return "conversational"
	}

	// Question patterns.
	questionMarkers := []string{"what", "how", "why", "when", "where", "who", "which", "can you explain", "tell me"}
	for _, m := range questionMarkers {
		if strings.HasPrefix(lower, m) || strings.Contains(lower, "?") {
			return "question"
		}
	}

	// Code/technical patterns.
	codeMarkers := []string{
		"write code", "implement", "function", "class", "api",
		"debug", "fix bug", "refactor", "unit test", "test suite",
		"write tests", "failing test", "run the tests",
	}
	for _, m := range codeMarkers {
		if containsIntentMarker(lower, m) {
			return "code"
		}
	}

	// Task/action patterns.
	taskMarkers := []string{"create", "build", "make", "set up", "configure", "install", "deploy", "update", "delete", "remove", "send", "schedule"}
	for _, m := range taskMarkers {
		if containsIntentMarker(lower, m) {
			return "task"
		}
	}

	// Creative patterns.
	creativeMarkers := []string{"write", "compose", "draft", "generate", "brainstorm", "design", "story", "poem"}
	for _, m := range creativeMarkers {
		if containsIntentMarker(lower, m) {
			return "creative"
		}
	}

	// Conversational.
	if len(content) < 50 {
		return "conversational"
	}

	return "general"
}

func isSocialConversationalTurn(lower string) bool {
	lower = strings.TrimSpace(lower)
	if lower == "" {
		return false
	}

	normalized := normalizeIntentLexicon(lower)
	padded := " " + normalized + " "
	socialMarkers := []string{
		"hello", "hi", "hey", "thanks", "thank you",
		"good morning", "good afternoon", "good evening",
		"how are you", "how's it going", "hows it going",
		"what's new", "whats new", "what is new",
		"anything new", "anything new with you",
		"what's up", "whats up", "what is up",
		"what's going on", "whats going on",
		"what's shakin", "whats shakin",
		"what's shaking", "whats shaking",
		"what's the good word", "whats the good word",
	}
	for _, marker := range socialMarkers {
		if strings.Contains(padded, " "+normalizeIntentLexicon(marker)+" ") {
			return true
		}
	}

	// Very short, clearly phatic turns should stay conversational even when
	// phrased informally as a question.
	words := len(strings.Fields(lower))
	return words <= 6 && (strings.HasPrefix(lower, "yo") || strings.HasPrefix(lower, "sup"))
}

func normalizeIntentLexicon(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func containsIntentMarker(lower, marker string) bool {
	if strings.TrimSpace(lower) == "" || strings.TrimSpace(marker) == "" {
		return false
	}
	padded := " " + normalizeIntentLexicon(lower) + " "
	needle := " " + normalizeIntentLexicon(marker) + " "
	return strings.Contains(padded, needle)
}

// classifyComplexity estimates task complexity from content and session context.
// Matches Rust's classify_complexity with feature extraction.
func classifyComplexity(content string, sessionTurns int) string {
	words := len(strings.Fields(content))
	lower := strings.ToLower(content)
	artifactCount := boundedAuthoringArtifactCount(lower)

	// Direct artifact authoring/editing requests should not get upcast on word
	// count or embedded artifact-body structure alone. Multiple explicit
	// artifacts can still be bounded direct work.
	if looksLikeBoundedAuthoringTask(lower) {
		if artifactCount > 1 {
			return "moderate"
		}
		return "simple"
	}

	subtasks := extractSubtasks(content)

	// Multi-step tasks are inherently more complex.
	if len(subtasks) >= 3 {
		return "complex"
	}

	// Long, detailed requests.
	if words > 100 {
		return "complex"
	}

	// Medium-length requests with technical markers.
	techMarkers := []string{"integrate", "migrate", "architecture", "system", "pipeline", "workflow"}
	techCount := 0
	for _, m := range techMarkers {
		if strings.Contains(lower, m) {
			techCount++
		}
	}
	if techCount >= 2 || (words > 40 && techCount >= 1) {
		return "complex"
	}

	if words > 30 || len(subtasks) > 0 {
		return "moderate"
	}

	return "simple"
}

func looksLikeSingleStepAuthoringTask(lower string, subtaskCount int) bool {
	return looksLikeBoundedAuthoringTask(lower) && boundedAuthoringArtifactCount(lower) <= 1
}

func looksLikeBoundedAuthoringTask(lower string) bool {
	actionMarkers := []string{
		"create", "write", "draft", "make", "add", "update", "edit",
	}
	artifactMarkers := []string{
		"note", "document", "doc", "markdown", ".md", "file", "vault", "obsidian",
	}
	if !containsAnyMarker(lower, actionMarkers) || !containsAnyMarker(lower, artifactMarkers) {
		return false
	}
	artifactCount := boundedAuthoringArtifactCount(lower)
	if artifactCount == 0 || artifactCount > 3 {
		return false
	}

	complexityEscalators := []string{
		"analyze", "analysis", "investigate", "report", "summarize", "summary",
		"root cause", "compare", "evaluate", "plan", "strategy", "workflow",
		"system", "architecture", "pipeline", "debug", "fix bug", "implement",
	}
	return !containsAnyMarker(lower, complexityEscalators)
}

var authoringFilePattern = regexp.MustCompile(`\b[a-z0-9][a-z0-9._-]*\.(md|markdown|txt|json|yaml|yml|toml)\b`)

func boundedAuthoringArtifactCount(lower string) int {
	matches := authoringFilePattern.FindAllString(lower, -1)
	if len(matches) > 0 {
		seen := make(map[string]struct{}, len(matches))
		for _, match := range matches {
			seen[match] = struct{}{}
		}
		return len(seen)
	}

	quantifiedArtifacts := []struct {
		markers []string
		count   int
	}{
		{[]string{"two notes", "two files", "two documents", "2 notes", "2 files", "2 documents"}, 2},
		{[]string{"three notes", "three files", "three documents", "3 notes", "3 files", "3 documents"}, 3},
	}
	for _, qa := range quantifiedArtifacts {
		if containsAnyMarker(lower, qa.markers) {
			return qa.count
		}
	}

	if containsAnyMarker(lower, []string{"note", "notes", "file", "files", "document", "documents", "doc", "docs"}) {
		return 1
	}
	return 0
}

func containsAnyMarker(lower string, markers []string) bool {
	for _, marker := range markers {
		if containsIntentMarker(lower, marker) {
			return true
		}
	}
	return false
}

// capabilityTokens extracts capability-relevant tokens from user content.
// Matches Rust's capability_tokens: non-alphanumeric split, min 4 chars.
func capabilityTokens(content string) []string {
	words := lexiconTokens(content)
	seen := make(map[string]struct{}, len(words))
	var tokens []string
	for _, w := range words {
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		tokens = append(tokens, w)
	}
	return tokens
}

func lexiconTokens(content string) []string {
	words := strings.FieldsFunc(strings.ToLower(content), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	var tokens []string
	for _, w := range words {
		if len(w) < 4 {
			continue
		}
		tokens = append(tokens, w)
	}
	return tokens
}

// PlannedActionKind enumerates the possible planned actions from task synthesis.
// Matches Rust's PlannedAction enum.
type PlannedActionKind int

const (
	PlannedActionExecuteDirectly      PlannedActionKind = iota // Handle in current agent
	PlannedActionDelegateToSpecialist                          // Route to a specialist subagent
	PlannedActionComposeSubagent                               // Create a new subagent on the fly
)

// FormatPlannedAction converts a PlannedAction string (from SynthesizeTaskState) to
// a human-readable label. Matches Rust's format_planned_action().
func FormatPlannedAction(action string) string {
	switch action {
	case "execute_directly":
		return "Execute Directly"
	case "delegate_to_specialist":
		return "Delegate to Specialist"
	case "compose_subagent":
		return "Compose Sub-Agent"
	default:
		return "Execute Directly"
	}
}

// PlannedActionToKind parses a planned action string into a typed enum.
func PlannedActionToKind(action string) PlannedActionKind {
	switch action {
	case "delegate_to_specialist":
		return PlannedActionDelegateToSpecialist
	case "compose_subagent":
		return PlannedActionComposeSubagent
	default:
		return PlannedActionExecuteDirectly
	}
}

// ActionGateDecision is the result of MapPlannedAction — tells the pipeline
// whether to continue with standard inference or reroute.
type ActionGateDecision int

const (
	ActionGateContinue          ActionGateDecision = iota // Proceed with standard inference
	ActionGateDelegate                                    // Reroute to delegation path
	ActionGateSpecialistPropose                           // Propose specialist creation
)

// MapPlannedAction maps a PlannedAction + decomposition result into a gate decision.
// Matches Rust's map_planned_action(): integrates task synthesis with the
// decomposition gate to decide whether standard inference, delegation, or
// specialist creation is the right path.
//
// The decision is conservative: delegation/specialist only fires when both the
// planner AND the decomposition gate agree (or when the planner has high confidence
// and the decomposition gate didn't explicitly centralize).
func MapPlannedAction(synthesis TaskSynthesis, decomp *DecompositionResult) ActionGateDecision {
	kind := PlannedActionToKind(synthesis.PlannedAction)

	switch kind {
	case PlannedActionDelegateToSpecialist:
		// Planner wants delegation. Accept if decomp agrees or is at least not centralized.
		if decomp != nil && decomp.Decision == DecompDelegated {
			return ActionGateDelegate
		}
		// High-confidence planner overrides neutral decomposition.
		if synthesis.Confidence >= 0.7 && (decomp == nil || decomp.Decision != DecompCentralized) {
			return ActionGateDelegate
		}
		// Low-confidence planner: fall through to standard inference.
		return ActionGateContinue

	case PlannedActionComposeSubagent:
		// Subagent composition requires both planner confidence and capability gap.
		if synthesis.CapabilityFit < 0.3 && synthesis.Confidence >= 0.6 {
			return ActionGateSpecialistPropose
		}
		// If decomp explicitly proposed a specialist, honor it.
		if decomp != nil && decomp.Decision == DecompSpecialistProposal {
			return ActionGateSpecialistPropose
		}
		return ActionGateContinue

	default:
		// PlannedActionExecuteDirectly — always continue.
		return ActionGateContinue
	}
}

// RetrievalStrategy describes the memory retrieval approach for a turn.
// Matches Rust's decide_retrieval_strategy() output.
type RetrievalStrategy struct {
	Strategy string // "semantic", "recency", "hybrid", "none"
	Budget   int    // Token budget for retrieval
	Reason   string // Why this strategy was chosen
}

// DecideRetrievalStrategy determines the optimal memory retrieval approach
// based on task synthesis results and session context.
// Matches Rust's decide_retrieval_strategy(): a separate decision function
// that decouples retrieval policy from retrieval execution (H10 stage separation).
func DecideRetrievalStrategy(synthesis TaskSynthesis, sessionTurns int, defaultBudget int) RetrievalStrategy {
	// No retrieval for simple conversational turns without history.
	if !synthesis.RetrievalNeeded && sessionTurns <= 1 {
		return RetrievalStrategy{Strategy: "none", Budget: 0, Reason: "simple conversational turn, no history"}
	}

	// Questions benefit from semantic retrieval (find relevant memories).
	if synthesis.Intent == "question" {
		budget := defaultBudget
		if synthesis.Complexity == "complex" {
			budget = defaultBudget * 2 // Double budget for complex questions.
		}
		return RetrievalStrategy{Strategy: "semantic", Budget: budget, Reason: "question intent benefits from semantic search"}
	}

	// Code and task intents need hybrid retrieval (recent context + semantic).
	if synthesis.Intent == "code" || synthesis.Intent == "task" {
		return RetrievalStrategy{Strategy: "hybrid", Budget: defaultBudget, Reason: "task/code intent needs recent + semantic context"}
	}

	// Long conversations need recency retrieval to maintain coherence.
	if sessionTurns > 10 {
		return RetrievalStrategy{Strategy: "recency", Budget: defaultBudget / 2, Reason: "long conversation, prioritize recent context"}
	}

	// Default: semantic for anything with retrieval need.
	if synthesis.RetrievalNeeded {
		return RetrievalStrategy{Strategy: "semantic", Budget: defaultBudget, Reason: "retrieval needed based on task analysis"}
	}

	return RetrievalStrategy{Strategy: "none", Budget: 0, Reason: "no retrieval benefit detected"}
}

// matchCapabilities compares capability tokens against available skills.
// Returns (fit_ratio, missing_skills).
func matchCapabilities(capTokens, skills []string) (float64, []string) {
	if len(capTokens) == 0 {
		return 1.0, nil
	}

	skillSet := make(map[string]bool, len(skills))
	for _, s := range skills {
		for _, word := range lexiconTokens(s) {
			skillSet[word] = true
		}
	}

	matched := 0
	var missing []string
	for _, tok := range capTokens {
		if skillSet[tok] {
			matched++
		} else {
			missing = append(missing, tok)
		}
	}

	fit := float64(matched) / float64(len(capTokens))
	return fit, missing
}
