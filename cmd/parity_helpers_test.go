package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiffStrings(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, nil},
		{"a has extra", []string{"a", "b", "c"}, []string{"a"}, []string{"b", "c"}},
		{"b has extra", []string{"a"}, []string{"a", "b", "c"}, nil},
		{"no overlap", []string{"x", "y"}, []string{"a", "b"}, []string{"x", "y"}},
		{"empty a", nil, []string{"a"}, nil},
		{"empty b", []string{"a"}, nil, []string{"a"}},
		{"both empty", nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffStrings(tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("diffStrings(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCountGlobFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create some Go files.
	for _, name := range []string{"loop.go", "retrieval.go", "tools.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package agent"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	count := countGlobFiles(root, []string{"internal/agent/*.go"})
	if count != 3 {
		t.Errorf("countGlobFiles = %d, want 3", count)
	}
}

func TestCountGlobFiles_NoMatch(t *testing.T) {
	root := t.TempDir()
	count := countGlobFiles(root, []string{"nonexistent/*.go"})
	if count != 0 {
		t.Errorf("countGlobFiles = %d, want 0", count)
	}
}

func TestCountGlobFiles_IgnoresFuzz(t *testing.T) {
	root := t.TempDir()
	fuzzDir := filepath.Join(root, "crates", "roboticus-agent", "fuzz")
	if err := os.MkdirAll(fuzzDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fuzzDir, "test.rs"), []byte("fn main(){}"), 0o644); err != nil {
		t.Fatal(err)
	}

	count := countGlobFiles(root, []string{"crates/roboticus-agent/fuzz/*.rs"})
	if count != 0 {
		t.Errorf("countGlobFiles with fuzz dir = %d, want 0 (should be ignored)", count)
	}
}

func TestFindNewRustFiles(t *testing.T) {
	root := t.TempDir()
	crateDir := filepath.Join(root, "crates", "roboticus-agent", "src")
	if err := os.MkdirAll(crateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crateDir, "lib.rs"), []byte("pub mod agent;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crateDir, "agent.rs"), []byte("pub fn run(){}"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := findNewRustFiles(root)
	if len(files) != 2 {
		t.Errorf("findNewRustFiles = %v, want 2 files", files)
	}
}

func TestFindNewRustFiles_IgnoresTarget(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "crates", "roboticus-agent", "target", "debug")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "build.rs"), []byte("fn main(){}"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := findNewRustFiles(root)
	if len(files) != 0 {
		t.Errorf("findNewRustFiles should skip target dir, got %v", files)
	}
}

func TestFindNewRustFiles_EmptyDir(t *testing.T) {
	root := t.TempDir()
	crateDir := filepath.Join(root, "crates")
	if err := os.MkdirAll(crateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	files := findNewRustFiles(root)
	if len(files) != 0 {
		t.Errorf("findNewRustFiles on empty crates dir = %v, want empty", files)
	}
}

func TestFindNewRustFiles_NoCratesDir(t *testing.T) {
	root := t.TempDir()
	files := findNewRustFiles(root)
	if files != nil && len(files) != 0 {
		t.Errorf("findNewRustFiles with no crates dir = %v, want empty", files)
	}
}

func TestFindMissingFunctions_AllPresent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package agent
func HybridSearch() {}
func EpisodicMemory() {}
`
	if err := os.WriteFile(filepath.Join(dir, "retrieval.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	missing := findMissingFunctions(root, []string{"internal/agent/*.go"},
		[]string{"hybrid_search", "episodic_memory"}, nil)
	if len(missing) != 0 {
		t.Errorf("findMissingFunctions reported missing: %v", missing)
	}
}

func TestFindMissingFunctions_SomeMissing(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package agent
func HybridSearch() {}
`
	if err := os.WriteFile(filepath.Join(dir, "retrieval.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	missing := findMissingFunctions(root, []string{"internal/agent/*.go"},
		[]string{"hybrid_search", "missing_function"}, nil)
	if len(missing) != 1 || missing[0] != "missing_function" {
		t.Errorf("findMissingFunctions = %v, want [missing_function]", missing)
	}
}
