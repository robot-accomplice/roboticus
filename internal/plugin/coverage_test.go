package plugin

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- mock process runner for ScriptPlugin tests ----------

type mockRunner struct {
	stdout []byte
	stderr []byte
	err    error
}

func (m *mockRunner) Run(_ context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error) {
	return m.stdout, m.stderr, m.err
}

// ---------- Registry: strict-mode permission enforcement ----------

func TestRegistry_StrictMode_RejectsUndeclaredPermission(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{
		StrictMode: true,
		Allowed:    []string{"read"},
	})

	p := &testPlugin{
		name: "perm-plugin", version: "1.0.0",
		tools: []ToolDef{{Name: "tool1", Permissions: []string{"write"}}},
	}
	err := reg.Register(p)
	if err == nil {
		t.Fatal("expected rejection for undeclared permission")
	}
	if !strings.Contains(err.Error(), "undeclared permission") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRegistry_StrictMode_AcceptsDeclaredPermission(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{
		StrictMode: true,
		Allowed:    []string{"read", "write"},
	})

	p := &testPlugin{
		name: "perm-ok", version: "1.0.0",
		tools: []ToolDef{{Name: "tool1", Permissions: []string{"read"}}},
	}
	if err := reg.Register(p); err != nil {
		t.Fatalf("should accept declared permission: %v", err)
	}
}

func TestRegistry_StrictMode_CaseInsensitive(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{
		StrictMode: true,
		Allowed:    []string{"READ"},
	})

	p := &testPlugin{
		name: "ci-plugin", version: "1.0.0",
		tools: []ToolDef{{Name: "t", Permissions: []string{"read"}}},
	}
	if err := reg.Register(p); err != nil {
		t.Fatalf("case-insensitive permission match failed: %v", err)
	}
}

// ---------- Registry: InitAll with failing plugins ----------

type failingPlugin struct {
	testPlugin
}

func (f *failingPlugin) Init() error { return errors.New("init boom") }

func TestRegistry_InitAll_PartialFailure(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})

	_ = reg.Register(&testPlugin{name: "good", version: "1.0.0"})
	_ = reg.Register(&failingPlugin{testPlugin{name: "bad", version: "1.0.0"}})

	errs := reg.InitAll()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}

	// Verify status: good=active, bad=error
	for _, info := range reg.List() {
		switch info.Name {
		case "good":
			if info.Status != StatusActive {
				t.Errorf("good plugin status = %s, want active", info.Status)
			}
		case "bad":
			if info.Status != StatusError {
				t.Errorf("bad plugin status = %s, want error", info.Status)
			}
		}
	}
}

// ---------- Registry: ExecuteTool on missing tool ----------

func TestRegistry_ExecuteTool_NotFound(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	_ = reg.Register(&testPlugin{name: "p1", version: "1.0.0"})
	reg.InitAll()

	_, err := reg.ExecuteTool(context.Background(), "nonexistent-tool", nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}

// ---------- Registry: AllTools skips non-active plugins ----------

func TestRegistry_AllTools_SkipsDisabled(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	_ = reg.Register(&testPlugin{
		name: "active-p", version: "1.0.0",
		tools: []ToolDef{{Name: "t1"}},
	})
	_ = reg.Register(&testPlugin{
		name: "disabled-p", version: "1.0.0",
		tools: []ToolDef{{Name: "t2"}},
	})
	reg.InitAll()
	_ = reg.Disable("disabled-p")

	tools := reg.AllTools()
	for _, tool := range tools {
		if tool.Name == "t2" {
			t.Error("disabled plugin tools should not appear in AllTools")
		}
	}
}

// ---------- Registry: name too long ----------

func TestRegistry_Register_NameTooLong(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	long := strings.Repeat("a", 129)
	err := reg.Register(&testPlugin{name: long, version: "1.0.0"})
	if err == nil {
		t.Error("name >128 chars should be rejected")
	}
}

// ---------- ScanDirectory ----------

func TestRegistry_ScanDirectory_Empty(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	n, err := reg.ScanDirectory("")
	if err != nil || n != 0 {
		t.Errorf("empty dir should return 0, nil; got %d, %v", n, err)
	}
}

func TestRegistry_ScanDirectory_FindsManifests(t *testing.T) {
	base := t.TempDir()

	// Create two plugin directories with manifests.
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(base, name)
		os.MkdirAll(dir, 0o755)
		content := fmt.Sprintf("name = %q\nversion = \"0.1.0\"\ndescription = \"test %s\"", name, name)
		os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(content), 0o644)
	}

	reg := NewRegistry(nil, nil, PermissionPolicy{})
	n, err := reg.ScanDirectory(base)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 2 {
		t.Errorf("found %d plugins, want 2", n)
	}

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("registered %d, want 2", len(list))
	}
}

