package daemon

import (
	"strings"
	"testing"

	"roboticus/internal/core"
)

// TestServiceInstallConfig_EmbedsConfigPath pins the v1.0.6 invariant that
// ServiceInstallConfig produces a service.Config whose Arguments carry
// `serve --config <configPath>`. Without this, an installed service starts
// with no arguments and silently falls back to whatever the service
// context's default config lookup resolves — potentially a DIFFERENT
// agent/DB/workspace than the operator ran install under.
//
// Regression target: the pre-v1.0.6 ServiceConfig (later NewServiceOnly)
// discarded its cfg parameter entirely and returned only Name/Display/Description.
func TestServiceInstallConfig_EmbedsConfigPath(t *testing.T) {
	cfg := core.DefaultConfig()
	const path = "/custom/roboticus.toml"

	svc := ServiceInstallConfig(&cfg, path)

	if svc.Name == "" {
		t.Fatalf("Name must be set; got empty")
	}
	if len(svc.Arguments) < 3 {
		t.Fatalf("Arguments should carry [serve --config <path>]; got %v", svc.Arguments)
	}
	if svc.Arguments[0] != "serve" {
		t.Fatalf("first arg should be 'serve'; got %q", svc.Arguments[0])
	}
	found := false
	for i := 0; i < len(svc.Arguments)-1; i++ {
		if svc.Arguments[i] == "--config" && svc.Arguments[i+1] == path {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Arguments must contain `--config %s`; got %v", path, svc.Arguments)
	}
}

// TestServiceInstallConfig_EmptyPathSkipsConfigArg verifies that an empty
// configPath does NOT result in a dangling `--config` with no value. That
// would break the service invocation completely. If the caller didn't
// resolve a path, the service falls back to default lookup (which is the
// pre-v1.0.6 behavior, not worse).
func TestServiceInstallConfig_EmptyPathSkipsConfigArg(t *testing.T) {
	cfg := core.DefaultConfig()
	svc := ServiceInstallConfig(&cfg, "")
	for _, a := range svc.Arguments {
		if a == "--config" {
			t.Fatalf("empty configPath should not emit a --config arg; got %v", svc.Arguments)
		}
	}
	if len(svc.Arguments) == 0 || svc.Arguments[0] != "serve" {
		t.Fatalf("expected at minimum [serve]; got %v", svc.Arguments)
	}
}

// TestServiceInstallConfig_SnapshotsRoboticusEnv verifies the install
// context captures ROBOTICUS_* environment variables. Operators who run
// install under `ROBOTICUS_PROFILE=prod roboticus daemon install` expect
// that profile to persist into the installed service. Without the env
// snapshot, the service starts with empty env and reverts to defaults.
func TestServiceInstallConfig_SnapshotsRoboticusEnv(t *testing.T) {
	// Set an isolated test env var.
	key := "ROBOTICUS_TEST_INSTALL_CONFIG_REGR"
	value := "from-install-time"
	t.Setenv(key, value)

	// A foreign env that is NOT in any whitelist category must not
	// leak through.
	t.Setenv("FOREIGN_SHOULD_NOT_LEAK", "bad")

	cfg := core.DefaultConfig()
	svc := ServiceInstallConfig(&cfg, "/tmp/cfg.toml")

	if svc.EnvVars == nil {
		t.Fatalf("EnvVars must be populated when ROBOTICUS_* env is set; got nil")
	}
	got, ok := svc.EnvVars[key]
	if !ok {
		t.Fatalf("EnvVars missing %q; got keys %v", key, mapKeys(svc.EnvVars))
	}
	if got != value {
		t.Fatalf("EnvVars[%q] = %q; want %q", key, got, value)
	}
	// Whitelist boundary: every key must be ROBOTICUS_*, PATH, or HOME.
	// Foreign keys must NOT appear.
	for k := range svc.EnvVars {
		if strings.HasPrefix(k, "ROBOTICUS_") || k == "PATH" || k == "HOME" {
			continue
		}
		t.Fatalf("EnvVars carries foreign key %q; whitelist is ROBOTICUS_* | PATH | HOME only", k)
	}
	if _, leaked := svc.EnvVars["FOREIGN_SHOULD_NOT_LEAK"]; leaked {
		t.Fatalf("foreign env leaked into install snapshot")
	}
}

// TestServiceInstallConfig_SnapshotsPATH is the v1.0.6 self-audit P1-H
// regression. Systemd/launchd services inherit a minimal PATH (often
// just /usr/bin:/bin), NOT the operator's shell PATH. If the operator
// had /opt/homebrew/bin (for `ollama`), $HOME/.local/bin (pip), or a
// virtualenv bin dir on PATH, subprocess launches from the service
// (Ollama, Playwright MCP via npx, Python MCP servers) would silently
// fail with "not found." Pre-audit the PATH snapshot was intentionally
// NOT included with the rationale "services inherit PATH from the
// service manager" — which is technically true but practically wrong,
// because the service manager's PATH is the wrong PATH.
func TestServiceInstallConfig_SnapshotsPATH(t *testing.T) {
	const customPath = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	t.Setenv("PATH", customPath)

	cfg := core.DefaultConfig()
	svc := ServiceInstallConfig(&cfg, "/tmp/cfg.toml")
	got, ok := svc.EnvVars["PATH"]
	if !ok {
		t.Fatalf("EnvVars missing PATH — service will inherit service-manager minimal PATH and subprocess tool lookups will fail")
	}
	if got != customPath {
		t.Fatalf("PATH in EnvVars = %q; want %q (verbatim snapshot, not rewritten)", got, customPath)
	}
}

// TestServiceInstallConfig_SnapshotsHOME mirrors the PATH concern for
// $HOME — some code paths expand `~` after the config loads (plugins,
// user-edited paths, external MCP configs). If the service runs as a
// system account and we don't seed HOME, those ~ references resolve
// to the wrong user's home or to an empty string.
func TestServiceInstallConfig_SnapshotsHOME(t *testing.T) {
	const operatorHome = "/Users/operator"
	t.Setenv("HOME", operatorHome)

	cfg := core.DefaultConfig()
	svc := ServiceInstallConfig(&cfg, "/tmp/cfg.toml")
	got, ok := svc.EnvVars["HOME"]
	if !ok {
		t.Fatalf("EnvVars missing HOME — tilde-expansion in the installed service will resolve against the service user's home, not the operator's")
	}
	if got != operatorHome {
		t.Fatalf("HOME in EnvVars = %q; want %q", got, operatorHome)
	}
}

func mapKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// TestServiceInstallConfig_RejectsRelativePaths is the v1.0.6 P2-H
// guardrail. ServiceInstallConfig is a leaf — it takes whatever
// configPath it's handed and bakes it into service arguments. The
// caller (cmd/admin/daemon.go and cmd/admin/service.go) is responsible
// for calling cmdutil.EffectiveConfigPathAbs so only absolute paths
// ever land here. This test documents the invariant by asserting that
// a relative path, if it ever slips through to this leaf, is STILL
// embedded verbatim — which surfaces as immediately-wrong service
// behavior rather than silently-wrong behavior.
//
// The actual "reject" happens one layer up in the install command; but
// by pinning the leaf's behavior as "verbatim pass-through", we
// separate concerns cleanly: caller absolutizes, leaf trusts.
func TestServiceInstallConfig_RelativeIsEmbeddedVerbatim(t *testing.T) {
	cfg := core.DefaultConfig()
	const relPath = "configs/prod.toml"
	svc := ServiceInstallConfig(&cfg, relPath)

	found := false
	for i := 0; i < len(svc.Arguments)-1; i++ {
		if svc.Arguments[i] == "--config" && svc.Arguments[i+1] == relPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ServiceInstallConfig should pass configPath verbatim so the install layer's absolutize step is the single authority; Arguments=%v", svc.Arguments)
	}
}
