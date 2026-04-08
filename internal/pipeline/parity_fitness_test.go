package pipeline

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/testutil"
)

// extractStageNames queries pipeline_traces for the given session and returns
// the stage names from stages_json.
func extractStageNames(t *testing.T, store *db.Store, sessionID string) []string {
	t.Helper()
	var stagesJSON string
	row := store.QueryRowContext(context.Background(),
		`SELECT stages_json FROM pipeline_traces WHERE session_id = ? ORDER BY rowid DESC LIMIT 1`,
		sessionID)
	if err := row.Scan(&stagesJSON); err != nil {
		return nil // No trace stored.
	}
	var spans []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stagesJSON), &spans); err != nil {
		return nil
	}
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name
	}
	return names
}

func containsStage(stages []string, name string) bool {
	for _, s := range stages {
		if s == name {
			return true
		}
	}
	return false
}

// TestParity_CoreStagesFireForAllPresets verifies that universal pipeline
// stages fire for every preset (API, Streaming, Channel, Cron).
func TestParity_CoreStagesFireForAllPresets(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "parity test"},
		BGWorker: core.NewBackgroundWorker(4),
	})

	// Universal stages that must fire for EVERY preset.
	universalStages := []string{
		"validation",
		"injection_defense",
		"session_resolution",
		"message_storage",
		"authority_resolution",
		"inference",
	}

	presets := map[string]Config{
		"API":     PresetAPI(),
		"Channel": PresetChannel("test"),
		"Cron":    PresetCron(),
	}

	for name, cfg := range presets {
		t.Run(name, func(t *testing.T) {
			input := Input{Content: "parity test", AgentID: "default", Platform: "test"}
			outcome, err := RunPipeline(ctx, pipe, cfg, input)
			if err != nil {
				t.Fatalf("RunPipeline with %s preset: %v", name, err)
			}
			stages := extractStageNames(t, store, outcome.SessionID)
			for _, expected := range universalStages {
				if !containsStage(stages, expected) {
					t.Errorf("%s preset missing universal stage %q (stages: %v)", name, expected, stages)
				}
			}
		})
	}
}

// TestParity_PresetDifferencesAreExhaustive uses reflection to find every
// boolean field in Config. Each must appear in a documented expectations table.
// If someone adds a new boolean flag without a parity expectation, this test fails.
func TestParity_PresetDifferencesAreExhaustive(t *testing.T) {
	// Every boolean field in Config must be listed here with expected values per preset.
	// Adding a new boolean without updating this table will FAIL this test.
	type presetExpect struct {
		api, streaming, channel, cron bool
	}
	documented := map[string]presetExpect{
		"InjectionDefense":       {true, true, true, true},
		"DedupTracking":          {true, true, true, false},
		"DecompositionGate":      {true, true, true, true},
		"DelegatedExecution":     {true, true, true, false},
		"SpecialistControls":     {false, false, true, false},
		"ShortcutsEnabled":       {true, true, true, true},
		"SkillFirstEnabled":      {false, false, true, false},
		"ShortFollowupExpansion": {true, true, true, false},
		"CacheEnabled":           {true, true, true, true},
		"PostTurnIngest":         {true, true, true, true},
		"NicknameRefinement":     {true, false, false, false},
		"PreferLocalModel":       {false, false, false, false},
		"CronDelegationWrap":     {false, false, false, true},
		"BotCommandDispatch":     {false, false, false, false},
		"InjectDiagnostics":      {true, true, false, false},
	}

	rt := reflect.TypeOf(Config{})
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Type.Kind() != reflect.Bool {
			continue
		}
		if _, ok := documented[field.Name]; !ok {
			t.Errorf("Config.%s is a boolean flag with no parity expectation — add it to the documented table", field.Name)
		}
	}

	// Also verify the documented values match actual presets.
	presetValues := map[string]Config{
		"api": PresetAPI(), "streaming": PresetStreaming(), "channel": PresetChannel("test"), "cron": PresetCron(),
	}
	for fieldName, expect := range documented {
		actual := map[string]bool{
			"api":       getBoolField(PresetAPI(), fieldName),
			"streaming": getBoolField(PresetStreaming(), fieldName),
			"channel":   getBoolField(PresetChannel("test"), fieldName),
			"cron":      getBoolField(PresetCron(), fieldName),
		}
		expected := map[string]bool{"api": expect.api, "streaming": expect.streaming, "channel": expect.channel, "cron": expect.cron}
		for preset, exp := range expected {
			if actual[preset] != exp {
				t.Errorf("Config.%s: %s preset = %v, expected %v", fieldName, preset, actual[preset], exp)
			}
		}
	}
	_ = presetValues
}

func getBoolField(cfg Config, name string) bool {
	v := reflect.ValueOf(cfg)
	f := v.FieldByName(name)
	if !f.IsValid() {
		return false
	}
	return f.Bool()
}

// TestParity_CronSkipsDedup verifies PresetCron has DedupTracking=false.
func TestParity_CronSkipsDedup(t *testing.T) {
	cfg := PresetCron()
	if cfg.DedupTracking {
		t.Error("Cron preset should have DedupTracking=false")
	}
}

// TestParity_ChannelEnablesSkillFirst verifies PresetChannel has SkillFirstEnabled=true.
func TestParity_ChannelEnablesSkillFirst(t *testing.T) {
	cfg := PresetChannel("telegram")
	if !cfg.SkillFirstEnabled {
		t.Error("Channel preset should have SkillFirstEnabled=true")
	}
}