func TestRegistry_ScanDirectory_YAMLManifest(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "yamlplugin")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte("name: yamlplugin\nversion: 1.0.0\n"), 0o644)

	reg := NewRegistry(nil, nil, PermissionPolicy{})
	n, err := reg.ScanDirectory(base)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("found %d, want 1", n)
	}
}

func TestRegistry_ScanDirectory_SkipsInvalidManifest(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "invalid")
	os.MkdirAll(dir, 0o755)
	// Manifest with no name field.
	os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte("version = \"1.0.0\"\n"), 0o644)

	reg := NewRegistry(nil, nil, PermissionPolicy{})
	n, _ := reg.ScanDirectory(base)
	if n != 0 {
		t.Errorf("should skip manifest without name, got %d", n)
	}
}

func TestRegistry_ScanDirectory_SkipsDenied(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "blocked")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte("name = \"blocked\"\nversion = \"1.0.0\"\n"), 0o644)

	reg := NewRegistry(nil, []string{"blocked"}, PermissionPolicy{})
	n, _ := reg.ScanDirectory(base)
	if n != 0 {
		t.Errorf("should skip denied plugin, got %d", n)
	}
}

func TestRegistry_ScanDirectory_NonexistentDir(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	_, err := reg.ScanDirectory("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestRegistry_ScanDirectory_IgnoresNonManifestFiles(t *testing.T) {
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "readme.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(base, "config.json"), []byte("{}"), 0o644)

	reg := NewRegistry(nil, nil, PermissionPolicy{})
	n, err := reg.ScanDirectory(base)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("should find 0 plugins, got %d", n)
	}
}

// ---------- FileHash / DirHash ----------

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	hash1, err := FileHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 == "" {
		t.Error("hash should not be empty")
	}

	// Same content = same hash.
	hash2, err := FileHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 != hash2 {
		t.Error("deterministic hash expected")
	}

	// Different content = different hash.
	os.WriteFile(path, []byte("changed"), 0o644)
	hash3, _ := FileHash(path)
	if hash1 == hash3 {
		t.Error("different content should produce different hash")
	}
}

func TestFileHash_NotFound(t *testing.T) {
	_, err := FileHash("/nonexistent/file")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDirHash(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb"), 0o644)

	hash1, err := DirHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 == "" {
		t.Error("hash should not be empty")
	}

	// Modify a file and hash changes.
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("changed"), 0o644)
	hash2, _ := DirHash(dir)
	if hash1 == hash2 {
		t.Error("directory hash should change when file content changes")
	}
}

func TestDirHash_NotFound(t *testing.T) {
	_, err := DirHash("/nonexistent/directory")
	if err == nil {
		t.Error("expected error for missing directory")
	}
}

// ---------- ScriptPlugin: ExecuteTool via mock runner ----------

