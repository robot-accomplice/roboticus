package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
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
			fmt.Scanln(&input)
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
				fmt.Scanln(&input)
				if input == "" || strings.ToLower(input) == "y" || strings.ToLower(input) == "yes" {
					fmt.Println("\n  [models]")
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
	Short: "Exercise a model with test prompts to verify connectivity and quality",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prompts := []string{
			"Respond with exactly: OK",
			"What is 2 + 2?",
			"Say hello in one word.",
		}

		model := ""
		if len(args) > 0 {
			model = args[0]
		}

		fmt.Println()
		pass, fail := 0, 0
		for _, prompt := range prompts {
			body := map[string]any{"content": prompt}
			if model != "" {
				body["model"] = model
			}
			resp, err := apiPost("/api/agent/message", body)
			if err != nil {
				fail++
				fmt.Printf("  FAIL: %v\n", err)
				continue
			}
			content := fmt.Sprintf("%v", resp["content"])
			if content != "" && content != "<nil>" {
				pass++
				if len(content) > 60 {
					content = content[:60] + "..."
				}
				fmt.Printf("  PASS: %s\n", content)
			} else if errMsg, ok := resp["error"].(string); ok {
				fail++
				fmt.Printf("  FAIL: %s\n", errMsg)
			} else {
				fail++
				fmt.Printf("  FAIL: empty response\n")
			}
		}

		fmt.Printf("\n  Result: %d/%d passed\n\n", pass, pass+fail)
		return nil
	},
}

var modelsSuggestCmd = &cobra.Command{
	Use:   "suggest",
	Short: "Scan providers and suggest an optimal fallback chain configuration",
	Long: `Suggest probes all configured providers for available models, ranks them
by locality (local first) and cost (cheapest first), then outputs a
suggested primary + fallback chain with ready-to-paste TOML config.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("\n  Scanning for available models...\n")

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
			name    string
			local   bool
			cost    float64
		}
		var available []modelEntry

		for provName, provURL := range providers {
			models := probeProvider(provName, provURL)
			isLocal := false
			if pc, ok := config["providers"].(map[string]any); ok {
				if p, ok := pc[provName].(map[string]any); ok {
					isLocal, _ = p["is_local"].(bool)
				}
			}
			for _, m := range models {
				available = append(available, modelEntry{
					name:  provName + "/" + m,
					local: isLocal,
				})
			}
		}

		if len(available) == 0 {
			fmt.Println("  No models discovered from any provider.")
			return nil
		}

		// Rank: local first, then by name.
		sort.Slice(available, func(i, j int) bool {
			if available[i].local != available[j].local {
				return available[i].local
			}
			return available[i].name < available[j].name
		})

		// Take top 6 for the suggested chain.
		chain := available
		if len(chain) > 6 {
			chain = chain[:6]
		}

		fmt.Println("  Suggested fallback chain:\n")
		for i, m := range chain {
			role := fmt.Sprintf("fallback%d", i)
			if i == 0 {
				role = "primary  "
			}
			locality := "cloud"
			if m.local {
				locality = "local"
			}
			fmt.Printf("  %-10s %s  (%s)\n", role, m.name, locality)
		}

		// Print TOML snippet.
		fmt.Println("\n  TOML:\n")
		if len(chain) > 0 {
			fmt.Printf("  [models]\n")
			fmt.Printf("  primary = %q\n", chain[0].name)
			if len(chain) > 1 {
				var fbs []string
				for _, m := range chain[1:] {
					fbs = append(fbs, fmt.Sprintf("%q", m.name))
				}
				fmt.Printf("  fallbacks = [%s]\n", strings.Join(fbs, ", "))
			}
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
exercises each model across multiple test prompts, and reports pass/fail results.
This re-establishes the metascore quality baseline from scratch.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
			if fbs, ok := models["fallback"].([]any); ok {
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

		fmt.Printf("\n  Found %d configured model(s):\n\n", len(configured))
		for i, model := range configured {
			role := "fallback"
			if i == 0 {
				role = "primary"
			}
			fmt.Printf("    %-10s %s\n", role, model)
		}

		// Step 2: Confirm.
		fmt.Printf("\n  This will flush all quality scores and exercise each model.\n  Proceed? [Y/n] ")
		var input string
		fmt.Scanln(&input)
		if input != "" && input != "y" && input != "Y" && input != "yes" {
			fmt.Println("  Cancelled.")
			return nil
		}

		// Step 3: Flush all scores.
		fmt.Println("\n  Step 2: Flushing all quality scores...")
		resetData, err := apiPost("/api/models/reset", nil)
		if err != nil {
			return fmt.Errorf("failed to reset scores: %w", err)
		}
		cleared, _ := resetData["cleared"].(float64)
		fmt.Printf("  Cleared %.0f observation entries.\n", cleared)

		// Step 4: Exercise each model.
		fmt.Println("\n  Step 3: Exercising models...\n")
		type result struct {
			model string
			pass  int
			fail  int
		}
		var results []result
		prompts := []string{
			"Respond with exactly: OK",
			"What is 2 + 2?",
			"Summarize this in one word: The quick brown fox jumps over the lazy dog.",
		}

		for _, model := range configured {
			fmt.Printf("  --- %s ---\n", model)
			pass, fail := 0, 0
			for _, prompt := range prompts {
				resp, err := apiPost("/api/agent/message", map[string]any{
					"content": prompt,
					"model":   model,
				})
				if err != nil {
					fail++
					fmt.Printf("    FAIL: %v\n", err)
				} else if resp["content"] != nil && resp["content"] != "" {
					pass++
					// Truncate response for display.
					content := fmt.Sprintf("%v", resp["content"])
					if len(content) > 60 {
						content = content[:60] + "..."
					}
					fmt.Printf("    PASS: %s\n", content)
				} else if errMsg, ok := resp["error"].(string); ok {
					fail++
					fmt.Printf("    FAIL: %s\n", errMsg)
				} else {
					fail++
					fmt.Printf("    FAIL: empty response\n")
				}
			}
			results = append(results, result{model, pass, fail})
			fmt.Println()
		}

		// Step 5: Summary.
		fmt.Println("  Baseline Results:\n")
		fmt.Printf("  %-35s %-6s %-6s %s\n", "MODEL", "PASS", "FAIL", "STATUS")
		fmt.Println("  " + "─────────────────────────────────── ────── ────── ──────")
		for _, r := range results {
			status := "PASS"
			if r.fail > 0 && r.pass == 0 {
				status = "FAIL"
			} else if r.fail > 0 {
				status = "DEGRADED"
			}
			fmt.Printf("  %-35s %-6d %-6d %s\n", r.model, r.pass, r.fail, status)
		}
		fmt.Println()
		return nil
	},
}

func init() {
	modelsCmd.AddCommand(modelsListCmd, modelsDiagnosticsCmd, modelsScanCmd,
		modelsExerciseCmd, modelsSuggestCmd, modelsResetCmd, modelsBaselineCmd)
	rootCmd.AddCommand(modelsCmd)
}
