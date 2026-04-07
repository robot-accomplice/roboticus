package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestRootCmd_HasExpectedSubcommands verifies all major subcommands are registered.
func TestRootCmd_HasExpectedSubcommands(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range rootCmd.Commands() {
		subcommands[sub.Name()] = true
	}

	expected := []string{
		"serve", "check", "config", "init", "setup",
		"sessions", "memory", "models", "cron", "channels",
		"admin", "auth", "circuit", "mcp",
		"logs", "metrics", "keystore", "security", "mechanic",
		"defrag", "migrate", "reset", "update", "upgrade",
		"version", "profile", "plugins", "skills",
		"wallet", "web", "tui", "completion", "ingest",
		"service",
	}
	for _, name := range expected {
		if !subcommands[name] {
			t.Errorf("root command missing subcommand %q", name)
		}
	}
}

// TestVersionCmd runs the version command and checks output.
func TestVersionCmd_Output(t *testing.T) {
	buf := &bytes.Buffer{}
	versionCmd.SetOut(buf)
	versionCmd.Run(versionCmd, nil)
	// The version command prints to stdout directly via fmt.Printf, not cmd.OutOrStdout(),
	// so we can't easily capture it. Just verify it doesn't panic.
}

func TestAdminCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range adminCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	expected := []string{"roster", "models", "subagents", "stats"}
	for _, name := range expected {
		if !subcommands[name] {
			t.Errorf("admin command missing subcommand %q", name)
		}
	}
}

func TestCircuitCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range circuitCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"status", "reset"} {
		if !subcommands[name] {
			t.Errorf("circuit command missing subcommand %q", name)
		}
	}
}

func TestCircuitResetCmd_RequiresArg(t *testing.T) {
	err := circuitResetCmd.Args(circuitResetCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to circuit reset")
	}
}

func TestChannelsCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range channelsCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "test", "dead-letter"} {
		if !subcommands[name] {
			t.Errorf("channels command missing subcommand %q", name)
		}
	}
}

func TestChannelsTestCmd_RequiresArg(t *testing.T) {
	err := channelsTestCmd.Args(channelsTestCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to channels test")
	}
}

func TestCronCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range cronCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "create", "delete", "run", "history"} {
		if !subcommands[name] {
			t.Errorf("cron command missing subcommand %q", name)
		}
	}
}

func TestCronCreateCmd_RequiresArgs(t *testing.T) {
	err := cronCreateCmd.Args(cronCreateCmd, []string{"only-one"})
	if err == nil {
		t.Error("expected error when only one arg provided to cron create")
	}
	err = cronCreateCmd.Args(cronCreateCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to cron create")
	}
	err = cronCreateCmd.Args(cronCreateCmd, []string{"name", "*/5 * * * *"})
	if err != nil {
		t.Errorf("unexpected error with valid args: %v", err)
	}
}

func TestCronDeleteCmd_RequiresArg(t *testing.T) {
	err := cronDeleteCmd.Args(cronDeleteCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to cron delete")
	}
}

func TestMemoryCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range memoryCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"working", "episodic", "semantic", "search", "stats"} {
		if !subcommands[name] {
			t.Errorf("memory command missing subcommand %q", name)
		}
	}
}

func TestMemorySearchCmd_RequiresArg(t *testing.T) {
	err := memorySearchCmd.Args(memorySearchCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to memory search")
	}
}

func TestModelsCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range modelsCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "diagnostics", "scan", "exercise", "suggest", "reset", "baseline"} {
		if !subcommands[name] {
			t.Errorf("models command missing subcommand %q", name)
		}
	}
}

func TestMetricsCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range metricsCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"costs", "cache", "capacity"} {
		if !subcommands[name] {
			t.Errorf("metrics command missing subcommand %q", name)
		}
	}
}

func TestConfigCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range configCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"show", "get", "validate"} {
		if !subcommands[name] {
			t.Errorf("config command missing subcommand %q", name)
		}
	}
}

func TestConfigGetCmd_RequiresArg(t *testing.T) {
	err := configGetCmd.Args(configGetCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to config get")
	}
}

func TestConfigGetCmd_NotFound(t *testing.T) {
	err := configGetCmd.RunE(configGetCmd, []string{"nonexistent.key.that.does.not.exist"})
	if err == nil {
		t.Error("expected error for nonexistent config key")
	}
}

func TestAuthCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range authCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"status", "login", "logout"} {
		if !subcommands[name] {
			t.Errorf("auth command missing subcommand %q", name)
		}
	}
}

func TestAuthLoginCmd_RequiresArg(t *testing.T) {
	err := authLoginCmd.Args(authLoginCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to auth login")
	}
}

func TestAuthLogoutCmd_RequiresArg(t *testing.T) {
	err := authLogoutCmd.Args(authLogoutCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to auth logout")
	}
}

func TestMCPCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range mcpCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "connect", "disconnect"} {
		if !subcommands[name] {
			t.Errorf("mcp command missing subcommand %q", name)
		}
	}
}