func TestScriptPlugin_ExecuteTool_Success(t *testing.T) {
	dir := t.TempDir()
	// Create a script file so discoverScripts finds it.
	scriptPath := filepath.Join(dir, "greet.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello"), 0o755)

	manifest := Manifest{
		Name:    "mock-plugin",
		Version: "1.0.0",
		Tools:   []ManifestTool{{Name: "greet", Description: "say hello"}},
	}

	runner := &mockRunner{stdout: []byte("hello from script"), stderr: nil, err: nil}
	sp := NewScriptPluginWithRunner(manifest, dir, runner)

	result, err := sp.ExecuteTool(context.Background(), "greet", json.RawMessage(`{"prompt":"hi"}`))
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Output)
	}
	if result.Output != "hello from script" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestScriptPlugin_ExecuteTool_ScriptError(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fail.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1"), 0o755)

	manifest := Manifest{
		Name:    "fail-plugin",
		Version: "1.0.0",
		Tools:   []ManifestTool{{Name: "fail", Description: "fails"}},
	}

	runner := &mockRunner{
		stdout: nil,
		stderr: []byte("something went wrong"),
		err:    errors.New("exit status 1"),
	}
	sp := NewScriptPluginWithRunner(manifest, dir, runner)

	result, err := sp.ExecuteTool(context.Background(), "fail", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ExecuteTool should not return error, got: %v", err)
	}
	if result.Success {
		t.Error("result should be failure")
	}
	if !strings.Contains(result.Output, "exit status 1") {
		t.Errorf("output should contain error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "something went wrong") {
		t.Errorf("output should contain stderr: %s", result.Output)
	}
}

func TestScriptPlugin_ExecuteTool_LargeOutputTruncated(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "big.sh"), []byte("#!/bin/sh"), 0o755)

	manifest := Manifest{
		Name:  "big-plugin",
		Version: "1.0.0",
		Tools: []ManifestTool{{Name: "big", Description: "big output"}},
	}

	// Create output larger than 10MB.
	bigOutput := make([]byte, 11*1024*1024)
	for i := range bigOutput {
		bigOutput[i] = 'A'
	}

	runner := &mockRunner{stdout: bigOutput, err: nil}
	sp := NewScriptPluginWithRunner(manifest, dir, runner)

	result, err := sp.ExecuteTool(context.Background(), "big", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Error("expected success even with truncation")
	}
	if !strings.Contains(result.Output, "truncated") {
		t.Error("large output should be truncated")
	}
}

func TestScriptPlugin_ExecuteTool_ToolNotFound(t *testing.T) {
	dir := t.TempDir()
	manifest := Manifest{Name: "sp", Version: "1.0.0"}
	sp := NewScriptPluginWithRunner(manifest, dir, &mockRunner{})

	_, err := sp.ExecuteTool(context.Background(), "no-such-tool", nil)
	if err == nil {
		t.Error("expected error for missing tool")
	}
}

// ---------- ScriptPlugin: Init with requirements ----------

func TestScriptPlugin_Init_MissingRequiredCommand(t *testing.T) {
	manifest := Manifest{
		Name:    "req-plugin",
		Version: "1.0.0",
		Requirements: []Requirement{
			{Name: "missing-tool", Command: "this-command-does-not-exist-12345", InstallHint: "install it"},
		},
	}
	sp := NewScriptPlugin(manifest, t.TempDir())
	err := sp.Init()
	if err == nil {
		t.Error("should fail when required command not found")
	}
	if !strings.Contains(err.Error(), "missing requirement") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestScriptPlugin_Init_OptionalMissingCommand(t *testing.T) {
	manifest := Manifest{
		Name:    "opt-plugin",
		Version: "1.0.0",
		Requirements: []Requirement{
			{Name: "optional-tool", Command: "this-command-does-not-exist-99999", Optional: true},
		},
	}
	sp := NewScriptPlugin(manifest, t.TempDir())
	err := sp.Init()
	if err != nil {
		t.Errorf("optional requirement should not cause failure: %v", err)
	}
}

func TestScriptPlugin_Init_EmptyCommand(t *testing.T) {
	manifest := Manifest{
		Name:    "empty-cmd",
		Version: "1.0.0",
		Requirements: []Requirement{
			{Name: "noop", Command: ""},
		},
	}
	sp := NewScriptPlugin(manifest, t.TempDir())
	if err := sp.Init(); err != nil {
		t.Errorf("empty command should be skipped: %v", err)
	}
}

// ---------- ScriptPlugin: WithTimeout, WithEnv, Shutdown, Hash ----------

func TestScriptPlugin_WithTimeout(t *testing.T) {
	manifest := Manifest{Name: "to", Version: "1.0.0"}
	sp := NewScriptPlugin(manifest, t.TempDir())
	sp.WithTimeout(5 * time.Second)
	if sp.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", sp.timeout)
	}
}

