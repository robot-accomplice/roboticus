package core

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

//go:embed theme_assets/*
//go:embed theme_assets/**/*
var bundledThemeAssets embed.FS

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

// DownloadThemeTextures materializes URL-based or bundled textures into the
// theme's textures/ dir. Returns updated texture entries with Kind="file" and
// local filename as Value.
func DownloadThemeTextures(themeID string, textures map[string]ThemeTextureEntry) (map[string]ThemeTextureEntry, error) {
	dir := filepath.Join(ThemeAssetDir(themeID), "textures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return textures, err
	}

	result := make(map[string]ThemeTextureEntry, len(textures))
	for name, tex := range textures {
		if tex.Kind != "url" && tex.Kind != "bundled" {
			result[name] = tex
			continue
		}
		filename := textureFilename(name, tex.Value)
		localPath := filepath.Join(dir, filename)

		switch tex.Kind {
		case "url":
			log.Info().Str("theme", themeID).Str("texture", name).Str("url", tex.Value).Msg("downloading theme texture")
			if err := downloadFile(tex.Value, localPath); err != nil {
				return textures, fmt.Errorf("download texture %s: %w", name, err)
			}
		case "bundled":
			log.Info().Str("theme", themeID).Str("texture", name).Str("asset", tex.Value).Msg("materializing bundled theme texture")
			if err := copyBundledThemeAsset(tex.Value, localPath); err != nil {
				return textures, fmt.Errorf("materialize bundled texture %s: %w", name, err)
			}
		}

		result[name] = ThemeTextureEntry{Kind: "file", Value: filename, Tile: tex.Tile}
		log.Info().Str("theme", themeID).Str("texture", name).Str("file", filename).Msg("texture downloaded")
	}

	return result, nil
}

func textureFilename(name, source string) string {
	base := filepath.Base(strings.TrimSpace(source))
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return name + ext
	default:
		return name + ".jpg"
	}
}

func bundledThemeAssetPath(rel string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(rel)))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid bundled asset path")
	}
	return "theme_assets/" + clean, nil
}

func copyBundledThemeAsset(rel, localPath string) error {
	data, err := ReadBundledThemeAsset(rel)
	if err != nil {
		return err
	}
	return os.WriteFile(localPath, data, 0o644)
}

// ReadBundledThemeAsset returns the bytes for a bundled theme asset.
func ReadBundledThemeAsset(rel string) ([]byte, error) {
	assetPath, err := bundledThemeAssetPath(rel)
	if err != nil {
		return nil, err
	}
	data, err := bundledThemeAssets.ReadFile(assetPath)
	if err != nil {
		return nil, fmt.Errorf("read bundled theme asset %s: %w", rel, err)
	}
	return data, nil
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
