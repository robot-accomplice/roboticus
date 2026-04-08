package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// routesDir returns the path to internal/api/routes relative to the pipeline package.
func routesDir() string { return filepath.Join("..", "api", "routes") }

// pipelineFiles returns the paths to all non-test .go files in the pipeline package.
func pipelineFiles() []string {
	entries, _ := os.ReadDir(".")
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			files = append(files, e.Name())
		}
	}
	return files
}

// readRouteFile reads a route file by name from internal/api/routes.
func readRouteFile(name string) string {
	data, _ := os.ReadFile(filepath.Join(routesDir(), name))
	return string(data)
}

// TestMandate_ConnectorsDoNotContainPipelineLogic (Rule 4.1)
// Verifies route handler files do not call pipeline-internal functions directly.
func TestMandate_ConnectorsDoNotContainPipelineLogic(t *testing.T) {
	forbidden := []string{
		"EvaluateDecomposition",
		"CompactContext",
		"SynthesizeTaskState",
		"ResolveAuthority",
		"FilterToolOutput",
		"SanitizeModelOutput",
		"BuildGuardContext",
		"CheckInput(", // injection.CheckInput
		"DedupTracker",
		"PostTurnIngest",
	}

	routeFiles := []string{"agent.go", "sessions.go", "channels.go", "admin.go", "health.go"}
	for _, file := range routeFiles {
		src := readRouteFile(file)
		if src == "" {
			continue
		}
		for _, fn := range forbidden {
			if strings.Contains(src, fn) {
				t.Errorf("routes/%s contains %q — pipeline-internal logic in connector (Rule 4.1)", file, fn)
			}
		}
	}
}

// TestMandate_PipelineOwnsAllDocumentedStages (Rule 4.2)
// Verifies every stage listed in Run()'s docstring has a matching BeginSpan call.
func TestMandate_PipelineOwnsAllDocumentedStages(t *testing.T) {
	src, err := os.ReadFile("pipeline.go")
	if err != nil {
		t.Fatalf("read pipeline.go: %v", err)
	}
	content := string(src)

	// Extract documented stages from the Run() docstring.
	docStages := []string{
		"validation",
		"injection_defense",
		"dedup_check",
		"session_resolution",
		"message_storage",
		"decomposition_gate",
		"authority_resolution",
		"skill_dispatch",
		"shortcut_dispatch",
		"inference",
	}

	for _, stage := range docStages {
		spanCall := `BeginSpan("` + stage + `")`
		if !strings.Contains(content, spanCall) {
			t.Errorf("pipeline.go documents stage %q but has no BeginSpan(%q) call (Rule 4.2)", stage, stage)
		}
	}
}

// TestMandate_ConsentIsPipelineOwned (Rule 4.4)
// Verifies cross-channel consent logic lives in the pipeline, not in connectors.
func TestMandate_ConsentIsPipelineOwned(t *testing.T) {
	// Consent MUST be called from pipeline.go.
	src, _ := os.ReadFile("pipeline.go")
	if !strings.Contains(string(src), "checkCrossChannelConsent") {
		t.Error("pipeline.go does not call checkCrossChannelConsent — consent must be pipeline-owned (Rule 4.4)")
	}

	// Consent MUST NOT be called from any connector file.
	consentFunctions := []string{"checkCrossChannelConsent", "hasfulfilledConsent", "ConsentBlocked", "ConsentGranted"}
	entries, _ := os.ReadDir(routesDir())
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(routesDir(), e.Name()))
		src := string(data)
		for _, fn := range consentFunctions {
			if strings.Contains(src, fn) {
				t.Errorf("routes/%s contains %q — consent logic must be pipeline-owned, not in connectors (Rule 4.4)",
					e.Name(), fn)
			}
		}
	}
}

// TestMandate_DependenciesPointInward (Rule 5.1)
// Verifies pipeline package does not import api or api/routes.
func TestMandate_DependenciesPointInward(t *testing.T) {
	for _, file := range pipelineFiles() {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, `"roboticus/internal/api"`) || strings.Contains(content, `"roboticus/internal/api/routes"`) {
			t.Errorf("pipeline/%s imports api package — dependencies must point inward (Rule 5.1)", file)
		}
	}
}

