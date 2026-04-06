package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// --- MockCompleter ---

// MockCompleter is a configurable mock for llm.Completer.
// It returns canned responses in order and records all calls.
type MockCompleter struct {
	mu        sync.Mutex
	responses []*llm.Response
	errors    []error
	calls     []*llm.Request
	callIdx   int
}

// NewMockCompleter creates a mock that returns the given responses in sequence.
func NewMockCompleter(responses ...*llm.Response) *MockCompleter {
	return &MockCompleter{responses: responses}
}

// NewMockCompleterWithErrors creates a mock that returns errors on specified indices.
func NewMockCompleterWithErrors(responses []*llm.Response, errors []error) *MockCompleter {
	return &MockCompleter{responses: responses, errors: errors}
}

// SimpleResponse creates a simple text response for testing.
func SimpleResponse(content string) *llm.Response {
	return &llm.Response{
		ID:           "test-resp-001",
		Model:        "test-model",
		Content:      content,
		FinishReason: "stop",
		Usage:        llm.Usage{InputTokens: 10, OutputTokens: 5},
	}
}

// ToolCallResponse creates a response with tool calls.
func ToolCallResponse(toolName, args string) *llm.Response {
	return &llm.Response{
		ID:    "test-resp-tc",
		Model: "test-model",
		ToolCalls: []llm.ToolCall{{
			ID:   "tc-001",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      toolName,
				Arguments: args,
			},
		}},
		FinishReason: "tool_calls",
		Usage:        llm.Usage{InputTokens: 15, OutputTokens: 8},
	}
}

func (m *MockCompleter) Complete(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, req)
	idx := m.callIdx
	m.callIdx++

	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}

	if idx < len(m.responses) {
		return m.responses[idx], nil
	}

	// Default: return a simple response.
	return SimpleResponse("default test response"), nil
}

func (m *MockCompleter) Stream(ctx context.Context, req *llm.Request) (<-chan llm.StreamChunk, <-chan error) {
	chunks := make(chan llm.StreamChunk, 1)
	errs := make(chan error, 1)

	resp, err := m.Complete(ctx, req)
	if err != nil {
		errs <- err
	} else {
		chunks <- llm.StreamChunk{Delta: resp.Content, FinishReason: "stop"}
	}
	close(chunks)
	close(errs)
	return chunks, errs
}

// Calls returns all recorded requests.
func (m *MockCompleter) Calls() []*llm.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*llm.Request, len(m.calls))
	copy(result, m.calls)
	return result
}

// CallCount returns the number of calls made.
func (m *MockCompleter) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// --- MockHTTPServer ---

// MockLLMServer creates an httptest.Server that responds to LLM API calls.
// The handler receives the parsed request body and returns a configured response.
func MockLLMServer(t *testing.T, handler func(body map[string]any) (int, any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		status, resp := handler(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- Seed Helpers ---

// SeedSession creates a test session and returns its ID.
func SeedSession(t *testing.T, store *db.Store, agentID, scopeKey string) string {
	t.Helper()
	id := db.NewID()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		id, agentID, scopeKey)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return id
}

// SeedMessage adds a message to a session.
func SeedMessage(t *testing.T, store *db.Store, sessionID, role, content string) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, ?, ?)`,
		db.NewID(), sessionID, role, content)
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
}

// SeedWorkingMemory adds entries to working_memory.
func SeedWorkingMemory(t *testing.T, store *db.Store, sessionID string, entries []string) {
	t.Helper()
	for _, entry := range entries {
		_, err := store.ExecContext(context.Background(),
			`INSERT INTO working_memory (id, session_id, entry_type, content) VALUES (?, ?, 'note', ?)`,
			db.NewID(), sessionID, entry)
		if err != nil {
			t.Fatalf("seed working memory: %v", err)
		}
	}
}

// SeedEpisodicMemory adds entries to episodic_memory.
func SeedEpisodicMemory(t *testing.T, store *db.Store, entries []string) {
	t.Helper()
	for _, entry := range entries {
		_, err := store.ExecContext(context.Background(),
			`INSERT INTO episodic_memory (id, classification, content) VALUES (?, 'test', ?)`,
			db.NewID(), entry)
		if err != nil {
			t.Fatalf("seed episodic memory: %v", err)
		}
	}
}

// SeedSemanticMemory adds a key-value pair to semantic_memory.
func SeedSemanticMemory(t *testing.T, store *db.Store, category, key, value string) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO semantic_memory (id, category, key, value) VALUES (?, ?, ?, ?)`,
		db.NewID(), category, key, value)
	if err != nil {
		t.Fatalf("seed semantic memory: %v", err)
	}
}

// SeedProceduralMemory adds tool stats.
func SeedProceduralMemory(t *testing.T, store *db.Store, toolName string, successes, failures int) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO procedural_memory (id, name, steps, success_count, failure_count) VALUES (?, ?, '', ?, ?)`,
		db.NewID(), toolName, successes, failures)
	if err != nil {
		t.Fatalf("seed procedural memory: %v", err)
	}
}

// --- Assertion Helpers ---

// AssertJSON checks a JSON response body for a key-value match.
func AssertJSON(t *testing.T, body []byte, key string, expected any) {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	got, ok := data[key]
	if !ok {
		t.Errorf("key %q not found in JSON response", key)
		return
	}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", expected) {
		t.Errorf("JSON[%q] = %v, want %v", key, got, expected)
	}
}
