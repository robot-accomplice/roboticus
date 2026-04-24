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

func repoRootPath(parts ...string) string {
	base := filepath.Clean(filepath.Join("..", ".."))
	all := append([]string{base}, parts...)
	return filepath.Join(all...)
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func walkGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

func countCodeLines(src string) int {
	count := 0
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		count++
	}
	return count
}

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
			if importPath == "roboticus/internal/agent" ||
				strings.HasPrefix(importPath, "roboticus/internal/agent/") {
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

// TestArchitecture_RoutesUseRunPipeline verifies connectors use the package-level
// wrapper, not direct runner method calls. This preserves a single canonical
// pipeline entry point for instrumentation and future decoration.
func TestArchitecture_RoutesUseRunPipeline(t *testing.T) {
	routesDir := filepath.Join("routes")
	for _, path := range walkGoFiles(t, routesDir) {
		src := readRepoFile(t, path)
		if strings.Contains(src, "p.Run(") {
			t.Errorf("%s calls p.Run(...) directly — use pipeline.RunPipeline(...) instead", path)
		}
	}
}

// TestArchitecture_ConnectorFilesInvokeRunPipeline verifies the main agent-facing
// connectors stay on the unified pipeline path.
func TestArchitecture_ConnectorFilesInvokeRunPipeline(t *testing.T) {
	files := []string{
		filepath.Join("routes", "agent.go"),
		filepath.Join("routes", "sessions.go"),
		filepath.Join("routes", "admin_webhooks.go"),
	}
	for _, path := range files {
		src := readRepoFile(t, path)
		if !strings.Contains(src, "pipeline.RunPipeline(") {
			t.Errorf("%s must invoke pipeline.RunPipeline(...) — connectors stay thin via the unified pipeline", path)
		}
	}

	routeFamilies := map[string][]string{
		"routing_admin": {
			filepath.Join("routes", "routing_admin.go"),
			filepath.Join("routes", "routing_admin_exercise.go"),
			filepath.Join("routes", "routing_admin_exercise_sender.go"),
			filepath.Join("routes", "routing_admin_dataset.go"),
			filepath.Join("routes", "routing_admin_scores.go"),
		},
	}
	for family, paths := range routeFamilies {
		familyInvokesPipeline := false
		for _, path := range paths {
			src := readRepoFile(t, path)
			if strings.Contains(src, "pipeline.RunPipeline(") {
				familyInvokesPipeline = true
				break
			}
		}
		if !familyInvokesPipeline {
			t.Errorf("%s route surface must invoke pipeline.RunPipeline(...) via the unified connector path", family)
		}
	}

	cronFiles := []string{
		filepath.Join("routes", "cron.go"),
		filepath.Join("routes", "cron_run_now.go"),
	}
	cronInvokesPipeline := false
	for _, path := range cronFiles {
		src := readRepoFile(t, path)
		if strings.Contains(src, "pipeline.RunPipeline(") {
			cronInvokesPipeline = true
			break
		}
	}
	if !cronInvokesPipeline {
		t.Errorf("cron route surface must invoke pipeline.RunPipeline(...) via the unified connector path")
	}
}

// TestArchitecture_ConnectorFilesAreStructurallyThin keeps the main connectors
// close to parse -> call -> format. If a file grows beyond the threshold,
// business logic is likely leaking out of the pipeline.
func TestArchitecture_ConnectorFilesAreStructurallyThin(t *testing.T) {
	limits := map[string]int{
		filepath.Join("routes", "agent.go"):    190, // v1.0.2: +10 for SecurityClaim construction (Gap 2 fix)
		filepath.Join("routes", "sessions.go"): 350,
		filepath.Join("routes", "cron.go"):     320,
	}
	for path, maxLines := range limits {
		codeLines := countCodeLines(readRepoFile(t, path))
		if codeLines > maxLines {
			t.Errorf("%s has %d code lines (max %d) — check whether business logic leaked out of the pipeline",
				path, codeLines, maxLines)
		}
	}
}

// TestArchitecture_ConnectorsDoNotContainPolicyDecisions catches business-logic
// drift into connectors. Policy, routing, and complexity decisions belong in
// the pipeline.
func TestArchitecture_ConnectorsDoNotContainPolicyDecisions(t *testing.T) {
	forbiddenPatterns := map[string]string{
		"classify_complexity": "complexity classification belongs in the pipeline",
		"select_routed_model": "model selection belongs in the pipeline",
		"PolicyEngine":        "policy evaluation belongs in the pipeline",
	}
	files := []string{
		filepath.Join("routes", "agent.go"),
		filepath.Join("routes", "sessions.go"),
		filepath.Join("routes", "cron.go"),
		filepath.Join("routes", "admin.go"),
	}
	for _, path := range files {
		src := readRepoFile(t, path)
		for pattern, reason := range forbiddenPatterns {
			if strings.Contains(src, pattern) {
				t.Errorf("%s contains %q — %s", path, pattern, reason)
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
			if importPath == "roboticus/internal/pipeline" {
				t.Errorf("%s imports pipeline — channel adapters must not depend on pipeline",
					entry.Name())
			}
			if importPath == "roboticus/internal/agent" ||
				strings.HasPrefix(importPath, "roboticus/internal/agent/") {
				t.Errorf("%s imports agent — channel adapters must not depend on agent",
					entry.Name())
			}
		}
	}
}

// TestArchitecture_PipelineDoesNotDependOnAPI preserves the dependency DAG:
// API may depend on pipeline, but pipeline must never depend on API.
func TestArchitecture_PipelineDoesNotDependOnAPI(t *testing.T) {
	pipelineDir := repoRootPath("internal", "pipeline")
	fset := token.NewFileSet()
	for _, path := range walkGoFiles(t, pipelineDir) {
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "roboticus/internal/api" ||
				strings.HasPrefix(importPath, "roboticus/internal/api/") {
				t.Errorf("%s imports %s — pipeline must not depend on API", path, importPath)
			}
		}
	}
}

// TestArchitecture_DocsStaySynchronizedOnOffPipelineExemptions verifies the
// approved exemption list is identical across both architecture documents.
func TestArchitecture_DocsStaySynchronizedOnOffPipelineExemptions(t *testing.T) {
	architecture := readRepoFile(t, repoRootPath("ARCHITECTURE.md"))
	rules := readRepoFile(t, repoRootPath("architecture_rules.md"))

	requiredMentions := []string{
		"/api/interview/start",
		"/api/interview/turn",
		"/api/interview/finish",
		"AnalyzeSession",
		"AnalyzeTurn",
	}

	for _, phrase := range requiredMentions {
		if !strings.Contains(architecture, phrase) {
			t.Fatalf("ARCHITECTURE.md is missing required exemption text %q", phrase)
		}
		if !strings.Contains(rules, phrase) {
			t.Fatalf("architecture_rules.md is missing required exemption text %q", phrase)
		}
	}
}

// TestArchitecture_PipelineHasNoAppStateReferences ensures the pipeline operates
// through capability interfaces and pipeline-scoped types, not concrete API state.
func TestArchitecture_PipelineHasNoAppStateReferences(t *testing.T) {
	pipelineDir := repoRootPath("internal", "pipeline")
	for _, path := range walkGoFiles(t, pipelineDir) {
		src := readRepoFile(t, path)
		if strings.Contains(src, "AppState") {
			t.Errorf("%s mentions AppState — pipeline must not depend on API state", path)
		}
	}
}

// TestArchitecture_MDDocumentsOffPipelineExemption keeps any intentional
// exceptions explicit. The interview flow is the approved off-pipeline path.
func TestArchitecture_MDDocumentsOffPipelineExemption(t *testing.T) {
	arch := readRepoFile(t, repoRootPath("ARCHITECTURE.md"))
	if !strings.Contains(arch, "Off-pipeline") || !strings.Contains(arch, "/api/interview") {
		t.Fatal("ARCHITECTURE.md must document the off-pipeline interview exemption")
	}
}
