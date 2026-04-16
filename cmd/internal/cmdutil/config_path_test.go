package cmdutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestEffectiveConfigPathAbs_AbsolutizesRelative is the v1.0.6 P2-H
// regression. Pre-fix, `roboticus daemon install --config configs/prod.toml`
// would bake the literal relative string `configs/prod.toml` into the
// service manager's Arguments. When the service manager later started
// roboticus from ITS working directory (/, /var/root, etc.), the
// relative path resolved against that, not the operator's install-time
// shell CWD — so the installed service ran against the default config
// lookup (or nothing) instead of `configs/prod.toml`.
//
// v1.0.6: the install command uses EffectiveConfigPathAbs which
// absolutizes against the current CWD before handing off to
// daemon.Install. This test confirms a relative --config value becomes
// an absolute path.
func TestEffectiveConfigPathAbs_AbsolutizesRelative(t *testing.T) {
	// Use viper to simulate --config flag behavior.
	saved := viper.GetString("config")
	t.Cleanup(func() { viper.Set("config", saved) })
	viper.Set("config", "configs/prod.toml")

	abs, err := EffectiveConfigPathAbs()
	if err != nil {
		t.Fatalf("EffectiveConfigPathAbs: %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("expected absolute path; got %q", abs)
	}
	// Defense in depth: no filesystem component should be a literal "." or
	// ".." — IsAbs is the source of truth but these add a clear failure
	// message if IsAbs ever lies on some exotic FS.
	if filepath.Dir(abs) == "." {
		t.Fatalf("path root degenerated to '.'; got %q", abs)
	}
}

// TestEffectiveConfigPathAbs_PreservesAbsolute confirms passing an
// already-absolute path through EffectiveConfigPathAbs returns the
// same path unchanged (no double-join, no mangling).
func TestEffectiveConfigPathAbs_PreservesAbsolute(t *testing.T) {
	saved := viper.GetString("config")
	t.Cleanup(func() { viper.Set("config", saved) })
	abs := "/tmp/roboticus-test-prod.toml"
	viper.Set("config", abs)

	got, err := EffectiveConfigPathAbs()
	if err != nil {
		t.Fatalf("EffectiveConfigPathAbs: %v", err)
	}
	if got != abs {
		t.Fatalf("absolute path should pass through unchanged; want %q got %q", abs, got)
	}
}

// TestEffectiveConfigPathAbs_UsesHomeDefaultWhenUnset ensures the
// default-fallback branch (no --config, no ROBOTICUS_CONFIG) also
// returns an absolute path — the home-derived default is already
// absolute, but we confirm the signature doesn't error.
func TestEffectiveConfigPathAbs_UsesHomeDefaultWhenUnset(t *testing.T) {
	saved := viper.GetString("config")
	t.Cleanup(func() { viper.Set("config", saved) })
	viper.Set("config", "")
	t.Setenv("ROBOTICUS_CONFIG", "")

	got, err := EffectiveConfigPathAbs()
	if err != nil {
		t.Fatalf("EffectiveConfigPathAbs: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("default config path should be absolute; got %q", got)
	}
}

// Silence unused-import lint on Windows CI where filepath is the only
// test-scope reference.
var _ = os.Environ
