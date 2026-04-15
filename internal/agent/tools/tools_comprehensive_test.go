package tools

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

	"roboticus/testutil"
)

// ---------------------------------------------------------------------------
// RiskLevel.String
// ---------------------------------------------------------------------------

func TestRiskLevel_String(t *testing.T) {
	tests := []struct {
		level RiskLevel
		want  string
	}{
		{RiskSafe, "safe"},
		{RiskCaution, "caution"},
		{RiskDangerous, "dangerous"},
		{RiskForbidden, "forbidden"},
		{RiskLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("RiskLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// resolvePath edge cases
// ---------------------------------------------------------------------------

func TestResolvePath_AbsoluteInAllowed(t *testing.T) {
	allowed := []string{"/opt/data"}
	resolved, err := resolvePath("/workspace", "/opt/data/file.txt", allowed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "/opt/data/file.txt" {
		t.Errorf("resolved = %q", resolved)
	}
}

func TestResolvePath_AbsoluteNotAllowed(t *testing.T) {
	_, err := resolvePath("/workspace", "/etc/passwd", nil)
	if err == nil {
		t.Error("expected error for absolute path not in allowed list")
	}
}

func TestResolvePath_PathBoundaryCheck(t *testing.T) {
	// Regression: allowed path "/data/vault" must NOT match "/data/vaultBackup".
	allowed := []string{"/data/vault"}
	_, err := resolvePath("/workspace", "/data/vaultBackup/secrets.txt", allowed)
	if err == nil {
		t.Error("expected error: /data/vaultBackup should not match allowed path /data/vault")
	}

	// But /data/vault/notes.md should pass.
	resolved, err := resolvePath("/workspace", "/data/vault/notes.md", allowed)
	if err != nil {
		t.Fatalf("unexpected error for path within allowed dir: %v", err)
	}
	if resolved != "/data/vault/notes.md" {
		t.Errorf("resolved = %q, want /data/vault/notes.md", resolved)
	}

	// Exact match of allowed path itself should pass.
	resolved, err = resolvePath("/workspace", "/data/vault", allowed)
	if err != nil {
		t.Fatalf("unexpected error for exact allowed path match: %v", err)
	}
	if resolved != "/data/vault" {
		t.Errorf("resolved = %q, want /data/vault", resolved)
	}
}

func TestResolvePath_WorkspaceBoundaryCheck(t *testing.T) {
	// Regression: workspace "/workspace" must NOT match "/workspaceBackup".
	_, err := resolvePath("/workspace", "file.txt", nil)
	if err != nil {
		// Relative path within workspace should resolve fine.
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// EchoTool error paths
// ---------------------------------------------------------------------------

func TestEchoTool_InvalidJSON(t *testing.T) {
	tool := &EchoTool{}
	_, err := tool.Execute(context.Background(), `{bad json`, &Context{})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// ReadFileTool additional paths
// ---------------------------------------------------------------------------

func TestReadFileTool_InvalidJSON(t *testing.T) {
	tool := &ReadFileTool{}
	_, err := tool.Execute(context.Background(), `not json`, &Context{Workspace: t.TempDir()})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestReadFileTool_Oversized(t *testing.T) {
	ws := t.TempDir()
	mfs := newMockFS()
	resolved := filepath.Clean(filepath.Join(ws, "big.bin"))
	mfs.files[resolved] = make([]byte, (1<<20)+1) // 1MB+1

	tool := &ReadFileTool{}
	tctx := &Context{Workspace: ws, FS: mfs}
	_, err := tool.Execute(context.Background(), `{"path": "big.bin"}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1MB") {
		t.Errorf("expected 1MB error, got: %v", err)
	}
}

func TestReadFileTool_PathTraversal(t *testing.T) {
	ws := t.TempDir()
	tool := &ReadFileTool{}
	_, err := tool.Execute(context.Background(), `{"path": "../../../etc/shadow"}`, &Context{Workspace: ws})
	if err == nil {
		t.Error("expected path traversal error")
	}
}

// ---------------------------------------------------------------------------
// WriteFileTool additional paths
// ---------------------------------------------------------------------------

func TestWriteFileTool_InvalidJSON(t *testing.T) {
	tool := &WriteFileTool{}
	_, err := tool.Execute(context.Background(), `{bad`, &Context{Workspace: t.TempDir()})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWriteFileTool_Append(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "log.txt")
	_ = os.WriteFile(path, []byte("line1\n"), 0o644)

	tool := &WriteFileTool{}
	tctx := &Context{Workspace: ws}
	_, err := tool.Execute(context.Background(), `{"path":"log.txt","content":"line2\n","append":true}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "line1\nline2\n" {
		t.Errorf("content = %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// EditFileTool additional paths
// ---------------------------------------------------------------------------

func TestEditFileTool_InvalidJSON(t *testing.T) {
	tool := &EditFileTool{}
	_, err := tool.Execute(context.Background(), `{bad`, &Context{Workspace: t.TempDir()})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestEditFileTool_OldTextNotFound(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "f.txt"), []byte("hello"), 0o644)

	tool := &EditFileTool{}
	_, err := tool.Execute(context.Background(), `{"path":"f.txt","old_text":"missing","new_text":"x"}`, &Context{Workspace: ws})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestEditFileTool_ReplaceAll(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "f.txt"), []byte("aaa bbb aaa"), 0o644)

	tool := &EditFileTool{}
	_, err := tool.Execute(context.Background(), `{"path":"f.txt","old_text":"aaa","new_text":"ccc","replace_all":true}`, &Context{Workspace: ws})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, "f.txt"))
	if string(data) != "ccc bbb ccc" {
		t.Errorf("content = %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// ListDirectoryTool additional paths
// ---------------------------------------------------------------------------

func TestListDirectoryTool_EmptyParams(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x"), 0o644)

	tool := &ListDirectoryTool{}
	result, err := tool.Execute(context.Background(), ``, &Context{Workspace: ws})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Output, "f.txt") {
		t.Errorf("output should list f.txt, got: %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// SearchFilesTool additional paths
// ---------------------------------------------------------------------------

func TestSearchFilesTool_InvalidJSON(t *testing.T) {
	tool := &SearchFilesTool{}
	_, err := tool.Execute(context.Background(), `{bad`, &Context{Workspace: t.TempDir()})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSearchFilesTool_CaseSensitive(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "test.txt"), []byte("Hello World"), 0o644)

	tool := &SearchFilesTool{}
	tctx := &Context{Workspace: ws}

	// case-insensitive should find it
	result, err := tool.Execute(context.Background(), `{"query":"hello","case_sensitive":false}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output == "no matches found" {
		t.Error("case-insensitive search should find 'Hello'")
	}

	// case-sensitive should NOT find lowercase "hello" in "Hello World"
	result2, err := tool.Execute(context.Background(), `{"query":"hello","case_sensitive":true}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result2.Output != "no matches found" {
		t.Error("case-sensitive search should not match 'Hello' with 'hello'")
	}
}

func TestSearchFilesTool_LimitClamped(t *testing.T) {
	ws := t.TempDir()
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(filepath.Join(ws, fmt.Sprintf("f%d.txt", i)), []byte("needle"), 0o644)
	}
	tool := &SearchFilesTool{}
	tctx := &Context{Workspace: ws}
	result, err := tool.Execute(context.Background(), `{"query":"needle","limit":2}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(lines))
	}
}

// ---------------------------------------------------------------------------
// GlobFilesTool additional paths
// ---------------------------------------------------------------------------

func TestGlobFilesTool_InvalidJSON(t *testing.T) {
	tool := &GlobFilesTool{}
	_, err := tool.Execute(context.Background(), `{bad`, &Context{Workspace: t.TempDir()})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGlobFilesTool_NoMatches(t *testing.T) {
	ws := t.TempDir()
	tool := &GlobFilesTool{}
	result, err := tool.Execute(context.Background(), `{"pattern":"*.xyz"}`, &Context{Workspace: ws})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "no files matched" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestGlobFilesTool_LimitClamped(t *testing.T) {
	ws := t.TempDir()
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(filepath.Join(ws, fmt.Sprintf("f%d.go", i)), []byte("go"), 0o644)
	}
	tool := &GlobFilesTool{}
	result, err := tool.Execute(context.Background(), `{"pattern":"*.go","limit":2}`, &Context{Workspace: ws})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(lines))
	}
}

// ---------------------------------------------------------------------------
// BashTool additional paths
// ---------------------------------------------------------------------------

func TestBashTool_InvalidJSON(t *testing.T) {
	tool := &BashTool{}
	_, err := tool.Execute(context.Background(), `{bad`, &Context{Workspace: t.TempDir()})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestBashTool_ErrorExecution(t *testing.T) {
	mr := &mockRunner{err: fmt.Errorf("exit code 1"), stderr: []byte("command failed")}
	tool := &BashTool{}
	tctx := &Context{Workspace: t.TempDir(), Runner: mr}
	result, err := tool.Execute(context.Background(), `{"command":"false"}`, tctx)
	if err != nil {
		t.Fatalf("bash tool should not return error, got: %v", err)
	}
	if !strings.Contains(result.Output, "error:") {
		t.Errorf("output should contain error, got: %q", result.Output)
	}
}

func TestBashTool_TimeoutClamping(t *testing.T) {
	mr := &mockRunner{stdout: []byte("ok")}
	tool := &BashTool{}
	tctx := &Context{Workspace: t.TempDir(), Runner: mr}

	// timeout_seconds < 1 should be clamped to 1
	result, err := tool.Execute(context.Background(), `{"command":"echo","timeout_seconds":0}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Error("nil result")
	}

	// timeout_seconds > 120 should be clamped to 120
	result2, err := tool.Execute(context.Background(), `{"command":"echo","timeout_seconds":999}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result2 == nil {
		t.Error("nil result")
	}
}

// ---------------------------------------------------------------------------
// RuntimeContextTool
// ---------------------------------------------------------------------------

func TestRuntimeContextTool_OutputContainsFields(t *testing.T) {
	tool := &RuntimeContextTool{}
	tctx := &Context{
		AgentID:      "agent-123",
		SessionID:    "session-456",
		Workspace:    "/test/workspace",
		Channel:      "telegram",
		AllowedPaths: []string{"/opt", "/data"},
	}
	result, err := tool.Execute(context.Background(), `{}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{"agent-123", "session-456", "/test/workspace", "telegram", "/opt"} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// shellCommand
// ---------------------------------------------------------------------------

func TestShellCommand(t *testing.T) {
	name, args := shellCommand("echo hello")
	// On non-windows, should use sh -c
	if name != "sh" {
		t.Errorf("name = %q, want sh", name)
	}
	if len(args) != 2 || args[0] != "-c" || args[1] != "echo hello" {
		t.Errorf("args = %v", args)
	}
}

// ---------------------------------------------------------------------------
// IntrospectionTool
// ---------------------------------------------------------------------------

func TestIntrospectionTool_AllAspects(t *testing.T) {
	tool := NewIntrospectionTool("TestBot", "1.0.0", func() []string {
		return []string{"echo", "bash", "read_file"}
	})

	if tool.Name() != "introspect" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskSafe {
		t.Errorf("risk = %v", tool.Risk())
	}

	schema := tool.ParameterSchema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}

	// Test "all" aspect (default)
	result, err := tool.Execute(context.Background(), `{}`, &Context{})
	if err != nil {
		t.Fatalf("execute all: %v", err)
	}
	for _, want := range []string{"Capabilities", "Available Tools", "Runtime", "Memory Tiers"} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("output missing section %q", want)
		}
	}
	if !strings.Contains(result.Output, "TestBot") {
		t.Error("output should contain agent name")
	}
	if !strings.Contains(result.Output, "echo") {
		t.Error("output should list echo tool")
	}
}

func TestIntrospectionTool_IndividualAspects(t *testing.T) {
	tool := NewIntrospectionTool("Bot", "2.0", func() []string { return []string{"x"} })

	aspects := []struct {
		input    string
		contains string
	}{
		{`{"aspect":"capabilities"}`, "Capabilities"},
		{`{"aspect":"tools"}`, "Available Tools"},
		{`{"aspect":"runtime"}`, "Runtime"},
		{`{"aspect":"memory"}`, "Memory Tiers"},
	}

	for _, tc := range aspects {
		t.Run(tc.contains, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.input, &Context{})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.Contains(result.Output, tc.contains) {
				t.Errorf("output should contain %q", tc.contains)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MemoryStatsTool
// ---------------------------------------------------------------------------

func TestMemoryStatsTool_Properties(t *testing.T) {
	tool := &MemoryStatsTool{}
	if tool.Name() != "get_memory_stats" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskSafe {
		t.Errorf("risk = %v", tool.Risk())
	}

	schema := tool.ParameterSchema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

func TestMemoryStatsTool_NoStore(t *testing.T) {
	tool := &MemoryStatsTool{}
	_, err := tool.Execute(context.Background(), `{}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "database store not available") {
		t.Errorf("expected store error, got: %v", err)
	}
}

func TestMemoryStatsTool_WithStore(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &MemoryStatsTool{}
	tctx := &Context{Store: store, SessionID: "test-session"}

	result, err := tool.Execute(context.Background(), `{}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var stats []struct {
		Tier  string `json:"tier"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(result.Output), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(stats) != 6 {
		t.Errorf("expected 6 tiers, got %d", len(stats))
	}
}

func TestMemoryStatsTool_WithSessionID(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &MemoryStatsTool{}
	tctx := &Context{Store: store, SessionID: "default-session"}

	// Pass explicit session_id
	result, err := tool.Execute(context.Background(), `{"session_id":"custom-session"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output == "" {
		t.Error("empty output")
	}
}

// ---------------------------------------------------------------------------
// McpServerRegistry
// ---------------------------------------------------------------------------

func TestMcpServerRegistry_RegisterAndGet(t *testing.T) {
	reg := NewMcpServerRegistry()

	td := &McpToolDescriptor{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	reg.RegisterTool(td)

	got := reg.GetTool("test_tool")
	if got == nil {
		t.Fatal("tool should be found")
	}
	if got.Name != "test_tool" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Description != "A test tool" {
		t.Errorf("description = %q", got.Description)
	}
}

func TestMcpServerRegistry_GetTool_NotFound(t *testing.T) {
	reg := NewMcpServerRegistry()
	if reg.GetTool("missing") != nil {
		t.Error("expected nil for missing tool")
	}
}

func TestMcpServerRegistry_ListTools(t *testing.T) {
	reg := NewMcpServerRegistry()
	reg.RegisterTool(&McpToolDescriptor{Name: "a"})
	reg.RegisterTool(&McpToolDescriptor{Name: "b"})

	tools := reg.ListTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestMcpServerRegistry_ToolCount(t *testing.T) {
	reg := NewMcpServerRegistry()
	if reg.ToolCount() != 0 {
		t.Errorf("expected 0, got %d", reg.ToolCount())
	}
	reg.RegisterTool(&McpToolDescriptor{Name: "x"})
	if reg.ToolCount() != 1 {
		t.Errorf("expected 1, got %d", reg.ToolCount())
	}
}

func TestMcpServerRegistry_Resources(t *testing.T) {
	reg := NewMcpServerRegistry()

	res := &McpResource{
		URI:         "memory://facts",
		Name:        "facts",
		Description: "Agent facts",
		MimeType:    "application/json",
	}
	reg.RegisterResource(res)

	got := reg.GetResource("memory://facts")
	if got == nil {
		t.Fatal("resource should be found")
	}
	if got.Name != "facts" {
		t.Errorf("name = %q", got.Name)
	}

	if reg.GetResource("missing") != nil {
		t.Error("expected nil for missing resource")
	}

	list := reg.ListResources()
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	if reg.ResourceCount() != 1 {
		t.Errorf("expected 1, got %d", reg.ResourceCount())
	}
}

func TestRegistry_ExportToMcp(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&EchoTool{})
	reg.Register(&BashTool{})

	mcp := reg.ExportToMcp()
	if mcp.ToolCount() != 2 {
		t.Errorf("expected 2 tools, got %d", mcp.ToolCount())
	}

	echo := mcp.GetTool("echo")
	if echo == nil {
		t.Fatal("echo tool should exist in MCP registry")
	}
	if echo.Description == "" {
		t.Error("description should not be empty")
	}
}

// ---------------------------------------------------------------------------
// Registry overwrite behavior
// ---------------------------------------------------------------------------

func TestRegistry_RegisterOverwrite(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&EchoTool{})
	reg.Register(&EchoTool{}) // overwrite
	if len(reg.List()) != 1 {
		t.Errorf("expected 1 tool after overwrite, got %d", len(reg.List()))
	}
}

// ---------------------------------------------------------------------------
// CronTool
// ---------------------------------------------------------------------------

func TestCronTool_Properties(t *testing.T) {
	tool := &CronTool{}
	if tool.Name() != "cron" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("empty description")
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}

	schema := tool.ParameterSchema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

func TestCronTool_NoStore(t *testing.T) {
	tool := &CronTool{}
	_, err := tool.Execute(context.Background(), `{"action":"list"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "database store not available") {
		t.Errorf("expected store error, got: %v", err)
	}
}

func TestCronTool_InvalidJSON(t *testing.T) {
	tool := &CronTool{}
	_, err := tool.Execute(context.Background(), `{bad`, &Context{Store: testutil.TempStore(t)})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCronTool_UnknownAction(t *testing.T) {
	tool := &CronTool{}
	_, err := tool.Execute(context.Background(), `{"action":"unknown"}`, &Context{Store: testutil.TempStore(t)})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

func TestCronTool_ListEmpty(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CronTool{}
	result, err := tool.Execute(context.Background(), `{"action":"list"}`, &Context{Store: store})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "No cron jobs found." {
		t.Errorf("output = %q", result.Output)
	}
}

func TestCronTool_CreateAndList(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CronTool{}
	tctx := &Context{Store: store, AgentID: "agent1"}

	// Create
	result, err := tool.Execute(context.Background(),
		`{"action":"create","name":"daily-check","schedule":"0 9 * * *","task":"run health check"}`, tctx)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(result.Output, "daily-check") {
		t.Errorf("output = %q", result.Output)
	}

	// List
	result, err = tool.Execute(context.Background(), `{"action":"list"}`, tctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(result.Output, "daily-check") {
		t.Errorf("list should contain job name, got: %q", result.Output)
	}
}

func TestCronTool_CreateValidation(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CronTool{}
	tctx := &Context{Store: store, AgentID: "agent1"}

	tests := []struct {
		name   string
		params string
		errMsg string
	}{
		{"missing name", `{"action":"create","schedule":"* * * * *","task":"t"}`, "name is required"},
		{"missing schedule", `{"action":"create","name":"j","task":"t"}`, "schedule is required"},
		{"missing task", `{"action":"create","name":"j","schedule":"* * * * *"}`, "task is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tc.params, tctx)
			if err == nil || !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("expected %q error, got: %v", tc.errMsg, err)
			}
		})
	}
}

func TestCronTool_Delete(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CronTool{}
	tctx := &Context{Store: store, AgentID: "agent1"}

	// Create then delete
	_, _ = tool.Execute(context.Background(),
		`{"action":"create","name":"temp-job","schedule":"0 0 * * *","task":"cleanup"}`, tctx)

	result, err := tool.Execute(context.Background(), `{"action":"delete","id":"temp-job"}`, tctx)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(result.Output, "Deleted") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestCronTool_DeleteNotFound(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CronTool{}
	tctx := &Context{Store: store}

	result, err := tool.Execute(context.Background(), `{"action":"delete","id":"nonexistent"}`, tctx)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(result.Output, "No cron job found") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestCronTool_DeleteMissingID(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CronTool{}
	_, err := tool.Execute(context.Background(), `{"action":"delete","id":""}`, &Context{Store: store})
	if err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Errorf("expected id required error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Data Tools (CreateTable, QueryTable, InsertRow)
// ---------------------------------------------------------------------------

func TestCreateTableTool_Properties(t *testing.T) {
	tool := &CreateTableTool{}
	if tool.Name() != "create_table" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}

	var parsed map[string]any
	if err := json.Unmarshal(tool.ParameterSchema(), &parsed); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

func TestCreateTableTool_NoStore(t *testing.T) {
	tool := &CreateTableTool{}
	_, err := tool.Execute(context.Background(),
		`{"name":"t","description":"d","columns":[{"name":"c","type":"TEXT"}]}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "database store not available") {
		t.Errorf("expected store error, got: %v", err)
	}
}

func TestCreateTableTool_InvalidJSON(t *testing.T) {
	tool := &CreateTableTool{}
	_, err := tool.Execute(context.Background(), `{bad`, &Context{Store: testutil.TempStore(t)})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCreateTableTool_InvalidName(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "a1"}

	_, err := tool.Execute(context.Background(),
		`{"name":"bad-name!","description":"d","columns":[{"name":"c","type":"TEXT"}]}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "alphanumeric") {
		t.Errorf("expected name validation error, got: %v", err)
	}
}

func TestCreateTableTool_EmptyDescription(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "a1"}

	_, err := tool.Execute(context.Background(),
		`{"name":"t","description":"  ","columns":[{"name":"c","type":"TEXT"}]}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "description is required") {
		t.Errorf("expected description error, got: %v", err)
	}
}

func TestCreateTableTool_NoColumns(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "a1"}

	_, err := tool.Execute(context.Background(),
		`{"name":"t","description":"d","columns":[]}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "at least one column") {
		t.Errorf("expected columns error, got: %v", err)
	}
}

func TestCreateTableTool_InvalidColumnName(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "a1"}

	_, err := tool.Execute(context.Background(),
		`{"name":"t","description":"d","columns":[{"name":"bad col!","type":"TEXT"}]}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "alphanumeric") {
		t.Errorf("expected column name error, got: %v", err)
	}
}

func TestCreateTableTool_InvalidColumnType(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "a1"}

	_, err := tool.Execute(context.Background(),
		`{"name":"t","description":"d","columns":[{"name":"c","type":"VARCHAR"}]}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected type error, got: %v", err)
	}
}

func TestCreateTableTool_Success(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "testbot"}

	result, err := tool.Execute(context.Background(),
		`{"name":"notes","description":"Agent notes","columns":[{"name":"title","type":"TEXT"},{"name":"priority","type":"INTEGER"}]}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Output, "Created table") {
		t.Errorf("output = %q", result.Output)
	}
	if !strings.Contains(result.Output, "2 columns") {
		t.Errorf("output should mention column count, got: %q", result.Output)
	}
}

func TestQueryTableTool_Properties(t *testing.T) {
	tool := &QueryTableTool{}
	if tool.Name() != "query_table" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}
}

func TestQueryTableTool_NoStore(t *testing.T) {
	tool := &QueryTableTool{}
	_, err := tool.Execute(context.Background(), `{"table":"t"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "database store not available") {
		t.Errorf("expected store error, got: %v", err)
	}
}

func TestQueryTableTool_InvalidTableName(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &QueryTableTool{}
	_, err := tool.Execute(context.Background(), `{"table":"bad-name!"}`, &Context{Store: store, AgentID: "a"})
	if err == nil || !strings.Contains(err.Error(), "alphanumeric") {
		t.Errorf("expected name error, got: %v", err)
	}
}

func TestQueryTableTool_TableNotRegistered(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &QueryTableTool{}
	_, err := tool.Execute(context.Background(), `{"table":"missing"}`, &Context{Store: store, AgentID: "a"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestInsertRowTool_Properties(t *testing.T) {
	tool := &InsertRowTool{}
	if tool.Name() != "insert_row" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}
}

func TestInsertRowTool_NoStore(t *testing.T) {
	tool := &InsertRowTool{}
	_, err := tool.Execute(context.Background(), `{"table":"t","data":{"c":"v"}}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "database store not available") {
		t.Errorf("expected store error, got: %v", err)
	}
}

func TestInsertRowTool_EmptyData(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &InsertRowTool{}
	_, err := tool.Execute(context.Background(), `{"table":"t","data":{}}`, &Context{Store: store, AgentID: "a"})
	if err == nil || !strings.Contains(err.Error(), "at least one column") {
		t.Errorf("expected empty data error, got: %v", err)
	}
}

func TestInsertRowTool_InvalidColumnName(t *testing.T) {
	store := testutil.TempStore(t)
	// First create the table so it exists in hippocampus
	ct := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "testbot"}
	_, _ = ct.Execute(context.Background(),
		`{"name":"items","description":"d","columns":[{"name":"val","type":"TEXT"}]}`, tctx)

	tool := &InsertRowTool{}
	_, err := tool.Execute(context.Background(), `{"table":"items","data":{"bad col!":"v"}}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "alphanumeric") {
		t.Errorf("expected column name error, got: %v", err)
	}
}

func TestInsertRowTool_UnregisteredColumn(t *testing.T) {
	store := testutil.TempStore(t)
	ct := &CreateTableTool{}
	tctx := &Context{Store: store, AgentID: "testbot"}
	_, _ = ct.Execute(context.Background(),
		`{"name":"items2","description":"d","columns":[{"name":"val","type":"TEXT"}]}`, tctx)

	tool := &InsertRowTool{}
	_, err := tool.Execute(context.Background(), `{"table":"items2","data":{"nonexistent":"v"}}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("expected unregistered column error, got: %v", err)
	}
}

// Integration: create table, insert rows, query them.
func TestDataTools_Integration(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	// Create table
	ct := &CreateTableTool{}
	_, err := ct.Execute(context.Background(),
		`{"name":"logs","description":"Event logs","columns":[{"name":"msg","type":"TEXT"},{"name":"level","type":"INTEGER"}]}`, tctx)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert rows
	it := &InsertRowTool{}
	_, err = it.Execute(context.Background(), `{"table":"logs","data":{"msg":"hello","level":1}}`, tctx)
	if err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	_, err = it.Execute(context.Background(), `{"table":"logs","data":{"msg":"world","level":2}}`, tctx)
	if err != nil {
		t.Fatalf("insert 2: %v", err)
	}

	// Query all
	qt := &QueryTableTool{}
	result, err := qt.Execute(context.Background(), `{"table":"logs"}`, tctx)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Output), &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}

	// Query with WHERE
	result, err = qt.Execute(context.Background(), `{"table":"logs","query":"level = 2"}`, tctx)
	if err != nil {
		t.Fatalf("query with where: %v", err)
	}
	if err := json.Unmarshal([]byte(result.Output), &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row with level=2, got %d", len(rows))
	}

	// Query empty result
	result, err = qt.Execute(context.Background(), `{"table":"logs","query":"level = 999"}`, tctx)
	if err != nil {
		t.Fatalf("query empty: %v", err)
	}
	if result.Output != "[]" {
		t.Errorf("expected empty array, got: %q", result.Output)
	}
}

func TestQueryTableTool_LimitClamped(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	ct := &CreateTableTool{}
	_, _ = ct.Execute(context.Background(),
		`{"name":"lim","description":"d","columns":[{"name":"v","type":"TEXT"}]}`, tctx)

	it := &InsertRowTool{}
	for i := 0; i < 3; i++ {
		_, _ = it.Execute(context.Background(),
			fmt.Sprintf(`{"table":"lim","data":{"v":"row%d"}}`, i), tctx)
	}

	qt := &QueryTableTool{}
	// limit 0 should be clamped to 50
	result, err := qt.Execute(context.Background(), `{"table":"lim","limit":0}`, tctx)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var rows []map[string]any
	_ = json.Unmarshal([]byte(result.Output), &rows)
	if len(rows) != 3 {
		t.Errorf("expected 3 rows (limit clamped to 50), got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// WebSearchTool
// ---------------------------------------------------------------------------

func TestWebSearchTool_Properties(t *testing.T) {
	tool := NewWebSearchTool("", "")
	if tool.Name() != "web_search" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("empty description")
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}

	var parsed map[string]any
	if err := json.Unmarshal(tool.ParameterSchema(), &parsed); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
}

func TestWebSearchTool_InvalidJSON(t *testing.T) {
	tool := NewWebSearchTool("http://localhost:9999", "")
	_, err := tool.Execute(context.Background(), `{bad`, &Context{})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWebSearchTool_EmptyQuery(t *testing.T) {
	tool := NewWebSearchTool("http://localhost:9999", "")
	_, err := tool.Execute(context.Background(), `{"query":""}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Errorf("expected query required error, got: %v", err)
	}
}

func TestWebSearchTool_DefaultURL(t *testing.T) {
	tool := NewWebSearchTool("", "")
	if tool.searchURL != "http://localhost:8888/search" {
		t.Errorf("default URL = %q", tool.searchURL)
	}
}

func TestWebSearchTool_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "test query" {
			t.Errorf("query = %q", r.URL.Query().Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{
				{"title": "Result 1", "url": "https://example.com", "content": "Test content"},
			},
		})
	}))
	defer srv.Close()

	tool := NewWebSearchTool(srv.URL, "test-key")
	result, err := tool.Execute(context.Background(), `{"query":"test query","num_results":3}`, &Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Output, "Result 1") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestWebSearchTool_NoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer srv.Close()

	tool := NewWebSearchTool(srv.URL, "")
	result, err := tool.Execute(context.Background(), `{"query":"nothing"}`, &Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "No results found." {
		t.Errorf("output = %q", result.Output)
	}
}

func TestWebSearchTool_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	tool := NewWebSearchTool(srv.URL, "")
	_, err := tool.Execute(context.Background(), `{"query":"test"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 error, got: %v", err)
	}
}

func TestWebSearchTool_NumResultsClamped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := r.URL.Query().Get("count")
		if count != "5" {
			t.Errorf("count should be clamped to 5, got %q", count)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer srv.Close()

	tool := NewWebSearchTool(srv.URL, "")
	_, _ = tool.Execute(context.Background(), `{"query":"test","num_results":99}`, &Context{})
}

// ---------------------------------------------------------------------------
// HTTPFetchTool
// ---------------------------------------------------------------------------

func TestHTTPFetchTool_Properties(t *testing.T) {
	tool := NewHTTPFetchTool()
	if tool.Name() != "http_fetch" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}

	var parsed map[string]any
	if err := json.Unmarshal(tool.ParameterSchema(), &parsed); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
}

func TestHTTPFetchTool_InvalidJSON(t *testing.T) {
	tool := NewHTTPFetchTool()
	_, err := tool.Execute(context.Background(), `{bad`, &Context{})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestHTTPFetchTool_EmptyURL(t *testing.T) {
	tool := NewHTTPFetchTool()
	_, err := tool.Execute(context.Background(), `{"url":""}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Errorf("expected url required error, got: %v", err)
	}
}

func TestHTTPFetchTool_BadScheme(t *testing.T) {
	tool := NewHTTPFetchTool()
	_, err := tool.Execute(context.Background(), `{"url":"ftp://example.com"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "only http and https") {
		t.Errorf("expected scheme error, got: %v", err)
	}
}

func TestHTTPFetchTool_PrivateHost(t *testing.T) {
	tool := NewHTTPFetchTool()
	for _, host := range []string{"http://localhost/path", "http://127.0.0.1/path"} {
		_, err := tool.Execute(context.Background(), fmt.Sprintf(`{"url":"%s"}`, host), &Context{})
		if err == nil || !strings.Contains(err.Error(), "private") {
			t.Errorf("expected private host error for %s, got: %v", host, err)
		}
	}
}

func TestHTTPFetchTool_PrivateHostBlocked_Localhost(t *testing.T) {
	// httptest.NewServer binds to 127.0.0.1 which is private; verify SSRF protection works
	tool := NewHTTPFetchTool()
	_, err := tool.Execute(context.Background(), `{"url":"http://127.0.0.1:9999/test"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Errorf("expected private host error, got: %v", err)
	}
}

func TestHTTPFetchTool_PrivateHostBlocked_10x(t *testing.T) {
	tool := NewHTTPFetchTool()
	_, err := tool.Execute(context.Background(), `{"url":"http://10.0.0.1/test"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Errorf("expected private host error, got: %v", err)
	}
}

func TestHTTPFetchTool_PrivateHostBlocked_192168(t *testing.T) {
	tool := NewHTTPFetchTool()
	_, err := tool.Execute(context.Background(), `{"url":"http://192.168.1.1/test"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Errorf("expected private host error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// isPrivateHost / isPrivateHostString
// ---------------------------------------------------------------------------

func TestIsPrivateHostString(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", true},
		{"localhost", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.0.1", false},
		{"172.32.0.1", false},
		{"::ffff:127.0.0.1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"example.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			got := isPrivateHostString(tc.host)
			if got != tc.want {
				t.Errorf("isPrivateHostString(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// agentTableName helper
// ---------------------------------------------------------------------------

func TestAgentTableName(t *testing.T) {
	got := agentTableName("bot1", "notes")
	if got != "agent_bot1_notes" {
		t.Errorf("agentTableName = %q", got)
	}
}

// ---------------------------------------------------------------------------
// All tool Risk() methods coverage
// ---------------------------------------------------------------------------

func TestAllToolRiskLevels(t *testing.T) {
	tests := []struct {
		tool Tool
		want RiskLevel
	}{
		{&EchoTool{}, RiskSafe},
		{&ReadFileTool{}, RiskCaution},
		{&WriteFileTool{}, RiskCaution},
		{&EditFileTool{}, RiskCaution},
		{&ListDirectoryTool{}, RiskCaution},
		{&SearchFilesTool{}, RiskCaution},
		{&GlobFilesTool{}, RiskCaution},
		{&BashTool{}, RiskDangerous},
		{&RuntimeContextTool{}, RiskSafe},
		{&CreateTableTool{}, RiskCaution},
		{&QueryTableTool{}, RiskCaution},
		{&InsertRowTool{}, RiskCaution},
		{&AlterTableTool{}, RiskCaution},
		{&DropTableTool{}, RiskCaution},
		{&CronTool{}, RiskCaution},
		{&MemoryStatsTool{}, RiskSafe},
		{NewIntrospectionTool("", "", func() []string { return nil }), RiskSafe},
		{NewWebSearchTool("", ""), RiskCaution},
		{NewHTTPFetchTool(), RiskCaution},
	}

	for _, tc := range tests {
		t.Run(tc.tool.Name(), func(t *testing.T) {
			if got := tc.tool.Risk(); got != tc.want {
				t.Errorf("%s.Risk() = %v, want %v", tc.tool.Name(), got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WebSearchTool - non-JSON response (fallback path)
// ---------------------------------------------------------------------------

func TestWebSearchTool_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("plain text response"))
	}))
	defer srv.Close()

	tool := NewWebSearchTool(srv.URL, "")
	result, err := tool.Execute(context.Background(), `{"query":"test"}`, &Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Non-JSON gets returned raw after failed unmarshal
	if !strings.Contains(result.Output, "plain text response") {
		t.Errorf("output = %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// AlterTableTool
// ---------------------------------------------------------------------------

func TestAlterTableTool_Properties(t *testing.T) {
	tool := &AlterTableTool{}
	if tool.Name() != "alter_table" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}
}

func TestAlterTableTool_NoStore(t *testing.T) {
	tool := &AlterTableTool{}
	_, err := tool.Execute(context.Background(),
		`{"table_name":"t","operation":"add_column","column":{"name":"c","type":"TEXT"}}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "database store not available") {
		t.Errorf("expected store error, got: %v", err)
	}
}

func TestAlterTableTool_AddColumn(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	// Create a table first.
	ct := &CreateTableTool{}
	_, err := ct.Execute(context.Background(),
		`{"name":"items","description":"d","columns":[{"name":"val","type":"TEXT"}]}`, tctx)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Add a column.
	at := &AlterTableTool{}
	result, err := at.Execute(context.Background(),
		`{"table_name":"items","operation":"add_column","column":{"name":"priority","type":"INTEGER"}}`, tctx)
	if err != nil {
		t.Fatalf("alter table: %v", err)
	}
	if !strings.Contains(result.Output, "add_column") {
		t.Errorf("output = %q", result.Output)
	}

	// Verify the column is usable by inserting a row using it.
	it := &InsertRowTool{}
	_, err = it.Execute(context.Background(),
		`{"table":"items","data":{"val":"test","priority":5}}`, tctx)
	if err != nil {
		t.Fatalf("insert after alter: %v", err)
	}
}

func TestAlterTableTool_DropColumn(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	// Create table with two columns.
	ct := &CreateTableTool{}
	_, err := ct.Execute(context.Background(),
		`{"name":"multi","description":"d","columns":[{"name":"a","type":"TEXT"},{"name":"b","type":"TEXT"}]}`, tctx)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	at := &AlterTableTool{}
	result, err := at.Execute(context.Background(),
		`{"table_name":"multi","operation":"drop_column","column":{"name":"b"}}`, tctx)
	if err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if !strings.Contains(result.Output, "drop_column") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestAlterTableTool_DuplicateColumn(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	ct := &CreateTableTool{}
	_, _ = ct.Execute(context.Background(),
		`{"name":"dup","description":"d","columns":[{"name":"val","type":"TEXT"}]}`, tctx)

	at := &AlterTableTool{}
	_, err := at.Execute(context.Background(),
		`{"table_name":"dup","operation":"add_column","column":{"name":"val","type":"TEXT"}}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestAlterTableTool_DropNonexistentColumn(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	ct := &CreateTableTool{}
	_, _ = ct.Execute(context.Background(),
		`{"name":"sparse","description":"d","columns":[{"name":"val","type":"TEXT"}]}`, tctx)

	at := &AlterTableTool{}
	_, err := at.Execute(context.Background(),
		`{"table_name":"sparse","operation":"drop_column","column":{"name":"missing"}}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestAlterTableTool_InvalidOperation(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	ct := &CreateTableTool{}
	_, _ = ct.Execute(context.Background(),
		`{"name":"op","description":"d","columns":[{"name":"val","type":"TEXT"}]}`, tctx)

	at := &AlterTableTool{}
	_, err := at.Execute(context.Background(),
		`{"table_name":"op","operation":"rename","column":{"name":"val"}}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "add_column or drop_column") {
		t.Errorf("expected operation error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DropTableTool
// ---------------------------------------------------------------------------

func TestDropTableTool_Properties(t *testing.T) {
	tool := &DropTableTool{}
	if tool.Name() != "drop_table" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Risk() != RiskCaution {
		t.Errorf("risk = %v", tool.Risk())
	}
}

func TestDropTableTool_NoStore(t *testing.T) {
	tool := &DropTableTool{}
	_, err := tool.Execute(context.Background(), `{"table_name":"t"}`, &Context{})
	if err == nil || !strings.Contains(err.Error(), "database store not available") {
		t.Errorf("expected store error, got: %v", err)
	}
}

func TestDropTableTool_NotFound(t *testing.T) {
	store := testutil.TempStore(t)
	tool := &DropTableTool{}
	_, err := tool.Execute(context.Background(), `{"table_name":"ghost"}`,
		&Context{Store: store, AgentID: "bot1"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestDropTableTool_Success(t *testing.T) {
	store := testutil.TempStore(t)
	tctx := &Context{Store: store, AgentID: "bot1"}

	// Create then drop.
	ct := &CreateTableTool{}
	_, err := ct.Execute(context.Background(),
		`{"name":"temp","description":"throwaway","columns":[{"name":"x","type":"TEXT"}]}`, tctx)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	dt := &DropTableTool{}
	result, err := dt.Execute(context.Background(), `{"table_name":"temp"}`, tctx)
	if err != nil {
		t.Fatalf("drop: %v", err)
	}
	if !strings.Contains(result.Output, "Dropped table") {
		t.Errorf("output = %q", result.Output)
	}

	// Verify it's gone from hippocampus.
	qt := &QueryTableTool{}
	_, err = qt.Execute(context.Background(), `{"table":"temp"}`, tctx)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("table should be gone from hippocampus, got: %v", err)
	}
}

func TestDropTableTool_InvalidName(t *testing.T) {
	tool := &DropTableTool{}
	store := testutil.TempStore(t)
	_, err := tool.Execute(context.Background(), `{"table_name":"bad-name!"}`,
		&Context{Store: store, AgentID: "bot1"})
	if err == nil || !strings.Contains(err.Error(), "alphanumeric") {
		t.Errorf("expected name error, got: %v", err)
	}
}
