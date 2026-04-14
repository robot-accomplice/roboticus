package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// setupMockAPI creates a mock server for API command tests.
// It returns the server and a cleanup function.
func setupMockAPI(t *testing.T, handler http.Handler) func() {
	t.Helper()
	server := httptest.NewServer(handler)
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	return func() {
		viper.Set("server.port", old)
		server.Close()
	}
}

func jsonHandler(data any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}
}

// findSubCmd looks up a subcommand path like "cron list" on rootCmd.
func findSubCmd(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	cmd := rootCmd
	for _, name := range path {
		var found *cobra.Command
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = sub
				break
			}
		}
		if found == nil {
			t.Fatalf("subcommand %q not found under %q", name, cmd.Name())
		}
		cmd = found
	}
	return cmd
}

func TestCronListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"jobs": []any{
			map[string]any{"id": "1", "name": "test-job", "schedule_expr": "*/5 * * * *", "enabled": true},
		},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "cron", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("cron list: %v", err)
	}
}

func TestCronListCmd_Empty(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"jobs": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "cron", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("cron list empty: %v", err)
	}
}

func TestCronHistoryCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"runs": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "cron", "history")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("cron history: %v", err)
	}
}

func TestSessionsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"sessions": []any{
			map[string]any{"id": "s1", "agent_id": "default", "scope_key": "test", "nickname": "Bot"},
		},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "sessions", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("sessions list: %v", err)
	}
}

func TestSessionsListCmd_Empty(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"sessions": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "sessions", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("sessions list empty: %v", err)
	}
}

func TestChannelsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"channels": []any{
			map[string]any{"platform": "slack", "status": "ok", "message_count": float64(42)},
		},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "channels", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("channels list: %v", err)
	}
}

func TestChannelsDeadLetterCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "channels", "dead-letter")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("channels dead-letter: %v", err)
	}
}

func TestChannelsDeadLetterCmd_WithEntries(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{
			map[string]any{"timestamp": "2026-01-01T00:00:00Z", "platform": "slack", "error": "timeout"},
		},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "channels", "dead-letter")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("channels dead-letter with entries: %v", err)
	}
}

func TestCircuitStatusCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"breakers": []any{
			map[string]any{"provider": "openai", "state": "closed", "failures": float64(0)},
		},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "circuit", "status")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("circuit status: %v", err)
	}
}

func TestModelsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"models": []any{
			map[string]any{"id": "gpt-4", "provider": "openai", "context_window": float64(128000)},
		},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "models", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("models list: %v", err)
	}
}

func TestMCPListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"connections": []any{
			map[string]any{"name": "test-server", "status": "connected", "tools_count": float64(5)},
		},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "mcp", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("mcp list: %v", err)
	}
}

func TestMCPListCmd_Empty(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"connections": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "mcp", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("mcp list empty: %v", err)
	}
}

func TestMemoryWorkingCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "memory", "working")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("memory working: %v", err)
	}
}

func TestMemoryEpisodicCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "memory", "episodic")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("memory episodic: %v", err)
	}
}

func TestMemorySemanticCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "memory", "semantic")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("memory semantic: %v", err)
	}
}

func TestMemorySearchCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"results": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "memory", "search")
	if err := cmd.RunE(cmd, []string{"test-query"}); err != nil {
		t.Fatalf("memory search: %v", err)
	}
}

func TestMetricsCostsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"total": 0.0,
	}))
	defer cleanup()

	cmd := findSubCmd(t, "metrics", "costs")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("metrics costs: %v", err)
	}
}

func TestMetricsCacheCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"hits": 0, "misses": 0,
	}))
	defer cleanup()

	cmd := findSubCmd(t, "metrics", "cache")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("metrics cache: %v", err)
	}
}

func TestMetricsCapacityCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"capacity": "ok",
	}))
	defer cleanup()

	cmd := findSubCmd(t, "metrics", "capacity")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("metrics capacity: %v", err)
	}
}

func TestAdminRosterCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"agents": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "admin", "roster")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("admin roster: %v", err)
	}
}

func TestAdminModelsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"models": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "admin", "models")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("admin models: %v", err)
	}
}

func TestAdminSubagentsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"subagents": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "admin", "subagents")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("admin subagents: %v", err)
	}
}

func TestSkillsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"skills": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "skills", "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("skills list: %v", err)
	}
}

func TestSkillsReloadCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"reloaded": true,
	}))
	defer cleanup()

	cmd := findSubCmd(t, "skills", "reload")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("skills reload: %v", err)
	}
}

func TestSkillsCatalogCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"catalog": []any{},
	}))
	defer cleanup()

	cmd := findSubCmd(t, "skills", "catalog")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("skills catalog: %v", err)
	}
}

func TestModelsDiagnosticsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"routing": "primary",
	}))
	defer cleanup()

	cmd := findSubCmd(t, "models", "diagnostics")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("models diagnostics: %v", err)
	}
}

func TestWalletBalanceCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"balance": "0.0",
	}))
	defer cleanup()

	cmd := findSubCmd(t, "wallet", "balance")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("wallet balance: %v", err)
	}
}

func TestWalletAddressCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"address": "0x1234",
	}))
	defer cleanup()

	cmd := findSubCmd(t, "wallet", "address")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("wallet address: %v", err)
	}
}
