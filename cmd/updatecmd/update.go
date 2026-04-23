package updatecmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"roboticus/cmd/internal/cmdutil"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"roboticus/internal/core"
)

var (
	updateCheckURL    = "https://api.github.com/repos/robot-accomplice/roboticus/releases/latest"
	UpdateHTTPClient  = &http.Client{Timeout: 30 * time.Second}
	updateReleasesURL = "https://github.com/robot-accomplice/roboticus/releases"
	UpdateRegistryURL = "https://roboticus.ai/registry/manifest.json"
	updateBinaryFunc  = performUpdate
	updateMaintenance = runUpdateMaintenance
)

type LatestRelease struct {
	TagName    string         `json:"tag_name"`
	HTMLURL    string         `json:"html_url"`
	Assets     []ReleaseAsset `json:"assets"`
	AssetsURL  string         `json:"assets_url"`
	TarballURL string         `json:"tarball_url"`
}

type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type RegistryManifest struct {
	Version string        `json:"version"`
	Packs   RegistryPacks `json:"packs"`
}

type RegistryPacks struct {
	Providers ProviderPack   `json:"providers"`
	Skills    SkillPack      `json:"skills"`
	Plugins   *PluginCatalog `json:"plugins,omitempty"`
}

type ProviderPack struct {
	SHA256 string `json:"sha256"`
	Path   string `json:"path"`
}

type SkillPack struct {
	SHA256 string            `json:"sha256"`
	Path   string            `json:"path"`
	Files  map[string]string `json:"files"`
}

type PluginCatalog struct {
	Catalog []PluginCatalogEntry `json:"catalog"`
}

type PluginCatalogEntry struct {
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description string  `json:"description"`
	Author      string  `json:"author"`
	SHA256      string  `json:"sha256"`
	Path        string  `json:"path"`
	MinVersion  *string `json:"min_version,omitempty"`
	Tier        string  `json:"tier"`
}

type updateState struct {
	BinaryVersion    string           `json:"binary_version"`
	LastCheck        string           `json:"last_check"`
	RegistryURL      string           `json:"registry_url"`
	InstalledContent installedContent `json:"installed_content"`
}

type installedContent struct {
	Providers *contentRecord `json:"providers,omitempty"`
	Skills    *skillsRecord  `json:"skills,omitempty"`
}

