package pipeline

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog/log"

	"roboticus/internal/agent"
	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/core"
	"roboticus/internal/db"
)

// PostTurnIngest runs background work after a turn completes.
// Matches Rust's post_turn_ingest: memory ingest and embedding generation.
// All work is submitted to the background worker pool so the response is not delayed.
func (p *Pipeline) PostTurnIngest(ctx context.Context, session *Session, turnID string, assistantContent string) {
	if p.bgWorker == nil {
		return
	}
	if assistantContent == "" {
		return
	}

	sessionID := session.ID

	// Extract the user content from the last user message for the ingest pair.
	var userContent string
	msgs := session.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			userContent = msgs[i].Content
			break
		}
	}

	p.bgWorker.Submit("postTurnIngest", func(bgCtx context.Context) {
		// 1. Generate and store embeddings for the assistant response.
		// Chunk the response and embed each chunk for ANN search.
		if p.store != nil {
			chunks := ChunkText(assistantContent, 512)
			for _, chunk := range chunks {
				p.storeChunkEmbedding(bgCtx, sessionID, turnID, chunk)
			}
		}

		// 2. Context checkpoint (periodic, Rust: save_checkpoint).
		p.maybeCheckpoint(bgCtx, session, turnID)

		// 3. Observer subagent dispatch (Rust: role="observer" receives turn summary).
		p.dispatchToObservers(bgCtx, sessionID, turnID, userContent, assistantContent)

		// 4. Reflection: generate structured episode summary (agentic architecture Layer 16).
		p.reflectOnTurn(bgCtx, userContent, session)

		// 5. Procedure detection: extract tool sequences and persist learned skills.
		p.detectAndPersistProcedures(bgCtx, session)

		// 5b. Executive state growth: record verified conclusions for answered
		// subgoals that have evidence support, open unresolved questions for
		// subgoals the turn could not close, and resolve any prior unresolved
		// questions the agent has now answered.
		p.growExecutiveState(bgCtx, session, assistantContent)

		// 6. Log the turn pair for analytics/debugging.
		if userContent != "" {
			log.Trace().
				Str("session", sessionID).
				Str("turn", turnID).
				Int("user_len", len(userContent)).
				Int("assistant_len", len(assistantContent)).
				Int("chunks", len(ChunkText(assistantContent, 512))).
				Msg("post-turn ingest completed")
		}
	})
}

