package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/testutil"
)

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

func TestListAgents_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	handler := ListAgents(store)
	req := httptest.NewRequest("GET", "/api/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	agents := body["agents"].([]any)
	if len(agents) != 1 {
		t.Errorf("got %d agents, want primary orchestrator only", len(agents))
	}
	first := agents[0].(map[string]any)
	if first["role"] != "orchestrator" {
		t.Errorf("role = %v, want orchestrator", first["role"])
	}
}

func TestListAgents_WithData(t *testing.T) {
	store := testutil.TempStore(t)
	_, err := store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, display_name, model, role, description, enabled, created_at)
		 VALUES ('a1', 'coder', 'Coder Agent', 'gpt-4', 'specialist', 'writes code', 1, datetime('now'))`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, role, enabled, created_at)
		 VALUES ('a2', 'reviewer', 'claude-3', 'reviewer', 0, datetime('now'))`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	handler := ListAgents(store)
	req := httptest.NewRequest("GET", "/api/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	agents := body["agents"].([]any)
	if len(agents) != 3 {
		t.Errorf("got %d agents, want orchestrator + 2 subagents", len(agents))
	}

	// First result is always the operator-facing orchestrator.
	first := agents[0].(map[string]any)
	if first["role"] != "orchestrator" {
		t.Errorf("first role = %v, want orchestrator", first["role"])
	}
	for _, raw := range agents[1:] {
		agent := raw.(map[string]any)
		if agent["role"] != "subagent" {
			t.Fatalf("sub_agents row %q role = %v, want normalized subagent", agent["name"], agent["role"])
		}
		if agent["source_role"] == "" {
			t.Fatalf("sub_agents row %q missing source_role metadata", agent["name"])
		}
	}
}

func TestListAgents_DBError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE sub_agents`); err != nil {
		t.Fatalf("drop: %v", err)
	}

	handler := ListAgents(store)
	req := httptest.NewRequest("GET", "/api/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// chiReq creates an httptest request with a chi URL parameter injected.
func chiReq(method, url, paramKey, paramVal string) *http.Request {
	req := httptest.NewRequest(method, url, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramKey, paramVal)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

func TestStartAgent_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, role, enabled, created_at)
		 VALUES ('a1', 'coder', 'gpt-4', 'specialist', 0, datetime('now'))`)

	handler := StartAgent(store)
	req := chiReq("POST", "/api/agents/a1/start", "id", "a1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "running" {
		t.Errorf("status = %v, want running", body["status"])
	}

	// Verify DB.
	var enabled int
	row := store.QueryRowContext(bgCtx, `SELECT enabled FROM sub_agents WHERE id = 'a1'`)
	if err := row.Scan(&enabled); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if enabled != 1 {
		t.Errorf("enabled = %d, want 1", enabled)
	}
}

func TestStartAgent_ByName(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, role, enabled, created_at)
		 VALUES ('a1', 'coder', 'gpt-4', 'specialist', 0, datetime('now'))`)

	handler := StartAgent(store)
	req := chiReq("POST", "/api/agents/coder/start", "id", "coder")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestStartAgent_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	handler := StartAgent(store)
	req := chiReq("POST", "/api/agents/missing/start", "id", "missing")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestStopAgent_Success(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO sub_agents (id, name, model, role, enabled, created_at)
		 VALUES ('a1', 'coder', 'gpt-4', 'specialist', 1, datetime('now'))`)

	handler := StopAgent(store)
	req := chiReq("POST", "/api/agents/a1/stop", "id", "a1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "stopped" {
		t.Errorf("status = %v, want stopped", body["status"])
	}

	// Verify DB.
	var enabled int
	row := store.QueryRowContext(bgCtx, `SELECT enabled FROM sub_agents WHERE id = 'a1'`)
	if err := row.Scan(&enabled); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if enabled != 0 {
		t.Errorf("enabled = %d, want 0", enabled)
	}
}

func TestStopAgent_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	handler := StopAgent(store)
	req := chiReq("POST", "/api/agents/missing/stop", "id", "missing")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// A2AHello
