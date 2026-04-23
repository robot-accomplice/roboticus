package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/agent"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
	"roboticus/internal/session"
	"roboticus/testutil"
)

// parityExecutor returns canned content for parity tests.
type parityExecutor struct{}

func (p *parityExecutor) RunLoop(_ context.Context, s *session.Session) (string, int, error) {
	s.AddAssistantMessage("parity response", nil)
	return "parity response", 1, nil
}

// parityServer boots a full server for parity integration tests.
func parityServer(t *testing.T) (*httptest.Server, *db.Store) {
	t.Helper()
	store := testutil.TempStore(t)

	mockLLM := testutil.MockLLMServer(t, func(body map[string]any) (int, any) {
		return 200, map[string]any{
			"id": "parity-test", "model": "test-model",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "parity response"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		}
	})

	llmSvc, err := llm.NewService(llm.ServiceConfig{
		Providers: []llm.Provider{{
			Name: "mock", URL: mockLLM.URL, Format: llm.FormatOpenAI,
			IsLocal: true, ChatPath: "/v1/chat/completions", AuthHeader: "Bearer",
		}},
		Primary: "mock/test-model",
	}, store)
	if err != nil {
		t.Fatalf("llm service: %v", err)
	}

	pipe := pipeline.New(pipeline.PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: agent.NewInjectionDetector(),
		Executor:  &parityExecutor{},
	})

	cfgVal := core.DefaultConfig()
	cfgVal.Agent.Name = "ParityTestBot"
	cfgVal.Agent.ID = "parity-test"
	cfg := &cfgVal

	state := &AppState{
		Store:    store,
		Pipeline: pipe,
		LLM:      llmSvc,
		Config:   cfg,
		EventBus: NewEventBus(64),
	}

	srv := NewServer(context.Background(), DefaultServerConfig(), state)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	return ts, store
}

// --- Parity Tests (matching Rust crates/roboticus-tests/src/parity.rs) ---

// TestParity_SessionListDBvsAPI writes sessions via DB, reads via API.
func TestParity_SessionListDBvsAPI(t *testing.T) {
	ts, ps := parityServer(t)
	client := ts.Client()

	// Write 2 sessions via DB.
	s1 := testutil.SeedSession(t, ps, "agent-1", "api:s1")
	s2 := testutil.SeedSession(t, ps, "agent-2", "api:s2")

	// Read via API.
	resp, err := client.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := parityReadJSON(t, resp)

	sessions, ok := body["sessions"].([]any)
	if !ok {
		t.Fatal("response missing sessions array")
	}
	if len(sessions) < 2 {
		t.Fatalf("expected >= 2 sessions, got %d", len(sessions))
	}

	// Verify required field names exist (CLI parsing depends on these).
	ids := make(map[string]bool)
	for _, raw := range sessions {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"id", "agent_id", "created_at"} {
			if _, exists := s[field]; !exists {
				t.Errorf("session missing field %q", field)
			}
		}
		if id, ok := s["id"].(string); ok {
			ids[id] = true
		}
	}
	if !ids[s1] || !ids[s2] {
		t.Error("API response missing one of the seeded sessions")
	}
}

// TestParity_SessionCreateAPIThenDBLookup creates via POST, verifies in DB.
func TestParity_SessionCreateAPIThenDBLookup(t *testing.T) {
	ts, ps := parityServer(t)
	client := ts.Client()

	payload, _ := json.Marshal(map[string]string{"agent_id": "parity-agent"})
	resp, err := client.Post(ts.URL+"/api/sessions", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	// Accept both 200 and 201.
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, string(b))
	}
	body := parityReadJSON(t, resp)

	apiID, ok := body["id"].(string)
	if !ok || apiID == "" {
		t.Fatal("response missing id field")
	}

	// DB lookup.
	var dbAgentID, dbStatus string
	row := ps.QueryRowContext(context.Background(),
		`SELECT agent_id, status FROM sessions WHERE id = ?`, apiID)
	if err := row.Scan(&dbAgentID, &dbStatus); err != nil {
		t.Fatalf("DB lookup failed: %v", err)
	}
	if dbAgentID != "parity-agent" {
		t.Errorf("DB agent_id = %q, want parity-agent", dbAgentID)
	}
	if dbStatus != "active" {
		t.Errorf("DB status = %q, want active", dbStatus)
	}
}

