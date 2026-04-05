package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShouldIgnoreRustFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"crates/roboticus-agent/src/retrieval.rs", false},
		{"crates/roboticus-agent/fuzz/fuzz_targets/fuzz_check_injection.rs", true},
		{"crates/roboticus-agent/fuzz/target/release/build/foo/out/generated.rs", true},
		{"crates/roboticus-api/target/debug/build/bar/out/bindgen.rs", true},
	}

	for _, tt := range tests {
		if got := shouldIgnoreRustFile(tt.path); got != tt.want {
			t.Errorf("shouldIgnoreRustFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestLooksLikeEndpointPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/sessions", true},
		{"{id}", true},
		{"x-api-key", false},
		{"hub.challenge", false},
		{"http://example.test/v1/models", false},
		{"content-type", false},
	}

	for _, tt := range tests {
		if got := looksLikeEndpointPath(tt.path); got != tt.want {
			t.Errorf("looksLikeEndpointPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestNormalizeEndpointPath(t *testing.T) {
	if got := normalizeEndpointPath("{id}"); got != "/{id}" {
		t.Fatalf("normalizeEndpointPath({id}) = %q, want /{id}", got)
	}
	if got := normalizeEndpointPath("/api/sessions"); got != "/api/sessions" {
		t.Fatalf("normalizeEndpointPath(/api/sessions) = %q, want /api/sessions", got)
	}
}

func TestExtractEndpointsFiltersNoise(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "crates/roboticus-api/src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	src := `
package routes
func register(r Router) {
	r.get("/api/sessions", handler)
	r.get("x-api-key", bad)
	r.get("hub.challenge", bad2)
	r.post("{id}", child)
}`
	if err := os.WriteFile(filepath.Join(dir, "routes.rs"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	got := extractEndpoints(root, "crates/roboticus-api/src")
	want := []string{"GET /api/sessions", "POST /{id}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractEndpoints() = %#v, want %#v", got, want)
	}
}

func TestFindMissingFunctionsUsesAliases(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal/plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	src := `package plugin
func NewRegistry() {}
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	missing := findMissingFunctions(root, []string{"internal/plugin/*.go"}, []string{"plugin_registry"}, map[string][]string{
		"plugin_registry": {"plugin_registry", "registry", "newregistry"},
	})
	if len(missing) != 0 {
		t.Fatalf("findMissingFunctions() reported missing aliases: %#v", missing)
	}
}
