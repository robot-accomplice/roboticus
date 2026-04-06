package cmd

import (
	"testing"
)

func TestAppsListCmd_NoError(t *testing.T) {
	err := appsListCmd.RunE(appsListCmd, nil)
	if err != nil {
		t.Fatalf("apps list: %v", err)
	}
}

func TestAppsInstallCmd_RequiresArg(t *testing.T) {
	err := appsInstallCmd.Args(appsInstallCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to apps install")
	}
}

func TestAppsInstallCmd_AcceptsArg(t *testing.T) {
	err := appsInstallCmd.Args(appsInstallCmd, []string{"/tmp/myapp"})
	if err != nil {
		t.Errorf("unexpected error with valid arg: %v", err)
	}
}

func TestAppsInstallCmd_RunE(t *testing.T) {
	// Does not actually install anything, just prints.
	err := appsInstallCmd.RunE(appsInstallCmd, []string{"/tmp/myapp"})
	if err != nil {
		t.Fatalf("apps install: %v", err)
	}
}

func TestAppsUninstallCmd_RequiresArg(t *testing.T) {
	err := appsUninstallCmd.Args(appsUninstallCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to apps uninstall")
	}
}

func TestAppsUninstallCmd_RunE(t *testing.T) {
	err := appsUninstallCmd.RunE(appsUninstallCmd, []string{"myapp"})
	if err != nil {
		t.Fatalf("apps uninstall: %v", err)
	}
}

func TestAppsCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range appsCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"list", "install", "uninstall"} {
		if !subcommands[name] {
			t.Errorf("apps command missing subcommand %q", name)
		}
	}
}