// ---------------------------------------------------------------------------

func TestA2AHello(t *testing.T) {
	handler := A2AHello()
	req := httptest.NewRequest("GET", "/.well-known/a2a", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["name"] != "roboticus" {
		t.Errorf("name = %v, want roboticus", body["name"])
	}
	if body["protocol"] != "a2a/1.0" {
		t.Errorf("protocol = %v, want a2a/1.0", body["protocol"])
	}
	caps, ok := body["capabilities"].([]any)
	if !ok || len(caps) == 0 {
		t.Fatal("capabilities missing or empty")
	}
}

// ---------------------------------------------------------------------------
// Memory Analytics
// ---------------------------------------------------------------------------

func TestGetMemoryAnalytics_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetMemoryAnalytics(store)
	req := httptest.NewRequest("GET", "/api/memory/analytics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["total_turns"].(float64) != 0 {
		t.Errorf("total_turns = %v, want 0", body["total_turns"])
	}
	if body["hit_rate"].(float64) != 0 {
		t.Errorf("hit_rate = %v, want 0", body["hit_rate"])
	}
	entryCounts := body["entry_counts"].(map[string]any)
	for _, tier := range []string{"working", "episodic", "semantic", "procedural", "relationship"} {
		if entryCounts[tier].(float64) != 0 {
			t.Errorf("entry_counts[%s] = %v, want 0", tier, entryCounts[tier])
		}
	}
}

func TestGetMemoryAnalytics_SeededData(t *testing.T) {
	store := testutil.TempStore(t)

	// Seed sessions, turns, context_snapshots, turn_feedback, and memory tables.
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s1', 'agent1', 'test')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t1', 's1')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t2', 's1')`)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO turns (id, session_id) VALUES ('t3', 's1')`)

	// Context snapshots: t1 has memory, t2 has no memory, t3 has memory.
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO context_snapshots (turn_id, complexity_level, token_budget, system_prompt_tokens, memory_tokens, history_tokens, created_at)
		 VALUES ('t1', 'L1', 4000, 500, 200, 300, datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO context_snapshots (turn_id, complexity_level, token_budget, system_prompt_tokens, memory_tokens, history_tokens, created_at)
		 VALUES ('t2', 'L0', 4000, 500, 0, 300, datetime('now'))`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO context_snapshots (turn_id, complexity_level, token_budget, system_prompt_tokens, memory_tokens, history_tokens, created_at)
		 VALUES ('t3', 'L2', 4000, 500, 400, 300, datetime('now'))`)

	// Turn feedback for memory ROI.
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turn_feedback (id, turn_id, session_id, grade) VALUES ('tf1', 't1', 's1', 5)`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO turn_feedback (id, turn_id, session_id, grade) VALUES ('tf2', 't2', 's1', 3)`)

	// Memory entries.
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO working_memory (id, session_id, entry_type, content) VALUES ('w1', 's1', 'note', 'test note')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO episodic_memory (id, classification, content) VALUES ('e1', 'event', 'test episode')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO episodic_memory (id, classification, content) VALUES ('e2', 'event', 'test episode 2')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence) VALUES ('sm1', 'facts', 'k1', 'v1', 0.9)`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO procedural_memory (id, name, steps) VALUES ('p1', 'proc1', 'step1,step2')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO relationship_memory (id, entity_id) VALUES ('r1', 'user-1')`)
	_, _ = store.ExecContext(bgCtx,
		`INSERT INTO relationship_memory (id, entity_id) VALUES ('r2', 'user-2')`)

	handler := GetMemoryAnalytics(store)
	req := httptest.NewRequest("GET", "/api/memory/analytics?hours=24", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)

	if body["total_turns"].(float64) != 3 {
		t.Errorf("total_turns = %v, want 3", body["total_turns"])
	}
	if body["retrieval_hits"].(float64) != 2 {
		t.Errorf("retrieval_hits = %v, want 2", body["retrieval_hits"])
	}

	// hit_rate = 2/3.
	hitRate := body["hit_rate"].(float64)
	if hitRate < 0.66 || hitRate > 0.67 {
		t.Errorf("hit_rate = %v, want ~0.6667", hitRate)
	}

	// Memory ROI: avg grade with memory = 5.0, without memory = 3.0, roi = (5-3)/3 = 0.6667.
	memoryROI := body["memory_roi"].(float64)
	if memoryROI < 0.66 || memoryROI > 0.67 {
		t.Errorf("memory_roi = %v, want ~0.6667", memoryROI)
	}

	// Tier distribution should have L0, L1, L2.
	tierDist := body["tier_distribution"].(map[string]any)
	if tierDist["L0"].(float64) != 1 {
		t.Errorf("tier_distribution[L0] = %v, want 1", tierDist["L0"])
	}
	if tierDist["L1"].(float64) != 1 {
		t.Errorf("tier_distribution[L1] = %v, want 1", tierDist["L1"])
	}
	if tierDist["L2"].(float64) != 1 {
		t.Errorf("tier_distribution[L2] = %v, want 1", tierDist["L2"])
	}

	// Entry counts.
	entryCounts := body["entry_counts"].(map[string]any)
	if entryCounts["working"].(float64) != 1 {
		t.Errorf("entry_counts[working] = %v, want 1", entryCounts["working"])
	}
	if entryCounts["episodic"].(float64) != 2 {
		t.Errorf("entry_counts[episodic] = %v, want 2", entryCounts["episodic"])
	}
	if entryCounts["semantic"].(float64) != 1 {
		t.Errorf("entry_counts[semantic] = %v, want 1", entryCounts["semantic"])
	}
	if entryCounts["procedural"].(float64) != 1 {
		t.Errorf("entry_counts[procedural] = %v, want 1", entryCounts["procedural"])
	}
	if entryCounts["relationship"].(float64) != 2 {
		t.Errorf("entry_counts[relationship] = %v, want 2", entryCounts["relationship"])
	}
}

