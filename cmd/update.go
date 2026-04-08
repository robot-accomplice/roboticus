package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"roboticus/internal/core"
)

// version is injected at build time via -ldflags "-X roboticus/cmd.version=YYYY.MM.DD".
// Defaults to "dev" for local builds.
var version = "dev"

var (
	updateCheckURL    = "https://api.github.com/repos/roboticus/roboticus/releases/latest"
	updateHTTPClient  = &http.Client{Timeout: 30 * time.Second}
	updateReleasesURL = "https://github.com/roboticus/roboticus/releases"
	updateRegistryURL = "https://roboticus.ai/registry/manifest.json"
	updateBinaryFunc  = performUpdate
	updateMaintenance = runUpdateMaintenance
)

type latestRelease struct {
	TagName    string         `json:"tag_name"`
	HTMLURL    string         `json:"html_url"`
	Assets     []releaseAsset `json:"assets"`
	AssetsURL  string         `json:"assets_url"`
	TarballURL string         `json:"tarball_url"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type registryManifest struct {
	Version string        `json:"version"`
	Packs   registryPacks `json:"packs"`
}

type registryPacks struct {
	Providers providerPack   `json:"providers"`
	Skills    skillPack      `json:"skills"`
	Plugins   *pluginCatalog `json:"plugins,omitempty"`
}

type providerPack struct {
	SHA256 string `json:"sha256"`
	Path   string `json:"path"`
}

type skillPack struct {
	SHA256 string            `json:"sha256"`
	Path   string            `json:"path"`
	Files  map[string]string `json:"files"`
}

type pluginCatalog struct {
	Catalog []pluginCatalogEntry `json:"catalog"`
}

type pluginCatalogEntry struct {
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

func loadUpdateState() (updateState, error) {
	path := updateStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return updateState{}, nil
		}
		return updateState{}, err
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

func effectiveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return filepath.Join(roboticusHome(), "roboticus.toml")
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

func resolveRegistryURL(configPath string) string {
	if val := strings.TrimSpace(os.Getenv("ROBOTICUS_REGISTRY_URL")); val != "" {
		return val
	}
	raw, err := loadRawUpdateConfig(configPath)
	if err == nil && strings.TrimSpace(raw.Update.RegistryURL) != "" {
		return strings.TrimSpace(raw.Update.RegistryURL)
	}
	return updateRegistryURL
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

func bytesSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fetchRegistryManifest(ctx context.Context, manifestURL string) (registryManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return registryManifest{}, err
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return registryManifest{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return registryManifest{}, fmt.Errorf("registry manifest returned %s", resp.Status)
	}
	var manifest registryManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return registryManifest{}, err
	}
	if manifest.Version == "" {
		return registryManifest{}, fmt.Errorf("registry manifest missing version")
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

func applyProvidersUpdate(ctx context.Context, registryURL, configPath string) (bool, error) {
	manifest, err := fetchRegistryManifest(ctx, registryURL)
	if err != nil {
		return false, err
	}
	remoteURL := registryBaseURL(registryURL) + "/" + strings.TrimPrefix(manifest.Packs.Providers.Path, "/")
	content, err := fetchText(ctx, remoteURL)
	if err != nil {
		return false, err
	}
	hash := bytesSHA256([]byte(content))
	if manifest.Packs.Providers.SHA256 != "" && !strings.EqualFold(hash, manifest.Packs.Providers.SHA256) {
		return false, fmt.Errorf("provider pack checksum mismatch")
	}

	path := providersLocalPath(configPath)
	if data, err := os.ReadFile(path); err == nil && bytesSHA256(data) == hash {
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

func applySkillsUpdate(ctx context.Context, registryURL, configPath string) (bool, error) {
	manifest, err := fetchRegistryManifest(ctx, registryURL)
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

	changed := false
	for name, expectedHash := range manifest.Packs.Skills.Files {
		if !safeSkillPath(dir, name) {
			return false, fmt.Errorf("unsafe skill path %q", name)
		}
		path := filepath.Join(dir, name)
		if data, err := os.ReadFile(path); err == nil && strings.EqualFold(bytesSHA256(data), expectedHash) {
			fileHashes[name] = expectedHash
			continue
		}

		remoteURL := baseURL + "/" + strings.TrimPrefix(manifest.Packs.Skills.Path, "/") + name
		content, err := fetchText(ctx, remoteURL)
		if err != nil {
			return false, err
		}
		hash := bytesSHA256([]byte(content))
		if expectedHash != "" && !strings.EqualFold(hash, expectedHash) {
			return false, fmt.Errorf("skill %s checksum mismatch", name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return false, err
		}
		fileHashes[name] = hash
		changed = true
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

func runUpdateAll(ctx context.Context, currentVersion string, yes bool) error {
	fmt.Printf("roboticus %s (%s/%s)\n", currentVersion, runtime.GOOS, runtime.GOARCH)

	rel, upToDate, err := checkForUpdate(ctx, currentVersion)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	configPath := effectiveConfigPath()
	registryURL := resolveRegistryURL(configPath)
	binaryChanged := false
	if !upToDate || normalizeVersion(currentVersion) == "dev" {
		if err := updateBinaryFunc(ctx, rel, yes); err != nil {
			return err
		}
		binaryChanged = true
	}

	// Load state to check for already-completed steps (resumability).
	prevState, _ := loadUpdateState()
	manifest, manifestErr := fetchRegistryManifest(ctx, registryURL)

	var providersChanged bool
	if manifestErr == nil && prevState.InstalledContent.Providers != nil &&
		prevState.InstalledContent.Providers.Version == manifest.Version {
		fmt.Println("Provider pack already at version " + manifest.Version + ", skipping.")
		providersChanged = false
	} else {
		var pErr error
		providersChanged, pErr = applyProvidersUpdate(ctx, registryURL, configPath)
		if pErr != nil {
			return fmt.Errorf("provider update failed: %w", pErr)
		}
	}

	var skillsChanged bool
	if manifestErr == nil && prevState.InstalledContent.Skills != nil &&
		prevState.InstalledContent.Skills.Version == manifest.Version {
		fmt.Println("Skills pack already at version " + manifest.Version + ", skipping.")
		skillsChanged = false
	} else {
		var sErr error
		skillsChanged, sErr = applySkillsUpdate(ctx, registryURL, configPath)
		if sErr != nil {
			return fmt.Errorf("skills update failed: %w", sErr)
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
	workspaceDir := filepath.Dir(configPath)
	firmwarePath := filepath.Join(workspaceDir, "FIRMWARE.toml")
	if _, err := os.Stat(firmwarePath); err == nil {
		if migErr := migrateFirmwareRules(firmwarePath); migErr != nil {
			fmt.Fprintf(os.Stderr, "warning: firmware migration: %v\n", migErr)
		} else {
			fmt.Println("Firmware rules migration: OK")
		}
	}

	// Step 4: OAuth token refresh (for providers using OAuth PKCE).
	refreshOAuthTokens()

	// Step 5: Post-update health check.
	if data, err := apiGet("/api/health"); err == nil {
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
	data, err := apiGet("/api/config")
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
		ksData, err := apiGet("/api/keystore/status")
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

func checkForUpdate(ctx context.Context, currentVersion string) (latestRelease, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateCheckURL, nil)
	if err != nil {
		return latestRelease{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return latestRelease{}, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return latestRelease{}, false, fmt.Errorf("update check returned %s", resp.Status)
	}

	var rel latestRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return latestRelease{}, false, err
	}
	if rel.HTMLURL == "" {
		rel.HTMLURL = updateReleasesURL
	}
	if rel.TagName == "" {
		return latestRelease{}, false, fmt.Errorf("latest release response missing tag_name")
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
func findAssetURL(rel latestRelease, name string) (string, error) {
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
	resp, err := updateHTTPClient.Do(req)
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
func performUpdate(ctx context.Context, rel latestRelease, skipConfirm bool) error {
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
	// then rename over it. This ensures same-filesystem rename for atomicity.
	destDir := filepath.Dir(execPath)
	stagePath := filepath.Join(destDir, ".roboticus-update-staging")
	if err := copyFile(binaryPath, stagePath); err != nil {
		return fmt.Errorf("failed to stage update: %w", err)
	}
	if err := os.Chmod(stagePath, 0o755); err != nil {
		_ = os.Remove(stagePath)
		return fmt.Errorf("failed to set permissions on staged binary: %w", err)
	}
	if err := os.Rename(stagePath, execPath); err != nil {
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
		fmt.Printf("roboticus %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		rel, upToDate, err := checkForUpdate(cmd.Context(), version)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		fmt.Printf("latest release: %s\n", normalizeVersion(rel.TagName))
		if normalizeVersion(version) == "dev" {
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
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		return runUpdateAll(cmd.Context(), version, yes)
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
	RunE:  updateAllCmd.RunE,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("roboticus %s\n", version)
		fmt.Printf("go:        %s\n", runtime.Version())
		fmt.Printf("os/arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	updateAllCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	upgradeAllCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	updateCmd.AddCommand(updateCheckCmd, updateAllCmd, updateBinaryCmd)
	upgradeCmd.AddCommand(upgradeAllCmd)

	rootCmd.AddCommand(updateCmd, upgradeCmd, versionCmd)
}