func TestScriptPlugin_WithEnv(t *testing.T) {
	manifest := Manifest{Name: "env", Version: "1.0.0"}
	sp := NewScriptPlugin(manifest, t.TempDir())
	sp.WithEnv(map[string]string{"FOO": "bar", "BAZ": "qux"})
	if sp.env["FOO"] != "bar" || sp.env["BAZ"] != "qux" {
		t.Errorf("env not set correctly: %v", sp.env)
	}
}

func TestScriptPlugin_Shutdown(t *testing.T) {
	sp := NewScriptPlugin(Manifest{Name: "s", Version: "1.0.0"}, t.TempDir())
	if err := sp.Shutdown(); err != nil {
		t.Errorf("shutdown should succeed: %v", err)
	}
}

func TestScriptPlugin_Hash(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "script.sh"), []byte("#!/bin/sh\necho hi"), 0o644)

	sp := NewScriptPlugin(Manifest{Name: "h", Version: "1.0.0"}, dir)
	hash, err := sp.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestScriptPlugin_CustomTimeout(t *testing.T) {
	manifest := Manifest{Name: "ct", Version: "1.0.0", TimeoutSeconds: 10}
	sp := NewScriptPlugin(manifest, t.TempDir())
	if sp.timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", sp.timeout)
	}
}

// ---------- ScriptPlugin: Tools() with dangerous flag and custom schema ----------

func TestScriptPlugin_Tools_DangerousAndSchema(t *testing.T) {
	manifest := Manifest{
		Name:    "schema-plugin",
		Version: "1.0.0",
		Tools: []ManifestTool{
			{
				Name:             "safe-tool",
				Description:      "a safe tool",
				Dangerous:        false,
				Permissions:      []string{"read"},
				ParametersSchema: `{"type":"object","properties":{"x":{"type":"string"}}}`,
			},
			{
				Name:        "danger-tool",
				Description: "a dangerous tool",
				Dangerous:   true,
			},
		},
	}

	sp := NewScriptPlugin(manifest, t.TempDir())
	tools := sp.Tools()
	if len(tools) != 2 {
		t.Fatalf("tools count = %d, want 2", len(tools))
	}

	// safe-tool checks
	if tools[0].RiskLevel != "safe" {
		t.Errorf("safe tool risk = %s", tools[0].RiskLevel)
	}
	if !strings.Contains(string(tools[0].Parameters), `"x"`) {
		t.Error("custom schema not applied")
	}
	if len(tools[0].Permissions) != 1 || tools[0].Permissions[0] != "read" {
		t.Errorf("permissions = %v", tools[0].Permissions)
	}

	// danger-tool checks
	if tools[1].RiskLevel != "dangerous" {
		t.Errorf("danger tool risk = %s", tools[1].RiskLevel)
	}
	// Should have default schema.
	if !strings.Contains(string(tools[1].Parameters), `"prompt"`) {
		t.Error("default schema not applied to tool without custom schema")
	}
}

// ---------- ScriptPlugin: discoverScripts with various extensions ----------

