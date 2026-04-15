package updatecmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	if got := ResolveRegistryURL(configPath); got != "https://config.example/manifest.json" {
		t.Fatalf("ResolveRegistryURL from config = %q", got)
	}

	t.Setenv("ROBOTICUS_REGISTRY_URL", "https://env.example/manifest.json")
	if got := ResolveRegistryURL(configPath); got != "https://env.example/manifest.json" {
		t.Fatalf("ResolveRegistryURL from env = %q", got)
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
	manifest := RegistryManifest{
		Version: "v2026.04.10",
		Packs: RegistryPacks{
			Providers: ProviderPack{
				Path:   "packs/providers.toml",
				SHA256: BytesSHA256([]byte(providersBody)),
			},
			Skills: SkillPack{
				Path: "packs/skills/",
				Files: map[string]string{
					"hello.md": BytesSHA256([]byte(skillBody)),
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

// TestApplyProvidersUpdate_MismatchErrorIsSelfDescribing locks in the
// post-incident debuggability contract: when the registry manifest's
// declared SHA256 doesn't match the bytes actually served for the
// provider pack, the resulting error string must carry enough
// information to triage the publishing pipeline without re-running curl
// by hand. Specifically: the URL fetched, the hash the manifest
// declared (expected), and the hash computed from the downloaded bytes
// (received) all appear in the error message.
//
// The same kind of mismatch surfaced as a bare "provider pack checksum
// mismatch" before this regression existed — that wording made
// operators conflate the binary update's "Checksum verified." message
// with the providers step's verification, since the providers step
// silently failed without saying which URL it had pulled from. The
// assertions below intentionally check for the URL, the expected hash,
// and the received hash literally so any future shortening of the
// error message would surface here.
func TestApplyProvidersUpdate_MismatchErrorIsSelfDescribing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "roboticus.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	// The manifest declares a SHA for bytes that DO NOT match what the
	// pack URL will actually serve. This is the exact shape of the bug
	// reported against `roboticus upgrade all` when the publishing
	// pipeline forgets to regenerate the manifest hash.
	declaredHash := BytesSHA256([]byte("the manifest claims THIS is the pack body"))
	servedBody := "but the URL serves THESE bytes instead"

	manifest := RegistryManifest{
		Version: "v2026.04.10",
		Packs: RegistryPacks{
			Providers: ProviderPack{
				Path:   "packs/providers.toml",
				SHA256: declaredHash,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		case "/registry/packs/providers.toml":
			_, _ = w.Write([]byte(servedBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registryURL := server.URL + "/registry/manifest.json"
	_, err := applyProvidersUpdate(context.Background(), registryURL, configPath)
	if err == nil {
		t.Fatalf("expected mismatch error; got nil")
	}
	msg := err.Error()
	expectedURL := server.URL + "/registry/packs/providers.toml"
	receivedHash := BytesSHA256([]byte(servedBody))
	for _, want := range []string{
		"checksum mismatch",
		"url=" + expectedURL,
		"expected=" + declaredHash,
		"received=" + receivedHash,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message must contain %q for triage; got %q", want, msg)
		}
	}
}

// TestApplySkillsUpdate_MismatchErrorIsSelfDescribing extends the same
// debuggability contract to skills. The skills path can mismatch on a
// per-file basis (each skill has its own hash entry in the manifest),
// so the error must also identify which specific skill file failed —
// that's the only signal that tells operators whether the publishing
// pipeline mishandled one file or the whole pack.
func TestApplySkillsUpdate_MismatchErrorIsSelfDescribing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "roboticus.toml")
	skillsDir := filepath.Join(home, "skills")
	if err := os.WriteFile(configPath, []byte("[skills]\ndirectory = \""+skillsDir+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	declaredHash := BytesSHA256([]byte("the manifest claims THIS is the skill body"))
	servedBody := "but the skill URL serves THESE bytes instead"

	manifest := RegistryManifest{
		Version: "v2026.04.10",
		Packs: RegistryPacks{
			Skills: SkillPack{
				Path: "packs/skills/",
				Files: map[string]string{
					"misaligned-skill.md": declaredHash,
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		case "/registry/packs/skills/misaligned-skill.md":
			_, _ = w.Write([]byte(servedBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registryURL := server.URL + "/registry/manifest.json"
	_, err := applySkillsUpdate(context.Background(), registryURL, configPath)
	if err == nil {
		t.Fatalf("expected skill mismatch error; got nil")
	}
	msg := err.Error()
	expectedURL := server.URL + "/registry/packs/skills/misaligned-skill.md"
	receivedHash := BytesSHA256([]byte(servedBody))
	for _, want := range []string{
		"misaligned-skill.md",
		"checksum mismatch",
		"url=" + expectedURL,
		"expected=" + declaredHash,
		"received=" + receivedHash,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("skill error message must contain %q for triage; got %q", want, msg)
		}
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
	t.Setenv("ROBOTICUS_CONFIG", configPath)



	binaryBytes := []byte("fake binary")
	binaryHash := sha256.Sum256(binaryBytes)
	checksumBody := hex.EncodeToString(binaryHash[:]) + "  " + binaryName() + "\n"
	providersBody := "[providers.ollama]\nurl = \"http://localhost:11434\"\n"
	skillBody := "# Example skill\n"
	manifest := RegistryManifest{
		Version: "v2026.04.10",
		Packs: RegistryPacks{
			Providers: ProviderPack{
				Path:   "providers.toml",
				SHA256: BytesSHA256([]byte(providersBody)),
			},
			Skills: SkillPack{
				Path: "skills/",
				Files: map[string]string{
					"example.md": BytesSHA256([]byte(skillBody)),
				},
			},
		},
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(LatestRelease{
				TagName: "v2026.04.10",
				Assets: []ReleaseAsset{
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
	origClient := UpdateHTTPClient
	origRegistryURL := UpdateRegistryURL
	origBinaryFunc := updateBinaryFunc
	origMaintenance := updateMaintenance
	updateCheckURL = server.URL + "/releases/latest"
	UpdateHTTPClient = server.Client()
	UpdateRegistryURL = server.URL + "/registry/manifest.json"
	calledBinary := false
	calledMaintenance := false
	updateBinaryFunc = func(ctx context.Context, rel LatestRelease, skipConfirm bool) error {
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
		UpdateHTTPClient = origClient
		UpdateRegistryURL = origRegistryURL
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
