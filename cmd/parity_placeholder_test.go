package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCLIParity_NoPlaceholderStringLiterals(t *testing.T) {
	assertNoPlaceholderLiterals(t, ".")
}

func assertNoPlaceholderLiterals(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			raw, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			lower := strings.ToLower(raw)
			if strings.Contains(lower, "not yet implemented") ||
				strings.Contains(lower, "not yet available in the go implementation") {
				t.Errorf("%s contains placeholder literal %q", path, raw)
			}
			return true
		})
	}
}
