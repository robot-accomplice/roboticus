package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"roboticus/internal/llm"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Query available models and routing diagnostics",
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured models and routing strategy",
	RunE: func(cmd *cobra.Command, args []string) error {
		config, configErr := apiGet("/api/config")
		available, availErr := apiGet("/api/models/available")

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
		data, err := apiGet("/api/models/routing-diagnostics")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var modelsScanCmd = &cobra.Command{
	Use:   "scan [provider]",
	Short: "Scan providers for available models",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Try to get provider URLs from config for direct probing.
		config, configErr := apiGet("/api/config")
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
					if _, err := apiPut("/api/config", body); err != nil {
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
		data, err := apiGet(path)
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

var modelsExerciseCmd = &cobra.Command{
	Use:   "exercise [model]",
	Short: "Exercise a model with the 25-prompt matrix to establish quality baseline",
	Long: `Exercise runs the model through the full prompt matrix across 6 intent classes
(Execution, Delegation, Introspection, Conversation, Memory Recall).

Use -n to run multiple iterations for statistical confidence.
Use --min-quality to set a pass/fail threshold — models below the threshold
are flagged for removal from the routing chain.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		model := ""
		if len(args) > 0 {
			model = args[0]
		}

		iterations, _ := cmd.Flags().GetInt("iterations")
		if iterations < 1 {
			iterations = 1
		}
		minQuality, _ := cmd.Flags().GetFloat64("min-quality")

		// Fetch config for per-model timeout resolution.
		exerciseCfg, _ := apiGet("/api/config")
		exerciseTimeout := resolveModelTimeout(exerciseCfg, model)

		totalPrompts := len(llm.ExerciseMatrix) * iterations
		fmt.Println()
		fmt.Printf("  Exercising %s with %d prompts x %d iteration(s) = %d calls across 6 intent classes (timeout: %s)...\n",
			func() string {
				if model != "" {
					return model
				}
				return "default model"
			}(),
			len(llm.ExerciseMatrix),
			iterations,
			totalPrompts,
			exerciseTimeout)
		if minQuality > 0 {
			fmt.Printf("  Minimum quality threshold: %.0f%% — models below this will be flagged.\n", minQuality*100)
		}
		fmt.Println()

		// Per-intent-class tracking.
		type intentStats struct {
			pass, fail int
			totalMs    int64
		}
		byIntent := make(map[llm.IntentClass]*intentStats)
		for _, ic := range llm.AllIntentClasses() {
			byIntent[ic] = &intentStats{}
		}

		pass, fail := 0, 0
		for iter := 0; iter < iterations; iter++ {
			if iterations > 1 {
				fmt.Printf("  ── Iteration %d/%d ──────────────────────────\n\n", iter+1, iterations)
			}
			for i, ep := range llm.ExerciseMatrix {
				body := map[string]any{"content": ep.Prompt}
				if model != "" {
					body["model"] = model
				}
				start := time.Now()
				resp, err := apiPostSlow("/api/agent/message", body, exerciseTimeout)
				latencyMs := time.Since(start).Milliseconds()

				promptNum := iter*len(llm.ExerciseMatrix) + i + 1
				prefix := fmt.Sprintf("  [%2d/%d] %-15s C%d", promptNum, totalPrompts, ep.Intent, ep.Complexity)

				stats := byIntent[ep.Intent]
				if err != nil {
					fail++
					stats.fail++
					fmt.Printf("%s FAIL: %v\n", prefix, err)
					continue
				}
				content := fmt.Sprintf("%v", resp["content"])
				if content != "" && content != "<nil>" {
					pass++
					stats.pass++
					stats.totalMs += latencyMs
					if len(content) > 50 {
						content = content[:50] + "..."
					}
					fmt.Printf("%s PASS %4dms: %s\n", prefix, latencyMs, content)
				} else {
					fail++
					stats.fail++
					fmt.Printf("%s FAIL: empty response\n", prefix)
				}
			}
			if iterations > 1 {
				fmt.Println()
			}
		}

		fmt.Printf("\n  ── Results ──────────────────────────────────\n")
		fmt.Printf("  Total: %d/%d passed\n\n", pass, pass+fail)
		fmt.Printf("  %-15s  %s  %s  %s\n", "Intent Class", "Pass", "Fail", "Avg Latency")
		fmt.Printf("  %-15s  %s  %s  %s\n", "───────────────", "────", "────", "───────────")
		for _, ic := range llm.AllIntentClasses() {
			s := byIntent[ic]
			avgMs := int64(0)
			if s.pass > 0 {
				avgMs = s.totalMs / int64(s.pass)
			}
			fmt.Printf("  %-15s  %4d  %4d  %6dms\n", ic, s.pass, s.fail, avgMs)
		}

		total := pass + fail
		qualityPct := 0.0
		if total > 0 {
			qualityPct = float64(pass) / float64(total)
		}
		fmt.Printf("\n  Quality score: %.0f%% (%d/%d)\n", qualityPct*100, pass, total)

		if minQuality > 0 && qualityPct < minQuality {
			modelLabel := model
			if modelLabel == "" {
				modelLabel = "default model"
			}
			fmt.Printf("\n  ⚠  %s scored %.0f%% — below the %.0f%% minimum quality threshold.\n", modelLabel, qualityPct*100, minQuality*100)
			fmt.Printf("     Consider removing this model from the routing chain.\n")
		}

		fmt.Println()
		return nil
	},
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

		config, err := apiGet("/api/config")
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
			if _, err := apiPut("/api/config", body); err != nil {
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
			if _, err := apiPut("/api/config", body); err != nil {
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
		data, err := apiPost("/api/models/reset", body)
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

var modelsBaselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Flush quality scores and re-exercise all configured models",
	Long: `Baseline discovers configured models, flushes all quality observations,
exercises each model with the full 20-prompt matrix across multiple iterations,
and reports per-model, per-intent-class latency scores (Avg/P50/P95).
This re-establishes the metascore quality baseline from scratch.

Matches the Rust reference: 20 prompts x N iterations per model, with a
per-intent-class latency scorecard and 6-axis metascore dimension reporting.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		iterations := 1
		if v, _ := cmd.Flags().GetInt("iterations"); v > 0 {
			iterations = v
		}
		newOnly, _ := cmd.Flags().GetBool("new-only")

		// Step 1: Discover configured models.
		fmt.Println("\n  Step 1: Discovering configured models...")
		config, err := apiGet("/api/config")
		if err != nil {
			return fmt.Errorf("cannot reach API: %w", err)
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

		if len(configured) == 0 {
			fmt.Println("  No models configured. Nothing to baseline.")
			return nil
		}

		// In --new-only mode, filter out models that already have exercise data.
		// Match by both full name (provider/model) and bare model name since
		// the exercise status API may store names with or without the provider prefix.
		if newOnly {
			status, err := apiGet("/api/models/exercise/status")
			if err == nil {
				if existing, ok := status["models"].(map[string]any); ok && len(existing) > 0 {
					var filtered []string
					for _, model := range configured {
						found := false
						// Try exact match first.
						if count, ok := existing[model]; ok {
							if toFloat(count) > 0 {
								fmt.Printf("  Skipping %s (already has %.0f exercise result(s))\n", model, toFloat(count))
								found = true
							}
						}
						// Try bare model name (strip provider/).
						if !found {
							bare := model
							if idx := strings.Index(model, "/"); idx >= 0 {
								bare = model[idx+1:]
							}
							for k, v := range existing {
								kBare := k
								if idx := strings.Index(k, "/"); idx >= 0 {
									kBare = k[idx+1:]
								}
								if kBare == bare && toFloat(v) > 0 {
									fmt.Printf("  Skipping %s (already has %.0f exercise result(s) as %s)\n", model, toFloat(v), k)
									found = true
									break
								}
							}
						}
						if !found {
							filtered = append(filtered, model)
						}
					}
					configured = filtered
				}
			}
			if len(configured) == 0 {
				fmt.Println("\n  All models already have exercise data. Nothing to do.")
				fmt.Println("  Run without --new-only to re-baseline all models.")
				return nil
			}
		}

		totalPrompts := len(llm.ExerciseMatrix) * iterations
		fmt.Printf("\n  Found %d model(s) to exercise:\n\n", len(configured))
		var localCount, cloudCount int
		for i, model := range configured {
			role := "fallback"
			if i == 0 {
				role = "primary"
			}
			timeout := resolveModelTimeout(config, model)
			locality := "cloud"
			if timeout > 120*time.Second {
				locality = "local"
				localCount++
			} else {
				cloudCount++
			}
			fmt.Printf("    %-10s %-40s  %s\n", role, model, locality)
		}

		// Estimate total duration. Local models average ~30-60s per prompt,
		// cloud models ~2-5s. Use conservative midpoints.
		localEstSec := localCount * totalPrompts * 45  // 45s avg per local prompt
		cloudEstSec := cloudCount * totalPrompts * 4   // 4s avg per cloud prompt
		totalEstMin := (localEstSec + cloudEstSec) / 60
		if totalEstMin < 1 {
			totalEstMin = 1
		}

		// Step 2: Confirm with duration warning.
		if newOnly {
			fmt.Printf("\n  This will exercise %d new model(s) without flushing existing scores.\n", len(configured))
		} else {
			fmt.Printf("\n  This will flush all quality scores and re-exercise each model.\n")
		}
		fmt.Printf("  %d prompts x %d iteration(s) = %d calls per model.\n\n", len(llm.ExerciseMatrix), iterations, totalPrompts)
		fmt.Printf("  ⏱  Estimated duration: ~%d minutes (%d local model(s) @ ~45s/prompt, %d cloud @ ~4s/prompt)\n", totalEstMin, localCount, cloudCount)
		if localCount > 0 {
			fmt.Printf("     Local models are significantly slower — especially on first run (cold start).\n")
			fmt.Printf("     Do not interrupt the process or the baseline data will be incomplete.\n")
		}
		fmt.Printf("\n  Proceed? [Y/n] ")
		var input string
		if _, err := fmt.Scanln(&input); err != nil && err.Error() != "unexpected newline" {
			fmt.Fprintf(os.Stderr, "  (stdin read error: %v)\n", err)
		}
		if input != "" && input != "y" && input != "Y" && input != "yes" {
			fmt.Println("  Cancelled.")
			return nil
		}

		// Step 3: Flush all scores (skip in --new-only mode — we're adding, not replacing).
		if newOnly {
			fmt.Println("\n  Step 2: Skipping score flush (--new-only mode)")
		} else {
			fmt.Println("\n  Step 2: Flushing all quality scores...")
			resetData, err := apiPost("/api/models/reset", nil)
			if err != nil {
				return fmt.Errorf("failed to reset scores: %w", err)
			}
			cleared, _ := resetData["cleared"].(float64)
			fmt.Printf("  Cleared %.0f observation entries.\n", cleared)
		}

		// Step 4: Exercise each model via /api/models/exercise (direct LLM quality scoring,
		// no pipeline overhead). Returns per-prompt quality scores 0-1.
		fmt.Printf("\n  Step 3: Exercising models...\n\n")

		type modelResult struct {
			model         string
			pass          int
			fail          int
			totalMs       int64
			avgQuality    float64
			intentQuality map[string]float64 // intent_class → avg quality 0-1
			latencies     map[string][]int64  // intent_class → latencies in ms
		}
		var results []modelResult

		for _, model := range configured {
			modelTimeout := resolveModelTimeout(config, model)
			fmt.Printf("  --- %s (timeout: %s) ---\n", model, modelTimeout)

			// Exercise per-prompt through the pipeline for immediate output.
			// Previous approach used bulk /api/models/exercise which blocked for
			// the entire model exercise with no progress output.
			mr := modelResult{
				model:         model,
				intentQuality: make(map[string]float64),
				latencies:     make(map[string][]int64),
			}

			intentSums := make(map[string]float64)
			intentCounts := make(map[string]int)
			var qualitySum float64
			var qualityCount int

			for i, ep := range llm.ExerciseMatrix {
				body := map[string]any{"content": ep.Prompt, "model": model}
				start := time.Now()
				resp, err := apiPostSlow("/api/agent/message", body, modelTimeout)
				latencyMs := time.Since(start).Milliseconds()
				intent := ep.Intent.String()
				label := fmt.Sprintf("[%d/%d] %s:C%d", i+1, len(llm.ExerciseMatrix), intent, ep.Complexity)
				mr.latencies[intent] = append(mr.latencies[intent], latencyMs)

				if err != nil {
					mr.fail++
					fmt.Printf("    %s FAIL  %v\n", label, err)
					continue
				}
				content := fmt.Sprintf("%v", resp["content"])
				if content != "" && content != "<nil>" {
					mr.pass++
					// Score the response quality using the same scoring function
					// as the server-side exercise endpoint.
					quality := llm.ScoreExerciseResponse(ep, content)
					qualitySum += quality
					qualityCount++
					intentSums[intent] += quality
					intentCounts[intent]++
					preview := content
					if len(preview) > 50 {
						preview = preview[:50] + "..."
					}
					fmt.Printf("    %s PASS  Q=%.2f  %.1fs: %s\n", label, quality, float64(latencyMs)/1000.0, preview)
				} else {
					mr.fail++
					fmt.Printf("    %s FAIL  empty response  %.1fs\n", label, float64(latencyMs)/1000.0)
				}
			}

			// Compute averages.
			if qualityCount > 0 {
				mr.avgQuality = qualitySum / float64(qualityCount)
			}
			for intent, sum := range intentSums {
				if intentCounts[intent] > 0 {
					mr.intentQuality[intent] = sum / float64(intentCounts[intent])
				}
			}

			// Print per-intent quality + latency scorecard.
			fmt.Printf("\n    Intent Quality:\n")
			for _, intent := range []string{"EXECUTION", "DELEGATION", "INTROSPECTION", "CONVERSATION", "MEMORY_RECALL", "TOOL_USE"} {
				q := mr.intentQuality[intent]
				bar := qualityBar(q)
				fmt.Printf("      %-16s %s %.2f\n", intent, bar, q)
			}
			printLatencyScorecard(mr.latencies)
			results = append(results, mr)
			fmt.Println()
		}

		// Step 5: Summary with per-model quality scores + baseline profile.
		fmt.Printf("  Baseline Results:\n\n")
		fmt.Printf("  %-35s  %-6s  %-6s  %-10s  %s\n", "MODEL", "PASS", "FAIL", "QUALITY", "STATUS")
		fmt.Println("  " + strings.Repeat("─", 75))
		for _, r := range results {
			status := "PASS"
			if r.fail > 0 && r.pass == 0 {
				status = "FAIL"
			} else if r.fail > 0 {
				status = "DEGRADED"
			}
			qBar := qualityBar(r.avgQuality)
			fmt.Printf("  %-35s  %-6d  %-6d  %s %.2f  %s\n", r.model, r.pass, r.fail, qBar, r.avgQuality, status)

			// Per-intent quality breakdown.
			if len(r.intentQuality) > 0 {
				for _, intent := range []string{"EXECUTION", "DELEGATION", "INTROSPECTION", "CONVERSATION", "MEMORY_RECALL", "TOOL_USE"} {
					q := r.intentQuality[intent]
					fmt.Printf("    %-18s %s %.2f\n", intent, qualityBar(q), q)
				}
			}

			// Print 6-axis baseline profile if we have a known baseline.
			if baseline, ok := llm.LookupBaseline(r.model); ok {
				fmt.Printf("    Baseline profile: Eff=%.2f Cost=%.2f Avail=%.2f Loc=%.1f Conf=%.1f Spd=%.2f\n",
					baseline.Efficacy, baseline.Cost, baseline.Availability,
					baseline.Locality, baseline.Confidence, baseline.Speed)
			}
		}
		// Cross-model comparison: rank all models by overall quality.
		if len(results) > 1 {
			// Sort by avgQuality descending.
			sorted := make([]modelResult, len(results))
			copy(sorted, results)
			for i := 0; i < len(sorted)-1; i++ {
				for j := i + 1; j < len(sorted); j++ {
					if sorted[j].avgQuality > sorted[i].avgQuality {
						sorted[i], sorted[j] = sorted[j], sorted[i]
					}
				}
			}

			fmt.Printf("\n  Model Comparison (ranked by quality):\n\n")
			fmt.Printf("  %-4s  %-30s  %-15s  %5s  %5s  %5s  %5s  %5s  %5s  %s\n",
				"RANK", "MODEL", "QUALITY", "EXEC", "DELEG", "INTRO", "CONV", "MEMRC", "TOOLS", "AVG MS")
			fmt.Println("  " + strings.Repeat("─", 105))
			for rank, r := range sorted {
				exec := r.intentQuality["EXECUTION"]
				deleg := r.intentQuality["DELEGATION"]
				intro := r.intentQuality["INTROSPECTION"]
				conv := r.intentQuality["CONVERSATION"]
				memrc := r.intentQuality["MEMORY_RECALL"]
				tools := r.intentQuality["TOOL_USE"]
				avgMs := int64(0)
				var totalLat int64
				var latCount int
				for _, lats := range r.latencies {
					for _, l := range lats {
						totalLat += l
						latCount++
					}
				}
				if latCount > 0 {
					avgMs = totalLat / int64(latCount)
				}
				modelLabel := r.model
				if len(modelLabel) > 30 {
					modelLabel = modelLabel[:27] + "..."
				}
				fmt.Printf("  #%-3d  %-30s  %s %3.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %4.0f%%  %5dms\n",
					rank+1, modelLabel, qualityBar(r.avgQuality), r.avgQuality*100,
					exec*100, deleg*100, intro*100, conv*100, memrc*100, tools*100, avgMs)
			}
			fmt.Println()

			// Highlight best/worst per intent class.
			intents := []string{"EXECUTION", "DELEGATION", "INTROSPECTION", "CONVERSATION", "MEMORY_RECALL", "TOOL_USE"}
			fmt.Printf("  Best per intent class:\n")
			for _, intent := range intents {
				bestModel := ""
				bestQ := 0.0
				for _, r := range results {
					if q, ok := r.intentQuality[intent]; ok && q > bestQ {
						bestQ = q
						bestModel = r.model
					}
				}
				if bestModel != "" {
					fmt.Printf("    %-18s  %s (%.0f%%)\n", intent, bestModel, bestQ*100)
				}
			}
			fmt.Println()

			// Flag underperformers — models below 50% overall quality.
			var underperformers []string
			for _, r := range sorted {
				if r.avgQuality < 0.5 && r.avgQuality > 0 {
					underperformers = append(underperformers, fmt.Sprintf("%s (%.0f%%)", r.model, r.avgQuality*100))
				}
			}
			if len(underperformers) > 0 {
				fmt.Printf("  Underperforming models (below 50%%):\n")
				for _, u := range underperformers {
					fmt.Printf("    - %s\n", u)
				}
				fmt.Printf("\n  Consider removing these from the routing chain with:\n")
				fmt.Printf("    roboticus models remove <model>\n\n")
			}
		}

		fmt.Println()
		return nil
	},
}

func init() {
	modelsExerciseCmd.Flags().IntP("iterations", "n", 1, "Number of iterations over the prompt matrix (default: 1 quick mode)")
	modelsExerciseCmd.Flags().Float64("min-quality", 0, "Minimum quality threshold (0.0-1.0) — flag models below this for removal")

	modelsBaselineCmd.Flags().IntP("iterations", "n", 1, "Number of iterations over the 20-prompt matrix per model")
	modelsBaselineCmd.Flags().Bool("new-only", false, "Only exercise models with no existing baseline data")
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
	intents := []string{llm.IntentExecution.String(), llm.IntentDelegation.String(), llm.IntentIntrospection.String(), llm.IntentConversation.String()}
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

// resolveModelTimeout determines the HTTP client timeout for a model based on config.
// Priority: model_overrides[model].timeout_seconds > is_local (300s) > cloud default (120s).
func resolveModelTimeout(config map[string]any, model string) time.Duration {
	// Check explicit per-model timeout override.
	if models, ok := config["models"].(map[string]any); ok {
		if overrides, ok := models["model_overrides"].(map[string]any); ok {
			if mo, ok := overrides[model].(map[string]any); ok {
				if ts, ok := mo["timeout_seconds"].(float64); ok && ts > 0 {
					return time.Duration(ts) * time.Second
				}
			}
		}
	}

	// Determine if the model's provider is local.
	providerName, _ := splitModelForDisplay(model)
	if providerName != "" {
		if providers, ok := config["providers"].(map[string]any); ok {
			if prov, ok := providers[providerName].(map[string]any); ok {
				if isLocal, ok := prov["is_local"].(bool); ok && isLocal {
					return 300 * time.Second // 5 min for local models (cold start, limited hardware)
				}
			}
		}
	}

	return 120 * time.Second // 2 min for cloud models
}

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
func splitModelForDisplay(spec string) (string, string) {
	if i := strings.Index(spec, "/"); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return "", spec
}

func init() {
	modelsCmd.AddCommand(modelsListCmd, modelsDiagnosticsCmd, modelsScanCmd,
		modelsExerciseCmd, modelsSuggestCmd, modelsResetCmd, modelsBaselineCmd)
	rootCmd.AddCommand(modelsCmd)
}
