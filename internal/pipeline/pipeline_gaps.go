package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// ── Topic Tag Derivation ────────────────────────────────────────────────────
// Matches Rust's text_overlap_score + topic tag assignment.
// Messages with >0.3 text overlap continue the same topic; otherwise, a new
// topic is assigned (incrementing tag: "topic-1", "topic-2", etc.).

// deriveTopicTag computes the topic tag for a new message by comparing it
// against recent session messages using text overlap scoring.
func (p *Pipeline) deriveTopicTag(session *Session, content string) string {
	msgs := session.Messages()
	if len(msgs) == 0 {
		return "topic-1"
	}

	// Find the current topic tag from the most recent message.
	currentTag := "topic-1"

	// Compare with the most recent assistant/user message content.
	lastContent := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" || msgs[i].Role == "user" {
			lastContent = msgs[i].Content
			break
		}
	}
	if lastContent == "" {
		return currentTag
	}

	// Text overlap scoring (Rust: text_overlap_score).
	overlap := textOverlapScore(content, lastContent)
	if overlap > 0.3 {
		return currentTag // Continuation of same topic.
	}

	// New topic: scan DB for highest topic number and increment.
	if p.store != nil {
		var maxTag string
		row := p.store.QueryRowContext(context.Background(),
			`SELECT topic_tag FROM session_messages WHERE session_id = ? AND topic_tag IS NOT NULL
			 ORDER BY created_at DESC LIMIT 1`,
			session.ID,
		)
		if err := row.Scan(&maxTag); err == nil && strings.HasPrefix(maxTag, "topic-") {
			currentTag = maxTag
		}
	}

	// Parse topic number and increment.
	num := 1
	if _, err := parseTopicNum(currentTag); err == nil {
		num, _ = parseTopicNum(currentTag)
		num++
	}
	return topicTagFromNum(num)
}

// textOverlapScore computes the Jaccard similarity between two texts using
// word-level tokens. Returns 0.0–1.0. Matches Rust's text_overlap_score.
func textOverlapScore(a, b string) float64 {
	wordsA := tokenizeWords(a)
	wordsB := tokenizeWords(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}

	intersection := 0
	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		setB[w] = true
		if setA[w] {
			intersection++
		}
	}

	union := len(setA)
	for w := range setB {
		if !setA[w] {
			union++
		}
	}
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

func tokenizeWords(text string) []string {
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	// Filter short words.
	var result []string
	for _, w := range words {
		if len(w) >= 3 {
			result = append(result, w)
		}
	}
	return result
}

func parseTopicNum(tag string) (int, error) {
	var n int
	_, err := parseTopicNumHelper(tag, &n)
	return n, err
}

func parseTopicNumHelper(tag string, n *int) (bool, error) {
	if !strings.HasPrefix(tag, "topic-") {
		return false, nil
	}
	num := 0
	for _, c := range tag[6:] {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else {
			break
		}
	}
	*n = num
	return true, nil
}

func topicTagFromNum(n int) string {
	return fmt.Sprintf("topic-%d", n)
}

// ── Bot Command Dispatch ────────────────────────────────────────────────────
// Matches Rust's bot_command_dispatch: handles /commands before inference.

// tryBotCommand checks if the input is a bot command (starts with /) and
// dispatches it. Returns nil if not a command or if the command is unknown.
func (p *Pipeline) tryBotCommand(ctx context.Context, input Input) *Outcome {
	content := strings.TrimSpace(input.Content)
	if !strings.HasPrefix(content, "/") {
		return nil
	}

	parts := strings.SplitN(content, " ", 2)
	cmd := strings.ToLower(parts[0])
	// arg is unused for now but available for future commands.

	switch cmd {
	case "/help":
		return &Outcome{
			SessionID: input.SessionID,
			Content:   "Available commands: /help, /status, /whoami, /clear",
		}
	case "/whoami":
		return &Outcome{
			SessionID: input.SessionID,
			Content:   "You are communicating via " + input.Platform + " as " + input.SenderID,
		}
	case "/status":
		return &Outcome{
			SessionID: input.SessionID,
			Content:   "System operational. Agent: " + input.AgentName,
		}
	case "/clear":
		// Clear session context by adding a system message.
		return &Outcome{
			SessionID: input.SessionID,
			Content:   "Context cleared. Starting fresh.",
		}
	}

	return nil // Unknown command, fall through to normal processing.
}

