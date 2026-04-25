package tools

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockFS is an in-memory filesystem for testing tools.
type mockFS struct {
	files map[string][]byte
	dirs  map[string][]fs.DirEntry
}

func newMockFS() *mockFS {
	return &mockFS{files: make(map[string][]byte), dirs: make(map[string][]fs.DirEntry)}
}
func (m *mockFS) ReadFile(path string) ([]byte, error) {
	if data, ok := m.files[path]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}
func (m *mockFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	m.files[path] = data
	return nil
}
func (m *mockFS) MkdirAll(_ string, _ os.FileMode) error { return nil }
func (m *mockFS) ReadDir(path string) ([]fs.DirEntry, error) {
	if entries, ok := m.dirs[path]; ok {
		return entries, nil
	}
	return nil, os.ErrNotExist
}
func (m *mockFS) Stat(path string) (fs.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return nil, nil // simplified
	}
	return nil, os.ErrNotExist
}
func (m *mockFS) Glob(_ string) ([]string, error)                           { return nil, nil }
func (m *mockFS) OpenFile(_ string, _ int, _ os.FileMode) (*os.File, error) { return nil, nil }
func (m *mockFS) Walk(_ string, _ filepath.WalkFunc) error                  { return nil }

// mockRunner records commands instead of executing them.
type mockRunner struct {
	stdout []byte
	stderr []byte
	err    error
	calls  []string
}

func (m *mockRunner) Run(_ context.Context, name string, args []string, _ string, _ []string) ([]byte, []byte, error) {
	m.calls = append(m.calls, name+" "+joinArgs(args))
	return m.stdout, m.stderr, m.err
}

func joinArgs(args []string) string {
	s := ""
	for _, a := range args {
		s += a + " "
	}
	return s
}

