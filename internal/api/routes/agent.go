package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
)

// EventPublisher is an interface for publishing events (avoids import cycle with api package).
type EventPublisher interface {
	PublishEvent(eventType string, data any)
}

// agentMessageRequest is the JSON body for POST /api/agent/message.
type agentMessageRequest struct {
	Content   string `json:"content"`
	SessionID string `json:"session_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Model     string `json:"model,omitempty"`
}

// AgentMessage handles standard (non-streaming) inference requests.
func AgentMessage(p pipeline.Runner, agentName string, bus ...EventPublisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req agentMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Content == "" {
			writeError(w, http.StatusBadRequest, "content is required")
			return
		}
		if req.AgentID == "" {
			req.AgentID = "default"
		}

		input := pipeline.Input{
			Content:   req.Content,
			SessionID: req.SessionID,
			AgentID:   req.AgentID,
			AgentName: agentName,
			Platform:  "api",
		}

		outcome, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetAPI(), input)
		if err != nil {
			writeError(w, core.HTTPStatusForError(err), err.Error())
			return
		}

		// Publish to WebSocket event bus if available.
		if len(bus) > 0 && bus[0] != nil {
			bus[0].PublishEvent("agent.reply", map[string]any{
				"session_id": outcome.SessionID,
				"content":    outcome.Content,
				"model":      outcome.Model,
				"tokens_in":  outcome.TokensIn,
				"tokens_out": outcome.TokensOut,
			})
		}

		writeJSON(w, http.StatusOK, outcome)
	}
}

// AgentMessageStream handles SSE streaming inference requests.
// The pipeline prepares full context (session history, memory, tools, system prompt)
// via StreamPreparer, returned in outcome.StreamRequest. This handler only does
// SSE plumbing — it never builds its own LLM request.
func AgentMessageStream(p pipeline.Runner, llmSvc *llm.Service, agentName string, finalizer ...pipeline.StreamFinalizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req agentMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Content == "" {
			writeError(w, http.StatusBadRequest, "content is required")
			return
		}
		if req.AgentID == "" {
			req.AgentID = "default"
		}

		input := pipeline.Input{
			Content:   req.Content,
			SessionID: req.SessionID,
			AgentID:   req.AgentID,
			AgentName: agentName,
			Platform:  "api",
		}

		// Run pipeline: validates input, resolves session, runs injection defense,
		// stores user message, and prepares the full streaming LLM request with
		// session context, memory retrieval, tools, and system prompt.
		outcome, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetStreaming(), input)
		if err != nil {
			writeError(w, core.HTTPStatusForError(err), err.Error())
			return
		}

		if !outcome.Stream {
			// Non-stream result (e.g., cache hit, shortcut, skill match).
			writeJSON(w, http.StatusOK, outcome)
			return
		}

		// Use the pipeline-prepared request. This includes full session history,
		// system prompt, memory context, and tool definitions — identical context
		// to what standard (non-streaming) inference would use.
		streamReq := outcome.StreamRequest
		if streamReq == nil {
			// Fallback: if StreamPreparer was not wired, build a minimal request.
			// This is a degraded path — log it so operators notice.
			log.Warn().Msg("SSE streaming without StreamPreparer — context will be incomplete")
			streamReq = &llm.Request{
				Messages: []llm.Message{{Role: "user", Content: req.Content}},
			}
		}
		streamReq.Stream = true

		// Set up SSE headers.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		// Stream from LLM using the pipeline-prepared request.
		chunks, errs := llmSvc.Stream(r.Context(), streamReq)

		var fullContent strings.Builder
		for chunk := range chunks {
			fullContent.WriteString(chunk.Delta)
			data, _ := json.Marshal(map[string]string{"delta": chunk.Delta})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		// Check for stream errors.
		select {
		case streamErr := <-errs:
			if streamErr != nil {
				data, _ := json.Marshal(map[string]string{"error": streamErr.Error()})
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		default:
		}

		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()

		// Post-stream finalization: run the same post-turn work that standard
		// inference does (Rule 7.2). Without this call, streaming turns would
		// silently lose memory ingest, embeddings, and observer dispatch.
		if len(finalizer) > 0 && finalizer[0] != nil {
			finalizer[0].FinalizeStream(r.Context(), outcome, fullContent.String())
		}
	}
}

// AgentStatus returns agent diagnostics matching the Rust response shape.
func AgentStatus(llmSvc *llm.Service, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := llmSvc.Status()
		primary := cfg.Models.Primary
		if primary == "" {
			primary = "auto"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"state":                  "running",
			"name":                   cfg.Agent.Name,
			"agent_name":             cfg.Agent.Name,
			"agent_id":               cfg.Agent.ID,
			"primary_model":          primary,
			"active_model":           primary,
			"primary_provider_state": "closed",
			"cache_entries":          0,
			"cache_hit_rate_pct":     0.0,
			"providers":              providers,
		})
	}
}