// dispatchToObservers sends a turn summary to all observer subagents.
// Matches Rust's post_turn_ingest observer dispatch: finds role="observer"
// subagents, ingests the turn as episodic memory attributed to each observer,
// and touches their last_used_at timestamp.
func (p *Pipeline) dispatchToObservers(ctx context.Context, sessionID, turnID, userContent, assistantContent string) {
	if p.store == nil {
		return
	}

	// Find observer subagents.
	rows, err := p.store.QueryContext(ctx,
		`SELECT id, name FROM sub_agents WHERE role = 'observer' AND enabled = 1`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	// Build turn summary for observers.
	userSnippet := userContent
	if len(userSnippet) > 500 {
		userSnippet = userSnippet[:500]
	}
	assistantSnippet := assistantContent
	if len(assistantSnippet) > 1000 {
		assistantSnippet = assistantSnippet[:1000]
	}
	summary := "[Turn Observation] User: " + userSnippet + "\nAssistant: " + assistantSnippet

	for rows.Next() {
		var observerID, observerName string
		if err := rows.Scan(&observerID, &observerName); err != nil {
			continue
		}

		// Ingest as episodic memory attributed to the observer.
		if _, err := p.store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance, owner_id)
			 VALUES (?, 'observation', ?, 4, ?)`,
			db.NewID(), summary, observerID,
		); err != nil {
			p.errBus.ReportIfErr(err, "pipeline", "store_observer_memory", core.SevWarning)
		}

		// Touch last_used_at timestamp.
		if _, err := p.store.ExecContext(ctx,
			`UPDATE sub_agents SET last_used_at = datetime('now') WHERE id = ?`,
			observerID,
		); err != nil {
			p.errBus.ReportIfErr(err, "pipeline", "touch_observer_timestamp", core.SevDebug)
		}

		log.Trace().Str("observer", observerName).Str("session", sessionID).Msg("observer subagent received turn summary")
	}
}

// storeChunkEmbedding generates an embedding for a text chunk and stores it
// in the embeddings table for ANN search. Falls back to n-gram hashing if
// no embedding provider is configured.
func (p *Pipeline) storeChunkEmbedding(ctx context.Context, sessionID, turnID, chunk string) {
	if p.store == nil {
		return
	}

	// Use the LLM service's embedding client if available, otherwise skip.
	// The embedding client with nil provider falls back to n-gram hashing,
	// which is what we want for local/offline operation.
	embedClient := p.embeddingClient()
	if embedClient == nil {
		return
	}

	vec, err := embedClient.EmbedSingle(ctx, chunk)
	if err != nil {
		log.Warn().Err(err).Str("session", sessionID).Msg("embedding generation failed")
		return
	}

	blob := db.EmbeddingToBlob(vec)

	id := db.NewID()
	_, err = p.store.ExecContext(ctx,
		`INSERT INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
		 VALUES (?, 'session_messages', ?, ?, ?, ?)`,
		id, turnID, truncatePreview(chunk, 200), blob, len(vec),
	)
	if err != nil {
		log.Warn().Err(err).Str("session", sessionID).Msg("embedding storage failed")
	}
}

// reflectOnTurn generates a structured episode summary and stores it as
// episodic memory. Wires reflection.go into the post-turn pipeline.
func (p *Pipeline) reflectOnTurn(ctx context.Context, userContent string, session *Session) {
	if p.store == nil || userContent == "" {
		return
	}

	// Extract tool events from session messages.
	// Track success/failure: a tool call is followed by a tool result message.
	// If the result starts with error patterns, mark as failure.
	var toolEvents []agentmemory.ToolEvent
	var errorMessages []string
	msgs := session.Messages()
	for i, msg := range msgs {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				success := true
				// Look ahead for the tool result message.
				if i+1 < len(msgs) && msgs[i+1].Role == "tool" {
					result := strings.ToLower(msgs[i+1].Content)
					if strings.HasPrefix(result, "error") || strings.HasPrefix(result, "failed") ||
						strings.HasPrefix(result, `{"error`) {
						success = false
						errorMessages = append(errorMessages, strings.TrimSpace(msgs[i+1].Content))
					}
				}
				toolEvents = append(toolEvents, agentmemory.ToolEvent{
					ToolName: tc.Function.Name,
					Success:  success,
				})
			}
		}
	}

	// Estimate turn duration from session timestamps.
	var turnDuration time.Duration
	if len(msgs) >= 2 {
		// Use session's created_at as proxy (actual turn timing would require
		// the turn record, which isn't available in the session struct).
		turnDuration = 0 // TODO: wire actual turn start time from pipeline context
	}

	// Enriched reflection: pass evidence items and verifier outcome so the
	// summary captures evidence refs, fix patterns, failed hypotheses, and
	// a blended result-quality score.
	verifyCtx := BuildVerificationContext(session)
	verifyResult := VerifyResponse(session.LastAssistantContent(), verifyCtx)
	summary := agentmemory.AnalyzeEpisode(agentmemory.EpisodeInput{
		UserContent:     userContent,
		AssistantAnswer: session.LastAssistantContent(),
		ToolEvents:      toolEvents,
		EvidenceItems:   verifyCtx.EvidenceItems,
		VerifierPassed:  verifyResult.Passed,
		ErrorMessages:   errorMessages,
		Duration:        turnDuration,
	})
	if summary == nil {
		return
	}

	// Store as episodic memory with high importance.
	formatted := summary.FormatForStorage()
	_, err := p.store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance)
		 VALUES (?, 'episode_summary', ?, 8)`,
		db.NewID(), formatted)
	if err != nil {
		log.Debug().Err(err).Msg("reflection: failed to store episode summary")
	} else {
		log.Debug().Str("outcome", summary.Outcome).Int("actions", len(summary.Actions)).
			Msg("reflection: episode summary stored")
	}
}

// ExecutiveGrowthResult reports what the auto-grow pass wrote for a turn.
type ExecutiveGrowthResult struct {
	TaskID              string
	VerifiedRecorded    int
	QuestionsOpened     int
	QuestionsResolved   int
	AssumptionsRecorded int
}

// growExecutiveState converts the outcome of the current turn into structured
// executive-state entries on the active task. Verified conclusions record the
// subgoals that were both covered in the response and supported by retrieved
// evidence. Unresolved questions record the subgoals the turn could not close.
// Existing unresolved questions whose keywords now appear in the response are
// resolved so the task graph stays current.
//
// Returns an ExecutiveGrowthResult so callers and tests can see what was
// written. All writes are also surfaced via structured logs with the
// "executive_write" category so operators can audit growth decisions even
// though post-turn runs after the trace is closed.
func (p *Pipeline) growExecutiveState(ctx context.Context, session *Session, assistantContent string) ExecutiveGrowthResult {
	result := ExecutiveGrowthResult{}
	if p.store == nil || session == nil {
		return result
	}
	content := strings.TrimSpace(assistantContent)
	if content == "" {
		return result
	}

	vctx := BuildVerificationContext(session)
	if len(vctx.Subgoals) == 0 {
		return result
	}

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	state, err := mm.LoadExecutiveState(ctx, session.ID, "")
	if err != nil {
		log.Debug().Err(err).Msg("executive: load state for growth failed")
		return result
	}
	if state == nil || state.TaskID == "" {
		return result
	}
	taskID := state.TaskID
	result.TaskID = taskID
	lowerResponse := strings.ToLower(content)

	// Record verified conclusions for subgoals that pass the same support
	// checks the verifier uses to avoid writing premature conclusions.
	for _, goal := range vctx.Subgoals {
		trimmed := strings.TrimSpace(goal)
		if trimmed == "" {
			continue
		}
		if !verificationGoalCovered(trimmed, lowerResponse) {
			continue
		}
		if len(vctx.EvidenceItems) == 0 {
			continue
		}
		if verificationGoalAllowsPlanInference(trimmed) {
			// Remediation / plan subgoals get their own check — do not record
			// them as verified conclusions unless tool-grounded.
			continue
		}
		if !verificationGoalSupportedByEvidence(trimmed, lowerResponse, vctx.EvidenceItems) {
			continue
		}
		contentEntry := "subgoal verified: " + truncate(trimmed, 120)
		exists, err := mm.HasExecutiveEntry(ctx, session.ID, taskID, agentmemory.EntryVerifiedConclusion, contentEntry)
		if err != nil {
			log.Debug().Err(err).Msg("executive: duplicate check failed")
			continue
		}
		if exists {
			continue
		}
		payload := agentmemory.VerifiedConclusionPayload{
			SupportingEvidence: vctx.EvidenceItems,
			VerifiedAt:         time.Now().UTC().Format(time.RFC3339),
		}
		if err := mm.RecordVerifiedConclusion(ctx, session.ID, taskID, contentEntry, payload); err != nil {
			log.Debug().Err(err).Msg("executive: record verified conclusion failed")
			continue
		}
		result.VerifiedRecorded++
		log.Debug().
			Str("session", session.ID).
			Str("task", taskID).
			Str("subgoal", trimmed).
			Str("category", "executive_write").
			Msg("executive verified_conclusion recorded")
	}

	// Record unresolved questions for subgoals that are not covered by the
	// response or that the response explicitly punted on.
	uncertaintyPresent := containsAny(lowerResponse,
		"don't know", "do not know", "unclear", "not enough", "insufficient",
		"need more", "i'm not certain", "we're not certain", "cannot confirm",
	)
	for _, goal := range vctx.Subgoals {
		trimmed := strings.TrimSpace(goal)
		if trimmed == "" {
			continue
		}
		covered := verificationGoalCovered(trimmed, lowerResponse)
		if covered && !uncertaintyPresent {
			continue
		}
		contentEntry := "unresolved: " + truncate(trimmed, 120)
		exists, err := mm.HasExecutiveEntry(ctx, session.ID, taskID, agentmemory.EntryUnresolvedQuestion, contentEntry)
		if err != nil {
			log.Debug().Err(err).Msg("executive: duplicate check failed")
			continue
		}
		if exists {
			continue
		}
		payload := agentmemory.UnresolvedQuestionPayload{
			BlockingSubgoal: truncate(trimmed, 120),
		}
		if err := mm.RecordUnresolvedQuestion(ctx, session.ID, taskID, contentEntry, payload); err != nil {
			log.Debug().Err(err).Msg("executive: record unresolved question failed")
			continue
		}
		result.QuestionsOpened++
		log.Debug().
			Str("session", session.ID).
			Str("task", taskID).
			Str("subgoal", trimmed).
			Str("category", "executive_write").
			Msg("executive unresolved_question opened")
	}

	// Resolve any prior unresolved question whose keywords now appear in the
	// response with enough confidence to consider it answered.
	for _, q := range state.UnresolvedQuestions {
		keywords := verificationKeywords(q.Content)
		if len(keywords) == 0 {
			continue
		}
		matches := 0
		for _, kw := range keywords {
			if strings.Contains(lowerResponse, kw) {
				matches++
			}
		}
		threshold := 1
		if len(keywords) >= 4 {
			threshold = 2
		}
		if matches < threshold {
			continue
		}
		// Only resolve if the response is not itself uncertain about this item.
		if uncertaintyPresent {
			continue
		}
		if err := mm.ResolveQuestion(ctx, session.ID, taskID, q.ID); err != nil {
			log.Debug().Err(err).Msg("executive: resolve question failed")
			continue
		}
		result.QuestionsResolved++
		log.Debug().
			Str("session", session.ID).
			Str("task", taskID).
			Str("question", q.Content).
			Str("category", "executive_write").
			Msg("executive unresolved_question resolved")
	}

	// Record assumptions the agent named explicitly in the response.
	for _, assumption := range extractAssumptions(content) {
		trimmed := strings.TrimSpace(assumption)
		if trimmed == "" {
			continue
		}
		entryContent := "assumption: " + truncate(trimmed, 160)
		exists, err := mm.HasExecutiveEntry(ctx, session.ID, taskID, agentmemory.EntryAssumption, entryContent)
		if err != nil {
			log.Debug().Err(err).Msg("executive: duplicate check failed for assumption")
			continue
		}
		if exists {
			continue
		}
		payload := agentmemory.AssumptionPayload{
			Source:     "response",
			Confidence: 0.5,
		}
		if err := mm.RecordAssumption(ctx, session.ID, taskID, entryContent, payload); err != nil {
			log.Debug().Err(err).Msg("executive: record assumption failed")
			continue
		}
		result.AssumptionsRecorded++
		log.Debug().
			Str("session", session.ID).
			Str("task", taskID).
			Str("assumption", trimmed).
			Str("category", "executive_write").
			Msg("executive assumption recorded")
	}

	if result.VerifiedRecorded+result.QuestionsOpened+result.QuestionsResolved+result.AssumptionsRecorded > 0 {
		log.Info().
			Str("session", session.ID).
			Str("task", taskID).
			Int("verified", result.VerifiedRecorded).
			Int("questions_opened", result.QuestionsOpened).
			Int("questions_resolved", result.QuestionsResolved).
			Int("assumptions", result.AssumptionsRecorded).
			Str("category", "executive_growth").
			Msg("executive state grown after turn")
	}
	return result
}

// assumptionMarkers are phrases that frequently precede an explicit
// assumption in natural-language text. The scan is case-insensitive.
var assumptionMarkers = []string{
	"assuming that ",
	"i'll assume ",
	"i will assume ",
	"i am assuming ",
	"i'm assuming ",
	"my assumption is that ",
	"we assume ",
	"assuming ",
	"presuming that ",
	"presumably, ",
	"if we assume ",
	"based on the assumption that ",
}

// extractAssumptions scans a response for explicit assumption markers and
// returns the clause following each marker. Returned clauses are deduplicated
// and trimmed to sentence-length content.
func extractAssumptions(response string) []string {
	lower := strings.ToLower(response)
	seen := make(map[string]struct{})
	var out []string
	for _, marker := range assumptionMarkers {
		start := 0
		for {
			idx := strings.Index(lower[start:], marker)
			if idx < 0 {
				break
			}
			abs := start + idx + len(marker)
			if abs >= len(response) {
				break
			}
			// Skip word-boundary false positives (e.g., "reassuming").
			if idx > 0 {
				prev := lower[start+idx-1]
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
					start = abs
					continue
				}
			}
			clause := extractAssumptionClause(response[abs:])
			if clause != "" {
				key := strings.ToLower(strings.TrimSpace(clause))
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					out = append(out, clause)
				}
			}
			start = abs
		}
	}
	return out
}

// extractAssumptionClause returns the clause following an assumption marker,
// bounded by sentence-ending punctuation or a newline.
func extractAssumptionClause(rest string) string {
	end := len(rest)
	for i, r := range rest {
		if r == '.' || r == '\n' || r == '!' || r == '?' || r == ';' {
			end = i
			break
		}
	}
	clause := strings.TrimSpace(rest[:end])
	if clause == "" || len(clause) < 4 {
		return ""
	}
	// Avoid picking up purely modal leftovers like "we are" after "assuming".
	if len(clause) > 200 {
		clause = clause[:200]
	}
	return clause
}

// detectAndPersistProcedures uses LearningExtractor's sliding-window procedure
// detection to find recurring multi-step tool sequences and persist them.
// This wires the full agent.LearningExtractor into production.
func (p *Pipeline) detectAndPersistProcedures(ctx context.Context, session *Session) {
	if p.store == nil {
		return
	}

	msgs := session.Messages()
	if len(msgs) < 3 {
		return
	}

	// Build tool call records from session messages.
	var calls []agent.ToolCallRecord
	for i, msg := range msgs {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				success := true
				if i+1 < len(msgs) && msgs[i+1].Role == "tool" {
					result := strings.ToLower(msgs[i+1].Content)
					success = !strings.HasPrefix(result, "error") &&
						!strings.HasPrefix(result, "failed") &&
						!strings.HasPrefix(result, `{"error`)
				}
				calls = append(calls, agent.ToolCallRecord{
					ToolName: tc.Function.Name,
					Success:  success,
				})
			}
		}
	}

	if len(calls) < 3 {
		return
	}

	// Use the full LearningExtractor for sliding-window detection.
	extractor := agent.NewLearningExtractor()
	candidates := extractor.DetectCandidateProcedures(calls)

	for _, proc := range candidates {
		agent.PersistLearnedSkill(ctx, p.store, proc)
		log.Debug().Str("procedure", strings.Join(proc.Steps, "-")).Int("count", proc.Count).
			Msg("procedure detection: persisted learned skill")

		// Promote repeatedly-observed procedures into a reusable workflow
		// record so procedural retrieval can surface them with steps and
		// metadata instead of only a tool-stat rollup.
		if proc.Count >= 3 {
			p.promoteProcedureToWorkflow(ctx, session, proc)
		}
	}
}

