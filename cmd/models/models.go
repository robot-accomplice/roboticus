package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"roboticus/cmd/internal/cmdutil"
	"roboticus/internal/core"
	"roboticus/internal/llm"
)

func exerciseConfigFingerprint(config map[string]any) string {
	b, err := json.Marshal(config)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func currentGitRevision() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func startExerciseRun(models []string, iterations int, config map[string]any, intentClass string) (string, error) {
	payload := map[string]any{
		"initiator":          "cli",
		"models":             models,
		"iterations":         iterations,
		"config_fingerprint": exerciseConfigFingerprint(config),
		"git_revision":       currentGitRevision(),
	}
	if strings.TrimSpace(intentClass) != "" {
		payload["notes"] = "intent filter: " + intentClass
	}
	resp, err := cmdutil.APIPost("/api/models/exercise/runs", payload)
	if err != nil {
		return "", err
	}
	runID, _ := resp["run_id"].(string)
	if runID == "" {
		return "", fmt.Errorf("exercise run start did not return run_id")
	}
	return runID, nil
}

func appendExerciseRunResult(runID string, o llm.PromptOutcome) error {
	errMsg := ""
	if o.Err != nil {
		errMsg = o.Err.Error()
	}
	_, err := cmdutil.APIPost("/api/models/exercise/runs/"+runID+"/results", map[string]any{
		"model":          o.Model,
		"intent_class":   o.Prompt.Intent.String(),
		"complexity":     o.Prompt.Complexity.String(),
		"prompt":         o.Prompt.Prompt,
		"content":        o.Content,
		"quality":        o.Quality,
		"latency_ms":     o.LatencyMs,
		"passed":         o.Passed,
		"error_msg":      errMsg,
		"resource_start": o.ResourceStart,
		"resource_end":   o.ResourceEnd,
	})
	return err
}

func completeExerciseRun(runID, status, notes string) error {
	_, err := cmdutil.APIPost("/api/models/exercise/runs/"+runID+"/complete", map[string]any{
		"status": status,
		"notes":  notes,
	})
	return err
}

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Query available models and routing diagnostics",
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured models and routing strategy",
	RunE: func(cmd *cobra.Command, args []string) error {
		config, configErr := cmdutil.APIGet("/api/config")
		available, availErr := cmdutil.APIGet("/api/models/available")

		// Configured models from config.
		if configErr == nil {
			fmt.Println("Configured:")

			primary := ""
			var fallbacks []string

			if llm, ok := config["llm"].(map[string]any); ok {
				if p, ok := llm["primary"].(string); ok {
					primary = p
				} else if p, ok := llm["model"].(string); ok {
					primary = p
				}
				if fb, ok := llm["fallbacks"].([]any); ok {
					for _, f := range fb {
						if s, ok := f.(string); ok {
							fallbacks = append(fallbacks, s)
						}
					}
				}
			}

			if primary != "" {
				fmt.Printf("  Primary: %s\n", primary)
			}
			if len(fallbacks) > 0 {
				fmt.Printf("  Fallbacks: %s\n", strings.Join(fallbacks, ", "))
			}
			if primary == "" && len(fallbacks) == 0 {
				fmt.Println("  (none)")
			}

			// Routing strategy.
			fmt.Println()
			routing := "default"
			var traits []string
			if llm, ok := config["llm"].(map[string]any); ok {
				if r, ok := llm["routing_strategy"].(string); ok && r != "" {
					routing = r
				} else if r, ok := llm["strategy"].(string); ok && r != "" {
					routing = r
				}
				if costAware, ok := llm["cost_aware"].(bool); ok && costAware {
					traits = append(traits, "cost-aware")
				}
				if localFirst, ok := llm["local_first"].(bool); ok && localFirst {
					traits = append(traits, "local-first")
				}
			}
			if len(traits) > 0 {
				fmt.Printf("Routing: %s (%s)\n", routing, strings.Join(traits, ", "))
			} else {
				fmt.Printf("Routing: %s\n", routing)
			}
		}

		// Available models summary.
		if availErr == nil {
			modelCount := 0
			providerCount := 0
			if models, ok := available["models"].([]any); ok {
				modelCount = len(models)
				providers := map[string]bool{}
				for _, m := range models {
					mm, _ := m.(map[string]any)
					if p, ok := mm["provider"].(string); ok {
						providers[p] = true
					}
				}
				providerCount = len(providers)
			}
			fmt.Println()
			fmt.Printf("Available: %d models from %d providers\n", modelCount, providerCount)
		}

		if configErr != nil && availErr != nil {
			return fmt.Errorf("could not reach API: %v", configErr)
		}

		return nil
	},
}

var modelsDiagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Show model routing diagnostics",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/models/routing-diagnostics")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var modelsScanCmd = &cobra.Command{
	Use:   "scan [provider]",
	Short: "Scan providers for available models",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Try to get provider URLs from config for direct probing.
		config, configErr := cmdutil.APIGet("/api/config")
		directResults := map[string][]string{}

		if configErr == nil {
			providers := extractProviders(config)
			for name, url := range providers {
				if len(args) > 0 && !strings.EqualFold(name, args[0]) {
					continue
				}
				models := probeProvider(name, url)
				if len(models) > 0 {
					directResults[name] = models
				}
			}
		}

		if len(directResults) > 0 {
			fmt.Println("Discovered models:")
			fmt.Println()
			var allDiscovered []string
			for provider, models := range directResults {
				fmt.Printf("  %s (%d models):\n", provider, len(models))
				for _, m := range models {
					fmt.Printf("    %s\n", m)
					allDiscovered = append(allDiscovered, provider+"/"+m)
				}
			}
			fmt.Println()

			// Offer to add to config.
			fmt.Print("  Add discovered models to your config? [y/N] ")
			var input string
			if _, err := fmt.Scanln(&input); err != nil && err.Error() != "unexpected newline" {
				fmt.Fprintf(os.Stderr, "  (stdin read error: %v)\n", err)
			}
			if strings.ToLower(input) == "y" || strings.ToLower(input) == "yes" {
				// Set the first as primary, rest as fallbacks.
				if len(allDiscovered) > 0 {
					body := map[string]any{
						"models": map[string]any{
							"primary":   allDiscovered[0],
							"fallbacks": allDiscovered[1:],
						},
					}
					if _, err := cmdutil.APIPut("/api/config", body); err != nil {
						fmt.Printf("  Failed to update config: %v\n", err)
					} else {
						fmt.Printf("  Config updated: primary=%s, %d fallback(s)\n", allDiscovered[0], len(allDiscovered)-1)
					}
				}
			} else {
				fmt.Print("  Print TOML snippet instead? [Y/n] ")
				if _, err := fmt.Scanln(&input); err != nil && err.Error() != "unexpected newline" {
					fmt.Fprintf(os.Stderr, "  (stdin read error: %v)\n", err)
				}
				if input == "" || strings.ToLower(input) == "y" || strings.ToLower(input) == "yes" {
					fmt.Println("  [models]")
					if len(allDiscovered) > 0 {
						fmt.Printf("  primary = %q\n", allDiscovered[0])
					}
					if len(allDiscovered) > 1 {
						var fbs []string
						for _, m := range allDiscovered[1:] {
							fbs = append(fbs, fmt.Sprintf("%q", m))
						}
						fmt.Printf("  fallbacks = [%s]\n", strings.Join(fbs, ", "))
					}
					fmt.Println()
				}
			}
			return nil
		}

		// Fall back to API-only scan.
		path := "/api/models/available?validation_level=scan"
		if len(args) > 0 {
			path += "&provider=" + args[0]
		}
		data, err := cmdutil.APIGet(path)
		if err != nil {
			return err
		}
		models, ok := data["models"].([]any)
		if !ok || len(models) == 0 {
			fmt.Println("No models discovered.")
			return nil
		}

		// Group by provider.
		grouped := map[string][]string{}
		for _, m := range models {
			mm, _ := m.(map[string]any)
			provider, _ := mm["provider"].(string)
			id, _ := mm["id"].(string)
			if provider == "" {
				provider = "unknown"
			}
			grouped[provider] = append(grouped[provider], id)
		}

		fmt.Println("Discovered models:")
		fmt.Println()
		for provider, ids := range grouped {
			fmt.Printf("  %s (%d models):\n", provider, len(ids))
			for _, id := range ids {
				fmt.Printf("    %s\n", id)
			}
		}
		return nil
	},
}

// extractProviders pulls provider name->URL mappings from the config response.
func extractProviders(config map[string]any) map[string]string {
	result := map[string]string{}

	// Try llm.providers array.
	if llm, ok := config["llm"].(map[string]any); ok {
		if providers, ok := llm["providers"].([]any); ok {
			for _, p := range providers {
				pm, _ := p.(map[string]any)
				name, _ := pm["name"].(string)
				url, _ := pm["url"].(string)
				if url == "" {
					url, _ = pm["base_url"].(string)
				}
				if name != "" && url != "" {
					result[name] = strings.TrimRight(url, "/")
				}
			}
		}
	}

	// Try top-level providers map.
	if providers, ok := config["providers"].(map[string]any); ok {
		for name, v := range providers {
			pm, _ := v.(map[string]any)
			if pm == nil {
				continue
			}
			url, _ := pm["url"].(string)
			if url == "" {
				url, _ = pm["base_url"].(string)
			}
			if url != "" {
				result[name] = strings.TrimRight(url, "/")
			}
		}
	}

	return result
}

// probeProvider attempts to discover models by hitting the provider's model endpoint directly.
func probeProvider(name, baseURL string) []string {
	client := &http.Client{Timeout: 5 * time.Second}

	// Try Ollama-style endpoint first.
	if models := probeOllama(client, baseURL); len(models) > 0 {
		return models
	}

	// Try OpenAI-compatible endpoint.
	if models := probeOpenAI(client, baseURL); len(models) > 0 {
		return models
	}

	return nil
}

