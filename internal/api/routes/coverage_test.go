package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "roboticus/internal/agent"
	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/browser"
	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/internal/mcp"
	"roboticus/internal/plugin"
	"roboticus/testutil"
)

// =============================================================================
// Browser routes
// =============================================================================

func TestBrowserStatus(t *testing.T) {
	b := browser.NewBrowser(browser.BrowserConfig{})
	handler := BrowserStatus(b)
	req := httptest.NewRequest("GET", "/api/browser/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if _, ok := body["running"]; !ok {
		t.Error("expected running field")
	}
	if _, ok := body["cdp_port"]; !ok {
		t.Error("expected cdp_port field")
	}
}

func TestBrowserStart_NoChromium(t *testing.T) {
	b := browser.NewBrowser(browser.BrowserConfig{ExecutablePath: "/nonexistent/chromium"})
	handler := BrowserStart(b)
	req := httptest.NewRequest("POST", "/api/browser/start", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestBrowserStop_NotRunning(t *testing.T) {
	b := browser.NewBrowser(browser.BrowserConfig{})
	handler := BrowserStop(b)
	req := httptest.NewRequest("POST", "/api/browser/stop", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Stop on a non-running browser is either a no-op or error; just verify no panic.
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 200 or 500", rec.Code)
	}
}

func TestBrowserAction_InvalidJSON(t *testing.T) {
	b := browser.NewBrowser(browser.BrowserConfig{})
	handler := BrowserAction(b)
	req := httptest.NewRequest("POST", "/api/browser/action",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBrowserAction_MissingAction(t *testing.T) {
	b := browser.NewBrowser(browser.BrowserConfig{})
	handler := BrowserAction(b)
	req := httptest.NewRequest("POST", "/api/browser/action",
		strings.NewReader(`{"url":"https://example.com"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["detail"] != "action is required" {
		t.Errorf("detail = %v", body["detail"])
	}
}

func TestBrowserAction_ExecuteOnNotRunning(t *testing.T) {
	b := browser.NewBrowser(browser.BrowserConfig{})
	handler := BrowserAction(b)
	req := httptest.NewRequest("POST", "/api/browser/action",
		strings.NewReader(`{"action":"navigate","url":"https://example.com"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Execute on a non-running browser returns a failure result (422).
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["success"] != false {
		t.Errorf("success = %v, want false", body["success"])
	}
}

// =============================================================================
// Plugin routes
// =============================================================================

type stubPlugin struct {
	name    string
	version string
	tools   []plugin.ToolDef
}

func (s *stubPlugin) Name() string    { return s.name }
func (s *stubPlugin) Version() string { return s.version }
func (s *stubPlugin) Tools() []plugin.ToolDef {
	return s.tools
}
func (s *stubPlugin) Init() error { return nil }
func (s *stubPlugin) ExecuteTool(_ context.Context, toolName string, _ json.RawMessage) (*plugin.ToolResult, error) {
	return &plugin.ToolResult{Success: true, Output: "executed " + toolName}, nil
}
func (s *stubPlugin) Shutdown() error { return nil }

func newTestRegistry(t *testing.T) *plugin.Registry {
	t.Helper()
	reg := plugin.NewRegistry(nil, nil, plugin.PermissionPolicy{})
	p := &stubPlugin{
		name:    "test-plugin",
		version: "1.0.0",
		tools:   []plugin.ToolDef{{Name: "test-tool", Description: "A test tool"}},
	}
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	errs := reg.InitAll()
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	return reg
}

func TestListPlugins_NilRegistry(t *testing.T) {
	handler := ListPlugins(nil)
	req := httptest.NewRequest("GET", "/api/plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	plugins := body["plugins"].([]any)
	if len(plugins) != 0 {
		t.Errorf("expected empty plugins, got %d", len(plugins))
	}
}

func TestListPlugins_WithPlugins(t *testing.T) {
	reg := newTestRegistry(t)
	handler := ListPlugins(reg)
	req := httptest.NewRequest("GET", "/api/plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	plugins := body["plugins"].([]any)
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(plugins))
	}
}

func TestListPluginTools_NilRegistry(t *testing.T) {
	handler := ListPluginTools(nil)
	req := httptest.NewRequest("GET", "/api/plugins/tools", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	tools := body["tools"].([]any)
	if len(tools) != 0 {
		t.Errorf("expected empty tools, got %d", len(tools))
	}
}

func TestListPluginTools_WithTools(t *testing.T) {
	reg := newTestRegistry(t)
	handler := ListPluginTools(reg)
	req := httptest.NewRequest("GET", "/api/plugins/tools", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	tools := body["tools"].([]any)
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}

func TestEnablePlugin_NilRegistry(t *testing.T) {
	r := chiRouter("POST", "/api/plugins/{name}/enable", EnablePlugin(nil, nil, nil))
	req := httptest.NewRequest("POST", "/api/plugins/test-plugin/enable", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestEnablePlugin_NotFound(t *testing.T) {
	reg := newTestRegistry(t)
	r := chiRouter("POST", "/api/plugins/{name}/enable", EnablePlugin(reg, nil, nil))
	req := httptest.NewRequest("POST", "/api/plugins/nonexistent/enable", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestEnablePlugin_Success(t *testing.T) {
	reg := newTestRegistry(t)
	tools := agent.NewToolRegistry()
	// First disable it.
	_ = reg.Disable("test-plugin")
	r := chiRouter("POST", "/api/plugins/{name}/enable", EnablePlugin(reg, tools, nil))
	req := httptest.NewRequest("POST", "/api/plugins/test-plugin/enable", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if tools.Get("test-tool") == nil {
		t.Fatal("plugin tool should be synced into main tool registry")
	}
}

func TestDisablePlugin_Success(t *testing.T) {
	reg := newTestRegistry(t)
	tools := agent.NewToolRegistry()
	agenttools.RegisterPluginTools(tools, reg)
	r := chiRouter("POST", "/api/plugins/{name}/disable", DisablePlugin(reg, tools, nil))
	req := httptest.NewRequest("POST", "/api/plugins/test-plugin/disable", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if tools.Get("test-tool") != nil {
		t.Fatal("disabled plugin tool should be removed from main tool registry")
	}
}

func TestDisablePlugin_NilRegistry(t *testing.T) {
	r := chiRouter("POST", "/api/plugins/{name}/disable", DisablePlugin(nil, nil, nil))
	req := httptest.NewRequest("POST", "/api/plugins/test-plugin/disable", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDisablePlugin_NotFound(t *testing.T) {
	reg := newTestRegistry(t)
	r := chiRouter("POST", "/api/plugins/{name}/disable", DisablePlugin(reg, nil, nil))
	req := httptest.NewRequest("POST", "/api/plugins/nonexistent/disable", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExecutePluginTool_NilRegistry(t *testing.T) {
	r := chiRouter("POST", "/api/plugins/tools/{tool}/execute", ExecutePluginTool(nil))
	req := httptest.NewRequest("POST", "/api/plugins/tools/test-tool/execute", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestExecutePluginTool_Success(t *testing.T) {
	reg := newTestRegistry(t)
	r := chiRouter("POST", "/api/plugins/tools/{tool}/execute", ExecutePluginTool(reg))
	req := httptest.NewRequest("POST", "/api/plugins/tools/test-tool/execute",
		strings.NewReader(`{"input":"hello"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["success"] != true {
		t.Errorf("success = %v, want true", body["success"])
	}
}

func TestExecutePluginTool_EmptyBody(t *testing.T) {
	reg := newTestRegistry(t)
	r := chiRouter("POST", "/api/plugins/tools/{tool}/execute", ExecutePluginTool(reg))
	req := httptest.NewRequest("POST", "/api/plugins/tools/test-tool/execute", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty body defaults to {})", rec.Code)
	}
}

func TestExecutePluginTool_NotFound(t *testing.T) {
	reg := newTestRegistry(t)
	r := chiRouter("POST", "/api/plugins/tools/{tool}/execute", ExecutePluginTool(reg))
	req := httptest.NewRequest("POST", "/api/plugins/tools/nonexistent/execute",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// =============================================================================
// MCP routes
// =============================================================================

func TestListMCPConnections_NilManager(t *testing.T) {
	handler := ListMCPConnections(nil)
	req := httptest.NewRequest("GET", "/api/mcp/connections", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	conns := body["connections"].([]any)
	if len(conns) != 0 {
		t.Errorf("expected empty connections, got %d", len(conns))
	}
}

func TestListMCPConnections_EmptyManager(t *testing.T) {
	mgr := mcp.NewConnectionManager()
	handler := ListMCPConnections(mgr)
	req := httptest.NewRequest("GET", "/api/mcp/connections", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	conns := body["connections"].([]any)
	if len(conns) != 0 {
		t.Errorf("expected 0 connections, got %d", len(conns))
	}
}

func TestListMCPTools_NilManager(t *testing.T) {
	handler := ListMCPTools(nil)
	req := httptest.NewRequest("GET", "/api/mcp/tools", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	tools := body["tools"].([]any)
	if len(tools) != 0 {
		t.Errorf("expected empty tools, got %d", len(tools))
	}
}

func TestListMCPTools_EmptyManager(t *testing.T) {
	mgr := mcp.NewConnectionManager()
	handler := ListMCPTools(mgr)
	req := httptest.NewRequest("GET", "/api/mcp/tools", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	// AllTools() may return nil which serializes as JSON null.
	if body["tools"] != nil {
		tools, ok := body["tools"].([]any)
		if ok && len(tools) != 0 {
			t.Errorf("expected 0 tools, got %d", len(tools))
		}
	}
}

func TestConnectMCPServer_NilManager(t *testing.T) {
	handler := ConnectMCPServer(nil, nil)
	req := httptest.NewRequest("POST", "/api/mcp/connect",
		strings.NewReader(`{"name":"test","transport":"stdio","command":"echo"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestConnectMCPServer_InvalidJSON(t *testing.T) {
	mgr := mcp.NewConnectionManager()
	handler := ConnectMCPServer(mgr, nil)
	req := httptest.NewRequest("POST", "/api/mcp/connect",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestConnectMCPServer_BadTransport(t *testing.T) {
	mgr := mcp.NewConnectionManager()
	handler := ConnectMCPServer(mgr, nil)
	req := httptest.NewRequest("POST", "/api/mcp/connect",
		strings.NewReader(`{"name":"test","transport":"unknown"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestDisconnectMCPServer_NilManager(t *testing.T) {
	r := chiRouter("POST", "/api/mcp/disconnect/{name}", DisconnectMCPServer(nil, nil))
	req := httptest.NewRequest("POST", "/api/mcp/disconnect/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDisconnectMCPServer_NotFound(t *testing.T) {
	mgr := mcp.NewConnectionManager()
	r := chiRouter("POST", "/api/mcp/disconnect/{name}", DisconnectMCPServer(mgr, nil))
	req := httptest.NewRequest("POST", "/api/mcp/disconnect/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// =============================================================================
// Approval routes
// =============================================================================

type mockApprovalService struct {
	pending []map[string]any
	all     []map[string]any
	byID    map[string]map[string]any
	errMap  map[string]error
}

func newMockApprovalSvc() *mockApprovalService {
	entry := map[string]any{
		"id":        "apr-1",
		"tool_name": "shell",
		"status":    "pending",
	}
	return &mockApprovalService{
		pending: []map[string]any{entry},
		all:     []map[string]any{entry},
		byID:    map[string]map[string]any{"apr-1": entry},
		errMap:  make(map[string]error),
	}
}

func (m *mockApprovalService) ListAllJSON() []map[string]any     { return m.all }
func (m *mockApprovalService) ListPendingJSON() []map[string]any { return m.pending }
func (m *mockApprovalService) GetJSON(id string) map[string]any  { return m.byID[id] }
func (m *mockApprovalService) Approve(id, _ string) error {
	if err, ok := m.errMap[id]; ok {
		return err
	}
	if _, ok := m.byID[id]; !ok {
		return fmt.Errorf("not found")
	}
	return nil
}
func (m *mockApprovalService) Deny(id, _, _ string) error {
	if err, ok := m.errMap[id]; ok {
		return err
	}
	if _, ok := m.byID[id]; !ok {
		return fmt.Errorf("not found")
	}
	return nil
}

func TestListApprovals_All(t *testing.T) {
	svc := newMockApprovalSvc()
	handler := ListApprovals(svc)
	req := httptest.NewRequest("GET", "/api/approvals", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	approvals := body["approvals"].([]any)
	if len(approvals) != 1 {
		t.Errorf("expected 1 approval, got %d", len(approvals))
	}
}

func TestListApprovals_Pending(t *testing.T) {
	svc := newMockApprovalSvc()
	handler := ListApprovals(svc)
	req := httptest.NewRequest("GET", "/api/approvals?status=pending", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	approvals := body["approvals"].([]any)
	if len(approvals) != 1 {
		t.Errorf("expected 1 pending approval, got %d", len(approvals))
	}
}

func TestListApprovals_NilResult(t *testing.T) {
	svc := &mockApprovalService{
		all:    nil,
		byID:   make(map[string]map[string]any),
		errMap: make(map[string]error),
	}
	handler := ListApprovals(svc)
	req := httptest.NewRequest("GET", "/api/approvals", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	approvals := body["approvals"].([]any)
	if len(approvals) != 0 {
		t.Errorf("expected empty approvals, got %d", len(approvals))
	}
}

func TestGetApproval_Found(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("GET", "/api/approvals/{id}", GetApproval(svc))
	req := httptest.NewRequest("GET", "/api/approvals/apr-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["id"] != "apr-1" {
		t.Errorf("id = %v, want apr-1", body["id"])
	}
}

func TestGetApproval_NotFound(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("GET", "/api/approvals/{id}", GetApproval(svc))
	req := httptest.NewRequest("GET", "/api/approvals/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestApproveRequest_Success(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("POST", "/api/approvals/{id}/approve", ApproveRequest(svc))
	req := httptest.NewRequest("POST", "/api/approvals/apr-1/approve",
		strings.NewReader(`{"operator":"admin"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "approved" {
		t.Errorf("status = %v, want approved", body["status"])
	}
}

func TestApproveRequest_DefaultOperator(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("POST", "/api/approvals/{id}/approve", ApproveRequest(svc))
	req := httptest.NewRequest("POST", "/api/approvals/apr-1/approve",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestApproveRequest_NotFound(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("POST", "/api/approvals/{id}/approve", ApproveRequest(svc))
	req := httptest.NewRequest("POST", "/api/approvals/nonexistent/approve",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDenyRequest_Success(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("POST", "/api/approvals/{id}/deny", DenyRequest(svc))
	req := httptest.NewRequest("POST", "/api/approvals/apr-1/deny",
		strings.NewReader(`{"operator":"admin","reason":"not needed"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["status"] != "denied" {
		t.Errorf("status = %v, want denied", body["status"])
	}
}

func TestDenyRequest_DefaultOperator(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("POST", "/api/approvals/{id}/deny", DenyRequest(svc))
	req := httptest.NewRequest("POST", "/api/approvals/apr-1/deny",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (operator defaults to api)", rec.Code)
	}
}

func TestDenyRequest_NotFound(t *testing.T) {
	svc := newMockApprovalSvc()
	r := chiRouter("POST", "/api/approvals/{id}/deny", DenyRequest(svc))
	req := httptest.NewRequest("POST", "/api/approvals/nonexistent/deny",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// =============================================================================
// Stats: GetCapacity, GetEscalationStats, GetRoutingDiagnostics
// =============================================================================

func TestGetCapacity_NilService(t *testing.T) {
	handler := GetCapacity(nil)
	req := httptest.NewRequest("GET", "/api/stats/capacity", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	providers := body["providers"].(map[string]any)
	if len(providers) != 0 {
		t.Errorf("expected empty providers, got %d", len(providers))
	}
}

func TestGetEscalationStats_NilService(t *testing.T) {
	handler := GetEscalationStats(nil)
	req := httptest.NewRequest("GET", "/api/stats/escalation", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestGetRoutingDiagnostics(t *testing.T) {
	cfg := coverageTestConfig()
	store := testutil.TempStore(t)
	handler := GetRoutingDiagnostics(store, cfg, nil)
	req := httptest.NewRequest("GET", "/api/stats/routing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	config := body["config"].(map[string]any)
	if _, ok := config["routing_mode"]; !ok {
		t.Error("expected routing_mode in config")
	}
	if _, ok := config["policy"]; !ok {
		t.Error("expected policy in config")
	}
	if _, ok := config["persisted_policy"]; !ok {
		t.Error("expected persisted_policy in config")
	}
	if _, ok := config["effective_policy"]; !ok {
		t.Error("expected effective_policy in config")
	}
	if _, ok := config["role_eligibility"]; !ok {
		t.Error("expected role_eligibility in config")
	}
	if _, ok := config["effective_targets"]; !ok {
		t.Error("expected effective_targets in config")
	}
}

// =============================================================================
// Themes: edge cases
// =============================================================================

func TestSetActiveTheme_EmptyThemeID(t *testing.T) {
	store := testutil.TempStore(t)
	handler := SetActiveTheme(store)
	req := httptest.NewRequest("PUT", "/api/themes/active",
		strings.NewReader(`{"theme_id":""}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSetActiveTheme_InvalidJSONBody(t *testing.T) {
	store := testutil.TempStore(t)
	handler := SetActiveTheme(store)
	req := httptest.NewRequest("PUT", "/api/themes/active",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetActiveTheme_DefaultWhenEmpty(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetActiveTheme(store)
	req := httptest.NewRequest("GET", "/api/themes/active", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["id"] != "ai-purple" {
		t.Errorf("id = %v, want ai-purple", body["id"])
	}
}

// =============================================================================
// Channel test route
// =============================================================================

func TestTestChannel_AllPlatforms(t *testing.T) {
	cfg := coverageTestConfig()
	r := chiRouter("GET", "/api/channels/{name}/test", TestChannel(cfg))

	for _, platform := range []string{"telegram", "whatsapp", "discord", "signal", "email", "matrix"} {
		req := httptest.NewRequest("GET", "/api/channels/"+platform+"/test", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("platform %s: status = %d, want 200", platform, rec.Code)
		}
	}
}

func TestTestChannel_UnknownPlatform(t *testing.T) {
	cfg := coverageTestConfig()
	r := chiRouter("GET", "/api/channels/{name}/test", TestChannel(cfg))
	req := httptest.NewRequest("GET", "/api/channels/unknown/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// =============================================================================
// Provider key routes
// =============================================================================

func TestSetProviderKey_NilKeystore(t *testing.T) {
	r := chiRouter("POST", "/api/providers/{provider}/key", SetProviderKey(nil))
	req := httptest.NewRequest("POST", "/api/providers/openai/key",
		strings.NewReader(`{"key":"sk-test"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSetProviderKey_InvalidJSON(t *testing.T) {
	r := chiRouter("POST", "/api/providers/{provider}/key", SetProviderKey(nil))
	req := httptest.NewRequest("POST", "/api/providers/openai/key",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSetProviderKey_EmptyKey(t *testing.T) {
	r := chiRouter("POST", "/api/providers/{provider}/key", SetProviderKey(nil))
	req := httptest.NewRequest("POST", "/api/providers/openai/key",
		strings.NewReader(`{"key":""}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// =============================================================================
// Session detail routes (new coverage)
// =============================================================================

func TestGetSessionFeedback_NoFeedback(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO sessions (id, agent_id, scope_key) VALUES ('s-fb', 'agent1', 'test')`)

	r := chiRouter("GET", "/api/sessions/{id}/feedback", GetSessionFeedback(store))
	req := httptest.NewRequest("GET", "/api/sessions/s-fb/feedback", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	feedback := body["feedback"].([]any)
	if len(feedback) != 0 {
		t.Errorf("expected 0 feedback, got %d", len(feedback))
	}
}

// =============================================================================
// Helpers
// =============================================================================

func TestSanitizeHTML(t *testing.T) {
	input := `<script>alert("xss")</script>`
	got := SanitizeHTML(input)
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("SanitizeHTML did not escape angle brackets: %s", got)
	}
}

func TestIntToNegStr(t *testing.T) {
	if got := intToNegStr(24); got != "-24" {
		t.Errorf("intToNegStr(24) = %q, want -24", got)
	}
}

// =============================================================================
// InstallPlugin route
// =============================================================================

func TestInstallPlugin_InvalidJSON(t *testing.T) {
	cfg := coverageTestConfig()
	handler := InstallPlugin(cfg, nil, nil, nil)
	req := httptest.NewRequest("POST", "/api/plugins/install",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestInstallPlugin_MissingFields(t *testing.T) {
	cfg := coverageTestConfig()
	handler := InstallPlugin(cfg, nil, nil, nil)
	req := httptest.NewRequest("POST", "/api/plugins/install",
		strings.NewReader(`{"name":"test"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestInstallPlugin_Success(t *testing.T) {
	cfg := coverageTestConfig()
	cfg.Plugins.Dir = t.TempDir()
	handler := InstallPlugin(cfg, nil, nil, nil)
	req := httptest.NewRequest("POST", "/api/plugins/install",
		strings.NewReader(`{"name":"my-plugin","content":"print('hello')"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["name"] != "my-plugin" {
		t.Errorf("name = %v", body["name"])
	}
}

func TestInstallPlugin_SourcePathHotRegisters(t *testing.T) {
	cfg := coverageTestConfig()
	cfg.Plugins.Dir = t.TempDir()
	reg := plugin.NewRegistry(nil, nil, plugin.PermissionPolicy{})
	tools := agent.NewToolRegistry()
	embedClient := llm.NewEmbeddingClient(nil)

	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "manifest.toml"), []byte(strings.TrimSpace(`
name = "hot-plugin"
version = "1.0.0"

[[tools]]
name = "echo"
description = "Echo"
`)), 0o644)
	_ = os.WriteFile(filepath.Join(sourceDir, "echo"), []byte("#!/bin/sh\necho ok\n"), 0o755)

	handler := InstallPlugin(cfg, reg, tools, embedClient)
	req := httptest.NewRequest("POST", "/api/plugins/install",
		strings.NewReader(fmt.Sprintf(`{"name":"hot-plugin","source_path":%q}`, sourceDir)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	pluginTools := reg.AllTools()
	if len(pluginTools) != 1 || pluginTools[0].Name != "echo" {
		t.Fatalf("plugin registry tools = %+v, want echo", pluginTools)
	}
	if tools.Get("echo") == nil {
		t.Fatal("plugin tool should be synced into main tool registry")
	}
	var descriptorFound bool
	for _, descriptor := range tools.Descriptors() {
		if descriptor.Name != "echo" {
			continue
		}
		descriptorFound = true
		if len(descriptor.Embedding) == 0 {
			t.Fatal("plugin tool descriptor should be embedded after hot install")
		}
	}
	if !descriptorFound {
		t.Fatal("plugin tool descriptor should exist after hot install")
	}
}

// =============================================================================
// InstallSkillFromCatalog route
// =============================================================================

func TestInstallSkillFromCatalog_InvalidJSON(t *testing.T) {
	cfg := coverageTestConfig()
	store := testutil.TempStore(t)
	handler := InstallSkillFromCatalog(cfg, store)
	req := httptest.NewRequest("POST", "/api/skills/catalog/install",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestInstallSkillFromCatalog_EmptyContent(t *testing.T) {
	cfg := coverageTestConfig()
	store := testutil.TempStore(t)
	handler := InstallSkillFromCatalog(cfg, store)
	req := httptest.NewRequest("POST", "/api/skills/catalog/install",
		strings.NewReader(`{"name":"test","content":""}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func coverageTestConfig() *core.Config {
	cfg := core.DefaultConfig()
	return &cfg
}
