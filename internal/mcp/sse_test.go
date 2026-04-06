package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSSETransport_SendAndReceive(t *testing.T) {
	// Create a server that accepts POST for sends and GET for SSE stream.
	var (
		mu       sync.Mutex
		received []json.RawMessage
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			received = append(received, json.RawMessage(body))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flusher", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			// Send a response for the initialize call.
			data := `{"jsonrpc":"2.0","id":0,"result":{"status":"ok"}}`
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			// Keep alive until client disconnects.
			<-r.Context().Done()
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewSSETransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	// Give the SSE listener goroutine a moment to connect.
	time.Sleep(100 * time.Millisecond)

	// Test Send.
	msg := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"test"}`)
	if err := transport.Send(ctx, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	sendCount := len(received)
	mu.Unlock()
	if sendCount != 1 {
		t.Errorf("server received %d messages, want 1", sendCount)
	}

	// Test Receive (reads the pre-sent SSE data event).
	resp, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if resp == nil {
		t.Fatal("received nil response")
	}

	var parsed map[string]any
	if err := json.Unmarshal(resp, &parsed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", parsed["jsonrpc"])
	}
}

func TestSSETransport_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// SSE stream: keep alive.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewSSETransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	err = transport.Send(ctx, json.RawMessage(`{"test":true}`))
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestSSETransport_Receive_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewSSETransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	// Cancel the context before trying to receive.
	receiveCtx, receiveCancel := context.WithCancel(context.Background())
	receiveCancel()

	_, err = transport.Receive(receiveCtx)
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestSSETransport_Close(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx := context.Background()
	transport, err := NewSSETransport(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewSSETransport: %v", err)
	}

	err = transport.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After close, the done channel should be closed (listenSSE goroutine exited).
	select {
	case <-transport.done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for done channel")
	}
}

func TestSSETransport_ListenSSE_SkipsNonDataLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send various SSE lines: comments, event names, empty data, and [DONE].
		fmt.Fprintf(w, ": this is a comment\n")
		fmt.Fprintf(w, "event: ping\n")
		fmt.Fprintf(w, "data: \n")       // empty data, should be skipped
		fmt.Fprintf(w, "data: [DONE]\n") // [DONE] sentinel, should be skipped
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"ok\"}\n\n")
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewSSETransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	// Should only receive the valid JSON data event.
	msg, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(msg, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["result"] != "ok" {
		t.Errorf("result = %v", parsed["result"])
	}
}

func TestSSETransport_Receive_ConnectionClosed(t *testing.T) {
	// Server sends one message then closes the connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"id\":1}\n\n")
		flusher.Flush()
		// Server closes connection immediately.
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewSSETransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	// First receive should succeed.
	msg, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("first Receive: %v", err)
	}
	if msg == nil {
		t.Fatal("first message should not be nil")
	}

	// After the server closes, the messages channel eventually closes.
	// Wait for the listenSSE goroutine to finish.
	select {
	case <-transport.done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for listener to close")
	}

	// Next receive should return error (channel closed).
	_, err = transport.Receive(ctx)
	if err == nil {
		t.Fatal("expected error after connection closed")
	}
}

// TestConnectSSE_Integration tests the full ConnectSSE flow against a mock MCP server.
func TestConnectSSE_Integration(t *testing.T) {
	// Track the request sequence.
	var (
		mu       sync.Mutex
		requests []map[string]any
	)

	// Mock MCP server that responds to initialize and tools/list via SSE.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)

			mu.Lock()
			requests = append(requests, req)
			mu.Unlock()

			method, _ := req["method"].(string)
			id, _ := req["id"].(float64)

			var result any
			switch method {
			case "initialize":
				result = map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]string{
						"name":    "mock-mcp",
						"version": "0.5.0",
					},
				}
			case "notifications/initialized":
				w.WriteHeader(http.StatusNoContent)
				return
			case "tools/list":
				result = map[string]any{
					"tools": []map[string]any{
						{
							"name":        "hello",
							"description": "Say hello",
							"inputSchema": map[string]any{"type": "object"},
						},
					},
				}
			default:
				result = map[string]any{}
			}

			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  result,
			}
			respBytes, _ := json.Marshal(resp)

			// Write as SSE data event on the POST response is not used by SSETransport.
			// SSETransport sends via POST but receives via the GET SSE stream.
			// We need to push to the SSE stream. Since the transport reads from GET,
			// we respond directly on POST and let SSETransport handle it.
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBytes)

		case http.MethodGet:
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flusher", 500)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	// ConnectSSE uses SSETransport which reads responses from the SSE GET stream.
	// But the mock server sends responses in POST replies, not on the SSE stream.
	// The SSETransport.Send does POST, and .Receive reads from the GET SSE channel.
	// This means ConnectSSE won't work with a simple mock because the protocol
	// expects responses on the SSE stream, not as POST response bodies.
	//
	// Instead, we test ConnectSSE with a server that echoes responses on SSE.

	// For ConnectSSE we need a more sophisticated mock that pushes responses
	// onto the SSE stream. Let's test that directly.
	t.Run("with SSE response stream", func(t *testing.T) {
		var (
			responseCh = make(chan json.RawMessage, 10)
		)

		srvSSE := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)

				method, _ := req["method"].(string)
				id, _ := req["id"].(float64)

				var result any
				switch method {
				case "initialize":
					result = map[string]any{
						"protocolVersion": "2024-11-05",
						"serverInfo": map[string]string{
							"name":    "mock-sse",
							"version": "0.9.0",
						},
					}
				case "notifications/initialized":
					w.WriteHeader(http.StatusOK)
					return
				case "tools/list":
					result = map[string]any{
						"tools": []map[string]any{
							{
								"name":        "greet",
								"description": "Greet someone",
								"inputSchema": map[string]any{"type": "object"},
							},
						},
					}
				default:
					result = map[string]any{}
				}

				resp := map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
				respBytes, _ := json.Marshal(resp)

				// Push response onto the SSE channel for the GET handler to deliver.
				responseCh <- json.RawMessage(respBytes)

				w.WriteHeader(http.StatusOK)

			case http.MethodGet:
				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "no flusher", 500)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				flusher.Flush()

				for {
					select {
					case msg := <-responseCh:
						fmt.Fprintf(w, "data: %s\n\n", string(msg))
						flusher.Flush()
					case <-r.Context().Done():
						return
					}
				}
			}
		}))
		defer srvSSE.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, err := ConnectSSE(ctx, "test-sse", srvSSE.URL)
		if err != nil {
			t.Fatalf("ConnectSSE: %v", err)
		}
		defer func() { _ = conn.Close() }()

		if conn.ServerName != "mock-sse" {
			t.Errorf("ServerName = %q", conn.ServerName)
		}
		if conn.ServerVersion != "0.9.0" {
			t.Errorf("ServerVersion = %q", conn.ServerVersion)
		}
		if len(conn.Tools) != 1 {
			t.Fatalf("tool count = %d, want 1", len(conn.Tools))
		}
		if conn.Tools[0].Name != "greet" {
			t.Errorf("tool[0].Name = %q", conn.Tools[0].Name)
		}
	})
}

func TestConnectSSE_InitializeFailure(t *testing.T) {
	responseCh := make(chan json.RawMessage, 10)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			id, _ := req["id"].(float64)

			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"error":   map[string]any{"code": -32600, "message": "bad initialize"},
			}
			respBytes, _ := json.Marshal(resp)
			responseCh <- json.RawMessage(respBytes)
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()

			for {
				select {
				case msg := <-responseCh:
					fmt.Fprintf(w, "data: %s\n\n", string(msg))
					flusher.Flush()
				case <-r.Context().Done():
					return
				}
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := ConnectSSE(ctx, "fail-sse", srv.URL)
	if err == nil {
		t.Fatal("expected ConnectSSE to fail when initialize returns error")
	}
	if !strings.Contains(err.Error(), "initialize failed") {
		t.Errorf("error = %q, should mention initialize failure", err.Error())
	}
}