func TestScriptPlugin_DiscoverScripts_Extensions(t *testing.T) {
	dir := t.TempDir()

	// Create script files with different extensions.
	os.WriteFile(filepath.Join(dir, "bash-tool.sh"), []byte("#!/bin/sh"), 0o755)
	os.WriteFile(filepath.Join(dir, "python-tool.py"), []byte("#!/usr/bin/env python3"), 0o755)
	os.WriteFile(filepath.Join(dir, "ruby-tool.rb"), []byte("#!/usr/bin/env ruby"), 0o755)
	os.WriteFile(filepath.Join(dir, "node-tool.js"), []byte("#!/usr/bin/env node"), 0o755)
	os.WriteFile(filepath.Join(dir, "noext-tool"), []byte("#!/bin/sh"), 0o755)

	manifest := Manifest{
		Name:    "multi-ext",
		Version: "1.0.0",
		Tools: []ManifestTool{
			{Name: "bash-tool"},
			{Name: "python-tool"},
			{Name: "ruby-tool"},
			{Name: "node-tool"},
			{Name: "noext-tool"},
			{Name: "missing-tool"},
		},
	}

	sp := NewScriptPlugin(manifest, dir)
	// Verify discovered scripts via ExecuteTool availability.
	runner := &mockRunner{stdout: []byte("ok")}
	sp.runner = runner

	for _, name := range []string{"bash-tool", "python-tool", "ruby-tool", "node-tool", "noext-tool"} {
		result, err := sp.ExecuteTool(context.Background(), name, nil)
		if err != nil {
			t.Errorf("tool %s should be found: %v", name, err)
			continue
		}
		if !result.Success {
			t.Errorf("tool %s should succeed", name)
		}
	}

	// missing-tool should fail.
	_, err := sp.ExecuteTool(context.Background(), "missing-tool", nil)
	if err == nil {
		t.Error("missing-tool should not be found")
	}
}

// ---------- ScriptPlugin: WithEnv propagated to runner ----------

func TestScriptPlugin_ExecuteTool_WithEnv(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "envtool.sh"), []byte("#!/bin/sh"), 0o755)

	manifest := Manifest{
		Name:    "env-exec",
		Version: "1.0.0",
		Tools:   []ManifestTool{{Name: "envtool"}},
	}

	var capturedEnv []string
	runner := &envCapturingRunner{captured: &capturedEnv, stdout: []byte("ok")}
	sp := NewScriptPluginWithRunner(manifest, dir, runner)
	sp.WithEnv(map[string]string{"CUSTOM_VAR": "custom_value"})

	_, err := sp.ExecuteTool(context.Background(), "envtool", json.RawMessage(`{"prompt":"test"}`))
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range capturedEnv {
		if e == "CUSTOM_VAR=custom_value" {
			found = true
		}
	}
	if !found {
		t.Error("CUSTOM_VAR should be in environment")
	}

	// Also check ROBOTICUS_INPUT is set.
	foundInput := false
	for _, e := range capturedEnv {
		if strings.HasPrefix(e, "ROBOTICUS_INPUT=") {
			foundInput = true
		}
	}
	if !foundInput {
		t.Error("ROBOTICUS_INPUT should be in environment")
	}
}

type envCapturingRunner struct {
	captured *[]string
	stdout   []byte
}

func (r *envCapturingRunner) Run(_ context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error) {
	*r.captured = env
	return r.stdout, nil, nil
}

// ---------- ValidateManifest: additional edge cases ----------

func TestValidateManifest_MissingVersion(t *testing.T) {
	m := &Manifest{Name: "ok-name", Version: ""}
	err := ValidateManifest(m)
	if err == nil {
		t.Error("empty version should be rejected")
	}
}

func TestValidateManifest_PathTraversalSlash(t *testing.T) {
	m := &Manifest{Name: "has/slash", Version: "1.0.0"}
	err := ValidateManifest(m)
	if err == nil {
		t.Error("name with slash should be rejected")
	}
}