// ── Delegation Execution ────────────────────────────────────────────────────
// Matches Rust's execute_delegation: orchestrate-subagents tool execution.

// executeDelegation attempts to execute delegated subtasks through the tool
// executor. Returns an Outcome if delegation succeeds, or nil to fall through
// to standard inference.
func (p *Pipeline) executeDelegation(ctx context.Context, session *Session, decomp *DecompositionResult, turnID string) *Outcome {
	if p.executor == nil || decomp == nil || len(decomp.Subtasks) == 0 {
		return nil
	}

	// Build the delegation prompt from subtasks.
	var sb strings.Builder
	sb.WriteString("Execute the following subtasks:\n")
	for i, st := range decomp.Subtasks {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, st))
	}

	// Inject delegation directive as a system message.
	session.AddSystemMessage(sb.String())

	// Run through the standard executor (which handles tool calls including
	// orchestrate-subagents if registered).
	result, turns, err := p.executor.RunLoop(ctx, session)
	if err != nil {
		log.Warn().Err(err).Str("session", session.ID).Msg("delegation execution failed, falling through to standard inference")
		return nil
	}

	// Evaluate delegation output quality (Rust: evaluate_output_heuristic).
	quality := evaluateOutputQuality(result, strings.Join(decomp.Subtasks, " "))
	if quality.Verdict == QualityRetry {
		// Retry once with quality feedback.
		session.AddSystemMessage("Your previous response was incomplete: " + quality.Reason + ". Please provide a complete response.")
		retryResult, retryTurns, retryErr := p.executor.RunLoop(ctx, session)
		if retryErr == nil {
			result = retryResult
			turns += retryTurns
		}
	}

	// Record delegation outcome (background).
	if p.bgWorker != nil {
		sessionID := session.ID
		subtaskCount := len(decomp.Subtasks)
		p.bgWorker.Submit("recordDelegation", func(bgCtx context.Context) {
			p.recordDelegationOutcome(bgCtx, sessionID, turnID, subtaskCount, quality)
		})
	}

	return &Outcome{
		SessionID:  session.ID,
		Content:    result,
		ReactTurns: turns,
	}
}

// recordDelegationOutcome persists delegation metrics to the database.
func (p *Pipeline) recordDelegationOutcome(ctx context.Context, sessionID, turnID string, subtaskCount int, quality QualityResult) {
	if p.store == nil {
		return
	}
	_, err := p.store.ExecContext(ctx,
		`INSERT OR IGNORE INTO delegation_outcomes (id, session_id, turn_id, subtask_count, pattern, quality_score, created_at)
		 VALUES (?, ?, ?, ?, 'fan-out', ?, datetime('now'))`,
		db.NewID(), sessionID, turnID, subtaskCount, quality.Score,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to record delegation outcome")
	}
}

// ── Quality Gate ────────────────────────────────────────────────────────────
// Matches Rust's evaluate_output_heuristic: checks delegation output quality.

// QualityVerdict indicates whether output is acceptable.
type QualityVerdict int

const (
	QualityPass QualityVerdict = iota
	QualityRetry
)

// QualityResult holds the output quality evaluation.
type QualityResult struct {
	Verdict QualityVerdict
	Score   int
	Reason  string
}

