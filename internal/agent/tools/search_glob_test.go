package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestListDirectoryTool_Execute(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "file1.txt"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "file2.go"), []byte("package main"), 0o644)
	_ = os.MkdirAll(filepath.Join(ws, "subdir"), 0o755)

	tool := &ListDirectoryTool{}
	tctx := &Context{Workspace: ws}
	result, err := tool.Execute(context.Background(), `{"path": "."}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output == "" {
		t.Error("should list directory contents")
	}
}

func TestSearchFilesTool_Execute(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "main.go"), []byte("package main\nfunc main() {}"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "util.go"), []byte("package main\nfunc helper() {}"), 0o644)

	tool := &SearchFilesTool{}
	tctx := &Context{Workspace: ws}
	result, err := tool.Execute(context.Background(), `{"pattern": "helper", "path": "."}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output == "" {
		t.Error("should find matches")
	}
}

func TestSearchFilesTool_NoMatch(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "file.txt"), []byte("hello world"), 0o644)

	tool := &SearchFilesTool{}
	tctx := &Context{Workspace: ws}
	result, err := tool.Execute(context.Background(), `{"pattern": "nonexistent_string_xyz", "path": "."}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Should return no results but not error.
	_ = result
}

func TestGlobFilesTool_Execute(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "a.go"), []byte("go"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "b.go"), []byte("go"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "c.txt"), []byte("txt"), 0o644)

	tool := &GlobFilesTool{}
	tctx := &Context{Workspace: ws}
	result, err := tool.Execute(context.Background(), `{"pattern": "*.go"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output == "" {
		t.Error("should find .go files")
	}
}

func TestResolvePath_Relative(t *testing.T) {
	ws := t.TempDir()
	resolved, err := resolvePath(ws, "subdir/file.txt", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	expected := filepath.Clean(filepath.Join(ws, "subdir/file.txt"))
	if resolved != expected {
		t.Errorf("resolved = %s, want %s", resolved, expected)
	}
}

func TestResolvePath_ParentTraversal(t *testing.T) {
	ws := t.TempDir()
	_, err := resolvePath(ws, "../../../etc/passwd", nil)
	if err == nil {
		t.Error("parent traversal should be blocked")
	}
}
