package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// commandMap returns a name→command map for all children of the given command.
func commandMap(cmd *cobra.Command) map[string]*cobra.Command {
	m := make(map[string]*cobra.Command)
	for _, sub := range cmd.Commands() {
		m[sub.Name()] = sub
	}
	return m
}

// TestCLI_GlobalFlags verifies all global persistent flags exist on rootCmd.
func TestCLI_GlobalFlags(t *testing.T) {
	flags := []struct {
		name      string
		shorthand string
	}{
		{"config", "c"},
		{"port", ""},
		{"bind", ""},
		{"url", ""},
		{"profile", ""},
		{"color", ""},
		{"theme", ""},
		{"no-draw", ""},
		{"nerdmode", ""},
		{"quiet", ""},
		{"json", ""},
	}

	for _, f := range flags {
		pf := rootCmd.PersistentFlags().Lookup(f.name)
		if pf == nil {
			t.Errorf("missing global persistent flag --%s", f.name)
			continue
		}
		if f.shorthand != "" && pf.Shorthand != f.shorthand {
			t.Errorf("flag --%s: expected shorthand %q, got %q", f.name, f.shorthand, pf.Shorthand)
		}
	}
}

// TestCLI_TopLevelCommands verifies all expected top-level commands are registered.
func TestCLI_TopLevelCommands(t *testing.T) {
	cmds := commandMap(rootCmd)

	expected := []string{
		"serve", "init", "setup", "check", "version",
		"update", "status", "mechanic", "logs", "circuit",
		"sessions", "memory", "skills", "mcp", "schedule",
		"metrics", "wallet", "auth", "config", "models",
		"plugins", "agents", "channels", "security", "keystore",
		"migrate", "daemon", "web", "reset", "uninstall",
		"completion", "cron", "profile", "tui", "ingest",
		"defrag", "admin", "upgrade",
	}

	for _, name := range expected {
		if _, ok := cmds[name]; !ok {
			t.Errorf("root command missing expected top-level command %q", name)
		}
	}
}

// TestCLI_Aliases verifies command aliases are correctly registered.
func TestCLI_Aliases(t *testing.T) {
	tests := []struct {
		cmdName string
		aliases []string
	}{
		{"serve", []string{"start", "run"}},
		{"setup", []string{"onboard"}},
		{"mechanic", []string{"doctor"}},
	}

	cmds := commandMap(rootCmd)

	for _, tt := range tests {
		cmd, ok := cmds[tt.cmdName]
		if !ok {
			t.Errorf("command %q not found on rootCmd", tt.cmdName)
			continue
		}
		aliasSet := make(map[string]bool)
		for _, a := range cmd.Aliases {
			aliasSet[a] = true
		}
		for _, want := range tt.aliases {
			if !aliasSet[want] {
				t.Errorf("command %q missing alias %q (has: %v)", tt.cmdName, want, cmd.Aliases)
			}
		}
	}
}

// TestCLI_SubcommandSets verifies expected subcommands exist for each parent command.
func TestCLI_SubcommandSets(t *testing.T) {
	tests := []struct {
		parentName string
		parent     *cobra.Command
		expected   []string
	}{
		{"update", updateCmd, []string{"check", "all", "binary"}},
		{"upgrade", upgradeCmd, []string{"all"}},
		{"admin", adminCmd, []string{"roster", "models", "subagents", "stats"}},
		{"cron", cronCmd, []string{"list", "create", "delete", "run", "history"}},
		{"schedule", scheduleCmd, []string{"list", "create", "delete", "run", "history"}},
		{"sessions", sessionsCmd, []string{"list", "show", "delete", "export", "create"}},
		{"memory", memoryCmd, []string{"working", "episodic", "semantic", "search", "stats"}},
		{"models", modelsCmd, []string{"list", "diagnostics", "scan", "exercise", "suggest", "reset", "baseline"}},
		{"config", configCmd, []string{"show", "get", "validate"}},
		{"auth", authCmd, []string{"status", "login", "logout"}},
		{"mcp", mcpCmd, []string{"list", "connect", "disconnect"}},
		{"circuit", circuitCmd, []string{"status", "reset"}},
		{"channels", channelsCmd, []string{"list", "test", "dead-letter"}},
		{"profile", profileCmd, []string{"list", "create", "switch", "delete"}},
		{"metrics", metricsCmd, []string{"costs", "cache", "capacity"}},
		{"skills", skillsCmd, []string{"list", "reload", "catalog"}},
		{"plugins", pluginsCmd, []string{"list", "info", "install", "uninstall", "enable", "disable", "search", "pack"}},
		{"keystore", keystoreCmd, []string{"status", "list", "set", "get", "remove", "import", "rekey"}},
		{"wallet", walletCmd, []string{"balance", "address"}},
		{"service", serviceCmd, []string{"install", "uninstall", "start", "stop", "restart", "status"}},
		{"daemon", daemonCmd, []string{"install", "uninstall", "start", "stop", "restart", "status"}},
		{"security", securityCmd, []string{"show", "audit"}},
	}

	for _, tt := range tests {
		subs := commandMap(tt.parent)
		for _, name := range tt.expected {
			if _, ok := subs[name]; !ok {
				t.Errorf("%s command missing subcommand %q", tt.parentName, name)
			}
		}
	}
}

// TestCLI_UpdateAll verifies both `update all` and `upgrade all` exist.
func TestCLI_UpdateAll(t *testing.T) {
	updateSubs := commandMap(updateCmd)
	if _, ok := updateSubs["all"]; !ok {
		t.Error("update command missing 'all' subcommand")
	}

	upgradeSubs := commandMap(upgradeCmd)
	if _, ok := upgradeSubs["all"]; !ok {
		t.Error("upgrade command missing 'all' subcommand")
	}
}

// TestCLI_ScheduleCronAlias verifies both `schedule` and `cron` are registered
// as top-level commands.
func TestCLI_ScheduleCronAlias(t *testing.T) {
	cmds := commandMap(rootCmd)

	if _, ok := cmds["cron"]; !ok {
		t.Error("root command missing 'cron' command")
	}
	if _, ok := cmds["schedule"]; !ok {
		t.Error("root command missing 'schedule' command")
	}
}
