package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// SSETransport communicates with an MCP server over HTTP SSE (Server-Sent Events).
// Outbound messages are POST requests; inbound messages arrive as SSE events.
type SSETransport struct {
	baseURL    string
	httpClient *http.Client
	messages   chan json.RawMessage
	cancel     context.CancelFunc
	done       chan struct{}
	mu         sync.Mutex
}

// NewSSETransport connects to an MCP SSE server at the given URL.
// It starts a background goroutine to consume the SSE event stream.
func NewSSETransport(ctx context.Context, url string) (*SSETransport, error) {
	ctx, cancel := context.WithCancel(ctx)

	t := &SSETransport{
		baseURL:    strings.TrimSuffix(url, "/"),
		httpClient: &http.Client{},
		messages:   make(chan json.RawMessage, 64),
		cancel:     cancel,
		done:       make(chan struct{}),
	}

	// Start SSE listener.
	go t.listenSSE(ctx)

	return t, nil
}

func (t *SSETransport) listenSSE(ctx context.Context) {
	defer close(t.done)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}
		select {
		case t.messages <- json.RawMessage(data):
		case <-ctx.Done():
			return
		}
	}
}

// Send posts a JSON-RPC message to the SSE server.
func (t *SSETransport) Send(ctx context.Context, msg json.RawMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(msg))
	if err != nil {
		return fmt.Errorf("mcp sse: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

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

// ConnectSSE connects to an MCP server via SSE transport.
func ConnectSSE(ctx context.Context, name, url string) (*Connection, error) {
	transport, err := NewSSETransport(ctx, url)
	if err != nil {
		return nil, err
	}

	conn := &Connection{
		Name:      name,
		transport: transport,
	}

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
