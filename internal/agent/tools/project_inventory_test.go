package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type scriptedProjectRunner struct {
	fn        func(name string, args []string, dir string) ([]byte, []byte, error)
	calls     []string
	responses [][]byte
}

func (r *scriptedProjectRunner) Run(_ context.Context, name string, args []string, dir string, _ []string) ([]byte, []byte, error) {
	r.calls = append(r.calls, filepath.Clean(dir)+"|"+name+"|"+joinArgs(args))
	if r.fn != nil {
		return r.fn(name, args, dir)
	}
	if len(r.responses) > 0 {
		out := r.responses[0]
		r.responses = r.responses[1:]
		return out, nil, nil
	}
	return nil, nil, nil
}

func TestProjectInventoryTool_Execute_EnumeratesProjectsSortedByLastEditDate(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}
	alphaFile := filepath.Join(alpha, "main.go")
	betaFile := filepath.Join(beta, "app.py")
	if err := os.WriteFile(alphaFile, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write alpha: %v", err)
	}
	if err := os.WriteFile(betaFile, []byte("print('beta')"), 0o644); err != nil {
		t.Fatalf("write beta: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(alphaFile, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatalf("chtimes alpha: %v", err)
	}
	if err := os.Chtimes(betaFile, now.Add(-24*time.Hour), now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("chtimes beta: %v", err)
	}

	tool := &ProjectInventoryTool{}
	tctx := &Context{
		Workspace:    root,
		AllowedPaths: []string{root},
	}
	result, err := tool.Execute(context.Background(), `{"root":"`+root+`"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload ProjectInventoryResult
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.ProjectCount != 2 {
		t.Fatalf("project_count = %d, want 2", payload.ProjectCount)
	}
	if len(payload.Projects) != 2 {
		t.Fatalf("projects length = %d, want 2", len(payload.Projects))
	}
	if payload.Projects[0].Name != "beta" {
		t.Fatalf("first project = %q, want beta", payload.Projects[0].Name)
	}
	if payload.Projects[0].Languages[0] != "Python" {
		t.Fatalf("beta languages = %v, want Python first", payload.Projects[0].Languages)
	}
	if payload.Projects[1].Languages[0] != "Go" {
		t.Fatalf("alpha languages = %v, want Go first", payload.Projects[1].Languages)
	}
	proof, ok := ParseInspectionProof(result.Metadata)
	if !ok {
		t.Fatal("expected inspection proof metadata")
	}
	if proof.ToolName != "inventory_projects" || proof.InspectionKind != "project_inventory" {
		t.Fatalf("proof = %+v", proof)
	}
	if proof.Count != 2 || proof.Empty {
		t.Fatalf("proof count/empty = %+v", proof)
	}
}

func TestProjectInventoryTool_Execute_ReportsGitRemoteDirection(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	file := filepath.Join(project, "main.go")
	if err := os.WriteFile(file, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}

	runner := &scriptedProjectRunner{responses: [][]byte{
		[]byte("true\n"),
		[]byte("main\n"),
		[]byte("0 3\n"),
	}}

	tool := &ProjectInventoryTool{}
	tctx := &Context{
		Workspace:    root,
		AllowedPaths: []string{root},
		Runner:       runner,
	}
	result, err := tool.Execute(context.Background(), `{"root":"`+root+`"}`, tctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload ProjectInventoryResult
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Projects) != 1 {
		t.Fatalf("projects length = %d, want 1", len(payload.Projects))
	}
	if payload.Projects[0].RemoteDirection != "ahead" {
		t.Fatalf("remote_direction = %q, want ahead (calls=%v)", payload.Projects[0].RemoteDirection, runner.calls)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("git calls = %d, want 3 (%v)", len(runner.calls), runner.calls)
	}
}