// promoteProcedureToWorkflow records a detected tool chain as a reusable
// workflow in procedural_memory. The session ID is written into the
// success_evidence list so operators can audit which run confirmed the
// promotion. Failures on the promotion path are logged but non-fatal — the
// learned_skills row is already persisted by the time we get here.
func (p *Pipeline) promoteProcedureToWorkflow(ctx context.Context, session *Session, proc agent.Procedure) {
	if p.store == nil {
		return
	}
	name := strings.Join(proc.Steps, " → ")
	if name == "" {
		return
	}
	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), p.store)
	if err := mm.RecordWorkflow(ctx, agentmemory.Workflow{
		Name:        name,
		Steps:       append([]string(nil), proc.Steps...),
		Category:    agentmemory.WorkflowCategoryWorkflow,
		Confidence:  0.75,
		ContextTags: []string{"auto_promoted", "tool_chain"},
	}); err != nil {
		log.Debug().Err(err).Str("workflow", name).Msg("workflow: promotion failed")
		return
	}
	evidence := session.ID
	if err := mm.RecordWorkflowSuccess(ctx, name, evidence); err != nil {
		log.Debug().Err(err).Str("workflow", name).Msg("workflow: success record failed")
		return
	}
	log.Info().
		Str("workflow", name).
		Int("count", proc.Count).
		Str("session", session.ID).
		Str("category", "workflow_promoted").
		Msg("procedure promoted to workflow")
}

