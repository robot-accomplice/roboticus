package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// SSETransport communicates with an MCP server over HTTP SSE (Server-Sent
// Events). Outbound messages are POST requests; inbound messages arrive as SSE
// events.
type SSETransport struct {
	baseURL         string
	messageURL      string
	httpClient      *http.Client
	headers         map[string]string
	messages        chan json.RawMessage
	cancel          context.CancelFunc
	done            chan struct{}
	streamReady     chan struct{}
	streamReadyOnce sync.Once
	mu              sync.RWMutex
}

// NewSSETransport connects to an MCP SSE server at the given URL.
func NewSSETransport(ctx context.Context, url string) (*SSETransport, error) {
	return NewSSETransportWithConfig(ctx, McpServerConfig{URL: url})
}

// NewSSETransportWithConfig connects to an MCP SSE server using the runtime
// configuration contract, including auth-bearing headers when configured.
func NewSSETransportWithConfig(ctx context.Context, cfg McpServerConfig) (*SSETransport, error) {
	ctx, cancel := context.WithCancel(ctx)

	baseURL := strings.TrimSuffix(cfg.URL, "/")
	t := &SSETransport{
		baseURL:         baseURL,
		messageURL:      baseURL,
		httpClient:      &http.Client{},
		headers:         cloneStringMap(cfg.Headers),
		messages:        make(chan json.RawMessage, 64),
		cancel:          cancel,
		done:            make(chan struct{}),
		streamReady:     make(chan struct{}),
		streamReadyOnce: sync.Once{},
	}

	go t.listenSSE(ctx)

	return t, nil
}

func (t *SSETransport) listenSSE(ctx context.Context) {
	defer close(t.done)
	defer close(t.messages)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	t.applyHeaders(req)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()

	t.markStreamReady()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventName string
	var dataLines []string
	dispatch := func() {
		if len(dataLines) == 0 {
			eventName = ""
			return
		}
		payload := strings.Join(dataLines, "\n")
		t.handleSSEEvent(ctx, eventName, payload)
		eventName = ""
		dataLines = nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			dispatch()
		case strings.HasPrefix(line, ":"):
			continue
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
			trimmed := strings.TrimSpace(data)
			if trimmed == "" || trimmed == "[DONE]" {
				continue
			}
			dataLines = append(dataLines, data)
		}
	}
	dispatch()
}

func (t *SSETransport) handleSSEEvent(ctx context.Context, eventName, payload string) {
	if payload == "" || payload == "[DONE]" {
		return
	}
	if eventName == "endpoint" {
		if resolved := resolveSSEEndpoint(t.baseURL, payload); resolved != "" {
			t.mu.Lock()
			t.messageURL = resolved
			t.mu.Unlock()
		}
		return
	}
	select {
	case t.messages <- json.RawMessage(payload):
	case <-ctx.Done():
	}
}

func resolveSSEEndpoint(baseURL, payload string) string {
	raw := strings.TrimSpace(payload)
	if raw == "" {
		return ""
	}

	// Accept raw path/URL, JSON strings, or small endpoint objects.
	if strings.HasPrefix(raw, "{") {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			for _, key := range []string{"url", "endpoint", "message_url"} {
				if value, ok := parsed[key].(string); ok {
					raw = value
					break
				}
			}
		}
	} else if strings.HasPrefix(raw, "\"") {
		var parsed string
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			raw = parsed
		}
	}

	base, err := neturl.Parse(baseURL)
	if err != nil {
		return raw
	}
	ref, err := neturl.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.TrimSuffix(base.ResolveReference(ref).String(), "/")
}

// Send posts a JSON-RPC message to the SSE server.
func (t *SSETransport) Send(ctx context.Context, msg json.RawMessage) error {
	if err := t.waitForStream(ctx); err != nil {
		return err
	}

	t.mu.RLock()
	messageURL := t.messageURL
	t.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messageURL, bytes.NewReader(msg))
	if err != nil {
		return fmt.Errorf("mcp sse: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	t.applyHeaders(req)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		log.Trace().Err(err).Msg("mcp sse: body drain failed")
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp sse: server returned %d", resp.StatusCode)
	}
	return nil
}

