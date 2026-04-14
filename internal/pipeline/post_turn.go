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

	summary := agentmemory.Reflect(userContent, toolEvents, turnDuration)
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
	}
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