func TestValidateManifest_InvalidToolName(t *testing.T) {
	m := &Manifest{
		Name:    "good-name",
		Version: "1.0.0",
		Tools:   []ManifestTool{{Name: "bad tool name!"}},
	}
	err := ValidateManifest(m)
	if err == nil {
		t.Error("tool name with spaces and symbols should be rejected")
	}
}

func TestValidateManifest_EmptyToolName(t *testing.T) {
	m := &Manifest{
		Name:    "good-name",
		Version: "1.0.0",
		Tools:   []ManifestTool{{Name: ""}},
	}
	err := ValidateManifest(m)
	if err == nil {
		t.Error("empty tool name should be rejected")
	}
}

func TestValidateManifest_ValidWithTools(t *testing.T) {
	m := &Manifest{
		Name:    "good-plugin",
		Version: "2.0.0",
		Tools:   []ManifestTool{{Name: "valid-tool"}, {Name: "another_tool"}},
	}
	if err := ValidateManifest(m); err != nil {
		t.Errorf("valid manifest rejected: %v", err)
	}
}

// ---------- PackPlugin / UnpackPlugin (archive.go) ----------

func TestPackPlugin_NoManifest(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(t.TempDir(), "test.zip")

	err := PackPlugin(dir, output)
	if err == nil {
		t.Error("should fail without manifest")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPackPlugin_Success(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte("name = \"pack-test\"\nversion = \"1.0.0\""), 0o644)
	os.WriteFile(filepath.Join(dir, "script.sh"), []byte("#!/bin/sh\necho hi"), 0o755)

	output := filepath.Join(t.TempDir(), "plugin.zip")
	err := PackPlugin(dir, output)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	// Verify the zip is valid.
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	names := make(map[string]bool)
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["manifest.toml"] {
		t.Error("zip should contain manifest.toml")
	}
	if !names["script.sh"] {
		t.Error("zip should contain script.sh")
	}
}

func TestPackPlugin_SubDirectories(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte("name: subdir-test\nversion: 1.0.0"), 0o644)
	subDir := filepath.Join(dir, "lib")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(subDir, "helper.py"), []byte("def help(): pass"), 0o644)

	output := filepath.Join(t.TempDir(), "plugin.zip")
	if err := PackPlugin(dir, output); err != nil {
		t.Fatal(err)
	}

	zr, _ := zip.OpenReader(output)
	defer zr.Close()

	foundHelper := false
	for _, f := range zr.File {
		if f.Name == "lib/helper.py" {
			foundHelper = true
		}
	}
	if !foundHelper {
		t.Error("zip should include files in subdirectories")
	}
}

func TestUnpackPlugin_Success(t *testing.T) {
	// First create a valid archive.
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "manifest.toml"), []byte("name = \"unpack\"\nversion = \"1\""), 0o644)
	os.WriteFile(filepath.Join(srcDir, "run.sh"), []byte("#!/bin/sh"), 0o755)

	archivePath := filepath.Join(t.TempDir(), "plugin.zip")
	if err := PackPlugin(srcDir, archivePath); err != nil {
		t.Fatal(err)
	}

	// Now unpack.
	destDir := filepath.Join(t.TempDir(), "unpacked")
	if err := UnpackPlugin(archivePath, destDir); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(destDir, "manifest.toml")); err != nil {
		t.Error("manifest.toml should exist after unpack")
	}
	if _, err := os.Stat(filepath.Join(destDir, "run.sh")); err != nil {
		t.Error("run.sh should exist after unpack")
	}
}

func TestUnpackPlugin_MissingArchive(t *testing.T) {
	err := UnpackPlugin("/nonexistent/archive.zip", t.TempDir())
	if err == nil {
		t.Error("should fail for nonexistent archive")
	}
}

