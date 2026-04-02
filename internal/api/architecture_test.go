package api

import (
	"go/ast"
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

// TestArchitecture_RoutesDontUseConcretePipeline verifies route handlers accept pipeline.Runner,
// not *pipeline.Pipeline. This enforces the connector-factory pattern: connectors (routes) must
// depend on the Runner interface so the pipeline can be decorated, mocked, or swapped.
func TestArchitecture_RoutesDontUseConcretePipeline(t *testing.T) {
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
		f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				continue
			}
			for _, param := range fn.Type.Params.List {
				// Check for *pipeline.Pipeline (a StarExpr wrapping a SelectorExpr).
				star, ok := param.Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				sel, ok := star.X.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				if ident.Name == "pipeline" && sel.Sel.Name == "Pipeline" {
					t.Errorf("%s: func %s accepts *pipeline.Pipeline — use pipeline.Runner interface instead",
						entry.Name(), fn.Name.Name)
				}
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