func TestReadFileTool_Execute(t *testing.T) {
	ws := t.TempDir()
	mfs := newMockFS()
	resolved := filepath.Clean(filepath.Join(ws, "test.txt"))
	mfs.files[resolved] = []byte("hello world")

	tool := &ReadFileTool{}
	tctx := &Context{Workspace: ws, FS: mfs}

	result, err := tool.Execute(context.Background(), `{"path": "test.txt"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "hello world" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestReadFileTool_NotFound(t *testing.T) {
	mfs := newMockFS()
	tool := &ReadFileTool{}
	tctx := &Context{Workspace: t.TempDir(), FS: mfs}

	_, err := tool.Execute(context.Background(), `{"path": "missing.txt"}`, tctx)
	if err == nil {
		t.Error("should error for missing file")
	}
}

func TestFilesystemInspectionTools_AdvertiseAbsoluteAllowedPaths(t *testing.T) {
	read := &ReadFileTool{}
	if !strings.Contains(read.Description(), "absolute path inside allowed_paths") {
		t.Fatalf("read_file description = %q", read.Description())
	}
	if !strings.Contains(string(read.ParameterSchema()), "absolute allowed path") {
		t.Fatalf("read_file schema = %s", string(read.ParameterSchema()))
	}

	list := &ListDirectoryTool{}
	if !strings.Contains(list.Description(), "absolute directory inside allowed_paths") {
		t.Fatalf("list_directory description = %q", list.Description())
	}
	if !strings.Contains(string(list.ParameterSchema()), "absolute allowed directory") {
		t.Fatalf("list_directory schema = %s", string(list.ParameterSchema()))
	}

	glob := &GlobFilesTool{}
	if !strings.Contains(glob.Description(), "absolute path inside allowed_paths") {
		t.Fatalf("glob_files description = %q", glob.Description())
	}
	if !strings.Contains(string(glob.ParameterSchema()), "absolute allowed directory") {
		t.Fatalf("glob_files schema = %s", string(glob.ParameterSchema()))
	}
	if !strings.Contains(glob.Description(), "**/*.md") {
		t.Fatalf("glob_files description should advertise actual extension examples, got %q", glob.Description())
	}
	if !strings.Contains(string(glob.ParameterSchema()), "**/*.md") {
		t.Fatalf("glob_files schema should advertise actual extension examples, got %s", string(glob.ParameterSchema()))
	}

	write := &WriteFileTool{}
	if !strings.Contains(write.Description(), "absolute path inside allowed_paths") {
		t.Fatalf("write_file description = %q", write.Description())
	}
	if !strings.Contains(string(write.ParameterSchema()), "absolute allowed path") {
		t.Fatalf("write_file schema = %s", string(write.ParameterSchema()))
	}

	edit := &EditFileTool{}
	if !strings.Contains(edit.Description(), "absolute path inside allowed_paths") {
		t.Fatalf("edit_file description = %q", edit.Description())
	}
	if !strings.Contains(string(edit.ParameterSchema()), "absolute allowed path") {
		t.Fatalf("edit_file schema = %s", string(edit.ParameterSchema()))
	}
}

func TestListDirectoryTool_Execute_EmitsInspectionProof(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "alpha.txt"), []byte("A"), 0o644); err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	if err := os.Mkdir(filepath.Join(ws, "nested"), 0o755); err != nil {
		t.Fatalf("seed nested dir: %v", err)
	}

	tool := &ListDirectoryTool{}
	tctx := &Context{Workspace: ws}
	result, err := tool.Execute(context.Background(), `{"path":"."}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	proof, ok := ParseInspectionProof(result.Metadata)
	if !ok {
		t.Fatal("expected inspection proof metadata")
	}
	if proof.ToolName != "list_directory" || proof.InspectionKind != "directory_listing" {
		t.Fatalf("proof = %+v", proof)
	}
	if proof.Count != 2 || proof.Empty {
		t.Fatalf("proof count/empty = %+v", proof)
	}
}

func TestGlobFilesTool_Execute_EmitsInspectionProof(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "alpha.md"), []byte("A"), 0o644); err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "beta.txt"), []byte("B"), 0o644); err != nil {
		t.Fatalf("seed beta: %v", err)
	}

	tool := &GlobFilesTool{}
	tctx := &Context{Workspace: ws}
	result, err := tool.Execute(context.Background(), `{"path":".","pattern":"*.md"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	proof, ok := ParseInspectionProof(result.Metadata)
	if !ok {
		t.Fatal("expected inspection proof metadata")
	}
	if proof.ToolName != "glob_files" || proof.InspectionKind != "file_glob" {
		t.Fatalf("proof = %+v", proof)
	}
	if proof.Count != 1 || proof.Empty {
		t.Fatalf("proof count/empty = %+v", proof)
	}
	if proof.Pattern != "*.md" {
		t.Fatalf("pattern = %q", proof.Pattern)
	}
}

func TestWriteFileTool_Execute(t *testing.T) {
	ws := t.TempDir()
	tool := &WriteFileTool{}
	tctx := &Context{Workspace: ws} // real FS — write needs real file handles

	result, err := tool.Execute(context.Background(), `{"path": "out.txt", "content": "data"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	// Verify via real filesystem.
	data, _ := os.ReadFile(filepath.Join(ws, "out.txt"))
	if string(data) != "data" {
		t.Errorf("written = %q", string(data))
	}
	proof, ok := ParseArtifactProof(result.Metadata)
	if !ok {
		t.Fatal("expected artifact proof metadata")
	}
	if proof.Path != "out.txt" || proof.ArtifactKind != "workspace_file" {
		t.Fatalf("proof = %+v", proof)
	}
	if !proof.ExactContentIncluded || proof.Content != "data" {
		t.Fatalf("proof exact content = %+v", proof)
	}
	if !strings.Contains(result.Output, `"proof_type":"artifact_write"`) {
		t.Fatalf("result output should carry structured proof, got %q", result.Output)
	}
}

func TestWriteFileTool_RejectsProtectedSourceArtifact(t *testing.T) {
	ws := t.TempDir()
	tool := &WriteFileTool{}
	tctx := &Context{
		Workspace:              ws,
		ProtectedReadOnlyPaths: []string{"requirements.txt"},
	}

	_, err := tool.Execute(context.Background(), `{"path": "requirements.txt", "content": "mutated"}`, tctx)
	if err == nil {
		t.Fatal("expected protected source artifact rejection")
	}
	if !strings.Contains(err.Error(), "prompt-declared source artifact") {
		t.Fatalf("error = %v", err)
	}
}

func TestReadFileTool_Execute_EmitsArtifactReadProof(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "input.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tool := &ReadFileTool{}
	tctx := &Context{Workspace: ws}

	result, err := tool.Execute(context.Background(), `{"path":"input.txt"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.Output != "hello" {
		t.Fatalf("output = %q, want hello", result.Output)
	}
	proof, ok := ParseArtifactReadProof(result.Metadata)
	if !ok {
		t.Fatal("expected artifact read proof metadata")
	}
	if proof.Path != "input.txt" || proof.ArtifactKind != "workspace_file" {
		t.Fatalf("proof = %+v", proof)
	}
	if !proof.ExactContentIncluded || proof.Content != "hello" {
		t.Fatalf("proof exact content = %+v", proof)
	}
}

func TestObsidianWriteTool_Execute(t *testing.T) {
	vault := t.TempDir()
	tool := &ObsidianWriteTool{VaultPath: vault}
	tctx := &Context{
		Workspace:    t.TempDir(),
		AllowedPaths: []string{vault},
	}

	result, err := tool.Execute(context.Background(), `{"path":"Projects/Daily Note","content":"# Notes"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	data, err := os.ReadFile(filepath.Join(vault, "Projects", "Daily Note.md"))
	if err != nil {
		t.Fatalf("read written note: %v", err)
	}
	if string(data) != "# Notes" {
		t.Fatalf("written note = %q, want %q", string(data), "# Notes")
	}
	proof, ok := ParseArtifactProof(result.Metadata)
	if !ok {
		t.Fatal("expected artifact proof metadata")
	}
	if proof.Path != "Projects/Daily Note.md" || proof.ArtifactKind != "obsidian_note" {
		t.Fatalf("proof = %+v", proof)
	}
	if !proof.ExactContentIncluded || proof.Content != "# Notes" {
		t.Fatalf("proof exact content = %+v", proof)
	}
}

func TestObsidianWriteTool_RejectsProtectedSourceArtifact(t *testing.T) {
	vault := t.TempDir()
	tool := &ObsidianWriteTool{VaultPath: vault}
	tctx := &Context{
		Workspace:              t.TempDir(),
		AllowedPaths:           []string{vault},
		ProtectedReadOnlyPaths: []string{"Projects/Daily Note.md"},
	}

	_, err := tool.Execute(context.Background(), `{"path":"Projects/Daily Note","content":"# mutated"}`, tctx)
	if err == nil {
		t.Fatal("expected protected source artifact rejection")
	}
	if !strings.Contains(err.Error(), "prompt-declared source artifact") {
		t.Fatalf("error = %v", err)
	}
}

func TestEditFileTool_Execute(t *testing.T) {
	ws := t.TempDir()
	// Seed a file to edit.
	_ = os.WriteFile(filepath.Join(ws, "code.go"), []byte("func old() {}"), 0o644)

	tool := &EditFileTool{}
	tctx := &Context{Workspace: ws} // real FS for edit

	result, err := tool.Execute(context.Background(), `{"path": "code.go", "old_text": "old", "new_text": "new"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	data, _ := os.ReadFile(filepath.Join(ws, "code.go"))
	if string(data) != "func new() {}" {
		t.Errorf("edited = %q", string(data))
	}
}

func TestBashTool_Execute(t *testing.T) {
	mr := &mockRunner{stdout: []byte("hello from shell\n")}
	tool := &BashTool{}
	tctx := &Context{Workspace: "/workspace", Runner: mr}
	params := `{"command": "echo hello", "timeout_seconds": 5}`

	result, err := tool.Execute(context.Background(), params, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output == "" {
		t.Error("should have output")
	}
	if len(mr.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(mr.calls))
	}
}

func TestResolvePath_RejectsHomeShortcut(t *testing.T) {
	_, err := resolvePath("/workspace", "~/Downloads", nil)
	if err == nil {
		t.Fatal("expected home shortcut to be rejected")
	}
	if !strings.Contains(err.Error(), "home-directory shortcuts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBashTool_Properties(t *testing.T) {
	tool := &BashTool{}
	if tool.Name() != "bash" {
		t.Errorf("name = %s", tool.Name())
	}
	if tool.Risk() != RiskDangerous {
		t.Errorf("risk = %v", tool.Risk())
	}
	schema := tool.ParameterSchema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
}

func TestEchoTool(t *testing.T) {
	tool := &EchoTool{}
	if tool.Name() != "echo" {
		t.Errorf("name = %s", tool.Name())
	}
	if tool.Risk() != RiskSafe {
		t.Errorf("risk = %v", tool.Risk())
	}
	result, err := tool.Execute(context.Background(), `{"message": "hello"}`, &Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "hello" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestRuntimeContextTool(t *testing.T) {
	tool := &RuntimeContextTool{}
	if tool.Name() != "get_runtime_context" {
		t.Errorf("name = %s", tool.Name())
	}
	tctx := &Context{
		SessionID: "s1", AgentID: "a1", AgentName: "bot",
		Workspace: "/ws", Channel: "api",
		AllowedPaths:           []string{"/safe", "/vault"},
		ProtectedReadOnlyPaths: []string{"requirements.txt"},
	}
	result, err := tool.Execute(context.Background(), `{}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output == "" {
		t.Error("should have output")
	}
	if !strings.Contains(result.Output, "Effective Path Policy") {
		t.Fatalf("runtime context should expose effective path policy, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "absolute paths must fall under an allowed path") {
		t.Fatalf("runtime context should expose absolute-path constraint, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "allowed paths are roots") {
		t.Fatalf("runtime context should expose inherited allowed-root semantics, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "requirements.txt") {
		t.Fatalf("runtime context should expose protected read-only inputs, got %q", result.Output)
	}
}

func TestAllToolProperties(t *testing.T) {
	tools := []Tool{
		&EchoTool{},
		&ReadFileTool{},
		&WriteFileTool{},
		&EditFileTool{},
		&ListDirectoryTool{},
		&SearchFilesTool{},
		&GlobFilesTool{},
		&BashTool{},
		&RuntimeContextTool{},
	}
	for _, tool := range tools {
		t.Run(tool.Name(), func(t *testing.T) {
			if tool.Name() == "" {
				t.Error("empty name")
			}
			if tool.Description() == "" {
				t.Error("empty description")
			}
			schema := tool.ParameterSchema()
			if len(schema) == 0 {
				t.Error("empty schema")
			}
			// Verify schema is valid JSON.
			var parsed any
			if err := json.Unmarshal(schema, &parsed); err != nil {
				t.Errorf("invalid schema JSON: %v", err)
			}
		})
	}
}

func TestContext_GetFS_Default(t *testing.T) {
	c := &Context{}
	fs := c.GetFS()
	if fs == nil {
		t.Fatal("default FS should not be nil")
	}
	if _, ok := fs.(OSFileSystem); !ok {
		t.Error("default should be OSFileSystem")
	}
}

func TestContext_GetFS_Custom(t *testing.T) {
	mfs := newMockFS()
	c := &Context{FS: mfs}
	if c.GetFS() != mfs {
		t.Error("should return custom FS")
	}
}

func TestContext_GetRunner_Default(t *testing.T) {
	c := &Context{}
	r := c.GetRunner()
	if r == nil {
		t.Fatal("default runner should not be nil")
	}
}

var _ = time.Second // keep time import for BashTool timeout