// TestMandate_NoDuplicatedBusinessLogicInRoutes (Rule 6.3)
// Verifies connector files don't contain business logic functions.
func TestMandate_NoDuplicatedBusinessLogicInRoutes(t *testing.T) {
	forbidden := []string{
		"func evaluateDecomposition",
		"func compactContext",
		"func synthesizeTaskState",
		"func resolveAuthority",
		"func filterToolOutput",
		"func sanitizeModelOutput",
		"func buildGuardContext",
		"func deriveTopicTag",
		"func textOverlapScore",
	}

	entries, _ := os.ReadDir(routesDir())
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(routesDir(), e.Name()))
		src := string(data)
		for _, fn := range forbidden {
			if strings.Contains(src, fn) {
				t.Errorf("routes/%s defines %q — business logic must not be duplicated in connectors (Rule 6.3)",
					e.Name(), fn)
			}
		}
	}
}

// TestMandate_StreamingCallsFinalizeStream (Rule 7.2)
// Verifies the streaming handler calls FinalizeStream after the SSE loop.
func TestMandate_StreamingCallsFinalizeStream(t *testing.T) {
	src := readRouteFile("agent.go")
	if src == "" {
		t.Skip("agent.go not found")
	}

	// The streaming handler must contain FinalizeStream call.
	if !strings.Contains(src, "FinalizeStream") {
		t.Error("routes/agent.go streaming handler does not call FinalizeStream — Rule 7.2 violation")
	}
}

// TestMandate_StreamingAndStandardRemainPreInferenceEquivalent (Rule 7.2)
// Verifies streaming and non-streaming presets differ only where the
// architecture rules explicitly allow them to differ before inference.
func TestMandate_StreamingAndStandardRemainPreInferenceEquivalent(t *testing.T) {
	api := PresetAPI()
	stream := PresetStreaming()

	allowedDifferences := map[string][2]any{
		"InferenceMode":      {InferenceStandard, InferenceStreaming},
		"GuardSet":           {GuardSetFull, GuardSetStream},
		"CacheGuardSet":      {GuardSetCached, GuardSetNone},
		"NicknameRefinement": {true, false},
		"ChannelLabel":       {"api", "streaming"},
	}

	apiValue := reflect.ValueOf(api)
	streamValue := reflect.ValueOf(stream)
	configType := apiValue.Type()
	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		apiField := apiValue.Field(i).Interface()
		streamField := streamValue.Field(i).Interface()
		if reflect.DeepEqual(apiField, streamField) {
			continue
		}
		allowed, ok := allowedDifferences[field.Name]
		if !ok {
			t.Fatalf("streaming preset diverges from API preset for pre-inference field %s: api=%v stream=%v",
				field.Name, apiField, streamField)
		}
		if !reflect.DeepEqual(apiField, allowed[0]) || !reflect.DeepEqual(streamField, allowed[1]) {
			t.Fatalf("streaming preset has unexpected %s values: got api=%v stream=%v, want api=%v stream=%v",
				field.Name, apiField, streamField, allowed[0], allowed[1])
		}
		delete(allowedDifferences, field.Name)
	}

	if len(allowedDifferences) != 0 {
		var leftovers []string
		for name := range allowedDifferences {
			leftovers = append(leftovers, name)
		}
		t.Fatalf("allowed streaming/API difference list is stale; unmatched entries: %s", strings.Join(leftovers, ", "))
	}
}

// TestMandate_ErrorMappingOwnedByCore (Rule 4.1)
// Verifies route files use core.HTTPStatusForError, not local error mappers.
func TestMandate_ErrorMappingOwnedByCore(t *testing.T) {
	src := readRouteFile("agent.go")
	if strings.Contains(src, "func pipelineErrorStatus") {
		t.Error("routes/agent.go contains local pipelineErrorStatus — error mapping must be in core (Rule 4.1)")
	}
}
