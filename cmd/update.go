package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// version is injected at build time via -ldflags "-X goboticus/cmd.version=YYYY.MM.DD".
// Defaults to "dev" for local builds.
var version = "dev"

var (
	updateCheckURL    = "https://api.github.com/repos/goboticus/goboticus/releases/latest"
	updateHTTPClient  = &http.Client{Timeout: 5 * time.Second}
	updateReleasesURL = "https://github.com/goboticus/goboticus/releases"
)

type latestRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
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

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for updates",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("goboticus %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("goboticus %s\n", version)
		fmt.Printf("go:        %s\n", runtime.Version())
		fmt.Printf("os/arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(updateCmd, versionCmd)
}
