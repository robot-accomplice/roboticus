package api

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// importsOf parses a Go file and returns its import paths.
func importsOf(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var imports []string
	for _, imp := range f.Imports {
		imports = append(imports, strings.Trim(imp.Path.Value, `"`))
	}
	return imports
}

// assertNoForbiddenImports walks every non-test .go file under dir and fails
// if any of them import a path matching one of the forbidden prefixes.
func assertNoForbiddenImports(t *testing.T, dir string, forbidden []string, reason string) {
	t.Helper()
	files := walkGoFiles(t, dir)
	if len(files) == 0 {
		t.Skipf("no Go files in %s", dir)
	}
	for _, path := range files {
		for _, imp := range importsOf(t, path) {
			for _, prefix := range forbidden {
				if imp == prefix || strings.HasPrefix(imp, prefix+"/") {
					t.Errorf("%s imports %s -- %s", path, imp, reason)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 1. Layered dependency enforcement
// ---------------------------------------------------------------------------

func TestFitness_DB_IsLowLevel(t *testing.T) {
	assertNoForbiddenImports(t,
		repoRootPath("internal", "db"),
		[]string{
			"roboticus/internal/api",
			"roboticus/internal/pipeline",
			"roboticus/internal/agent",
			"roboticus/internal/llm",
		},
		"internal/db is a low-level layer and must not depend on higher layers",
	)
}

func TestFitness_Core_IsLowestLayer(t *testing.T) {
	assertNoForbiddenImports(t,
		repoRootPath("internal", "core"),
		[]string{
			"roboticus/internal/api",
			"roboticus/internal/pipeline",
			"roboticus/internal/agent",
			"roboticus/internal/llm",
		},
		"internal/core is the lowest layer and must not depend on higher layers",
	)
}

func TestFitness_LLM_DoesNotImportPipelineOrAPI(t *testing.T) {
	assertNoForbiddenImports(t,
		repoRootPath("internal", "llm"),
		[]string{
			"roboticus/internal/pipeline",
			"roboticus/internal/api",
		},
		"internal/llm must not depend on pipeline or API",
	)
}

func TestFitness_Schedule_DoesNotImportAPI(t *testing.T) {
	assertNoForbiddenImports(t,
		repoRootPath("internal", "schedule"),
		[]string{
			"roboticus/internal/api",
		},
		"internal/schedule must not depend on API",
	)
}

// TestFitness_NoCyclicDependencies verifies that key dependency edges are
// one-directional. In each pair, the "lower" package must NOT import the
// "higher" package. (The reverse direction -- higher importing lower -- is
// the intended dependency flow and is fine.)
func TestFitness_NoCyclicDependencies(t *testing.T) {
	// Each entry: the lower-level package must not import the higher-level one.
	// Format: {lowerPkg, higherPkg (forbidden import), description}
	pairs := [][3]string{
		{"internal/pipeline", "internal/api", "pipeline must not import api"},
		{"internal/db", "internal/api", "db must not import api"},
		{"internal/db", "internal/pipeline", "db must not import pipeline"},
		{"internal/core", "internal/api", "core must not import api"},
		{"internal/core", "internal/agent", "core must not import agent"},
		{"internal/llm", "internal/api", "llm must not import api"},
		{"internal/llm", "internal/pipeline", "llm must not import pipeline"},
	}
	for _, pair := range pairs {
		dirLower := repoRootPath(pair[0])
		forbidden := "roboticus/" + pair[1]

		for _, path := range walkGoFiles(t, dirLower) {
			for _, imp := range importsOf(t, path) {
				if imp == forbidden || strings.HasPrefix(imp, forbidden+"/") {
					t.Errorf("cycle risk (%s): %s imports %s", pair[2], path, imp)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Guard chain completeness
// ---------------------------------------------------------------------------

func TestFitness_DefaultGuardChain_ContainsRequiredGuards(t *testing.T) {
	// Import the pipeline package indirectly: we parse the source of guards.go
	// and verify DefaultGuardChain references all required guard types.
	src := readRepoFile(t, repoRootPath("internal", "pipeline", "guards.go"))

	// Find the FullGuardChain function body (DefaultGuardChain delegates to it).
	idx := strings.Index(src, "func FullGuardChain()")
	if idx < 0 {
		t.Fatal("FullGuardChain function not found in guards.go")
	}
	body := src[idx:]
	// Trim to closing brace of the function.
	braceDepth := 0
	for i, ch := range body {
		if ch == '{' {
			braceDepth++
		}
		if ch == '}' {
			braceDepth--
			if braceDepth == 0 {
				body = body[:i+1]
				break
			}
		}
	}

	required := []string{
		"EmptyResponseGuard",
		"SystemPromptLeakGuard",
		"InternalMarkerGuard",
		"ContentClassificationGuard",
		"RepetitionGuard",
	}
	for _, guard := range required {
		if !strings.Contains(body, guard) {
			t.Errorf("FullGuardChain must include %s", guard)
		}
	}
}

func TestFitness_DefaultGuardChain_DelegatesCorrectly(t *testing.T) {
	// DefaultGuardChain must delegate to FullGuardChain.
	src := readRepoFile(t, repoRootPath("internal", "pipeline", "guards.go"))

	idx := strings.Index(src, "func DefaultGuardChain()")
	if idx < 0 {
		t.Fatal("DefaultGuardChain function not found")
	}
	body := src[idx : idx+200]
	if !strings.Contains(body, "FullGuardChain()") {
		t.Error("DefaultGuardChain must delegate to FullGuardChain")
	}
}

func TestFitness_AllGuards_ImplementCheckMethod(t *testing.T) {
	// Every struct instantiated in FullGuardChain must have a Check method
	// defined in the pipeline package. We scan all .go files for
	// func (g *XxxGuard) Check(...) GuardResult signatures.
	pipelineDir := repoRootPath("internal", "pipeline")

	// Collect all guard type names from FullGuardChain body.
	src := readRepoFile(t, filepath.Join(pipelineDir, "guards.go"))
	idx := strings.Index(src, "func FullGuardChain()")
	if idx < 0 {
		t.Fatal("FullGuardChain not found")
	}
	body := src[idx:]
	braceDepth := 0
	for i, ch := range body {
		if ch == '{' {
			braceDepth++
		}
		if ch == '}' {
			braceDepth--
			if braceDepth == 0 {
				body = body[:i+1]
				break
			}
		}
	}

	// Extract guard type names (patterns like &FooGuard{} or NewFooGuard()).
	var guardTypes []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		// Match &TypeName{}.
		if strings.HasPrefix(trimmed, "&") && strings.Contains(trimmed, "Guard{") {
			name := strings.TrimPrefix(trimmed, "&")
			name = strings.SplitN(name, "{", 2)[0]
			guardTypes = append(guardTypes, name)
		}
		// Match NewTypeNameGuard() constructors -- the underlying type is TypeNameGuard.
		if strings.HasPrefix(trimmed, "New") && strings.Contains(trimmed, "Guard(") {
			name := strings.TrimPrefix(trimmed, "New")
			name = strings.SplitN(name, "(", 2)[0]
			guardTypes = append(guardTypes, name)
		}
	}

	if len(guardTypes) == 0 {
		t.Fatal("could not extract guard types from FullGuardChain")
	}

	// Collect all Check method receivers across the pipeline package.
	var allSrc strings.Builder
	for _, f := range walkGoFiles(t, pipelineDir) {
		allSrc.WriteString(readRepoFile(t, f))
		allSrc.WriteString("\n")
	}
	combined := allSrc.String()

	for _, gt := range guardTypes {
		// Look for "func (g *GuardType) Check(" pattern.
		sig := ") Check("
		receiver := "*" + gt
		pattern := receiver + sig
		if !strings.Contains(combined, pattern) {
			t.Errorf("guard %s registered in FullGuardChain but no Check method found", gt)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Connector-factory pattern invariants
// ---------------------------------------------------------------------------

func TestFitness_RouteHandlers_MaxLOC(t *testing.T) {
	routesDir := repoRootPath("internal", "api", "routes")
	files := walkGoFiles(t, routesDir)
	if len(files) == 0 {
		t.Skip("no route handler files found")
	}

	const maxLOC = 500

	// Known exceptions: these files predate the fitness gate and need
	// refactoring tracked separately.
	exceptions := map[string]int{
		"admin.go":          650, // TODO: split admin commands into pipeline actions
		"revenue.go":        600, // revenue CRUD is inherently wide
		"session_detail.go": 600, // session sub-routes gathered in one file
	}

	for _, path := range files {
		loc := countCodeLines(readRepoFile(t, path))
		base := filepath.Base(path)
		limit := maxLOC
		if exc, ok := exceptions[base]; ok {
			limit = exc
		}
		if loc > limit {
			t.Errorf("%s has %d code lines (max %d) -- business logic is leaking out of the pipeline",
				path, loc, limit)
		}
	}
}

func TestFitness_Agent_DoesNotImportRoutes(t *testing.T) {
	assertNoForbiddenImports(t,
		repoRootPath("internal", "agent"),
		[]string{
			"roboticus/internal/api/routes",
		},
		"internal/agent must not import route handlers",
	)
}

func TestFitness_TestUtil_LimitedInternalImports(t *testing.T) {
	testutilDir := repoRootPath("testutil")
	if _, err := os.Stat(testutilDir); os.IsNotExist(err) {
		t.Skip("testutil directory not found")
	}

	allowed := map[string]bool{
		"roboticus/internal/db":   true,
		"roboticus/internal/core": true,
		"roboticus/internal/llm":  true, // test mocks for llm.Completer
	}

	for _, path := range walkGoFiles(t, testutilDir) {
		for _, imp := range importsOf(t, path) {
			if !strings.HasPrefix(imp, "roboticus/internal/") {
				continue
			}
			if !allowed[imp] {
				t.Errorf("%s imports %s -- testutil may only import internal/db and internal/core",
					path, imp)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 4. Pipeline invocation discipline
// ---------------------------------------------------------------------------

func TestFitness_Daemon_UsesRunPipeline(t *testing.T) {
	daemonDir := repoRootPath("internal", "daemon")
	if _, err := os.Stat(daemonDir); os.IsNotExist(err) {
		t.Skip("daemon directory not found")
	}
	files := walkGoFiles(t, daemonDir)
	if len(files) == 0 {
		t.Skip("no Go files in internal/daemon")
	}

	foundRunPipeline := false
	for _, path := range files {
		src := readRepoFile(t, path)
		if strings.Contains(src, "pipeline.RunPipeline(") || strings.Contains(src, "pipeline.RunPipeline (") {
			foundRunPipeline = true
		}
		// Check for direct pipeline runner calls (p.Run, pipe.Run, etc.)
		// but not unrelated method calls like loop.Run.
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			// Match patterns like "d.pipe.Run(" or "p.Run(" but not "loop.Run("
			if (strings.Contains(trimmed, ".pipe.Run(") ||
				strings.Contains(trimmed, "pipeline.Run(")) &&
				!strings.Contains(trimmed, "RunPipeline") {
				t.Errorf("%s calls pipeline runner directly -- daemon must use pipeline.RunPipeline: %s",
					path, trimmed)
			}
		}
	}
	if !foundRunPipeline {
		t.Error("internal/daemon does not call pipeline.RunPipeline -- daemon must orchestrate via the unified pipeline entry point")
	}
}

func TestFitness_Channels_DontCallRunPipelineDirectly(t *testing.T) {
	channelDir := repoRootPath("internal", "channel")
	if _, err := os.Stat(channelDir); os.IsNotExist(err) {
		t.Skip("channel directory not found")
	}
	for _, path := range walkGoFiles(t, channelDir) {
		src := readRepoFile(t, path)
		if strings.Contains(src, "pipeline.RunPipeline(") {
			t.Errorf("%s calls pipeline.RunPipeline -- channel adapters send messages, daemon orchestrates",
				path)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. Security boundary
// ---------------------------------------------------------------------------

func TestFitness_Security_DoesNotImportAPIOrAgent(t *testing.T) {
	secDir := repoRootPath("internal", "security")
	if _, err := os.Stat(secDir); os.IsNotExist(err) {
		t.Skip("internal/security not found")
	}
	assertNoForbiddenImports(t,
		secDir,
		[]string{
			"roboticus/internal/api",
			"roboticus/internal/agent",
		},
		"internal/security must not depend on API or agent layers",
	)
}

func TestFitness_Wallet_DoesNotImportAPI(t *testing.T) {
	walletDir := repoRootPath("internal", "wallet")
	if _, err := os.Stat(walletDir); os.IsNotExist(err) {
		t.Skip("internal/wallet not found")
	}
	assertNoForbiddenImports(t,
		walletDir,
		[]string{
			"roboticus/internal/api",
		},
		"internal/wallet must not depend on API layer",
	)
}

// ---------------------------------------------------------------------------
// Guard registry completeness (cross-check with DefaultGuardChain)
// ---------------------------------------------------------------------------

func TestFitness_GuardRegistry_CoversRequiredGuards(t *testing.T) {
	src := readRepoFile(t, repoRootPath("internal", "pipeline", "guard_registry.go"))

	// The default registry must register at least these guard names.
	required := []string{
		"EmptyResponseGuard",
		"ContentClassificationGuard",
		"RepetitionGuard",
		"SystemPromptLeakGuard",
		"InternalMarkerGuard",
	}
	for _, guard := range required {
		if !strings.Contains(src, guard) {
			t.Errorf("NewDefaultGuardRegistry must register %s", guard)
		}
	}
}