// TestParity_MessagesDBWriteAPIRead writes messages via DB, reads via API.
func TestParity_MessagesDBWriteAPIRead(t *testing.T) {
	ts, ps := parityServer(t)
	client := ts.Client()

	sid := testutil.SeedSession(t, ps, "agent-1", "api:msg-test")
	testutil.SeedMessage(t, ps, sid, "user", "hello from DB")
	testutil.SeedMessage(t, ps, sid, "assistant", "hello back from DB")

	resp, err := client.Get(ts.URL + "/api/sessions/" + sid + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := parityReadJSON(t, resp)

	messages, ok := body["messages"].([]any)
	if !ok {
		t.Fatal("response missing messages array")
	}
	if len(messages) < 2 {
		t.Fatalf("expected >= 2 messages, got %d", len(messages))
	}

	// Verify field names.
	for _, raw := range messages {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"id", "role", "content", "created_at"} {
			if _, exists := m[field]; !exists {
				t.Errorf("message missing field %q", field)
			}
		}
	}
}

// TestParity_ConfigDisplayHasRequiredSections verifies GET /api/config shape.
func TestParity_ConfigDisplayHasRequiredSections(t *testing.T) {
	ts, _ := parityServer(t)
	client := ts.Client()

	resp, err := client.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := parityReadJSON(t, resp)

	required := []string{"agent", "server", "models"}
	for _, section := range required {
		if _, ok := body[section]; !ok {
			t.Errorf("config missing required section %q", section)
		}
	}

	// Verify agent name.
	if agentSection, ok := body["agent"].(map[string]any); ok {
		if name, _ := agentSection["name"].(string); name != "ParityTestBot" {
			t.Errorf("config agent.name = %q, want ParityTestBot", name)
		}
	}
}

// TestParity_ConfigSerializationDeterministic verifies two calls produce identical JSON.
func TestParity_ConfigSerializationDeterministic(t *testing.T) {
	ts, _ := parityServer(t)
	client := ts.Client()

	resp1, _ := client.Get(ts.URL + "/api/config")
	b1, _ := io.ReadAll(resp1.Body)
	_ = resp1.Body.Close()

	resp2, _ := client.Get(ts.URL + "/api/config")
	b2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()

	if string(b1) != string(b2) {
		t.Error("config serialization is non-deterministic — two calls produced different JSON")
	}
}

// TestParity_HealthAndConfigAgentNameAgree verifies agent name consistency.
func TestParity_HealthAndConfigAgentNameAgree(t *testing.T) {
	ts, _ := parityServer(t)
	client := ts.Client()

	healthResp, _ := client.Get(ts.URL + "/api/health")
	healthBody := parityReadJSON(t, healthResp)
	healthAgent, _ := healthBody["agent"].(string)

	configResp, _ := client.Get(ts.URL + "/api/config")
	configBody := parityReadJSON(t, configResp)
	var configAgent string
	if agentSection, ok := configBody["agent"].(map[string]any); ok {
		configAgent, _ = agentSection["name"].(string)
	}

	if healthAgent != configAgent {
		t.Errorf("health agent=%q != config agent.name=%q", healthAgent, configAgent)
	}
}

// TestParity_SessionCountReflectsDBState verifies count increases after DB inserts.
func TestParity_SessionCountReflectsDBState(t *testing.T) {
	ts, ps := parityServer(t)
	client := ts.Client()

	// Initially should have 0 sessions.
	resp1, _ := client.Get(ts.URL + "/api/sessions")
	body1 := parityReadJSON(t, resp1)
	sessions1, _ := body1["sessions"].([]any)
	count1 := len(sessions1)

	// Insert 3 sessions with unique scope keys.
	for i := 0; i < 3; i++ {
		testutil.SeedSession(t, ps, "agent-count", "api:count-"+db.NewID())
	}

	resp2, _ := client.Get(ts.URL + "/api/sessions")
	body2 := parityReadJSON(t, resp2)
	sessions2, _ := body2["sessions"].([]any)
	count2 := len(sessions2)

	if count2 != count1+3 {
		t.Errorf("session count after inserts = %d, want %d", count2, count1+3)
	}
}

// TestParity_ErrorResponse404ForMissingSession verifies 404 for nonexistent session.
func TestParity_ErrorResponse404ForMissingSession(t *testing.T) {
	ts, _ := parityServer(t)
	client := ts.Client()

	resp, err := client.Get(ts.URL + "/api/sessions/nonexistent-session-id-12345")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

// TestParity_SkillsListShapeForCLI verifies the skills list shape.
func TestParity_SkillsListShapeForCLI(t *testing.T) {
	ts, _ := parityServer(t)
	client := ts.Client()

	resp, err := client.Get(ts.URL + "/api/skills")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := parityReadJSON(t, resp)
	if _, ok := body["skills"]; !ok {
		t.Error("response missing skills array")
	}
}

// TestParity_CronJobsListShape verifies the cron jobs list shape.
func TestParity_CronJobsListShape(t *testing.T) {
	ts, _ := parityServer(t)
	client := ts.Client()

	resp, err := client.Get(ts.URL + "/api/cron/jobs")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := parityReadJSON(t, resp)
	if _, ok := body["jobs"]; !ok {
		t.Error("response missing jobs array")
	}
}

// --- helpers ---

func parityReadJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return data
}
