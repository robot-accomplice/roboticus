package api

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func routeBase(path string) string {
	return filepath.Base(path)
}

func routeContains(t *testing.T, dir string, pattern string) []string {
	t.Helper()
	var matches []string
	for _, path := range walkGoFiles(t, dir) {
		src := readRepoFile(t, path)
		if strings.Contains(src, pattern) {
			matches = append(matches, path)
		}
	}
	return matches
}

// TestFitness_Routes_DoNotWriteFiles enforces the thin-connector rule for
// filesystem mutation. Routes must not own installation, config writing, or
// other file mutation behavior.
func TestFitness_Routes_DoNotWriteFiles(t *testing.T) {
	routesDir := repoRootPath("internal", "api", "routes")
	for _, pattern := range []string{"os.WriteFile(", "os.MkdirAll(", "os.Remove(", "os.Rename("} {
		matches := routeContains(t, routesDir, pattern)
		if len(matches) > 0 {
			t.Fatalf("route layer contains forbidden filesystem mutation %q in: %s", pattern, strings.Join(matches, ", "))
		}
	}
}

// TestFitness_Routes_DoNotOwnConfigMutation enforces that config loading,
// merging, and persistence must sit behind a dedicated capability seam rather
// than the route layer.
func TestFitness_Routes_DoNotOwnConfigMutation(t *testing.T) {
	routesDir := repoRootPath("internal", "api", "routes")
	for _, pattern := range []string{
		"core.ConfigFilePath(",
		"core.LoadConfigFromFile(",
		"core.MarshalTOML(",
	} {
		matches := routeContains(t, routesDir, pattern)
		if len(matches) > 0 {
			t.Fatalf("route layer contains forbidden config-ownership pattern %q in: %s", pattern, strings.Join(matches, ", "))
		}
	}
}

// TestFitness_Routes_DoNotOwnRuntimeIdentity enforces that runtime identity
// generation and persistence belong in a runtime capability, not a route file.
func TestFitness_Routes_DoNotOwnRuntimeIdentity(t *testing.T) {
	routesDir := repoRootPath("internal", "api", "routes")
	for _, pattern := range []string{
		"ed25519.GenerateKey(",
		"sha256.Sum256(",
		"device_private_key",
	} {
		matches := routeContains(t, routesDir, pattern)
		if len(matches) > 0 {
			t.Fatalf("route layer contains forbidden runtime-identity pattern %q in: %s", pattern, strings.Join(matches, ", "))
		}
	}
}

// TestFitness_Routes_DoNotOwnSkillsLifecycle enforces that skill installation
// and mutation behavior must move behind a dedicated service/capability seam.
func TestFitness_Routes_DoNotOwnSkillsLifecycle(t *testing.T) {
	routesDir := repoRootPath("internal", "api", "routes")
	for _, pattern := range []string{
		"INSERT INTO skills",
		"UPDATE skills",
		"DELETE FROM skills",
	} {
		matches := routeContains(t, routesDir, pattern)
		if len(matches) > 0 {
			t.Fatalf("route layer contains forbidden skills lifecycle pattern %q in: %s", pattern, strings.Join(matches, ", "))
		}
	}
}

// TestFitness_Routes_DoNotOwnRevenueLifecycle enforces that revenue/service
// domain transitions move behind a dedicated service boundary rather than
// remaining route-local CRUD.
func TestFitness_Routes_DoNotOwnRevenueLifecycle(t *testing.T) {
	routesDir := repoRootPath("internal", "api", "routes")
	for _, pattern := range []string{
		"revenue_opportunities",
		"service_requests",
	} {
		matches := routeContains(t, routesDir, pattern)
		if len(matches) > 0 {
			t.Fatalf("route layer contains forbidden revenue/service lifecycle pattern %q in: %s", pattern, strings.Join(matches, ", "))
		}
	}
}

// TestFitness_Routes_DoNotOwnDomainLifecycleTransitions enforces that route
// files do not perform direct domain-table lifecycle SQL. Domain transitions
// belong behind capability or service seams, not in connectors.
func TestFitness_Routes_DoNotOwnDomainLifecycleTransitions(t *testing.T) {
	routesDir := repoRootPath("internal", "api", "routes")
	domainTables := []string{
		"cron_jobs",
		"cron_runs",
		"discovered_agents",
		"identity",
		"paired_devices",
		"revenue_opportunities",
		"semantic_memory",
		"service_requests",
		"session_messages",
		"sessions",
		"skills",
		"sub_agents",
	}

	lifecycleSQL := regexp.MustCompile(`(?i)(INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+([a-z_]+)`)
	var violations []string
	for _, path := range walkGoFiles(t, routesDir) {
		src := readRepoFile(t, path)
		for _, match := range lifecycleSQL.FindAllStringSubmatch(src, -1) {
			table := strings.ToLower(match[2])
			for _, candidate := range domainTables {
				if table == candidate {
					violations = append(violations, path+": "+strings.TrimSpace(match[0]))
					break
				}
			}
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("route layer contains forbidden direct domain lifecycle SQL:\n%s", strings.Join(violations, "\n"))
	}
}
