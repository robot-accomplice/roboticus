package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var parityAuditCmd = &cobra.Command{
	Use:   "parity-audit",
	Short: "Compare goboticus against roboticus for feature parity gaps",
	RunE:  runParityAudit,
}

func init() {
	parityAuditCmd.Flags().String("roboticus-dir", "../roboticus", "Path to roboticus source")
	parityAuditCmd.Flags().String("goboticus-dir", ".", "Path to goboticus source")
	parityAuditCmd.Flags().String("output", "", "Output file (default: stdout)")
	rootCmd.AddCommand(parityAuditCmd)
}

// subsystem maps a roboticus crate/module to its goboticus equivalent.
type subsystem struct {
	Name         string
	RustPaths    []string // glob patterns relative to roboticus root
	GoPaths      []string // glob patterns relative to goboticus root
	KeyFunctions []string // Rust function names that must have Go equivalents
}

var subsystems = []subsystem{
	{
		Name:         "Pipeline",
		RustPaths:    []string{"crates/roboticus-api/src/pipeline*"},
		GoPaths:      []string{"internal/pipeline/*.go"},
		KeyFunctions: []string{"run_pipeline", "decomposition", "shortcut", "skill_first", "nickname"},
	},
	{
		Name:         "Memory",
		RustPaths:    []string{"crates/roboticus-agent/src/retrieval*", "crates/roboticus-agent/src/memory*"},
		GoPaths:      []string{"internal/agent/retrieval.go", "internal/agent/memory.go"},
		KeyFunctions: []string{"retrieve", "ingest_turn", "hybrid_search", "embed"},
	},
	{
		Name:         "LLM/Providers",
		RustPaths:    []string{"crates/roboticus-llm/src/*.rs"},
		GoPaths:      []string{"internal/llm/*.go"},
		KeyFunctions: []string{"complete", "stream", "cascade", "tiered", "embedding", "x402"},
	},
	{
		Name:         "Tools",
		RustPaths:    []string{"crates/roboticus-agent/src/tools/*.rs"},
		GoPaths:      []string{"internal/agent/tools/*.go"},
		KeyFunctions: []string{"web_search", "http_fetch", "read_file", "bash", "introspect", "mcp"},
	},
	{
		Name:         "Channels",
		RustPaths:    []string{"crates/roboticus-channels/src/*.rs"},
		GoPaths:      []string{"internal/channel/*.go"},
		KeyFunctions: []string{"telegram", "discord", "signal", "whatsapp", "voice", "email", "a2a"},
	},
	{
		Name:         "Scheduler",
		RustPaths:    []string{"crates/roboticus-cron/src/*.rs"},
		GoPaths:      []string{"internal/cron/*.go"},
		KeyFunctions: []string{"cron_worker", "scheduler", "lease"},
	},
	{
		Name:         "Wallet",
		RustPaths:    []string{"crates/roboticus-wallet/src/*.rs"},
		GoPaths:      []string{"internal/wallet/*.go"},
		KeyFunctions: []string{"rpc_call", "get_balance", "x402", "eip3009", "transfer"},
	},
	{
		Name:         "Config",
		RustPaths:    []string{"crates/roboticus-core/src/config/*.rs", "crates/roboticus-core/src/config.rs"},
		GoPaths:      []string{"internal/core/config.go", "internal/core/bundled_providers.toml"},
		KeyFunctions: []string{"validate", "normalize_paths", "merge_bundled", "provider_config"},
	},
	{
		Name:         "Guards",
		RustPaths:    []string{"crates/roboticus-agent/src/guard*"},
		GoPaths:      []string{"internal/pipeline/guards.go"},
		KeyFunctions: []string{"guard_chain", "empty_response", "system_prompt_leak", "content_classification"},
	},
	{
		Name:         "Context",
		RustPaths:    []string{"crates/roboticus-agent/src/context*"},
		GoPaths:      []string{"internal/agent/context.go"},
		KeyFunctions: []string{"context_builder", "compaction", "semantic_compress", "anti_fade"},
	},
	{
		Name:         "Skills/Plugins",
		RustPaths:    []string{"crates/roboticus-agent/src/skill*", "crates/roboticus-agent/src/plugin*"},
		GoPaths:      []string{"internal/agent/skills.go", "internal/agent/skill_watcher.go", "internal/plugin/*.go"},
		KeyFunctions: []string{"skill_loader", "skill_watcher", "plugin_registry"},
	},
	{
		Name:         "Dashboard",
		RustPaths:    []string{"crates/roboticus-api/src/dashboard*"},
		GoPaths:      []string{"internal/api/dashboard*.go", "internal/api/dashboard_spa.html"},
		KeyFunctions: []string{"dashboard_handler", "csp_nonce"},
	},
}

