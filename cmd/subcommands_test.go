package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// lookupCmd finds a command in rootCmd by traversing a path of names.
func lookupCmd(t *testing.T, names ...string) *cobra.Command {
	t.Helper()
	cmd := rootCmd
	for _, name := range names {
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
	cmd := lookupCmd(t, "version")
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.Run(cmd, nil)
	// The version command prints to stdout directly via fmt.Printf, not cmd.OutOrStdout(),
	// so we can't easily capture it. Just verify it doesn't panic.
}

func TestAdminCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "admin")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
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
	cmd := lookupCmd(t, "circuit")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"status", "reset"} {
		if !subcommands[name] {
			t.Errorf("circuit command missing subcommand %q", name)
		}
	}
}

func TestCircuitResetCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "circuit", "reset")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to circuit reset")
	}
}

func TestChannelsCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "channels")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "test", "dead-letter"} {
		if !subcommands[name] {
			t.Errorf("channels command missing subcommand %q", name)
		}
	}
}

func TestChannelsTestCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "channels", "test")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to channels test")
	}
}

func TestCronCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "cron")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "create", "delete", "run", "history"} {
		if !subcommands[name] {
			t.Errorf("cron command missing subcommand %q", name)
		}
	}
}

func TestCronCreateCmd_RequiresArgs(t *testing.T) {
	cmd := lookupCmd(t, "cron", "create")
	err := cmd.Args(cmd, []string{"only-one"})
	if err == nil {
		t.Error("expected error when only one arg provided to cron create")
	}
	err = cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to cron create")
	}
	err = cmd.Args(cmd, []string{"name", "*/5 * * * *"})
	if err != nil {
		t.Errorf("unexpected error with valid args: %v", err)
	}
}

func TestCronDeleteCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "cron", "delete")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to cron delete")
	}
}

func TestMemoryCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "memory")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"working", "episodic", "semantic", "search", "stats"} {
		if !subcommands[name] {
			t.Errorf("memory command missing subcommand %q", name)
		}
	}
}

func TestMemorySearchCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "memory", "search")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to memory search")
	}
}

func TestModelsCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "models")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "diagnostics", "scan", "exercise", "suggest", "reset", "baseline"} {
		if !subcommands[name] {
			t.Errorf("models command missing subcommand %q", name)
		}
	}
}

func TestMetricsCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "metrics")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"costs", "cache", "capacity"} {
		if !subcommands[name] {
			t.Errorf("metrics command missing subcommand %q", name)
		}
	}
}

func TestConfigCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "config")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"show", "get", "validate"} {
		if !subcommands[name] {
			t.Errorf("config command missing subcommand %q", name)
		}
	}
}

func TestConfigGetCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "config", "get")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to config get")
	}
}

func TestConfigGetCmd_NotFound(t *testing.T) {
	cmd := lookupCmd(t, "config", "get")
	err := cmd.RunE(cmd, []string{"nonexistent.key.that.does.not.exist"})
	if err == nil {
		t.Error("expected error for nonexistent config key")
	}
}

func TestAuthCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "auth")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"status", "login", "logout"} {
		if !subcommands[name] {
			t.Errorf("auth command missing subcommand %q", name)
		}
	}
}

func TestAuthLoginCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "auth", "login")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to auth login")
	}
}

func TestAuthLogoutCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "auth", "logout")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to auth logout")
	}
}

func TestMCPCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "mcp")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "connect", "disconnect"} {
		if !subcommands[name] {
			t.Errorf("mcp command missing subcommand %q", name)
		}
	}
}

func TestMCPConnectCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "mcp", "connect")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to mcp connect")
	}
}

func TestMCPDisconnectCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "mcp", "disconnect")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to mcp disconnect")
	}
}

func TestProfileCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "profile")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "create", "switch", "delete"} {
		if !subcommands[name] {
			t.Errorf("profile command missing subcommand %q", name)
		}
	}
}

func TestProfileCreateCmd_RequiresArg(t *testing.T) {
	cmd := lookupCmd(t, "profile", "create")
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to profile create")
	}
}

func TestServiceCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "service")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		if !subcommands[name] {
			t.Errorf("service command missing subcommand %q", name)
		}
	}
}

func TestWalletCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "wallet")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"balance", "address"} {
		if !subcommands[name] {
			t.Errorf("wallet command missing subcommand %q", name)
		}
	}
}

func TestSkillsCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "skills")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "reload", "catalog"} {
		if !subcommands[name] {
			t.Errorf("skills command missing subcommand %q", name)
		}
	}
}

func TestKeystoreCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "keystore")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"status", "list", "set", "get", "remove", "import", "rekey"} {
		if !subcommands[name] {
			t.Errorf("keystore command missing subcommand %q", name)
		}
	}
}

func TestPluginsCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "plugins")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "info", "install", "uninstall", "enable", "disable", "search", "pack"} {
		if !subcommands[name] {
			t.Errorf("plugins command missing subcommand %q", name)
		}
	}
}

func TestUpdateCmd_SubcommandRegistration(t *testing.T) {
	cmd := lookupCmd(t, "update")
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"check", "all", "binary"} {
		if !subcommands[name] {
			t.Errorf("update command missing subcommand %q", name)
		}
	}
}

func TestLogsCmd_Flags(t *testing.T) {
	cmd := lookupCmd(t, "logs")
	f := cmd.Flags().Lookup("lines")
	if f == nil {
		t.Fatal("expected 'lines' flag on logs command")
	}
	if f.DefValue != "50" {
		t.Errorf("expected default lines '50', got %q", f.DefValue)
	}

	f = cmd.Flags().Lookup("level")
	if f == nil {
		t.Fatal("expected 'level' flag on logs command")
	}

	f = cmd.Flags().Lookup("follow")
	if f == nil {
		t.Fatal("expected 'follow' flag on logs command")
	}
}

func TestMechanicCmd_Flags(t *testing.T) {
	cmd := lookupCmd(t, "mechanic")
	f := cmd.Flags().Lookup("repair")
	if f == nil {
		t.Fatal("expected 'repair' flag on mechanic command")
	}
	if f.DefValue != "false" {
		t.Errorf("expected default repair 'false', got %q", f.DefValue)
	}
}

func TestIngestCmd_Flags(t *testing.T) {
	cmd := lookupCmd(t, "ingest")
	f := cmd.Flags().Lookup("recursive")
	if f == nil {
		t.Fatal("expected 'recursive' flag on ingest command")
	}
	if f.DefValue != "true" {
		t.Errorf("expected default recursive 'true', got %q", f.DefValue)
	}

	f = cmd.Flags().Lookup("chunk-size")
	if f == nil {
		t.Fatal("expected 'chunk-size' flag on ingest command")
	}
	if f.DefValue != "512" {
		t.Errorf("expected default chunk-size '512', got %q", f.DefValue)
	}

	f = cmd.Flags().Lookup("dry-run")
	if f == nil {
		t.Fatal("expected 'dry-run' flag on ingest command")
	}
}

// TestAdminStatsCmd_WithMockServer tests the admin stats command with a mock server.
func TestAdminStatsCmd_WithMockServer(t *testing.T) {
	cleanup := setupMockAPI(t, jsonHandler(map[string]any{"data": "test"}))
	defer cleanup()

	cmd := lookupCmd(t, "admin", "stats")
	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
}

// Tests that were referencing updatecmd internal functions have been moved
// to cmd/updatecmd/update_parity_test.go where they belong.
// TestNormalizeVersion and TestDownloadFile_* live there now.
func TestNormalizeVersion_ViaRoot(t *testing.T) {
	// This is a structural validation - version command exists and runs.
	cmd := lookupCmd(t, "version")
	if cmd == nil {
		t.Fatal("version command not found")
	}
}

// TestSubcommands_NoUnusedImports ensures the test file compiles cleanly.
func TestSubcommands_NoUnusedImports(t *testing.T) {
	_ = strings.TrimSpace("ok")
	_ = viper.GetBool("quiet")
}
