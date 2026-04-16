package daemon

import (
	"os"
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

	// Also confirm a non-prefixed env DOESN'T leak in (we only capture
	// ROBOTICUS_*, never general environment).
	t.Setenv("PATH_SHOULD_NOT_LEAK", "bad")

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
	for k := range svc.EnvVars {
		if !strings.HasPrefix(k, "ROBOTICUS_") {
			t.Fatalf("EnvVars should only carry ROBOTICUS_*; found foreign key %q", k)
		}
	}
}

func mapKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// Silence unused-import lint on platforms where os isn't referenced in
// individual tests.
var _ = os.Environ