type contentRecord struct {
	Version     string `json:"version"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
}

type skillsRecord struct {
	Version     string            `json:"version"`
	Files       map[string]string `json:"files"`
	InstalledAt string            `json:"installed_at"`
}

type rawUpdateConfig struct {
	Update struct {
		RegistryURL string `toml:"registry_url"`
	} `toml:"update"`
	ProvidersFile string `toml:"providers_file"`
	Skills        struct {
		Directory string `toml:"directory"`
	} `toml:"skills"`
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func compareVersions(a, b string) int {
	a = normalizeVersion(a)
	b = normalizeVersion(b)
	if a == b {
		return 0
	}

	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		aPart := "0"
		bPart := "0"
		if i < len(aParts) {
			aPart = aParts[i]
		}
		if i < len(bParts) {
			bPart = bParts[i]
		}
		aNum, aErr := strconv.Atoi(aPart)
		bNum, bErr := strconv.Atoi(bPart)
		if aErr == nil && bErr == nil {
			switch {
			case aNum < bNum:
				return -1
			case aNum > bNum:
				return 1
			default:
				continue
			}
		}
		switch {
		case aPart < bPart:
			return -1
		case aPart > bPart:
			return 1
		}
	}
	return 0
}

func roboticusHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".roboticus"
	}
	return filepath.Join(home, ".roboticus")
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func updateStatePath() string {
	return filepath.Join(roboticusHome(), "update_state.json")
}

func legacyUpdateStatePath() string {
	return filepath.Join(roboticusHome(), "update-state.json")
}

func loadUpdateState() (updateState, error) {
	path := updateStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			legacyData, legacyErr := os.ReadFile(legacyUpdateStatePath())
			if legacyErr != nil {
				if os.IsNotExist(legacyErr) {
					return updateState{}, nil
				}
				return updateState{}, legacyErr
			}
			data = legacyData
		} else {
			return updateState{}, err
		}
	}
	var state updateState
	if err := json.Unmarshal(data, &state); err != nil {
		return updateState{}, err
	}
	return state, nil
}

func saveUpdateState(state updateState) error {
	path := updateStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func fileSHA256(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	return BytesSHA256(data), info.ModTime().UTC().Format(time.RFC3339), nil
}

func skillHashes(dir string) (map[string]string, string, error) {
	hashes := map[string]string{}
	var latest time.Time
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		hashes[filepath.ToSlash(rel)] = BytesSHA256(data)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if len(hashes) == 0 {
		return hashes, "", nil
	}
	return hashes, latest.UTC().Format(time.RFC3339), nil
}

func needsProviderStateRepair(rec *contentRecord) bool {
	return rec == nil || strings.TrimSpace(rec.Version) == "" || strings.TrimSpace(rec.SHA256) == "" || strings.TrimSpace(rec.InstalledAt) == ""
}

func needsSkillsStateRepair(rec *skillsRecord) bool {
	return rec == nil || strings.TrimSpace(rec.Version) == "" || strings.TrimSpace(rec.InstalledAt) == "" || len(rec.Files) == 0
}

func reconcileUpdateState(configPath, currentVersion string) (updateState, bool, error) {
	state, err := loadUpdateState()
	if err != nil {
		return updateState{}, false, err
	}
	changed := false

	if state.BinaryVersion == "" && strings.TrimSpace(currentVersion) != "" {
		state.BinaryVersion = normalizeVersion(currentVersion)
		changed = true
	}
	if state.RegistryURL == "" {
		state.RegistryURL = ResolveRegistryURL(configPath)
		changed = true
	}

	if needsProviderStateRepair(state.InstalledContent.Providers) {
		providersPath := providersLocalPath(configPath)
		if _, err := os.Stat(providersPath); err == nil {
			hash, installedAt, err := fileSHA256(providersPath)
			if err != nil {
				return updateState{}, false, err
			}
			state.InstalledContent.Providers = &contentRecord{
				Version:     "unknown",
				SHA256:      hash,
				InstalledAt: installedAt,
			}
			changed = true
		}
	}

	if needsSkillsStateRepair(state.InstalledContent.Skills) {
		skillsDir := skillsLocalDir(configPath)
		if info, err := os.Stat(skillsDir); err == nil && info.IsDir() {
			hashes, installedAt, err := skillHashes(skillsDir)
			if err != nil {
				return updateState{}, false, err
			}
			if len(hashes) > 0 {
				state.InstalledContent.Skills = &skillsRecord{
					Version:     "unknown",
					Files:       hashes,
					InstalledAt: installedAt,
				}
				changed = true
			}
		}
	}

	if !changed {
		return state, false, nil
	}
	if state.LastCheck == "" {
		state.LastCheck = nowRFC3339()
	}
	return state, true, saveUpdateState(state)
}

func loadRawUpdateConfig(path string) (rawUpdateConfig, error) {
	var cfg rawUpdateConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return rawUpdateConfig{}, err
	}
	return cfg, nil
}

func ResolveRegistryURL(configPath string) string {
	if val := strings.TrimSpace(os.Getenv("ROBOTICUS_REGISTRY_URL")); val != "" {
		return val
	}
	raw, err := loadRawUpdateConfig(configPath)
	if err == nil && strings.TrimSpace(raw.Update.RegistryURL) != "" {
		return strings.TrimSpace(raw.Update.RegistryURL)
	}
	return UpdateRegistryURL
}

func providersLocalPath(configPath string) string {
	raw, err := loadRawUpdateConfig(configPath)
	if err == nil && strings.TrimSpace(raw.ProvidersFile) != "" {
		return strings.TrimSpace(raw.ProvidersFile)
	}
	return filepath.Join(roboticusHome(), "providers.toml")
}

func skillsLocalDir(configPath string) string {
	raw, err := loadRawUpdateConfig(configPath)
	if err == nil && strings.TrimSpace(raw.Skills.Directory) != "" {
		return strings.TrimSpace(raw.Skills.Directory)
	}
	return filepath.Join(roboticusHome(), "skills")
}

func registryBaseURL(manifestURL string) string {
	if idx := strings.LastIndex(manifestURL, "/"); idx >= 0 {
		return manifestURL[:idx]
	}
	return manifestURL
}

func BytesSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func FetchRegistryManifest(ctx context.Context, manifestURL string) (RegistryManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return RegistryManifest{}, err
	}
	resp, err := UpdateHTTPClient.Do(req)
	if err != nil {
		return RegistryManifest{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return RegistryManifest{}, fmt.Errorf("registry manifest returned %s", resp.Status)
	}
	var manifest RegistryManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return RegistryManifest{}, err
	}
	if manifest.Version == "" {
		return RegistryManifest{}, fmt.Errorf("registry manifest missing version")
	}
	return manifest, nil
}

func fetchText(ctx context.Context, url string) (string, error) {
	path, err := downloadFile(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(path) }()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func safeSkillPath(baseDir, name string) bool {
	if strings.Contains(name, "..") || filepath.IsAbs(name) {
		return false
	}
	cleanBase := filepath.Clean(baseDir)
	full := filepath.Clean(filepath.Join(cleanBase, name))
	return full == cleanBase || strings.HasPrefix(full, cleanBase+string(os.PathSeparator))
}

// applyProvidersUpdate writes the registry's providers.toml only when the
// caller explicitly asks for a refresh OR there is no local copy yet.
//
// Design principle (set 2026-04-15 after the v1.0.5 incident): SHA
// verification is appropriate for IMMUTABLE downloaded artifacts — the
// binary, plugin archives, theme bundles. It is NOT appropriate for living
// configuration the user edits — providers.toml, skills/*.md. Once a user
// adds an API key, changes a tier, or adds a custom provider, the local
// hash will diverge from the registry's by design. Force-overwriting on
// every `roboticus upgrade all` would silently destroy that customization;
// SHA-locking it would surface a "checksum mismatch" failure that's just
// noise (the file is supposed to drift).
//
// Behavior matrix:
//
//	refreshConfig=false, local file exists  → SKIP download entirely.
//	                                           No fetch, no SHA check, no
//	                                           error. Print preservation
//	                                           notice. Returns changed=false.
//	refreshConfig=false, local file missing → FRESH INSTALL: download +
//	                                           verify + write. SHA check
//	                                           is meaningful here because
//	                                           there is no user content to
//	                                           clobber.
//	refreshConfig=true                       → ALWAYS download + verify +
//	                                           write, overwriting any local
//	                                           edits. The caller passing
//	                                           refreshConfig=true is the
//	                                           user opting in.
//
// SHA verification only fires on the download paths (cases 2 and 3); the
// preservation path skips it entirely so a stale registry SHA can't break
// a customized install.
func applyProvidersUpdate(ctx context.Context, registryURL, configPath string, refreshConfig bool) (bool, error) {
	path := providersLocalPath(configPath)
	if !refreshConfig {
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("Provider config preserved at %s (use --refresh-config to overwrite from registry).\n", path)
			return false, nil
		}
	}

	manifest, err := FetchRegistryManifest(ctx, registryURL)
	if err != nil {
		return false, err
	}
	remoteURL := registryBaseURL(registryURL) + "/" + strings.TrimPrefix(manifest.Packs.Providers.Path, "/")
	// Symmetric narration with the binary update path, which prints
	// "Downloading roboticus-darwin-arm64..." before the fetch. Without
	// this, a checksum-mismatch failure right after the binary's
	// "Checksum verified." line reads as if the same verification flipped
	// outcome — they are actually two separate verifications against two
	// separate sources of truth.
	fmt.Printf("Downloading provider pack from %s...\n", remoteURL)
	content, err := fetchText(ctx, remoteURL)
	if err != nil {
		return false, err
	}
	hash := BytesSHA256([]byte(content))
	if manifest.Packs.Providers.SHA256 != "" && !strings.EqualFold(hash, manifest.Packs.Providers.SHA256) {
		// Self-describing mismatch error: surface the URL fetched, the
		// hash the registry manifest declared, and the hash actually
		// computed from the downloaded bytes. An operator hitting this
		// can decide in one read whether the registry is publishing a
		// stale manifest or the download is being mutated in transit,
		// without needing to re-run curl by hand.
		return false, fmt.Errorf("provider pack checksum mismatch: url=%s expected=%s received=%s",
			remoteURL, manifest.Packs.Providers.SHA256, hash)
	}
	fmt.Println("Provider pack checksum verified.")

	if data, err := os.ReadFile(path); err == nil && BytesSHA256(data) == hash {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return false, err
	}

	state, err := loadUpdateState()
	if err != nil {
		return false, err
	}
	state.LastCheck = nowRFC3339()
	state.RegistryURL = registryURL
	state.InstalledContent.Providers = &contentRecord{
		Version:     manifest.Version,
		SHA256:      hash,
		InstalledAt: state.LastCheck,
	}
	return true, saveUpdateState(state)
}

// applySkillsUpdate applies the same fresh-install + opt-in-refresh
// principle as applyProvidersUpdate, but per-file: each skill manifest
// entry is independently preserved (when a local copy exists and the
// caller didn't pass refreshConfig) or refreshed (fresh install or
// explicit refresh).
//
// A user can have authored their own custom skill files under the same
// directory, intermixed with registry-published ones. The per-file
// preservation path makes sure those custom skills are never observed by
// the upgrade pipeline at all — we only look at filenames the manifest
// declares, and even those we leave alone unless they're missing or the
// caller asked for a refresh.
//
// SHA verification semantics match providers: only fires on download
// paths (fresh install or refresh), never on the preservation path.
func applySkillsUpdate(ctx context.Context, registryURL, configPath string, refreshConfig bool) (bool, error) {
	manifest, err := FetchRegistryManifest(ctx, registryURL)
	if err != nil {
		return false, err
	}
	dir := skillsLocalDir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}

	state, err := loadUpdateState()
	if err != nil {
		return false, err
	}
	baseURL := registryBaseURL(registryURL)
	fileHashes := map[string]string{}
	if state.InstalledContent.Skills != nil {
		for name, hash := range state.InstalledContent.Skills.Files {
			fileHashes[name] = hash
		}
	}

	// Tally counts so we can emit one summary line at the end rather
	// than narrating every single skill file. Per-file narration would
	// flood the operator's terminal on first install (manifests can list
	// dozens of skills); the summary keeps the signal without losing
	// observability.
	downloaded := 0
	cached := 0
	preserved := 0
	changed := false
	for name, expectedHash := range manifest.Packs.Skills.Files {
		if !safeSkillPath(dir, name) {
			return false, fmt.Errorf("unsafe skill path %q", name)
		}
		path := filepath.Join(dir, name)

		// Preservation path: a local copy exists and the caller did not
		// request a refresh. Skip the download entirely so user edits
		// are never observed (and therefore never flagged as drift).
		// We deliberately do NOT recompute the local hash here — that
		// would defeat the purpose of treating the file as user-owned.
		if !refreshConfig {
			if _, statErr := os.Stat(path); statErr == nil {
				preserved++
				if existing, ok := fileHashes[name]; ok && existing != "" {
					// keep prior recorded hash unchanged
				} else {
					// First-seen preserved file: stash the manifest's
					// expected hash as a soft reference so the state
					// file still tells operators which version was the
					// most recent registry-known version of this file.
					fileHashes[name] = expectedHash
				}
				continue
			}
		}

		// Already-cached path: local copy exists AND its bytes match
		// the manifest's expected hash exactly. This still applies on
		// refreshConfig runs — re-downloading bytes that already match
		// would be wasted work, and it also avoids a noisy "downloaded
		// X" message when nothing actually changed.
		if data, err := os.ReadFile(path); err == nil && strings.EqualFold(BytesSHA256(data), expectedHash) {
			fileHashes[name] = expectedHash
			cached++
			continue
		}

		remoteURL := baseURL + "/" + strings.TrimPrefix(manifest.Packs.Skills.Path, "/") + name
		content, err := fetchText(ctx, remoteURL)
		if err != nil {
			return false, err
		}
		hash := BytesSHA256([]byte(content))
		if expectedHash != "" && !strings.EqualFold(hash, expectedHash) {
			// Self-describing mismatch error matches the providers-pack
			// shape: skill name, URL, expected hash, received hash.
			// Anyone debugging the registry needs all four to confirm
			// whether the manifest is stale or the file was mutated in
			// transit.
			return false, fmt.Errorf("skill %s checksum mismatch: url=%s expected=%s received=%s",
				name, remoteURL, expectedHash, hash)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return false, err
		}
		fileHashes[name] = hash
		downloaded++
		changed = true
	}

	// Symmetric end-of-step narration with the providers-pack
	// "Provider pack checksum verified." line. Quiet on no-op runs
	// (nothing downloaded AND nothing newly cached AND nothing
	// preserved) so subsequent re-runs don't spam the operator with
	// redundant chatter.
	switch {
	case downloaded == 0 && preserved == 0 && cached == 0:
		// no-op
	case downloaded == 0 && preserved > 0:
		fmt.Printf("Skills preserved (%d file(s) kept; use --refresh-config to overwrite from registry).\n", preserved)
	case downloaded == 0:
		fmt.Printf("Skill pack already current (%d files cached).\n", cached)
	case cached == 0 && preserved == 0:
		fmt.Printf("Skill pack updated: %d file(s) downloaded and verified.\n", downloaded)
	default:
		fmt.Printf("Skill pack updated: %d downloaded, %d cached, %d preserved.\n", downloaded, cached, preserved)
	}

	state.LastCheck = nowRFC3339()
	state.RegistryURL = registryURL
	state.InstalledContent.Skills = &skillsRecord{
		Version:     manifest.Version,
		Files:       fileHashes,
		InstalledAt: state.LastCheck,
	}
	return changed, saveUpdateState(state)
}

func runUpdateAll(ctx context.Context, currentVersion string, yes, refreshConfig bool) error {
	fmt.Printf("roboticus %s (%s/%s)\n", currentVersion, runtime.GOOS, runtime.GOARCH)

	rel, upToDate, err := checkForUpdate(ctx, currentVersion)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	configPath := cmdutil.EffectiveConfigPath()
	registryURL := ResolveRegistryURL(configPath)
	if _, repaired, err := reconcileUpdateState(configPath, currentVersion); err != nil {
		return fmt.Errorf("failed to reconcile updater state: %w", err)
	} else if repaired {
		fmt.Println("Recovered updater state from existing local install.")
	}
	binaryChanged := false
	if !upToDate || normalizeVersion(currentVersion) == "dev" {
		if err := updateBinaryFunc(ctx, rel, yes); err != nil {
			return err
		}
		binaryChanged = true
	}

	// Load state to check for already-completed steps (resumability).
	prevState, _ := loadUpdateState()
	manifest, manifestErr := FetchRegistryManifest(ctx, registryURL)

	// The "already at version X" short-circuits below ONLY apply when the
	// caller didn't ask for a refresh. With --refresh-config the user has
	// explicitly opted in to re-pulling the registry-published file, so we
	// must run the underlying apply* function rather than skipping it on
	// the basis of a matching state-file version.
	var providersChanged bool
	if !refreshConfig && manifestErr == nil && prevState.InstalledContent.Providers != nil &&
		prevState.InstalledContent.Providers.Version == manifest.Version {
		fmt.Println("Provider pack already at version " + manifest.Version + ", skipping.")
		providersChanged = false
	} else {
		var pErr error
		providersChanged, pErr = applyProvidersUpdate(ctx, registryURL, configPath, refreshConfig)
		if pErr != nil {
			if refreshConfig {
				return fmt.Errorf("provider update failed: %w", pErr)
			}
			fmt.Printf("Warning: provider pack refresh skipped: %v\n", pErr)
			providersChanged = false
		}
	}

	var skillsChanged bool
	if !refreshConfig && manifestErr == nil && prevState.InstalledContent.Skills != nil &&
		prevState.InstalledContent.Skills.Version == manifest.Version {
		fmt.Println("Skills pack already at version " + manifest.Version + ", skipping.")
		skillsChanged = false
	} else {
		var sErr error
		skillsChanged, sErr = applySkillsUpdate(ctx, registryURL, configPath, refreshConfig)
		if sErr != nil {
			if refreshConfig {
				return fmt.Errorf("skills update failed: %w", sErr)
			}
			fmt.Printf("Warning: skills refresh skipped: %v\n", sErr)
			skillsChanged = false
		}
	}

	state, err := loadUpdateState()
	if err != nil {
		return err
	}
	state.LastCheck = nowRFC3339()
	state.RegistryURL = registryURL
	if binaryChanged {
		state.BinaryVersion = normalizeVersion(rel.TagName)
	}
	if err := saveUpdateState(state); err != nil {
		return err
	}
	if err := updateMaintenance(configPath); err != nil {
		return fmt.Errorf("post-update maintenance failed: %w", err)
	}

	if !binaryChanged && !providersChanged && !skillsChanged {
		fmt.Println("Already up to date.")
	}
	return nil
}

// collectFirmwarePaths returns all candidate locations where FIRMWARE.toml
// might live for a given configPath, in priority order. The primary path
// is the configured agent workspace (where setup.go writes new firmware).
// The secondary path is filepath.Dir(configPath) — legacy pre-workspace
// installs left firmware there, and we still want to migrate those during
// a maintenance pass so stale firmware on disk doesn't break the runtime.
// Returns unique paths; deduplicates when workspace == parent-of-config.
func collectFirmwarePaths(configPath string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		path := filepath.Join(dir, "FIRMWARE.toml")
		if _, dup := seen[path]; dup {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	// Primary: configured workspace (operator intent).
	if cfg, err := core.LoadConfigFromFile(configPath); err == nil {
		add(cfg.Agent.Workspace)
	}
	// Secondary: legacy parent-of-config location.
	add(filepath.Dir(configPath))
	return out
}

func runUpdateMaintenance(configPath string) error {
	// Step 1: Create a backup before any modifications.
	backupPath, err := core.BackupConfigFile(configPath, 10, 30)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: config backup failed: %v\n", err)
	} else if backupPath != "" {
		fmt.Printf("Config backed up to: %s\n", backupPath)
	}

	// Step 2: Legacy config migrations.
	if err := applyRemovedLegacyConfigMigration(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: legacy config migration: %v\n", err)
	}
	if err := applySecurityConfigMigration(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: security config migration: %v\n", err)
	}

	// Step 3: Firmware rules migration (TOML [[rules]] → [rules] table format).
	//
	// The personality setup flow writes FIRMWARE.toml to cfg.Agent.Workspace
	// (see cmd/configcmd/setup.go). Pre-v1.0.6 this maintenance path looked
	// for FIRMWARE.toml in filepath.Dir(configPath), which is the CONFIG
	// directory (~/.roboticus), not the WORKSPACE directory (typically
	// ~/.roboticus/workspace or operator-configured). That meant migration
	// silently skipped every operator who had a custom workspace — their
	// firmware stayed on the old [[rules]] format while the runtime moved
	// to [rules] table format.
	//
	// The fix is to resolve the workspace path from the loaded config. If
	// the config can't be loaded (e.g., first-time init or corrupt toml),
	// we fall back to the legacy parent-of-config path so pre-workspace
	// installs still migrate. As a safety net we ALSO attempt migration on
	// any stale FIRMWARE.toml that lingered in the config dir from older
	// versions — old file, new codebase should still migrate cleanly.
	for _, firmwarePath := range collectFirmwarePaths(configPath) {
		if _, err := os.Stat(firmwarePath); err != nil {
			continue
		}
		if migErr := migrateFirmwareRules(firmwarePath); migErr != nil {
			fmt.Fprintf(os.Stderr, "warning: firmware migration at %s: %v\n", firmwarePath, migErr)
		} else {
			fmt.Printf("Firmware rules migration: OK (%s)\n", firmwarePath)
		}
	}

	// Step 4: OAuth token refresh (for providers using OAuth PKCE).
	refreshOAuthTokens()

	// Step 5: Post-update health check.
	if data, err := cmdutil.APIGet("/api/health"); err == nil {
		if status, ok := data["status"].(string); ok && status == "ok" {
			fmt.Println("Post-update health check: OK")
		} else {
			fmt.Fprintln(os.Stderr, "warning: post-update health check returned non-OK status")
		}
	}

	return nil
}

// migrateFirmwareRules converts legacy [[rules]] TOML arrays to [rules] table format.
// This handles the firmware format change between Rust versions.
func migrateFirmwareRules(firmwarePath string) error {
	data, err := os.ReadFile(firmwarePath)
	if err != nil {
		return err
	}

	content := string(data)
	// Check if migration is needed: [[rules]] is the old format.
	if !strings.Contains(content, "[[rules]]") {
		return nil // Already in new format or no rules.
	}

	// Back up firmware before modification.
	backupPath := firmwarePath + ".bak." + time.Now().UTC().Format("20060102-150405")
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return fmt.Errorf("backup firmware: %w", err)
	}
	fmt.Printf("Firmware backed up to: %s\n", backupPath)

	// Parse and re-serialize to normalize format.
	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parse firmware TOML: %w", err)
	}

	// Re-marshal to normalize the format.
	out, err := toml.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("re-serialize firmware: %w", err)
	}

	return os.WriteFile(firmwarePath, out, 0o644)
}

// refreshOAuthTokens attempts to refresh OAuth tokens for providers that use them.
// This runs during update maintenance to ensure tokens don't expire silently.
func refreshOAuthTokens() {
	data, err := cmdutil.APIGet("/api/config")
	if err != nil {
		return // Server not running — skip.
	}
	providers, _ := data["providers"].(map[string]any)
	for name, v := range providers {
		pm, _ := v.(map[string]any)
		authMode, _ := pm["auth_mode"].(string)
		if authMode != "oauth" {
			continue
		}
		// Check if provider has a refresh token in keystore.
		ksData, err := cmdutil.APIGet("/api/keystore/status")
		if err != nil {
			continue
		}
		keys, _ := ksData["keys"].(map[string]any)
		refreshKey := name + "_refresh_token"
		if _, hasRefresh := keys[refreshKey]; !hasRefresh {
			continue
		}
		fmt.Printf("Refreshing OAuth token for %s...\n", name)
		// The actual refresh would need the token URL and client ID.
		// For now, log the intent — full implementation requires provider config.
		fmt.Printf("  (Token refresh for %s would require provider-specific config)\n", name)
	}
}

func applyRemovedLegacyConfigMigration(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "allowed_models") {
			changed = true
			continue
		}
		filtered = append(filtered, line)
	}
	if !changed {
		return nil
	}
	return os.WriteFile(configPath, []byte(strings.Join(filtered, "\n")), 0o600)
}

func applySecurityConfigMigration(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.Contains(content, "\n[security]\n") || strings.HasPrefix(content, "[security]\n") {
		return nil
	}

	section := "\n" +
		"# Security defaults added during update migration.\n" +
		"[security]\n" +
		"deny_on_empty_allowlist = true\n" +
		"workspace_only = true\n" +
		"threat_caution_ceiling = \"external\"\n"

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(configPath, []byte(content+section), 0o600)
}

func checkForUpdate(ctx context.Context, currentVersion string) (LatestRelease, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateCheckURL, nil)
	if err != nil {
		return LatestRelease{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := UpdateHTTPClient.Do(req)
	if err != nil {
		return LatestRelease{}, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return LatestRelease{}, false, fmt.Errorf("update check returned %s", resp.Status)
	}

	var rel LatestRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return LatestRelease{}, false, err
	}
	if rel.HTMLURL == "" {
		rel.HTMLURL = updateReleasesURL
	}
	if rel.TagName == "" {
		return LatestRelease{}, false, fmt.Errorf("latest release response missing tag_name")
	}

	normalizedCurrent := normalizeVersion(currentVersion)
	normalizedLatest := normalizeVersion(rel.TagName)
	upToDate := normalizedCurrent != "dev" && compareVersions(normalizedCurrent, normalizedLatest) >= 0
	return rel, upToDate, nil
}

// binaryName returns the expected release artifact name for the current platform.
func binaryName() string {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	return fmt.Sprintf("roboticus-%s-%s%s", runtime.GOOS, runtime.GOARCH, suffix)
}

// findAssetURL locates the download URL for a named asset in a release.
func findAssetURL(rel LatestRelease, name string) (string, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("release %s has no asset named %q", rel.TagName, name)
}

// downloadFile fetches a URL into a temp file and returns its path.
func downloadFile(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := UpdateHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s returned %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp("", "roboticus-update-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// parseChecksumFile reads a SHA256SUMS.txt file and returns a map of filename→hex-hash.
func parseChecksumFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// GNU coreutils format: "<hash>  <filename>" or "<hash> <filename>"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			result[parts[1]] = parts[0]
		}
	}
	return result, scanner.Err()
}

// verifyChecksum computes SHA256 of a file and compares against expected hex hash.
func verifyChecksum(path, expectedHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expectedHex) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHex, actual)
	}
	return nil
}

// performUpdate downloads the latest release, verifies its checksum, and replaces
// the running binary.
func performUpdate(ctx context.Context, rel LatestRelease, skipConfirm bool) error {
	name := binaryName()

	// Locate binary asset.
	binaryURL, err := findAssetURL(rel, name)
	if err != nil {
		return err
	}

	// Locate checksum asset.
	checksumURL, err := findAssetURL(rel, "SHA256SUMS.txt")
	if err != nil {
		return fmt.Errorf("release %s missing SHA256SUMS.txt: %w", rel.TagName, err)
	}

	if !skipConfirm {
		fmt.Printf("Update to %s? [y/N] ", rel.TagName)
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(scanner.Text())), "y") {
			fmt.Println("Update cancelled.")
			return nil
		}
	}

	// Download checksum file.
	fmt.Printf("Downloading checksums...\n")
	checksumPath, err := downloadFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	defer func() { _ = os.Remove(checksumPath) }()

	checksums, err := parseChecksumFile(checksumPath)
	if err != nil {
		return fmt.Errorf("failed to parse checksums: %w", err)
	}
	expectedHash, ok := checksums[name]
	if !ok {
		return fmt.Errorf("SHA256SUMS.txt has no entry for %s", name)
	}

	// Download binary.
	fmt.Printf("Downloading %s...\n", name)
	binaryPath, err := downloadFile(ctx, binaryURL)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer func() { _ = os.Remove(binaryPath) }()

	// Verify checksum.
	if err := verifyChecksum(binaryPath, expectedHash); err != nil {
		return fmt.Errorf("binary verification failed: %w", err)
	}
	fmt.Println("Checksum verified.")

	// Make executable.
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Replace the running binary atomically.
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Move the downloaded binary into the same directory as the current binary,
	// then replace it. Same-filesystem rename is required for atomicity.
	//
	// OS boundary: on Unix, a running binary can be os.Rename()'d over in place
	// — the kernel keeps the old inode alive for the running process while the
	// new file takes the name. Windows does NOT allow that: a running .exe
	// holds an exclusive write lock and a direct rename over it fails with
	// "The process cannot access the file because it is being used by another
	// process." Windows *does* allow renaming the running exe itself (the lock
	// is on the open handle, not the directory entry), so the canonical
	// Windows dance is: move running exe aside (→ <path>.old), move staged exe
	// into place, schedule .old for delete-on-reboot. That platform-specific
	// dance lives in replaceRunningBinary (see update_unix.go /
	// update_windows.go). Previously this code path used a bare os.Rename
	// everywhere, which is why the v1.0.6 architecture audit flagged Windows
	// self-update as pre-release broken.
	destDir := filepath.Dir(execPath)
	stagePath := filepath.Join(destDir, ".roboticus-update-staging")
	if err := copyFile(binaryPath, stagePath); err != nil {
		return fmt.Errorf("failed to stage update: %w", err)
	}
	if err := os.Chmod(stagePath, 0o755); err != nil {
		_ = os.Remove(stagePath)
		return fmt.Errorf("failed to set permissions on staged binary: %w", err)
	}
	if err := replaceRunningBinary(stagePath, execPath); err != nil {
		_ = os.Remove(stagePath)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("Updated to %s.\n", rel.TagName)
	return nil
}

// copyFile copies src to dst using read+write (works across filesystems).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// --- Commands ---

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for updates or update the binary",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default behavior when no subcommand: check for updates.
		return updateCheckCmd.RunE(cmd, args)
	},
}

var updateCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for available updates",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("roboticus %s (%s/%s)\n", cmdutil.Version, runtime.GOOS, runtime.GOARCH)
		rel, upToDate, err := checkForUpdate(cmd.Context(), cmdutil.Version)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		fmt.Printf("latest release: %s\n", normalizeVersion(rel.TagName))
		if normalizeVersion(cmdutil.Version) == "dev" {
			fmt.Printf("current build is a dev build; compare manually at %s\n", rel.HTMLURL)
			return nil
		}
		if upToDate {
			fmt.Println("status: up to date")
			return nil
		}
		fmt.Printf("status: update available at %s\n", rel.HTMLURL)
		return nil
	},
}

var updateAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Download and install the latest release",
	Long: `Download and install the latest binary release.

Provider config (providers.toml) and skills are treated as user-owned
data. By default they are PRESERVED across upgrades — only the binary
is updated, and only the binary's checksum is verified.

Pass --refresh-config to also overwrite providers.toml and skills/*.md
with the registry-published versions (this DOES verify their checksums
and WILL clobber any local edits to those files).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		refreshConfig, _ := cmd.Flags().GetBool("refresh-config")
		return runUpdateAll(cmd.Context(), cmdutil.Version, yes, refreshConfig)
	},
}

var updateBinaryCmd = &cobra.Command{
	Use:   "binary",
	Short: "Download and install the latest binary (alias for 'update all')",
	RunE:  updateAllCmd.RunE,
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the roboticus runtime",
}

var upgradeAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Upgrade to the latest release (alias for 'update all')",
	Long:  updateAllCmd.Long,
	RunE:  updateAllCmd.RunE,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("roboticus %s\n", cmdutil.Version)
		fmt.Printf("go:        %s\n", runtime.Version())
		fmt.Printf("os/arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

// updateProvidersCmd exposes the provider pack update as a standalone
// command. Invoking this command directly is itself the explicit refresh
// signal (the user is going out of their way to ask for the pack), so
// applyProvidersUpdate is called with refreshConfig=true. The standalone
// path is the documented escape hatch for users who want to overwrite
// their local providers.toml without re-running a binary upgrade.
var updateProvidersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Refresh provider pack from registry (overwrites local providers.toml)",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := cmdutil.EffectiveConfigPath()
		registryURL := ResolveRegistryURL(configPath)
		changed, err := applyProvidersUpdate(cmd.Context(), registryURL, configPath, true)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Println("Provider pack already up to date.")
		} else {
			fmt.Println("Provider pack updated successfully.")
		}
		return nil
	},
}

// updateSkillsCmd exposes the skills update as a standalone command.
// Same explicit-refresh semantics as updateProvidersCmd.
var updateSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Refresh skills from registry (overwrites local skill files)",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := cmdutil.EffectiveConfigPath()
		registryURL := ResolveRegistryURL(configPath)
		changed, err := applySkillsUpdate(cmd.Context(), registryURL, configPath, true)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Println("Skills already up to date.")
		} else {
			fmt.Println("Skills updated successfully.")
		}
		return nil
	},
}

func init() {
	updateAllCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	updateAllCmd.Flags().Bool("refresh-config", false,
		"Also overwrite providers.toml and skills/*.md with the registry-published versions (clobbers local edits)")
	upgradeAllCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	upgradeAllCmd.Flags().Bool("refresh-config", false,
		"Also overwrite providers.toml and skills/*.md with the registry-published versions (clobbers local edits)")

	updateCmd.AddCommand(updateCheckCmd, updateAllCmd, updateBinaryCmd, updateProvidersCmd, updateSkillsCmd)
	upgradeCmd.AddCommand(upgradeAllCmd)
}
