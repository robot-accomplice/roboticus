package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestBoundary_PipelineDepsFieldCount verifies the PipelineDeps struct stays
// narrow. If someone adds a broad dependency, this test forces a conversation
// about whether the dependency is justified by stage ownership (Rule 5.2).
func TestBoundary_PipelineDepsFieldCount(t *testing.T) {
	const maxFields = 18 // Currently 18 (15 deps + Workspace + AllowedPaths + CheckpointPolicy). Ceiling of 18.

	rt := reflect.TypeOf(PipelineDeps{})
	if rt.NumField() > maxFields {
		t.Errorf("PipelineDeps has %d fields (max %d) — review for dependency narrowing (Rule 5.2)",
			rt.NumField(), maxFields)
	}
}

// TestBoundary_PipelineDepsFieldsAreNarrow verifies each field in PipelineDeps
// is either an interface, a pointer to a narrow concrete type from an inner
// package, or a primitive. Rejects *api.AppState, *daemon.Daemon, etc.
func TestBoundary_PipelineDepsFieldsAreNarrow(t *testing.T) {
	forbidden := []string{"AppState", "Daemon", "Server", "Router"}

	rt := reflect.TypeOf(PipelineDeps{})
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		typeName := field.Type.String()
		for _, f := range forbidden {
			if strings.Contains(typeName, f) {
				t.Errorf("PipelineDeps.%s has type %s — forbidden broad dependency %q (Rule 5.2)",
					field.Name, typeName, f)
			}
		}
	}
}

// TestBoundary_NoAppStateInPipeline scans all .go files in the pipeline package
// for any reference to AppState. The pipeline must depend on narrow interfaces,
// not on the broad composition root (Rule 5.2).
func TestBoundary_NoAppStateInPipeline(t *testing.T) {
	pipelineDir := "."
	entries, err := os.ReadDir(pipelineDir)
	if err != nil {
		t.Fatalf("read pipeline dir: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// Skip test files — they may reference AppState in helper comments.
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pipelineDir, e.Name()))
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, "AppState") || strings.Contains(content, "appState") {
			t.Errorf("%s contains AppState reference — pipeline must not depend on the composition root (Rule 5.2)",
				e.Name())
		}
	}
}

// TestBoundary_InterfacesAreNarrow parses pipeline .go files for interface
// declarations and verifies each has at most 3 methods. Broad interfaces
// are architectural debt that defeats the purpose of dependency injection.
func TestBoundary_InterfacesAreNarrow(t *testing.T) {
	const maxMethods = 3

	pipelineDir := "."
	fset := token.NewFileSet()
	entries, err := os.ReadDir(pipelineDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(pipelineDir, e.Name()), nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				iface, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				methodCount := 0
				if iface.Methods != nil {
					methodCount = len(iface.Methods.List)
				}
				if methodCount > maxMethods {
					t.Errorf("interface %s in %s has %d methods (max %d) — interfaces should be narrow (Rule 5.2)",
						ts.Name.Name, e.Name(), methodCount, maxMethods)
				}
			}
		}
	}
}

// TestBoundary_DepsFieldsAreInterfacesOrNarrowTypes verifies that PipelineDeps
// fields are interfaces (preferred) or narrow concrete types. Non-interface
// fields must be from inner packages (db, llm, core), not from api or daemon.
func TestBoundary_DepsFieldsAreInterfacesOrNarrowTypes(t *testing.T) {
	rt := reflect.TypeOf(PipelineDeps{})
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		ft := field.Type

		// Dereference pointer.
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		// Interface types are always fine.
		if field.Type.Kind() == reflect.Interface {
			continue
		}

		// Check the package path for forbidden packages.
		pkg := ft.PkgPath()
		if strings.Contains(pkg, "internal/api") {
			t.Errorf("PipelineDeps.%s type %s comes from api package — pipeline deps must be from inner packages (Rule 5.1)",
				field.Name, field.Type)
		}
		if strings.Contains(pkg, "internal/daemon") {
			t.Errorf("PipelineDeps.%s type %s comes from daemon package (Rule 5.1)",
				field.Name, field.Type)
		}
	}
}