func runParityAudit(cmd *cobra.Command, args []string) error {
	rustDir, _ := cmd.Flags().GetString("roboticus-dir")
	goDir, _ := cmd.Flags().GetString("goboticus-dir")
	output, _ := cmd.Flags().GetString("output")

	var report strings.Builder
	report.WriteString("# Feature Parity Audit Report\n\n")
	report.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))
	report.WriteString("| Subsystem | Rust Files | Go Files | Coverage | Status |\n")
	report.WriteString("|-----------|-----------|---------|----------|--------|\n")

	var gaps []string
	var totalRust, totalGo int

	for _, sub := range subsystems {
		rustFiles := countGlobFiles(rustDir, sub.RustPaths)
		goFiles := countGlobFiles(goDir, sub.GoPaths)
		totalRust += rustFiles
		totalGo += goFiles

		// Check for key function presence in Go files.
		missing := findMissingFunctions(goDir, sub.GoPaths, sub.KeyFunctions)
		coverage := "full"
		status := "OK"

		if len(missing) > 0 {
			coverage = fmt.Sprintf("%d/%d", len(sub.KeyFunctions)-len(missing), len(sub.KeyFunctions))
			status = "GAP"
			for _, fn := range missing {
				gaps = append(gaps, fmt.Sprintf("- **%s**: missing `%s`", sub.Name, fn))
			}
		}

		if rustFiles > 0 && goFiles == 0 {
			coverage = "none"
			status = "MISSING"
		}

		report.WriteString(fmt.Sprintf("| %s | %d | %d | %s | %s |\n",
			sub.Name, rustFiles, goFiles, coverage, status))
	}

	report.WriteString(fmt.Sprintf("\n**Totals:** %d Rust files, %d Go files\n\n", totalRust, totalGo))

	if len(gaps) > 0 {
		report.WriteString("## Gaps Found\n\n")
		for _, g := range gaps {
			report.WriteString(g + "\n")
		}
		report.WriteString("\n")
	} else {
		report.WriteString("## No gaps detected.\n\n")
	}

	// Check for new Rust files not covered by any subsystem.
	newFiles := findNewRustFiles(rustDir)
	if len(newFiles) > 0 {
		report.WriteString("## New Rust Files (may need Go equivalents)\n\n")
		for _, f := range newFiles {
			report.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		report.WriteString("\n")
	}

	// API endpoint parity check.
	rustEndpoints := extractEndpoints(rustDir, "crates/roboticus-api/src")
	goEndpoints := extractEndpoints(goDir, "internal/api")
	if len(rustEndpoints) > 0 {
		missingEndpoints := diffStrings(rustEndpoints, goEndpoints)
		if len(missingEndpoints) > 0 {
			report.WriteString("## API Endpoints in Rust but not Go\n\n")
			for _, ep := range missingEndpoints {
				report.WriteString(fmt.Sprintf("- `%s`\n", ep))
			}
			report.WriteString("\n")
		}
	}

	text := report.String()
	if output != "" {
		return os.WriteFile(output, []byte(text), 0o644)
	}
	fmt.Print(text)
	return nil
}

func countGlobFiles(root string, patterns []string) int {
	seen := make(map[string]bool)
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, m := range matches {
			seen[m] = true
		}
	}
	return len(seen)
}

func findMissingFunctions(root string, patterns []string, funcs []string) []string {
	// Read all Go files matching the patterns and search for function names.
	var allContent strings.Builder
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err == nil {
				allContent.Write(data)
				allContent.WriteByte('\n')
			}
		}
	}

	content := strings.ToLower(allContent.String())
	var missing []string
	for _, fn := range funcs {
		// Check for the function name in various forms (camelCase, snake_case, etc).
		variants := []string{
			strings.ToLower(fn),
			strings.ToLower(strings.ReplaceAll(fn, "_", "")),
		}
		found := false
		for _, v := range variants {
			if strings.Contains(content, v) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fn)
		}
	}
	return missing
}

func findNewRustFiles(rustDir string) []string {
	var newFiles []string
	filepath.WalkDir(filepath.Join(rustDir, "crates"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".rs" {
			return nil
		}
		rel, _ := filepath.Rel(rustDir, path)
		newFiles = append(newFiles, rel)
		return nil
	})
	sort.Strings(newFiles)
	return newFiles
}

var routePattern = regexp.MustCompile(`\.(get|post|put|delete|patch)\s*\(\s*"([^"]+)"`)

func extractEndpoints(root, subdir string) []string {
	var endpoints []string
	seen := make(map[string]bool)

	dir := filepath.Join(root, subdir)
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		matches := routePattern.FindAllStringSubmatch(string(data), -1)
		for _, m := range matches {
			ep := strings.ToUpper(m[1]) + " " + m[2]
			if !seen[ep] {
				seen[ep] = true
				endpoints = append(endpoints, ep)
			}
		}
		return nil
	})
	sort.Strings(endpoints)
	return endpoints
}

func diffStrings(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, s := range b {
		bSet[s] = true
	}
	var diff []string
	for _, s := range a {
		if !bSet[s] {
			diff = append(diff, s)
		}
	}
	return diff
}
