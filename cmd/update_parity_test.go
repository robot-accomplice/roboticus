package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateStateRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	original := updateState{
		BinaryVersion: "2026.04.10",
		LastCheck:     "2026-04-06T10:00:00Z",
		RegistryURL:   "https://example.com/registry/manifest.json",
		InstalledContent: installedContent{
			Providers: &contentRecord{
				Version:     "2026.04.10",
				SHA256:      "abc123",
				InstalledAt: "2026-04-06T10:00:00Z",
			},
			Skills: &skillsRecord{
				Version:     "2026.04.10",
				Files:       map[string]string{"hello.md": "def456"},
				InstalledAt: "2026-04-06T10:00:00Z",
			},
		},
	}

	if err := saveUpdateState(original); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}
	loaded, err := loadUpdateState()
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if loaded.BinaryVersion != original.BinaryVersion || loaded.RegistryURL != original.RegistryURL {
		t.Fatalf("loaded state mismatch: %#v", loaded)
	}
	if loaded.InstalledContent.Providers == nil || loaded.InstalledContent.Skills == nil {
		t.Fatalf("expected installed content to round-trip: %#v", loaded)
	}
}

func TestResolveRegistryURL(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "roboticus.toml")
	if err := os.WriteFile(configPath, []byte("[update]\nregistry_url = \"https://config.example/manifest.json\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ROBOTICUS_REGISTRY_URL", "")
	if got := resolveRegistryURL(configPath); got != "https://config.example/manifest.json" {
		t.Fatalf("resolveRegistryURL from config = %q", got)
	}

	t.Setenv("ROBOTICUS_REGISTRY_URL", "https://env.example/manifest.json")
	if got := resolveRegistryURL(configPath); got != "https://env.example/manifest.json" {
		t.Fatalf("resolveRegistryURL from env = %q", got)
	}
}

func TestApplyProvidersUpdateAndSkillsUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "roboticus.toml")
	skillsDir := filepath.Join(home, "custom-skills")
	providersPath := filepath.Join(home, "custom-providers.toml")
	config := "" +
		"providers_file = \"" + providersPath + "\"\n" +
		"[skills]\n" +
		"directory = \"" + skillsDir + "\"\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	providersBody := "[providers.openai]\nurl = \"https://api.openai.com\"\n"
	skillBody := "# Hello\n"
	manifest := registryManifest{
		Version: "v2026.04.10",
		Packs: registryPacks{
			Providers: providerPack{
				Path:   "packs/providers.toml",
				SHA256: bytesSHA256([]byte(providersBody)),
			},
			Skills: skillPack{
				Path: "packs/skills/",
				Files: map[string]string{
					"hello.md": bytesSHA256([]byte(skillBody)),
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		case "/registry/packs/providers.toml":
			_, _ = w.Write([]byte(providersBody))
		case "/registry/packs/skills/hello.md":
			_, _ = w.Write([]byte(skillBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registryURL := server.URL + "/registry/manifest.json"
	changed, err := applyProvidersUpdate(context.Background(), registryURL, configPath)
	if err != nil {
		t.Fatalf("applyProvidersUpdate: %v", err)
	}
	if !changed {
		t.Fatal("expected provider update to report changes")
	}
	changed, err = applySkillsUpdate(context.Background(), registryURL, configPath)
	if err != nil {
		t.Fatalf("applySkillsUpdate: %v", err)
	}
	if !changed {
		t.Fatal("expected skills update to report changes")
	}

	if got, err := os.ReadFile(providersPath); err != nil || string(got) != providersBody {
		t.Fatalf("providers file mismatch: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(skillsDir, "hello.md")); err != nil || string(got) != skillBody {
		t.Fatalf("skill file mismatch: %q err=%v", got, err)
	}
}

func TestRunUpdateAllOrchestratesBinaryProvidersAndSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, "roboticus.toml")
	skillsDir := filepath.Join(home, "skills")
	if err := os.WriteFile(configPath, []byte("[skills]\ndirectory = \""+skillsDir+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	origCfg := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = origCfg }()

	binaryBytes := []byte("fake binary")
	binaryHash := sha256.Sum256(binaryBytes)
	checksumBody := hex.EncodeToString(binaryHash[:]) + "  " + binaryName() + "\n"
	providersBody := "[providers.ollama]\nurl = \"http://localhost:11434\"\n"
	skillBody := "# Example skill\n"
	manifest := registryManifest{
		Version: "v2026.04.10",
		Packs: registryPacks{
			Providers: providerPack{
				Path:   "providers.toml",
				SHA256: bytesSHA256([]byte(providersBody)),
			},
			Skills: skillPack{
				Path: "skills/",
				Files: map[string]string{
					"example.md": bytesSHA256([]byte(skillBody)),
				},
			},
		},
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(latestRelease{
				TagName: "v2026.04.10",
				Assets: []releaseAsset{
					{Name: binaryName(), BrowserDownloadURL: serverURL + "/downloads/" + binaryName()},
					{Name: "SHA256SUMS.txt", BrowserDownloadURL: serverURL + "/downloads/SHA256SUMS.txt"},
				},
			})
		case "/downloads/" + binaryName():
			_, _ = w.Write(binaryBytes)
		case "/downloads/SHA256SUMS.txt":
			_, _ = w.Write([]byte(checksumBody))
		case "/registry/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		case "/registry/providers.toml":
			_, _ = w.Write([]byte(providersBody))
		case "/registry/skills/example.md":
			_, _ = w.Write([]byte(skillBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	origCheckURL := updateCheckURL
	origClient := updateHTTPClient
	origRegistryURL := updateRegistryURL
	origBinaryFunc := updateBinaryFunc
	origMaintenance := updateMaintenance
	updateCheckURL = server.URL + "/releases/latest"
	updateHTTPClient = server.Client()
	updateRegistryURL = server.URL + "/registry/manifest.json"
	calledBinary := false
	calledMaintenance := false
	updateBinaryFunc = func(ctx context.Context, rel latestRelease, skipConfirm bool) error {
		calledBinary = true
		if rel.TagName != "v2026.04.10" {
			t.Fatalf("unexpected release tag: %s", rel.TagName)
		}
		return nil
	}
	updateMaintenance = func(path string) error {
		calledMaintenance = true
		if path != configPath {
			t.Fatalf("maintenance path = %q, want %q", path, configPath)
		}
		return nil
	}
	defer func() {
		updateCheckURL = origCheckURL
		updateHTTPClient = origClient
		updateRegistryURL = origRegistryURL
		updateBinaryFunc = origBinaryFunc
		updateMaintenance = origMaintenance
	}()

	if err := runUpdateAll(context.Background(), "2026.04.05", true); err != nil {
		t.Fatalf("runUpdateAll: %v", err)
	}
	if !calledBinary {
		t.Fatal("expected binary update to run")
	}
	if !calledMaintenance {
		t.Fatal("expected maintenance to run")
	}

	state, err := loadUpdateState()
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if state.BinaryVersion != "2026.04.10" {
		t.Fatalf("binary version = %q", state.BinaryVersion)
	}
	if state.InstalledContent.Providers == nil || state.InstalledContent.Skills == nil {
		t.Fatalf("expected provider and skills state, got %#v", state)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "example.md")); err != nil {
		t.Fatalf("expected skill file to be installed: %v", err)
	}
}

func TestApplyRemovedLegacyConfigMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roboticus.toml")
	before := "[security]\nallowed_models = [\"gpt-4\"]\nworkspace_only = true\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applyRemovedLegacyConfigMigration(path); err != nil {
		t.Fatalf("applyRemovedLegacyConfigMigration: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "allowed_models") {
		t.Fatalf("expected allowed_models to be removed, got %s", after)
	}
}

func TestApplySecurityConfigMigrationAddsSectionWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roboticus.toml")
	if err := os.WriteFile(path, []byte("[agent]\nname = \"roboticus\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applySecurityConfigMigration(path); err != nil {
		t.Fatalf("applySecurityConfigMigration: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(after)
	if !strings.Contains(content, "[security]") || !strings.Contains(content, "deny_on_empty_allowlist = true") {
		t.Fatalf("expected security section, got %s", content)
	}
}

func TestUpgradeAllCommandUsesSameWorkflow(t *testing.T) {
	origVersion := version
	origCheckURL := updateCheckURL
	origClient := updateHTTPClient
	origRegistryURL := updateRegistryURL
	origBinaryFunc := updateBinaryFunc
	origMaintenance := updateMaintenance
	defer func() {
		version = origVersion
		updateCheckURL = origCheckURL
		updateHTTPClient = origClient
		updateRegistryURL = origRegistryURL
		updateBinaryFunc = origBinaryFunc
		updateMaintenance = origMaintenance
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "roboticus.toml")
	if err := os.WriteFile(configPath, []byte("[skills]\ndirectory = \""+filepath.Join(home, "skills")+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	origCfg := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = origCfg }()

	providersBody := "[providers.openai]\nurl = \"https://api.openai.com\"\n"
	skillBody := "# Skill\n"
	manifest := registryManifest{
		Version: "v2026.04.10",
		Packs: registryPacks{
			Providers: providerPack{Path: "providers.toml", SHA256: bytesSHA256([]byte(providersBody))},
			Skills:    skillPack{Path: "skills/", Files: map[string]string{"skill.md": bytesSHA256([]byte(skillBody))}},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			_ = json.NewEncoder(w).Encode(latestRelease{TagName: "v2026.04.10"})
		case "/registry/manifest.json":
			_ = json.NewEncoder(w).Encode(manifest)
		case "/registry/providers.toml":
			_, _ = w.Write([]byte(providersBody))
		case "/registry/skills/skill.md":
			_, _ = w.Write([]byte(skillBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	version = "2026.04.05"
	updateCheckURL = server.URL + "/releases/latest"
	updateHTTPClient = server.Client()
	updateRegistryURL = server.URL + "/registry/manifest.json"
	calledBinary := false
	calledMaintenance := false
	updateBinaryFunc = func(ctx context.Context, rel latestRelease, skipConfirm bool) error {
		calledBinary = true
		return nil
	}
	updateMaintenance = func(path string) error {
		calledMaintenance = true
		return nil
	}

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"upgrade", "all", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute: %v", err)
	}
	if !calledBinary || !calledMaintenance {
		t.Fatalf("expected upgrade all to run full workflow, binary=%v maintenance=%v", calledBinary, calledMaintenance)
	}
}

func TestBinaryNameCurrentPlatformFormat(t *testing.T) {
	got := binaryName()
	want := "roboticus-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got != want {
		t.Fatalf("binaryName() = %q, want %q", got, want)
	}
}