func TestGetMemoryAnalytics_CustomHours(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetMemoryAnalytics(store)
	req := httptest.NewRequest("GET", "/api/memory/analytics?hours=168", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["period_hours"].(float64) != 168 {
		t.Errorf("period_hours = %v, want 168", body["period_hours"])
	}
}

// ---------------------------------------------------------------------------
// Config Raw
// ---------------------------------------------------------------------------

func TestGetConfigRaw_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	configDir := filepath.Join(tmp, ".roboticus")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "[server]\nport = 9090\n"
	if err := os.WriteFile(filepath.Join(configDir, "roboticus.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	handler := GetConfigRaw()
	req := httptest.NewRequest("GET", "/api/config/raw", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if rec.Body.String() != content {
		t.Errorf("body = %q, want %q", rec.Body.String(), content)
	}
}

func TestGetConfigRaw_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	handler := GetConfigRaw()
	req := httptest.NewRequest("GET", "/api/config/raw", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateConfigRaw_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	configDir := filepath.Join(tmp, ".roboticus")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	newContent := "[server]\nport = 8888\n"
	handler := UpdateConfigRaw()
	req := httptest.NewRequest("PUT", "/api/config/raw", strings.NewReader(newContent))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "updated" {
		t.Errorf("status = %v, want updated", body["status"])
	}

	// Verify file was written.
	data, err := os.ReadFile(filepath.Join(configDir, "roboticus.toml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != newContent {
		t.Errorf("file content = %q, want %q", string(data), newContent)
	}
}

func TestUpdateConfigRaw_EmptyBody(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	handler := UpdateConfigRaw()
	req := httptest.NewRequest("PUT", "/api/config/raw", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpdateConfigRaw_CreatesDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// core.WriteConfigRaw now creates .roboticus dir automatically.

	handler := UpdateConfigRaw()
	req := httptest.NewRequest("PUT", "/api/config/raw", strings.NewReader("[server]\nport=1\n"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (auto-creates dir), body = %s", rec.Code, rec.Body.String())
	}
}