// evaluateOutputQuality checks delegation output for common quality issues.
// Matches Rust's 5-check heuristic with scoring.
func evaluateOutputQuality(output, taskDescription string) QualityResult {
	output = strings.TrimSpace(output)

	// Check 1: Empty or too short.
	if len(output) < 20 {
		return QualityResult{QualityRetry, 10, "response too short"}
	}

	// Check 2: Placeholder patterns.
	lower := strings.ToLower(output)
	placeholders := []string{"[insert", "[your", "lorem ipsum", "placeholder", "example here", "todo"}
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			return QualityResult{QualityRetry, 20, "contains placeholder content"}
		}
	}

	// Check 3: Hollow leads (filler without substance).
	hollowLeads := []string{"i'll help", "great question", "sure thing", "absolutely", "of course"}
	for _, h := range hollowLeads {
		if strings.HasPrefix(lower, h) && len(output) < 200 {
			return QualityResult{QualityRetry, 30, "hollow lead without substance"}
		}
	}

	// Check 4: Disproportionately short for complex task.
	taskWords := len(strings.Fields(taskDescription))
	outputWords := len(strings.Fields(output))
	if taskWords >= 15 && outputWords < 30 {
		return QualityResult{QualityRetry, 35, "output too brief for task complexity"}
	}

	// Check 5: Substantiveness scoring.
	score := 50
	if outputWords > 200 {
		score = 85
	} else if outputWords > 80 {
		score = 75
	} else if outputWords > 40 {
		score = 60
	}
	// Bonus for structured output.
	if strings.Contains(output, "\n") || strings.Contains(output, "```") ||
		strings.Contains(output, "- ") || strings.Contains(output, "1.") {
		score += 10
	}
	if score > 100 {
		score = 100
	}

	if score >= 50 {
		return QualityResult{QualityPass, score, ""}
	}
	return QualityResult{QualityRetry, score, "low substantiveness score"}
}

// ── Prefer Local Model ──────────────────────────────────────────────────────
// Matches Rust's prefer_local_model: scan fallbacks for a local provider.

func (p *Pipeline) findLocalModel() string {
	if p.llmSvc == nil {
		return ""
	}
	// Check provider status for local providers.
	for _, ps := range p.llmSvc.Status() {
		if ps.IsLocal && ps.State == 0 { // 0 = closed (healthy)
			return ps.Name
		}
	}
	return ""
}

// ── Shortcut Cost Recording ─────────────────────────────────────────────────
// Matches Rust's record_cost for shortcuts: 0 tokens, "shortcut" variant marker.

func (p *Pipeline) recordShortcutCost(ctx context.Context, turnID, sessionID, channel string) {
	if p.store == nil {
		return
	}
	_, _ = p.store.ExecContext(ctx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost, tier, turn_id, created_at)
		 VALUES (?, 'shortcut', 'shortcut', 0, 0, 0.0, 'shortcut', ?, datetime('now'))`,
		db.NewID(), turnID,
	)
}

// ── Context Checkpointing ───────────────────────────────────────────────────
// Matches Rust's periodic context checkpoint in post_turn_ingest.

const checkpointIntervalTurns = 10

// maybeCheckpoint saves a context checkpoint if the turn count hits the interval.
func (p *Pipeline) maybeCheckpoint(ctx context.Context, session *Session, turnID string) {
	if p.store == nil {
		return
	}

	turnCount := session.TurnCount()
	if turnCount == 0 || turnCount%checkpointIntervalTurns != 0 {
		return
	}

	// Collect system messages as memory summary.
	var summaryParts []string
	for _, m := range session.Messages() {
		if m.Role == "system" {
			summaryParts = append(summaryParts, m.Content)
		}
	}
	memorySummary := strings.Join(summaryParts, "\n---\n")
	if len(memorySummary) > 2000 {
		memorySummary = memorySummary[:2000]
	}

	// Last message as digest.
	digest := ""
	msgs := session.Messages()
	if len(msgs) > 0 {
		digest = msgs[len(msgs)-1].Content
		if len(digest) > 500 {
			digest = digest[:500]
		}
	}

	// System prompt hash for versioning.
	h := sha256.Sum256([]byte(memorySummary))
	promptHash := hex.EncodeToString(h[:8])

	_, err := p.store.ExecContext(ctx,
		`INSERT INTO context_checkpoints (id, session_id, system_prompt_hash, memory_summary, conversation_digest, turn_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		db.NewID(), session.ID, promptHash, memorySummary, digest, turnCount,
	)
	if err != nil {
		log.Warn().Err(err).Str("session", session.ID).Int("turn", turnCount).Msg("checkpoint save failed")
	} else {
		log.Debug().Str("session", session.ID).Int("turn", turnCount).Msg("context checkpoint saved")
	}
}
