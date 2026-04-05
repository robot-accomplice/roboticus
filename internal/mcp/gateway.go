package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// GatewayToolDescriptor describes a tool for the MCP gateway.
// It mirrors ToolDescriptor but uses the MCP protocol field name "inputSchema".
type GatewayToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCallFunc is the callback signature for executing a tool via the gateway.
type ToolCallFunc func(ctx context.Context, name string, input json.RawMessage) (*ToolCallResult, error)

// ToolListFunc returns the tools available via the gateway.
type ToolListFunc func() []GatewayToolDescriptor

// Gateway is an MCP server that exposes the agent's tools to external MCP clients
// over HTTP using JSON-RPC 2.0.
type Gateway struct {
	listTools ToolListFunc
	callTool  ToolCallFunc
	mu        sync.Mutex
	sessions  map[string]chan struct{} // session ID -> done channel for SSE
}

// NewGateway creates a new MCP gateway server.
func NewGateway(listTools ToolListFunc, callTool ToolCallFunc) *Gateway {
	return &Gateway{
		listTools: listTools,
		callTool:  callTool,
		sessions:  make(map[string]chan struct{}),
	}
}

// gatewayRequest is a JSON-RPC 2.0 request as received by the gateway.
type gatewayRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // may be number, string, or null (notification)
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// gatewayResponse is a JSON-RPC 2.0 response sent by the gateway.
type gatewayResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// ServeHTTP implements http.Handler for the MCP gateway.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		g.handlePost(w, r)
	case http.MethodGet:
		g.handleSSE(w, r)
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (g *Gateway) handlePost(w http.ResponseWriter, r *http.Request) {
	var req gatewayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, -32700, "parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSONRPCError(w, req.ID, -32600, "invalid request: jsonrpc must be 2.0")
		return
	}

	// Notifications have no ID and expect no response.
	if req.Method == "notifications/initialized" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch req.Method {
	case "initialize":
		g.handleInitialize(w, req)
	case "tools/list":
		g.handleToolsList(w, req)
	case "tools/call":
		g.handleToolsCall(w, r, req)
	default:
		writeJSONRPCError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (g *Gateway) handleInitialize(w http.ResponseWriter, req gatewayRequest) {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]string{
			"name":    "goboticus",
			"version": "0.1.0",
		},
	}
	writeJSONRPCResult(w, req.ID, result)
}

func (g *Gateway) handleToolsList(w http.ResponseWriter, req gatewayRequest) {
	tools := g.listTools()
	if tools == nil {
		tools = []GatewayToolDescriptor{}
	}
	result := map[string]any{
		"tools": tools,
	}
	writeJSONRPCResult(w, req.ID, result)
}

func (g *Gateway) handleToolsCall(w http.ResponseWriter, r *http.Request, req gatewayRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSONRPCError(w, req.ID, -32602, "invalid params")
		return
	}

	if params.Name == "" {
		writeJSONRPCError(w, req.ID, -32602, "missing tool name")
		return
	}

	result, err := g.callTool(r.Context(), params.Name, params.Arguments)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32000, err.Error())
		return
	}

	mcpResult := map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": result.Content},
		},
		"isError": result.IsError,
	}
	writeJSONRPCResult(w, req.ID, mcpResult)
}

func (g *Gateway) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sessionID := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	done := make(chan struct{})

	g.mu.Lock()
	g.sessions[sessionID] = done
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.sessions, sessionID)
		g.mu.Unlock()
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	log.Debug().Str("session", sessionID).Msg("mcp gateway: SSE session started")

	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_, err := fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeJSONRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	resp := gatewayResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error().Err(err).Msg("mcp gateway: failed to write response")
	}
}

func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := gatewayResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: message},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error().Err(err).Msg("mcp gateway: failed to write error response")
	}
}
