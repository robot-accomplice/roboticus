package routes

import (
	"encoding/json"
	"fmt"
	"net/http"

	"goboticus/internal/llm"
	"goboticus/internal/pipeline"
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
func AgentMessage(p *pipeline.Pipeline, bus ...EventPublisher) http.HandlerFunc {
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
			AgentName: "Goboticus",
			Platform:  "api",
		}

		outcome, err := p.Run(r.Context(), pipeline.PresetAPI(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
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
func AgentMessageStream(p *pipeline.Pipeline, llmSvc *llm.Service) http.HandlerFunc {
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
			AgentName: "Goboticus",
			Platform:  "api",
		}

		// Run pipeline to get session set up.
		outcome, err := p.Run(r.Context(), pipeline.PresetStreaming(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if !outcome.Stream {
			// Non-stream result (e.g., cache hit).
			writeJSON(w, http.StatusOK, outcome)
			return
		}

		// Set up SSE headers.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		// Start LLM stream.
		streamReq := &llm.Request{
			Messages: []llm.Message{{Role: "user", Content: req.Content}},
			Stream:   true,
		}
		chunks, errs := llmSvc.Stream(r.Context(), streamReq)

		for chunk := range chunks {
			data, _ := json.Marshal(map[string]string{"delta": chunk.Delta})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		// Check for stream errors.
		select {
		case err := <-errs:
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		default:
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}
}

// AgentStatus returns agent diagnostics.
func AgentStatus(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := llmSvc.Status()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "running",
			"providers": providers,
		})
	}
}