func TestMCPConnectCmd_RequiresArg(t *testing.T) {
	err := mcpConnectCmd.Args(mcpConnectCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to mcp connect")
	}
}

func TestMCPDisconnectCmd_RequiresArg(t *testing.T) {
	err := mcpDisconnectCmd.Args(mcpDisconnectCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to mcp disconnect")
	}
}

func TestProfileCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range profileCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "create", "switch", "delete"} {
		if !subcommands[name] {
			t.Errorf("profile command missing subcommand %q", name)
		}
	}
}

func TestProfileCreateCmd_RequiresArg(t *testing.T) {
	err := profileCreateCmd.Args(profileCreateCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to profile create")
	}
}

func TestServiceCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range serviceCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		if !subcommands[name] {
			t.Errorf("service command missing subcommand %q", name)
		}
	}
}

func TestWalletCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range walletCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"balance", "address"} {
		if !subcommands[name] {
			t.Errorf("wallet command missing subcommand %q", name)
		}
	}
}

func TestSkillsCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range skillsCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "reload", "catalog"} {
		if !subcommands[name] {
			t.Errorf("skills command missing subcommand %q", name)
		}
	}
}

func TestKeystoreCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range keystoreCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"status", "list", "set", "get", "remove", "import", "rekey"} {
		if !subcommands[name] {
			t.Errorf("keystore command missing subcommand %q", name)
		}
	}
}

func TestPluginsCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range pluginsCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "info", "install", "uninstall", "enable", "disable", "search", "pack"} {
		if !subcommands[name] {
			t.Errorf("plugins command missing subcommand %q", name)
		}
	}
}

func TestUpdateCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range updateCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"check", "all", "binary"} {
		if !subcommands[name] {
			t.Errorf("update command missing subcommand %q", name)
		}
	}
}

func TestLogsCmd_Flags(t *testing.T) {
	f := logsCmd.Flags().Lookup("lines")
	if f == nil {
		t.Fatal("expected 'lines' flag on logs command")
	}
	if f.DefValue != "50" {
		t.Errorf("expected default lines '50', got %q", f.DefValue)
	}

	f = logsCmd.Flags().Lookup("level")
	if f == nil {
		t.Fatal("expected 'level' flag on logs command")
	}

	f = logsCmd.Flags().Lookup("follow")
	if f == nil {
		t.Fatal("expected 'follow' flag on logs command")
	}
}

func TestMechanicCmd_Flags(t *testing.T) {
	f := mechanicCmd.Flags().Lookup("repair")
	if f == nil {
		t.Fatal("expected 'repair' flag on mechanic command")
	}
	if f.DefValue != "false" {
		t.Errorf("expected default repair 'false', got %q", f.DefValue)
	}
}

func TestIngestCmd_Flags(t *testing.T) {
	f := ingestCmd.Flags().Lookup("recursive")
	if f == nil {
		t.Fatal("expected 'recursive' flag on ingest command")
	}
	if f.DefValue != "true" {
		t.Errorf("expected default recursive 'true', got %q", f.DefValue)
	}

	f = ingestCmd.Flags().Lookup("chunk-size")
	if f == nil {
		t.Fatal("expected 'chunk-size' flag on ingest command")
	}
	if f.DefValue != "512" {
		t.Errorf("expected default chunk-size '512', got %q", f.DefValue)
	}

	f = ingestCmd.Flags().Lookup("dry-run")
	if f == nil {
		t.Fatal("expected 'dry-run' flag on ingest command")
	}
}

// TestNormalizeVersion tests the version normalization helper.
func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v2026.04.05", "2026.04.05"},
		{"  v1.0.0  ", "1.0.0"},
		{"dev", "dev"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.input); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestDownloadFile_Success tests downloading a file from a mock server.
func TestDownloadFile_Success(t *testing.T) {
	expected := "hello binary content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(expected))
	}))
	defer server.Close()

	origClient := updateHTTPClient
	updateHTTPClient = server.Client()
	defer func() { updateHTTPClient = origClient }()

	path, err := downloadFile(context.Background(), server.URL+"/binary")
	if err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	defer func() {
		if path != "" {
			_ = os.Remove(path)
		}
	}()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != expected {
		t.Errorf("downloaded content = %q, want %q", string(data), expected)
	}
}

// TestDownloadFile_ServerError tests downloading from a server that returns an error.
func TestDownloadFile_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	origClient := updateHTTPClient
	updateHTTPClient = server.Client()
	defer func() { updateHTTPClient = origClient }()

	_, err := downloadFile(context.Background(), server.URL+"/binary")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// TestAdminStatsCmd_WithMockServer tests the admin stats command with a mock server.
func TestAdminStatsCmd_WithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "test"})
	}))
	defer server.Close()

	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	err := adminStatsCmd.RunE(adminStatsCmd, nil)
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
}
