package api

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitecture_RoutesDontImportAgent verifies the connector-factory pattern:
// route handlers (connectors) must NOT import internal/agent directly.
// All business logic goes through internal/pipeline (the factory).
func TestArchitecture_RoutesDontImportAgent(t *testing.T) {
	routesDir := filepath.Join("routes")
	entries, err := os.ReadDir(routesDir)
	if err != nil {
		t.Skipf("routes directory not found: %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(routesDir, entry.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "goboticus/internal/agent" ||
				strings.HasPrefix(importPath, "goboticus/internal/agent/") {
				t.Errorf("%s imports %s — route handlers must use pipeline, not agent directly",
					entry.Name(), importPath)
			}
		}
	}
}

// TestArchitecture_ChannelsDontImportPipeline verifies adapters don't depend on pipeline.
func TestArchitecture_ChannelsDontImportPipeline(t *testing.T) {
	channelDir := filepath.Join("..", "channel")
	entries, err := os.ReadDir(channelDir)
	if err != nil {
		t.Skipf("channel directory not found: %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(channelDir, entry.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "goboticus/internal/pipeline" {
				t.Errorf("%s imports pipeline — channel adapters must not depend on pipeline",
					entry.Name())
			}
			if importPath == "goboticus/internal/agent" ||
				strings.HasPrefix(importPath, "goboticus/internal/agent/") {
				t.Errorf("%s imports agent — channel adapters must not depend on agent",
					entry.Name())
			}
		}
	}
}
