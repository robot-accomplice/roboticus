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

	"github.com/spf13/cobra"
)

// version is injected at build time via -ldflags "-X roboticus/cmd.version=YYYY.MM.DD".
// Defaults to "dev" for local builds.
var version = "dev"

var (
	updateCheckURL    = "https://api.github.com/repos/roboticus/roboticus/releases/latest"
	updateHTTPClient  = &http.Client{Timeout: 30 * time.Second}
	updateReleasesURL = "https://github.com/roboticus/roboticus/releases"
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
		fmt.Printf("roboticus %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)

		rel, upToDate, err := checkForUpdate(cmd.Context(), version)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		if upToDate && normalizeVersion(version) != "dev" {
			fmt.Println("Already up to date.")
			return nil
		}

		yes, _ := cmd.Flags().GetBool("yes")
		return performUpdate(cmd.Context(), rel, yes)
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
