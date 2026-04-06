package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		json.NewEncoder(w).Encode(data)
	}
}

func TestCronListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"jobs": []any{
			map[string]any{"id": "1", "name": "test-job", "schedule_expr": "*/5 * * * *", "enabled": true},
		},
	}))
	defer cleanup()

	err := cronListCmd.RunE(cronListCmd, nil)
	if err != nil {
		t.Fatalf("cron list: %v", err)
	}
}

func TestCronListCmd_Empty(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"jobs": []any{},
	}))
	defer cleanup()

	err := cronListCmd.RunE(cronListCmd, nil)
	if err != nil {
		t.Fatalf("cron list empty: %v", err)
	}
}

func TestCronHistoryCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"runs": []any{},
	}))
	defer cleanup()

	err := cronHistoryCmd.RunE(cronHistoryCmd, nil)
	if err != nil {
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

	err := sessionsListCmd.RunE(sessionsListCmd, nil)
	if err != nil {
		t.Fatalf("sessions list: %v", err)
	}
}

func TestSessionsListCmd_Empty(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"sessions": []any{},
	}))
	defer cleanup()

	err := sessionsListCmd.RunE(sessionsListCmd, nil)
	if err != nil {
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

	err := channelsListCmd.RunE(channelsListCmd, nil)
	if err != nil {
		t.Fatalf("channels list: %v", err)
	}
}

func TestChannelsDeadLetterCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	err := channelsDeadLetterCmd.RunE(channelsDeadLetterCmd, nil)
	if err != nil {
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

	err := channelsDeadLetterCmd.RunE(channelsDeadLetterCmd, nil)
	if err != nil {
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

	err := circuitStatusCmd.RunE(circuitStatusCmd, nil)
	if err != nil {
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

	err := modelsListCmd.RunE(modelsListCmd, nil)
	if err != nil {
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

	err := mcpListCmd.RunE(mcpListCmd, nil)
	if err != nil {
		t.Fatalf("mcp list: %v", err)
	}
}

func TestMCPListCmd_Empty(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"connections": []any{},
	}))
	defer cleanup()

	err := mcpListCmd.RunE(mcpListCmd, nil)
	if err != nil {
		t.Fatalf("mcp list empty: %v", err)
	}
}

func TestMemoryWorkingCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	err := memoryWorkingCmd.RunE(memoryWorkingCmd, nil)
	if err != nil {
		t.Fatalf("memory working: %v", err)
	}
}

func TestMemoryEpisodicCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	err := memoryEpisodicCmd.RunE(memoryEpisodicCmd, nil)
	if err != nil {
		t.Fatalf("memory episodic: %v", err)
	}
}

func TestMemorySemanticCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	err := memorySemanticCmd.RunE(memorySemanticCmd, nil)
	if err != nil {
		t.Fatalf("memory semantic: %v", err)
	}
}

func TestMemorySearchCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"results": []any{},
	}))
	defer cleanup()

	err := memorySearchCmd.RunE(memorySearchCmd, []string{"test-query"})
	if err != nil {
		t.Fatalf("memory search: %v", err)
	}
}

func TestMetricsCostsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"total": 0.0,
	}))
	defer cleanup()

	err := metricsCostsCmd.RunE(metricsCostsCmd, nil)
	if err != nil {
		t.Fatalf("metrics costs: %v", err)
	}
}

func TestMetricsCacheCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"hits": 0, "misses": 0,
	}))
	defer cleanup()

	err := metricsCacheCmd.RunE(metricsCacheCmd, nil)
	if err != nil {
		t.Fatalf("metrics cache: %v", err)
	}
}

func TestMetricsCapacityCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"capacity": "ok",
	}))
	defer cleanup()

	err := metricsCapacityCmd.RunE(metricsCapacityCmd, nil)
	if err != nil {
		t.Fatalf("metrics capacity: %v", err)
	}
}

func TestAdminBreakerCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"breakers": []any{},
	}))
	defer cleanup()

	err := adminBreakerCmd.RunE(adminBreakerCmd, nil)
	if err != nil {
		t.Fatalf("admin breaker: %v", err)
	}
}

func TestAdminRosterCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"agents": []any{},
	}))
	defer cleanup()

	err := adminRosterCmd.RunE(adminRosterCmd, nil)
	if err != nil {
		t.Fatalf("admin roster: %v", err)
	}
}

func TestAdminModelsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"models": []any{},
	}))
	defer cleanup()

	err := adminModelsCmd.RunE(adminModelsCmd, nil)
	if err != nil {
		t.Fatalf("admin models: %v", err)
	}
}

func TestAdminSubagentsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"subagents": []any{},
	}))
	defer cleanup()

	err := adminSubagentsCmd.RunE(adminSubagentsCmd, nil)
	if err != nil {
		t.Fatalf("admin subagents: %v", err)
	}
}

func TestAdminChannelsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"channels": []any{},
	}))
	defer cleanup()

	err := adminChannelsCmd.RunE(adminChannelsCmd, nil)
	if err != nil {
		t.Fatalf("admin channels: %v", err)
	}
}

func TestAdminDeadLetterCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	err := adminDeadLetterCmd.RunE(adminDeadLetterCmd, nil)
	if err != nil {
		t.Fatalf("admin dead-letters: %v", err)
	}
}

func TestSkillsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"skills": []any{},
	}))
	defer cleanup()

	err := skillsListCmd.RunE(skillsListCmd, nil)
	if err != nil {
		t.Fatalf("skills list: %v", err)
	}
}

func TestSkillsReloadCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"reloaded": true,
	}))
	defer cleanup()

	err := skillsReloadCmd.RunE(skillsReloadCmd, nil)
	if err != nil {
		t.Fatalf("skills reload: %v", err)
	}
}

func TestSkillsCatalogCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"catalog": []any{},
	}))
	defer cleanup()

	err := skillsCatalogCmd.RunE(skillsCatalogCmd, nil)
	if err != nil {
		t.Fatalf("skills catalog: %v", err)
	}
}

func TestModelsDiagnosticsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"routing": "primary",
	}))
	defer cleanup()

	err := modelsDiagnosticsCmd.RunE(modelsDiagnosticsCmd, nil)
	if err != nil {
		t.Fatalf("models diagnostics: %v", err)
	}
}

func TestWalletBalanceCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"balance": "0.0",
	}))
	defer cleanup()

	err := walletBalanceCmd.RunE(walletBalanceCmd, nil)
	if err != nil {
		t.Fatalf("wallet balance: %v", err)
	}
}

func TestWalletAddressCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"address": "0x1234",
	}))
	defer cleanup()

	err := walletAddressCmd.RunE(walletAddressCmd, nil)
	if err != nil {
		t.Fatalf("wallet address: %v", err)
	}
}

func TestIntegrationsListCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"channels": []any{},
	}))
	defer cleanup()

	err := integrationsListCmd.RunE(integrationsListCmd, nil)
	if err != nil {
		t.Fatalf("integrations list: %v", err)
	}
}

func TestIntegrationsHealthCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{
		"channels": []any{
			map[string]any{"name": "slack", "enabled": true, "status": "connected"},
			map[string]any{"name": "discord", "enabled": false, "status": "disabled"},
			map[string]any{"name": "telegram", "enabled": true, "status": "error"},
		},
	}))
	defer cleanup()

	err := integrationsHealthCmd.RunE(integrationsHealthCmd, nil)
	if err != nil {
		t.Fatalf("integrations health: %v", err)
	}
}