func probeOllama(client *http.Client, baseURL string) []string {
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var data map[string]any
	if json.Unmarshal(body, &data) != nil {
		return nil
	}
	models, ok := data["models"].([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, m := range models {
		mm, _ := m.(map[string]any)
		if name, ok := mm["name"].(string); ok {
			result = append(result, name)
		} else if name, ok := mm["model"].(string); ok {
			result = append(result, name)
		}
	}
	return result
}

func probeOpenAI(client *http.Client, baseURL string) []string {
	resp, err := client.Get(baseURL + "/v1/models")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var data map[string]any
	if json.Unmarshal(body, &data) != nil {
		return nil
	}
	models, ok := data["data"].([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, m := range models {
		mm, _ := m.(map[string]any)
		if id, ok := mm["id"].(string); ok {
			result = append(result, id)
		}
	}
	return result
}

// modelsExerciseCmd is the single consolidated exercise command,
// replacing the formerly separate `exercise` (single-model) and
// `baseline` (all-models) commands. They did the same thing — exercise
// models through the prompt matrix, score, aggregate — just differing
// in selector and output shape. The consolidation makes the CLI a thin
// connector around internal/llm.ExerciseModels.
var modelsExerciseCmd = &cobra.Command{
	Use:   "exercise [model...]",
	Short: "Exercise models through the prompt matrix to establish quality baselines",
	Long: `Exercise runs models through the 35-prompt matrix across 7 intent classes
(Execution, Delegation, Introspection, Conversation, Memory Recall, Tool Use,
Coding) and produces per-intent quality + latency scorecards. The cross-model
comparison table is ALWAYS shown — absolute scores are meaningless without
peer context. Exercised models are highlighted with a star (★) so operators
can see their fresh run in the context of the full baselined landscape.

SELECTOR:
  No args     → exercise every configured model (primary + fallbacks)
  One or more → exercise just those models (useful for re-baselining a
                specific one without touching the others)

FLAGS:
  -n N             Run the matrix N times per model for statistical
                   confidence. Default: 1.
  --intent NAME    Exercise only one canonical intent slice from the
                   matrix (for example TOOL_USE or MEMORY_RECALL).
  --new-only       Skip models that already have baseline data. Only
                   applies when no model args are given.
  --flush          Flush all existing quality observations before
                   exercising. Preserves the old ` + "`baseline`" + ` command's
                   re-baseline-from-scratch semantics.
  --min-quality N  Flag models scoring below this threshold (0.0–1.0)
                   in the final output. Useful for CI gating.

The process begins with a warm-up stage for local models (two trivial
calls: cold + warm-transition) so cold-start cost doesn't pollute the
scored latency averages. Cloud models skip warm-up.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		iterations, _ := cmd.Flags().GetInt("iterations")
		if iterations < 1 {
			iterations = 1
		}
		minQuality, _ := cmd.Flags().GetFloat64("min-quality")
		newOnly, _ := cmd.Flags().GetBool("new-only")
		flush, _ := cmd.Flags().GetBool("flush")
		intentName, _ := cmd.Flags().GetString("intent")
		var intentFilter *llm.IntentClass
		intentLabel := ""
		if strings.TrimSpace(intentName) != "" {
			intent, err := llm.ParseIntentClassStrict(intentName)
			if err != nil {
				return err
			}
			intentFilter = &intent
			intentLabel = intent.String()
		}

		config, err := cmdutil.APIGet("/api/config")
		if err != nil {
			return fmt.Errorf("cannot reach API: %w", err)
		}
		exerciseCfg := decodeExerciseConfig(config)

		// Resolve the model list: explicit args win; otherwise enumerate
		// configured models.
		models, err := resolveExerciseModelList(config, args, newOnly)
		if err != nil {
			return err
		}
		if len(models) == 0 {
			fmt.Println("  No models to exercise.")
			return nil
		}

		// Pre-flight summary: how many calls, estimated duration,
		// what we'll do (flush or not).
		promptCount := len(llm.ExerciseMatrix)
		if intentFilter != nil {
			promptCount = 0
			for _, prompt := range llm.ExerciseMatrix {
				if prompt.Intent == *intentFilter {
					promptCount++
				}
			}
		}
		totalPrompts := promptCount * iterations
		fmt.Printf("\n  Exercising %d model(s):\n", len(models))
		for _, m := range models {
			label := "local"
			if !llm.ExerciseModelIsLocal(exerciseCfg, m) {
				label = "cloud"
			}
			fmt.Printf("    %-40s  %s\n", m, label)
		}
		if intentFilter != nil {
			fmt.Printf("  Intent filter: %s\n", intentLabel)
		}
		fmt.Printf("  %d prompts × %d iteration(s) = %d scored calls per model.\n", promptCount, iterations, totalPrompts)
		if flush && len(args) == 0 {
			fmt.Printf("  Quality scores will be FLUSHED before exercising (--flush).\n")
		}
		if minQuality > 0 {
			fmt.Printf("  Minimum quality threshold: %.0f%% — models below this will be flagged.\n", minQuality*100)
		}
		fmt.Println()

		// Optional score flush before exercising (preserves the old
		// `baseline` command's reset-first semantic).
		if flush {
			if _, err := cmdutil.APIPost("/api/models/reset", map[string]any{}); err != nil {
				fmt.Printf("  ⚠ flush failed: %v (continuing anyway)\n", err)
			} else {
				fmt.Println("  All quality scores flushed.")
			}
		}

		// Dispatch to the business-logic orchestrator.
		runID, err := startExerciseRun(models, iterations, config, intentLabel)
		if err != nil {
			return fmt.Errorf("start exercise run: %w", err)
		}
		runStatus := "completed"
		runNotes := ""
		defer func() {
			if err := completeExerciseRun(runID, runStatus, runNotes); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to finalize exercise run %s: %v\n", runID, err)
			}
		}()

		var persistErr error
		req := llm.ExerciseRequest{
			Models:       models,
			IntentFilter: intentFilter,
			Iterations:   iterations,
			SendPrompt:   cliPromptSender,
			SendWarmup:   cliWarmupSender(config),
			OnPrompt: func(o llm.PromptOutcome) {
				renderPromptProgress(o)
				if persistErr != nil {
					return
				}
				if err := appendExerciseRunResult(runID, o); err != nil {
					persistErr = err
				}
			},
			Progress:     os.Stdout,
			IsLocal:      func(m string) bool { return llm.ExerciseModelIsLocal(exerciseCfg, m) },
			ModelTimeout: func(m string) time.Duration { return llm.ExerciseModelTimeout(exerciseCfg, m) },
		}
		report, err := llm.ExerciseModels(cmd.Context(), req)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				runStatus = "canceled"
			} else {
				runStatus = "failed"
			}
			runNotes = err.Error()
			return fmt.Errorf("exercise: %w", err)
		}
		if persistErr != nil {
			runStatus = "failed"
			runNotes = persistErr.Error()
			return fmt.Errorf("persist exercise results: %w", persistErr)
		}

		// Always render the cross-model comparison — a single model's
		// 0.71 is uninterpretable without peer context. Exercised
		// models get the ★ highlight.
		renderExerciseReport(report, config, minQuality)
		return nil
	},
}

// resolveExerciseModelList turns the CLI args + flags into the final
// list of models to exercise. Rules:
//   - Explicit args → use those verbatim.
//   - No args → enumerate every configured model (primary + fallbacks).
//   - --new-only only applies when no args are given (explicit args
//     are the operator's expressed intent; --new-only filtering
//     against them would be surprising).
func resolveExerciseModelList(config map[string]any, args []string, newOnly bool) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}

	var configured []string
	if models, ok := config["models"].(map[string]any); ok {
		if p, ok := models["primary"].(string); ok && p != "" {
			configured = append(configured, p)
		}
		if fbs, ok := models["fallbacks"].([]any); ok {
			for _, fb := range fbs {
				if s, ok := fb.(string); ok && s != "" {
					configured = append(configured, s)
				}
			}
		}
	}

	if !newOnly {
		return configured, nil
	}

	// --new-only: filter out models that already have exercise data.
	status, err := cmdutil.APIGet("/api/models/exercise/status")
	if err != nil {
		return configured, nil // fail open; we'd rather over-exercise than silently skip a model
	}
	existing, _ := status["models"].(map[string]any)
	if len(existing) == 0 {
		return configured, nil
	}

	filtered := configured[:0]
	for _, m := range configured {
		if hasExistingData(existing, m) {
			fmt.Printf("  Skipping %s (already has baseline data)\n", m)
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered, nil
}

// hasExistingData checks whether a model name (full or bare) has
// exercise data in the status map. The API may return names with or
// without the provider prefix, so we check both forms.
func hasExistingData(existing map[string]any, model string) bool {
	if count, ok := existing[model]; ok && toFloat(count) > 0 {
		return true
	}
	bare := model
	if idx := strings.Index(model, "/"); idx >= 0 {
		bare = model[idx+1:]
	}
	if count, ok := existing[bare]; ok && toFloat(count) > 0 {
		return true
	}
	return false
}

// cliPromptSender is the CLI-side llm.ModelSender: dispatches one
// scored prompt through the pipeline via /api/agent/message.
func cliPromptSender(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
	body := map[string]any{"content": content, "no_cache": true, "no_escalate": true}
	if model != "" {
		body["model"] = model
	}
	start := time.Now()
	resp, err := cmdutil.APIPostSlow("/api/agent/message", body, timeout)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return "", latencyMs, err
	}
	contentStr := fmt.Sprintf("%v", resp["content"])
	if contentStr == "<nil>" {
		contentStr = ""
	}
	return contentStr, latencyMs, nil
}

// cliWarmupSender returns a WarmupSender bound to the current config
// for timeout resolution. Captures `config` so the returned closure
// doesn't need to refetch it per call.
func cliWarmupSender(config map[string]any) llm.WarmupSender {
	return func(ctx context.Context, model string, timeout time.Duration) llm.WarmupResult {
		body := map[string]any{
			"content":     llm.WarmupPrompt,
			"model":       model,
			"no_cache":    true,
			"no_escalate": true,
		}
		start := time.Now()
		_, err := cmdutil.APIPostSlow("/api/agent/message", body, timeout)
		latencyMs := time.Since(start).Milliseconds()
		res := llm.WarmupResult{LatencyMs: latencyMs}
		if err != nil {
			if latencyMs >= timeout.Milliseconds()-500 {
				res.TimedOut = true
			} else {
				res.Err = err
			}
		}
		return res
	}
}

// renderPromptPrefix is the OnBeforePromptFn callback. Prints the
// "[N/M] INTENT:Cx ... " prefix line (no newline) and starts a
// renderPromptProgress is the OnPromptFn callback that streams a
// result trailer (PASS/FAIL + quality + latency) onto the prefix
// line the orchestrator already printed. The orchestrator emits
// "[N/M] INTENT:Cx ... " with a spinner before the call; this
// callback closes out that line with the result and a newline.
//
// Per the v1.0.6 "no silent blocking calls" rule: prefix + spinner
// are emitted from inside the business logic (ExerciseModels uses
// core.RunWithSpinner) so every code path gets the same feedback
// uniformly — not just the CLI path.
func renderPromptProgress(o llm.PromptOutcome) {
	switch {
	case o.Err != nil:
		fmt.Printf("FAIL  %v\n", o.Err)
	case !o.Passed:
		fmt.Printf("FAIL  empty response  %.1fs\n", float64(o.LatencyMs)/1000.0)
	default:
		preview := o.Content
		if len(preview) > 50 {
			preview = preview[:50] + "..."
		}
		fmt.Printf("PASS  Q=%.2f  %.1fs: %s\n", o.Quality, float64(o.LatencyMs)/1000.0, preview)
	}
}

// renderExerciseReport renders the final per-model scorecards AND the
// cross-model comparison table that puts the exercised models' scores
// in the context of ALL baselined models. The comparison table is
// non-negotiable regardless of how many models the operator exercised
// — a single model's 0.71 is uninterpretable without peer scores.
//
// Exercised models are marked with ★ in the comparison table so
// operators can see their fresh-run results against the historical
// baseline of other configured models.
func renderExerciseReport(report llm.ExerciseReport, config map[string]any, minQuality float64) {
	intents := []string{"EXECUTION", "DELEGATION", "INTROSPECTION", "CONVERSATION", "MEMORY_RECALL", "TOOL_USE", "CODING"}

	// Per-model detailed scorecards for the freshly-exercised models.
	for _, r := range report.Models {
		status := "PASS"
		if r.Fail > 0 && r.Pass == 0 {
			status = "FAIL"
		} else if r.Fail > 0 {
			status = "DEGRADED"
		}
		fmt.Printf("\n  ── %s ──\n", r.Model)
		fmt.Printf("    Pass/Fail: %d / %d  (%s)\n", r.Pass, r.Fail, status)
		fmt.Printf("    Avg quality: %s %.2f\n", qualityBar(r.AvgQuality), r.AvgQuality)

		fmt.Printf("    Intent Quality:\n")
		for _, intent := range intents {
			q := r.IntentQuality[intent]
			fmt.Printf("      %-16s %s %.2f\n", intent, qualityBar(q), q)
		}
		printLatencyScorecard(r.Latencies)

		if baseline, ok := llm.LookupBaseline(r.Model); ok {
			fmt.Printf("    Baseline profile: Eff=%.2f Cost=%.2f Avail=%.2f Loc=%.1f Conf=%.1f Spd=%.2f\n",
				baseline.Efficacy, baseline.Cost, baseline.Availability,
				baseline.Locality, baseline.Confidence, baseline.Speed)
		}

		if !r.Warmup.Skipped {
			if r.Warmup.ColdStartTimedOut {
				fmt.Printf("    Cold-start: >%.0fs (timed out — actual value exceeds timeout)\n", float64(r.Warmup.ColdStartMs)/1000.0)
			} else {
				fmt.Printf("    Cold-start: %.1fs\n", float64(r.Warmup.ColdStartMs)/1000.0)
			}
			marker := ""
			if !r.Warmup.WarmTransitionOK {
				marker = "  ⚠ warm-up may not have taken — scored data may be unreliable"
			}
			fmt.Printf("    Warm-transition: %.1fs%s\n", float64(r.Warmup.WarmTransitionMs)/1000.0, marker)
		}
	}

	// Cross-model comparison — always rendered. Merge fresh results
	// with historical scorecard so operators see the exercised models
	// (★) in context of the full baselined landscape.
	rows := buildComparisonRows(report, config)
	if len(rows) > 0 {
		fmt.Printf("\n  Model Comparison (ranked by quality; ★ = exercised this run):\n\n")
		fmt.Printf("  %-5s  %-30s  %-15s  %5s  %5s  %5s  %5s  %5s  %5s  %5s  %s\n",
			"RANK", "MODEL", "QUALITY", "EXEC", "DELEG", "INTRO", "CONV", "MEMRC", "TOOLS", "CODE", "AVG MS")
		fmt.Println("  " + strings.Repeat("─", 118))
		for i, row := range rows {
			label := row.Model
			prefix := "  "
			if row.Exercised {
				prefix = "★ "
			}
			if len(label) > 30 {
				label = label[:27] + "..."
			}
			fmt.Printf("%s#%-3d  %-30s  %s %3.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %5dms\n",
				prefix, i+1, label, qualityBar(row.AvgQuality), row.AvgQuality*100,
				row.Intent["EXECUTION"]*100, row.Intent["DELEGATION"]*100, row.Intent["INTROSPECTION"]*100,
				row.Intent["CONVERSATION"]*100, row.Intent["MEMORY_RECALL"]*100, row.Intent["TOOL_USE"]*100,
				row.Intent["CODING"]*100,
				row.AvgLatencyMs)
		}
		fmt.Println()

		// Best-per-intent across the full landscape. Helpful for
		// operators deciding which model to route a specific intent
		// class to.
		fmt.Printf("  Best per intent class:\n")
		for _, intent := range intents {
			best := ""
			bestQ := 0.0
			for _, row := range rows {
				if q, ok := row.Intent[intent]; ok && q > bestQ {
					bestQ = q
					best = row.Model
				}
			}
			if best != "" {
				fmt.Printf("    %-18s  %s (%.0f%%)\n", intent, best, bestQ*100)
			}
		}
		fmt.Println()
	}

	// Flag underperformers from THIS RUN (not historical) — the fresh
	// data is what the operator is acting on right now.
	if minQuality > 0 {
		var flagged []string
		for _, r := range report.Models {
			if r.AvgQuality > 0 && r.AvgQuality < minQuality {
				flagged = append(flagged, fmt.Sprintf("%s (%.0f%%)", r.Model, r.AvgQuality*100))
			}
		}
		if len(flagged) > 0 {
			fmt.Printf("  ⚠  Models below %.0f%% quality threshold:\n", minQuality*100)
			for _, f := range flagged {
				fmt.Printf("    - %s\n", f)
			}
			fmt.Printf("\n  Consider removing these from the routing chain with:\n")
			fmt.Printf("    roboticus models remove <model>\n\n")
		}
	}
}

// comparisonRow is the merged view of one model in the comparison
// table: fresh data wins over historical for the exercised set.
type comparisonRow struct {
	Model        string
	Exercised    bool // true if in the current ExerciseReport
	AvgQuality   float64
	Intent       map[string]float64
	AvgLatencyMs int64
}

// buildComparisonRows fetches the historical scorecard and overlays
// the fresh ExerciseReport data so the final table shows every
// configured model's scores with the exercised ones highlighted.
// Falls open on scorecard fetch failure — we'd rather show the
// fresh-only rows than no comparison at all.
func buildComparisonRows(report llm.ExerciseReport, config map[string]any) []comparisonRow {
	byModel := make(map[string]comparisonRow)

	// Start with historical scorecard data for all known models.
	if data, err := cmdutil.APIGet("/api/models/exercise/scorecard"); err == nil {
		if rows, ok := data["rows"].([]any); ok {
			for _, raw := range rows {
				row, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				model, _ := row["model"].(string)
				if model == "" {
					continue
				}
				cr := comparisonRow{
					Model:        model,
					AvgQuality:   toFloat(row["avg_quality"]),
					Intent:       make(map[string]float64),
					AvgLatencyMs: int64(toFloat(row["avg_latency_ms"])),
				}
				if intents, ok := row["intent_quality"].(map[string]any); ok {
					for k, v := range intents {
						cr.Intent[k] = toFloat(v)
					}
				}
				byModel[model] = cr
			}
		}
	}

	// Overlay fresh exercise results — fresh data wins.
	for _, r := range report.Models {
		cr := comparisonRow{
			Model:      r.Model,
			Exercised:  true,
			AvgQuality: r.AvgQuality,
			Intent:     make(map[string]float64, len(r.IntentQuality)),
		}
		for k, v := range r.IntentQuality {
			cr.Intent[k] = v
		}
		var totalLat int64
		var latCount int64
		for _, lats := range r.Latencies {
			for _, l := range lats {
				totalLat += l
				latCount++
			}
		}
		if latCount > 0 {
			cr.AvgLatencyMs = totalLat / latCount
		}
		byModel[r.Model] = cr
	}

	rows := make([]comparisonRow, 0, len(byModel))
	for _, cr := range byModel {
		rows = append(rows, cr)
	}
	// Sort by AvgQuality descending (simple selection sort for small N).
	for i := 0; i < len(rows)-1; i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].AvgQuality > rows[i].AvgQuality {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	return rows
}

var modelsSuggestCmd = &cobra.Command{
	Use:   "suggest",
	Short: "Scan providers and recommend an optimal model chain with quality rationale",
	Long: `Suggest probes all configured providers for available models, scores them
using quality baselines, locality, and cost, then presents an interactive
recommendation with rationale for each model. You can apply the suggestion
directly to your config, merge it with existing fallbacks, or copy a TOML snippet.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println()
		fmt.Println("  Scanning for available models...")

		config, err := cmdutil.APIGet("/api/config")
		if err != nil {
			return fmt.Errorf("cannot reach API: %w", err)
		}

		providers := extractProviders(config)
		if len(providers) == 0 {
			fmt.Println("  No providers configured. Nothing to suggest.")
			return nil
		}

		type modelEntry struct {
			name     string
			local    bool
			costRate float64
			quality  llm.BaselineQualityInfo
			score    float64 // composite ranking score
		}
		var available []modelEntry
		providerNames := make([]string, 0, len(providers))

		for provName, provURL := range providers {
			providerNames = append(providerNames, provName)
			models := probeProvider(provName, provURL)
			isLocal := false
			costRate := 0.0
			if pc, ok := config["providers"].(map[string]any); ok {
				if p, ok := pc[provName].(map[string]any); ok {
					isLocal, _ = p["is_local"].(bool)
					if c, ok := p["cost_per_output_token"].(float64); ok {
						costRate = c
					}
				}
			}
			for _, m := range models {
				fullName := provName + "/" + m
				qi := llm.LookupBaselineQuality(fullName)
				available = append(available, modelEntry{
					name:     fullName,
					local:    isLocal,
					costRate: costRate,
					quality:  qi,
				})
			}
		}

		if len(available) == 0 {
			fmt.Println("  No models discovered from any provider.")
			return nil
		}

		fmt.Printf("  Found %d models across %s\n\n",
			len(available), strings.Join(providerNames, ", "))

		// Compute composite score: quality baseline + locality bonus - cost penalty.
		maxCost := 0.0
		for _, m := range available {
			if m.costRate > maxCost {
				maxCost = m.costRate
			}
		}
		for i := range available {
			m := &available[i]
			// Base quality: known baseline or unknown penalty.
			if m.quality.Known {
				m.score = m.quality.AvgQuality
			} else {
				m.score = 0.30 // unknown model penalty
			}
			// Locality bonus: local models are free and private.
			if m.local {
				m.score += 0.15
			}
			// Cost penalty: normalize to 0-0.2 range.
			if maxCost > 0 && m.costRate > 0 {
				m.score -= (m.costRate / maxCost) * 0.20
			}
		}

		// Sort by composite score descending.
		sort.Slice(available, func(i, j int) bool {
			if available[i].score != available[j].score {
				return available[i].score > available[j].score
			}
			return available[i].name < available[j].name
		})

		// Recommend top models, ensuring at least one cloud model for resilience.
		maxChain := 6
		if len(available) < maxChain {
			maxChain = len(available)
		}
		chain := available[:maxChain]

		// Check diversity: if all are local, swap the last for the best cloud model.
		allLocal := true
		for _, m := range chain {
			if !m.local {
				allLocal = false
				break
			}
		}
		if allLocal && len(available) > maxChain {
			for _, m := range available[maxChain:] {
				if !m.local {
					chain[maxChain-1] = m
					break
				}
			}
		}

		// Display recommendations.
		fmt.Println("  ━━━ Recommended Fallback Chain ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println()
		for i, m := range chain {
			role := "  FALLBACK"
			if i == 0 {
				role = "  PRIMARY "
			}
			locality := "Cloud"
			if m.local {
				locality = "Local"
			}
			costLabel := "Free"
			if m.costRate > 0 {
				costLabel = fmt.Sprintf("$%.6f/tok", m.costRate)
			}

			qualityLabel := "No baseline"
			bar := "░░░░░░░░░░"
			if m.quality.Known {
				bar = qualityBar(m.quality.AvgQuality)
				qualityLabel = fmt.Sprintf("%.2f", m.quality.AvgQuality)
			}

			fmt.Printf("%s  %s\n", role, m.name)
			fmt.Printf("            %s · %s · Quality: %s %s\n", locality, costLabel, bar, qualityLabel)

			// Rationale.
			if m.quality.Known {
				best := strings.ToLower(m.quality.BestIntent)
				worst := strings.ToLower(m.quality.WorstIntent)
				if best == worst {
					fmt.Printf("            Even performance across intent classes.\n")
				} else {
					bestQ := m.quality.ByIntent[m.quality.BestIntent]
					worstQ := m.quality.ByIntent[m.quality.WorstIntent]
					fmt.Printf("            Strongest at %s (%.0f%%). Weakest at %s (%.0f%%).\n",
						best, bestQ*100, worst, worstQ*100)
				}
			} else {
				fmt.Printf("            No evaluation data. Run 'roboticus models exercise %s' to benchmark.\n", m.name)
			}
			fmt.Println()
		}

		remaining := len(available) - len(chain)
		if remaining > 0 {
			fmt.Printf("  %d more model(s) available but not recommended\n", remaining)
			fmt.Printf("  (lower quality baselines or no evaluation data)\n")
		}
		fmt.Println()
		fmt.Println("  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println()

		// Interactive menu.
		fmt.Println("  [A] Apply to config (replace current primary + fallbacks)")
		fmt.Println("  [M] Merge with config (keep current primary, add as fallbacks)")
		fmt.Println("  [T] Show TOML snippet only")
		fmt.Println("  [N] Cancel")
		fmt.Println()
		fmt.Print("  Choice [A/M/T/N]: ")

		var input string
		if _, err := fmt.Scanln(&input); err != nil && err.Error() != "unexpected newline" {
			input = "n"
		}
		choice := strings.ToLower(strings.TrimSpace(input))

		chainNames := make([]string, len(chain))
		for i, m := range chain {
			chainNames[i] = m.name
		}

		switch choice {
		case "a", "apply":
			body := map[string]any{
				"models": map[string]any{
					"primary":   chainNames[0],
					"fallbacks": chainNames[1:],
				},
			}
			if _, err := cmdutil.APIPut("/api/config", body); err != nil {
				fmt.Printf("  Failed to update config: %v\n", err)
			} else {
				fmt.Printf("  Config updated: primary=%s, %d fallback(s)\n", chainNames[0], len(chainNames)-1)
				fmt.Println("  Restart roboticus to pick up the new model chain.")
			}

		case "m", "merge":
			// Fetch current config to preserve existing primary.
			currentModels, _ := config["models"].(map[string]any)
			currentPrimary, _ := currentModels["primary"].(string)
			currentFallbacks := []string{}
			if fb, ok := currentModels["fallbacks"].([]any); ok {
				for _, f := range fb {
					if s, ok := f.(string); ok {
						currentFallbacks = append(currentFallbacks, s)
					}
				}
			}
			if currentPrimary == "" {
				currentPrimary = chainNames[0]
			}
			// Append new models that aren't already in the chain.
			existing := map[string]bool{currentPrimary: true}
			for _, f := range currentFallbacks {
				existing[f] = true
			}
			merged := append([]string{}, currentFallbacks...)
			for _, name := range chainNames {
				if !existing[name] {
					merged = append(merged, name)
					existing[name] = true
				}
			}
			body := map[string]any{
				"models": map[string]any{
					"primary":   currentPrimary,
					"fallbacks": merged,
				},
			}
			if _, err := cmdutil.APIPut("/api/config", body); err != nil {
				fmt.Printf("  Failed to update config: %v\n", err)
			} else {
				fmt.Printf("  Config merged: primary=%s (kept), %d total fallback(s)\n", currentPrimary, len(merged))
				fmt.Println("  Restart roboticus to pick up the new model chain.")
			}

		case "t", "toml":
			fmt.Println()
			fmt.Println("  [models]")
			fmt.Printf("  primary = %q\n", chainNames[0])
			if len(chainNames) > 1 {
				var fbs []string
				for _, name := range chainNames[1:] {
					fbs = append(fbs, fmt.Sprintf("%q", name))
				}
				fmt.Printf("  fallbacks = [%s]\n", strings.Join(fbs, ", "))
			}

		default:
			fmt.Println("  Cancelled.")
		}
		fmt.Println()
		return nil
	},
}

var modelsResetCmd = &cobra.Command{
	Use:   "reset [model]",
	Short: "Reset quality scores for a model (or all models)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if len(args) > 0 {
			body["model"] = args[0]
		}
		data, err := cmdutil.APIPost("/api/models/reset", body)
		if err != nil {
			return err
		}

		if msg, ok := data["message"].(string); ok {
			fmt.Println(msg)
		} else {
			cleared, _ := data["cleared"].(float64)
			model := "(all)"
			if m, ok := data["model"].(string); ok && m != "" {
				model = m
			}
			fmt.Printf("Cleared %d quality observation(s) for %s\n", int(cleared), model)
		}
		return nil
	},
}

func init() {
	// Consolidated exercise command (v1.0.6). Formerly split into
	// `exercise` and `baseline`; see the command Long description
	// for the merge rationale.
	modelsExerciseCmd.Flags().IntP("iterations", "n", 1, "Number of iterations over the prompt matrix")
	modelsExerciseCmd.Flags().String("intent", "", "Exercise only one canonical intent class from the matrix")
	modelsExerciseCmd.Flags().Float64("min-quality", 0, "Minimum quality threshold (0.0-1.0) — flag models below this for removal")
	modelsExerciseCmd.Flags().Bool("new-only", false, "Only exercise models with no existing baseline data (ignored when explicit args are given)")
	modelsExerciseCmd.Flags().Bool("flush", false, "Flush ALL existing quality observations before exercising (preserves the old `baseline` command's reset-first semantics)")
}

// printLatencyScorecard prints a per-intent-class latency table (Avg/P50/P95).
// Matches the Rust reference's exercise_single_model_iterations() output.
func printLatencyScorecard(latencies map[string][]int64) {
	if len(latencies) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("    ┌──────────────────┬────────┬────────┬────────┐")
	fmt.Println("    │ Intent Class     │  Avg   │  P50   │  P95   │")
	fmt.Println("    ├──────────────────┼────────┼────────┼────────┤")

	var allLatencies []int64
	// Render every intent class the matrix exercises, sourced from
	// llm.AllIntentClasses so a future new class (CODING landed in
	// v1.0.6; more may follow) automatically appears in the latency
	// scorecard without further edits here.
	intentsOrdered := llm.AllIntentClasses()
	intents := make([]string, 0, len(intentsOrdered))
	for _, ic := range intentsOrdered {
		intents = append(intents, ic.String())
	}
	for _, intent := range intents {
		times, ok := latencies[intent]
		if !ok || len(times) == 0 {
			continue
		}
		allLatencies = append(allLatencies, times...)
		sorted := make([]int64, len(times))
		copy(sorted, times)
		sortInt64s(sorted)

		avg := float64(sumInt64s(sorted)) / float64(len(sorted)) / 1000.0
		p50 := float64(sorted[len(sorted)/2]) / 1000.0
		p95idx := int(float64(len(sorted)) * 0.95)
		if p95idx >= len(sorted) {
			p95idx = len(sorted) - 1
		}
		p95 := float64(sorted[p95idx]) / 1000.0

		fmt.Printf("    │ %-16s │ %5.1fs │ %5.1fs │ %5.1fs │\n", intent, avg, p50, p95)
	}

	// All-intents aggregate row.
	if len(allLatencies) > 0 {
		sorted := make([]int64, len(allLatencies))
		copy(sorted, allLatencies)
		sortInt64s(sorted)

		avg := float64(sumInt64s(sorted)) / float64(len(sorted)) / 1000.0
		p50 := float64(sorted[len(sorted)/2]) / 1000.0
		p95idx := int(float64(len(sorted)) * 0.95)
		if p95idx >= len(sorted) {
			p95idx = len(sorted) - 1
		}
		p95 := float64(sorted[p95idx]) / 1000.0

		fmt.Println("    ├──────────────────┼────────┼────────┼────────┤")
		fmt.Printf("    │ ALL              │ %5.1fs │ %5.1fs │ %5.1fs │\n", avg, p50, p95)
	}
	fmt.Println("    └──────────────────┴────────┴────────┴────────┘")
}

func sortInt64s(s []int64) {
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
}

func sumInt64s(s []int64) int64 {
	var total int64
	for _, v := range s {
		total += v
	}
	return total
}

func decodeExerciseConfig(config map[string]any) *core.Config {
	if len(config) == 0 {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg core.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// isCloudModel reports whether the given model's provider is cloud-hosted.
// Cloud models skip warm-up in the baseline harness because "cold start"
// for them is opaque (provider-side model routing, autoscaler spin-up,
// etc.) and measuring it from the client produces noise, not signal.
// The inverse of the local-provider check in resolveModelTimeout.
func isCloudModel(config map[string]any, model string) bool {
	return !llm.ExerciseModelIsLocal(decodeExerciseConfig(config), model)
}

// Warm-up orchestration (runWarmupStage, runWarmupCall) moved to
// internal/llm.RunWarmupStage + internal/llm.WarmupSender in the v1.0.6
// consolidation. CLI calls cliWarmupSender (defined near
// modelsExerciseCmd above) which delegates to the HTTP API.

// toFloat extracts a float64 from an any value (JSON numbers decode as float64).
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

// qualityBar renders a visual quality bar: ████████░░ for 0.8.
func qualityBar(q float64) string {
	const width = 10
	filled := int(q * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// splitModelForDisplay splits "provider/model" into (provider, model).
// Returns ("", input) if no slash present.
func init() {
	modelsCmd.AddCommand(modelsListCmd, modelsDiagnosticsCmd, modelsScanCmd,
		modelsExerciseCmd, modelsSuggestCmd, modelsResetCmd)
}