func TestUnpackPlugin_NoManifestInArchive(t *testing.T) {
	// Create a zip without a manifest.
	archivePath := filepath.Join(t.TempDir(), "bad.zip")
	f, _ := os.Create(archivePath)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("readme.txt")
	w.Write([]byte("no manifest here"))
	zw.Close()
	f.Close()

	err := UnpackPlugin(archivePath, t.TempDir())
	if err == nil {
		t.Error("should fail without manifest in archive")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUnpackPlugin_ZipSlipPrevention(t *testing.T) {
	// Create a zip with a path traversal entry.
	archivePath := filepath.Join(t.TempDir(), "evil.zip")
	f, _ := os.Create(archivePath)
	zw := zip.NewWriter(f)

	// Add a manifest so it passes the manifest check.
	w, _ := zw.Create("manifest.toml")
	w.Write([]byte("name = \"evil\"\nversion = \"1\""))

	// Add a file with path traversal.
	w, _ = zw.Create("../../../etc/evil.txt")
	w.Write([]byte("gotcha"))

	zw.Close()
	f.Close()

	destDir := filepath.Join(t.TempDir(), "dest")
	err := UnpackPlugin(archivePath, destDir)
	if err == nil {
		t.Error("should detect zip slip attempt")
	}
	if !strings.Contains(err.Error(), "illegal file path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUnpackPlugin_InvalidZip(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "notazip.zip")
	os.WriteFile(archivePath, []byte("this is not a zip file"), 0o644)

	err := UnpackPlugin(archivePath, t.TempDir())
	if err == nil {
		t.Error("should fail for invalid zip")
	}
}

// ---------- hasManifest ----------

func TestHasManifest(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"toml", "manifest.toml", true},
		{"yaml", "manifest.yaml", true},
		{"yml", "manifest.yml", true},
		{"none", "readme.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, tt.filename), []byte("name = \"test\""), 0o644)
			if got := hasManifest(dir); got != tt.want {
				t.Errorf("hasManifest(%s) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

// ---------- PackPlugin with bad output path ----------

func TestPackPlugin_BadOutputPath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte("name = \"test\"\nversion = \"1\""), 0o644)

	err := PackPlugin(dir, "/nonexistent/path/output.zip")
	if err == nil {
		t.Error("should fail with bad output path")
	}
}

// ---------- Round-trip: Pack then Unpack then ScanDirectory ----------

func TestPackUnpackScan_RoundTrip(t *testing.T) {
	// Create a plugin dir with manifest and script.
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "manifest.toml"), []byte("name = \"roundtrip\"\nversion = \"2.0.0\"\ndescription = \"round trip test\""), 0o644)
	os.WriteFile(filepath.Join(srcDir, "greet.sh"), []byte("#!/bin/sh\necho hello"), 0o755)

	// Pack it.
	archivePath := filepath.Join(t.TempDir(), "rt.zip")
	if err := PackPlugin(srcDir, archivePath); err != nil {
		t.Fatal(err)
	}

	// Unpack to a new location.
	destDir := filepath.Join(t.TempDir(), "plugins", "roundtrip")
	if err := UnpackPlugin(archivePath, destDir); err != nil {
		t.Fatal(err)
	}

	// Scan the parent directory.
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	n, err := reg.ScanDirectory(filepath.Dir(destDir))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("scan found %d plugins, want 1", n)
	}

	list := reg.List()
	if len(list) != 1 || list[0].Name != "roundtrip" {
		t.Errorf("unexpected plugin list: %v", list)
	}
}

// ---------- ScanDirectory with .yml manifest ----------

func TestRegistry_ScanDirectory_YMLManifest(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "ymlplugin")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "manifest.yml"), []byte("name: ymlplugin\nversion: 1.0.0\n"), 0o644)

	reg := NewRegistry(nil, nil, PermissionPolicy{})
	n, err := reg.ScanDirectory(base)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("found %d, want 1", n)
	}
}