// truncatePreview truncates text for storage as a content preview.
func truncatePreview(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ChunkText splits text into chunks at sentence boundaries, each up to maxChars.
// Exported for testing.
func ChunkText(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = 512
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxChars {
			chunks = append(chunks, strings.TrimSpace(remaining))
			break
		}

		// Find the best sentence boundary within the budget.
		cutPoint := findSentenceBoundary(remaining, maxChars)
		chunk := strings.TrimSpace(remaining[:cutPoint])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		remaining = strings.TrimSpace(remaining[cutPoint:])
	}

	return chunks
}

// findSentenceBoundary finds the best split point at a sentence boundary
// within maxChars. Falls back to word boundary, then hard cut.
func findSentenceBoundary(text string, maxChars int) int {
	if maxChars >= len(text) {
		return len(text)
	}

	// Look for sentence terminators (. ! ?) followed by space or end.
	bestSentence := -1
	for i := maxChars - 1; i > maxChars/3; i-- {
		if i >= len(text) {
			continue
		}
		r := rune(text[i])
		if (r == '.' || r == '!' || r == '?') && (i+1 >= len(text) || unicode.IsSpace(rune(text[i+1]))) {
			bestSentence = i + 1
			break
		}
	}
	if bestSentence > 0 {
		return bestSentence
	}

	// Fall back to word boundary.
	for i := maxChars; i > maxChars/2; i-- {
		if i < len(text) && unicode.IsSpace(rune(text[i])) {
			return i
		}
	}

	// Hard cut as last resort.
	return maxChars
}
