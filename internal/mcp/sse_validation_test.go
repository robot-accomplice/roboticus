// sse_validation_test.go is the v1.0.6 evidence for
// docs/testing/mcp-release-blocker-checklist.md item 3 ("Blessed SSE
// target is practically validated"). The user has no production SSE
// MCP target available for this release, so per their guidance we
// stand up a local SSE fixture that speaks the MCP protocol
// correctly and validate ConnectSSE against it end to end:
// initialize → tools/list → tools/call.
//
// What this is:
//   - A reproducible, hermetic SSE end-to-end test that produces
//     concrete evidence for the checklist (server name/version,
//     tool count, real tool-call result) without depending on any
//     external network or third-party server availability.
//   - The fixture uses the same JSON-RPC + SSE shape the production
//     ConnectSSE path consumes, so the test exercises the actual
//     transport / handshake / call code paths — not a parallel
//     mock that could diverge from real behavior.
//
// What this isn't:
//   - A substitute for validating against a real third-party SSE
//     MCP server. The release notes will be honest about this:
//     "SSE end-to-end validated against an in-tree fixture; no
//     blessed third-party SSE target available this release."
//
// Existing TestConnectSSE_Integration covers similar ground but
// stops at tools/list. This test extends to tools/call so the
// checklist's "at least one tools/call completes" requirement is
// pinned by a regression, not a manual run.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSSEReleaseChecklist_FullValidation exercises every requirement
// from MCP-release-blocker-checklist item 3 against a local SSE
// fixture. The fixture serves a Server-Sent Events stream and
// responds to MCP JSON-RPC over the SSE channel.
//
// Asserts (in order):
//
//  1. SSE endpoint is reachable
//  2. initialize succeeds; serverInfo carries name + version
//  3. tools/list returns a non-zero tool count
//  4. tools/call completes with an interpretable result
//  5. Server name/version match the fixture's declared values
//
// On pass: prints an evidence summary in the same format the
// checklist's "Evidence" section requests, so an operator can
// paste it into the release record without manual reformatting.
func TestSSEReleaseChecklist_FullValidation(t *testing.T) {
	const (
		fixtureName    = "release-checklist-sse-fixture"
		fixtureVersion = "v1.0.6-validation"
		toolName       = "echo"
		toolDesc       = "Echo back the input string. Fixture-only — no side effects."
	)

	// nextID is the JSON-RPC id the fixture echoes back; we use the
	// same id field so initialize/list/call responses match
	// requests deterministically.
	var (
		eventsMu      sync.Mutex
		writeEvent    func([]byte)
		eventQueue    [][]byte // events buffered until a GET subscriber arrives
		subscriberSet bool
	)

	// queueOrSendEvent: if a subscriber is connected, write
	// directly; otherwise buffer for the next subscriber.
	queueOrSendEvent := func(b []byte) {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		if subscriberSet && writeEvent != nil {
			writeEvent(b)
			return
		}
		eventQueue = append(eventQueue, b)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			method, _ := req["method"].(string)
			id := req["id"]

			// Notifications (e.g., notifications/initialized) get a
			// 200/empty reply per MCP spec.
			if method == "notifications/initialized" {
				w.WriteHeader(http.StatusOK)
				return
			}

			var result any
			switch method {
			case "initialize":
				result = map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]string{
						"name":    fixtureName,
						"version": fixtureVersion,
					},
				}
			case "tools/list":
				result = map[string]any{
					"tools": []map[string]any{
						{
							"name":        toolName,
							"description": toolDesc,
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"text": map[string]any{"type": "string"},
								},
								"required": []string{"text"},
							},
						},
					},
				}
			case "tools/call":
				params, _ := req["params"].(map[string]any)
				args, _ := params["arguments"].(map[string]any)
				text, _ := args["text"].(string)
				result = map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "echoed: " + text},
					},
					"isError": false,
				}
			default:
				result = map[string]any{}
			}

			respBytes, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  result,
			})

			// Acknowledge the POST; deliver the response on the SSE
			// stream (which is what SSETransport.Receive listens to).
			w.WriteHeader(http.StatusOK)
			queueOrSendEvent(respBytes)

		case http.MethodGet:
			// SSE stream. Set up the writeEvent closure so POST
			// handlers can push responses onto this stream.
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flusher", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()

			eventsMu.Lock()
			subscriberSet = true
			writeEvent = func(b []byte) {
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(b)
				_, _ = w.Write([]byte("\n\n"))
				flusher.Flush()
			}
			// Drain any events that arrived before the subscriber.
			for _, b := range eventQueue {
				writeEvent(b)
			}
			eventQueue = nil
			eventsMu.Unlock()

			<-r.Context().Done()

			eventsMu.Lock()
			subscriberSet = false
			writeEvent = nil
			eventsMu.Unlock()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := ConnectSSE(ctx, "release-validation", srv.URL)
	if err != nil {
		t.Fatalf("ConnectSSE failed (checklist item 3 blocker): %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Item 3.1: initialize succeeded; server name/version match.
	if conn.ServerName != fixtureName {
		t.Fatalf("expected serverName=%q from initialize; got %q", fixtureName, conn.ServerName)
	}
	if conn.ServerVersion != fixtureVersion {
		t.Fatalf("expected serverVersion=%q from initialize; got %q", fixtureVersion, conn.ServerVersion)
	}

	// Item 3.2: tools/list returns non-zero tool count.
	if len(conn.Tools) == 0 {
		t.Fatalf("expected non-zero tool count from tools/list; got 0 (checklist failure)")
	}
	if conn.Tools[0].Name != toolName {
		t.Fatalf("expected first tool to be %q; got %q", toolName, conn.Tools[0].Name)
	}

	// Item 3.3: at least one tools/call completes with an
	// interpretable result. ToolCallResult.Content is a string
	// concatenation of all text-type content blocks the server
	// returned (see CallTool's deserialization in client.go).
	const echoText = "checklist-validation-payload"
	callResult, err := conn.CallTool(ctx, toolName,
		json.RawMessage(`{"text":"`+echoText+`"}`))
	if err != nil {
		t.Fatalf("tools/call %s failed: %v", toolName, err)
	}
	if callResult.Content == "" {
		t.Fatalf("expected tools/call to return non-empty content; got empty string")
	}
	// Verify the echo loop carried our payload through the full
	// JSON-RPC + SSE round-trip — proves the call wasn't a no-op.
	if !strings.Contains(callResult.Content, echoText) {
		t.Fatalf("expected echo content to include %q (proves end-to-end round-trip); got %q",
			echoText, callResult.Content)
	}

	// Print evidence in the format docs/testing/mcp-release-
	// blocker-checklist.md item 3 requests, so an operator can
	// paste the test output into the release record.
	t.Logf("\n=== MCP checklist item 3 evidence ===\n"+
		"  endpoint:      %s\n"+
		"  server name:   %s\n"+
		"  server version: %s\n"+
		"  tool count:    %d\n"+
		"  tools/call:    %s({\"text\":%q}) → %q\n"+
		"  verdict:       PASS (in-tree SSE fixture; no blessed third-party target this release)\n",
		srv.URL, conn.ServerName, conn.ServerVersion, len(conn.Tools), toolName, echoText, callResult.Content)
}
