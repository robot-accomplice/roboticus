package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// ThemeDir returns the base directory for installed themes.
func ThemeDir() string {
	return filepath.Join(ConfigDir(), "themes")
}

// ThemeAssetDir returns the directory for a specific theme's files.
func ThemeAssetDir(themeID string) string {
	return filepath.Join(ThemeDir(), themeID)
}

// ThemeTextureEntry describes a texture for WriteThemeManifest/DownloadThemeTextures.
type ThemeTextureEntry struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Tile  bool   `json:"tile,omitempty"`
}

// WriteThemeManifest writes arbitrary JSON to a theme's manifest.json, creating
// the theme directory and textures subdirectory if needed.
func WriteThemeManifest(themeID string, manifestJSON []byte) error {
	dir := ThemeAssetDir(themeID)
	if err := os.MkdirAll(filepath.Join(dir, "textures"), 0o755); err != nil {
		return fmt.Errorf("create theme dir: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), manifestJSON, 0o644)
}

// DownloadThemeTextures downloads URL-based textures to the theme's textures/ dir.
// Returns updated texture entries with Kind="file" and local filename as Value.
func DownloadThemeTextures(themeID string, textures map[string]ThemeTextureEntry) (map[string]ThemeTextureEntry, error) {
	dir := filepath.Join(ThemeAssetDir(themeID), "textures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return textures, err
	}

	result := make(map[string]ThemeTextureEntry, len(textures))
	for name, tex := range textures {
		if tex.Kind != "url" {
			result[name] = tex
			continue
		}
		filename := name + ".jpg"
		if strings.HasSuffix(tex.Value, ".png") {
			filename = name + ".png"
		}
		localPath := filepath.Join(dir, filename)

		log.Info().Str("theme", themeID).Str("texture", name).Str("url", tex.Value).Msg("downloading theme texture")
		if err := downloadFile(tex.Value, localPath); err != nil {
			return textures, fmt.Errorf("download texture %s: %w", name, err)
		}

		result[name] = ThemeTextureEntry{Kind: "file", Value: filename, Tile: tex.Tile}
		log.Info().Str("theme", themeID).Str("texture", name).Str("file", filename).Msg("texture downloaded")
	}

	return result, nil
}

// downloadFile fetches a URL and writes it to localPath.
func downloadFile(url, localPath string) error {
	resp, err := http.Get(url) //nolint:gosec // theme texture URLs are admin-configured
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	return nil
}

// MarshalThemeManifest is a convenience for callers that need JSON bytes.
func MarshalThemeManifest(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