// Receive reads the next JSON-RPC message from the SSE stream.
func (t *SSETransport) Receive(ctx context.Context) (json.RawMessage, error) {
	select {
	case msg, ok := <-t.messages:
		if !ok {
			return nil, fmt.Errorf("mcp sse: connection closed")
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts down the SSE connection.
func (t *SSETransport) Close() error {
	t.cancel()
	<-t.done
	return nil
}

func (t *SSETransport) MessageURL() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.messageURL
}

func (t *SSETransport) applyHeaders(req *http.Request) {
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
}

func (t *SSETransport) waitForStream(ctx context.Context) error {
	select {
	case <-t.streamReady:
		return nil
	case <-time.After(100 * time.Millisecond):
		// Do not block indefinitely if the stream is slow to establish. The
		// current messageURL will still be used, and endpoint discovery may
		// update it before later calls.
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *SSETransport) markStreamReady() {
	t.streamReadyOnce.Do(func() {
		close(t.streamReady)
	})
}

// ConnectSSE connects to an MCP server via SSE transport.
func ConnectSSE(ctx context.Context, name, url string) (*Connection, error) {
	return ConnectSSEWithConfig(ctx, McpServerConfig{Name: name, URL: url, Transport: "sse"})
}

// ConnectSSEWithConfig connects to an MCP server via the runtime SSE config
// contract.
func ConnectSSEWithConfig(ctx context.Context, cfg McpServerConfig) (*Connection, error) {
	transport, err := NewSSETransportWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	conn := newConnection(cfg.Name, transport)

	if err := conn.initialize(ctx); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("mcp sse: initialize failed: %w", err)
	}

	if err := conn.listTools(ctx); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("mcp sse: list tools failed: %w", err)
	}

	return conn, nil
}

// ValidateSSETarget produces a structured validation artifact for a configured
// named SSE target.
func ValidateSSETarget(ctx context.Context, cfg McpServerConfig) SSEValidationEvidence {
	evidence := SSEValidationEvidence{
		Name:           cfg.Name,
		URL:            cfg.URL,
		AuthConfigured: len(cfg.Headers) > 0 || strings.TrimSpace(cfg.AuthTokenEnv) != "",
	}
	if cfg.Transport != "" && cfg.Transport != "sse" {
		evidence.FatalError = fmt.Sprintf("mcp: server %q is not configured for SSE transport", cfg.Name)
		return evidence
	}
	conn, err := ConnectSSEWithConfig(ctx, cfg)
	if err != nil {
		evidence.FatalError = err.Error()
		return evidence
	}
	defer func() { _ = conn.Close() }()

	evidence.InitializeOK = true
	evidence.ToolListOK = true
	evidence.ServerName = conn.ServerName
	evidence.ServerVersion = conn.ServerVersion
	evidence.ToolCount = len(conn.Tools)
	if transport, ok := conn.transport.(*SSETransport); ok {
		evidence.ResolvedPostURL = transport.MessageURL()
	}
	if len(conn.Tools) == 0 {
		return evidence
	}

	firstTool := conn.Tools[0].Name
	evidence.FirstTool = firstTool
	evidence.ToolCall.Tool = firstTool
	evidence.ToolCall.Attempted = true

	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result, err := conn.CallTool(callCtx, firstTool, nil)
	if err != nil {
		evidence.ToolCall.Error = err.Error()
		evidence.ToolCall.Interpretable = strings.TrimSpace(err.Error()) != ""
		return evidence
	}
	evidence.ToolCall.Interpretable = true
	evidence.ToolCall.ContentPreview = truncateEvidencePreview(result.Content)
	return evidence
}

func truncateEvidencePreview(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 240 {
		return s
	}
	return s[:240] + "..."
}
