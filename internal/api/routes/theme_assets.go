package routes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// ThemeDir returns the base directory for installed themes.
func ThemeDir() string {
	return filepath.Join(core.ConfigDir(), "themes")
}

// ThemeAssetDir returns the directory for a specific theme's files.
func ThemeAssetDir(themeID string) string {
	return filepath.Join(ThemeDir(), themeID)
}

// WriteThemeManifest writes a theme manifest JSON to its directory.
func WriteThemeManifest(themeID string, manifest ThemeManifest) error {
	dir := ThemeAssetDir(themeID)
	if err := os.MkdirAll(filepath.Join(dir, "textures"), 0o755); err != nil {
		return fmt.Errorf("create theme dir: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644)
}

// DownloadThemeTextures downloads texture files for a theme.
// For each texture with Kind="url", downloads the URL and saves to the textures/ dir.
// Updates the texture entries to Kind="file" with the local filename.
func DownloadThemeTextures(themeID string, manifest *ThemeManifest) error {
	dir := filepath.Join(ThemeAssetDir(themeID), "textures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	for name, tex := range manifest.Textures {
		if tex.Kind != "url" {
			continue
		}
		// Download the URL
		filename := name + ".jpg"
		if strings.HasSuffix(tex.Value, ".png") {
			filename = name + ".png"
		}
		localPath := filepath.Join(dir, filename)

		log.Info().Str("theme", themeID).Str("texture", name).Str("url", tex.Value).Msg("downloading theme texture")
		resp, err := http.Get(tex.Value)
		if err != nil {
			return fmt.Errorf("download texture %s: %w", name, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("download texture %s: HTTP %d", name, resp.StatusCode)
		}

		f, err := os.Create(localPath)
		if err != nil {
			return fmt.Errorf("create texture file: %w", err)
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			return fmt.Errorf("write texture: %w", err)
		}
		f.Close()

		// Update manifest entry to local file reference.
		manifest.Textures[name] = ThemeTexture{Kind: "file", Value: filename}
		log.Info().Str("theme", themeID).Str("texture", name).Str("file", filename).Msg("texture downloaded")
	}

	return nil
}

// ServeThemeTexture serves a texture file from a theme's textures directory.
// Route: GET /api/themes/{id}/textures/{filename}
func ServeThemeTexture() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		themeID := chi.URLParam(r, "id")
		filename := chi.URLParam(r, "filename")

		// Sanitize to prevent directory traversal.
		if strings.Contains(filename, "..") || strings.Contains(themeID, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		path := filepath.Join(ThemeAssetDir(themeID), "textures", filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.Error(w, "texture not found", http.StatusNotFound)
			return
		}

		// Set cache headers — textures don't change.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, path)
	}
}

// ResolveTextureURLs converts file-based texture references to servable URLs.
// Called when serializing theme data for the API response.
// ResolveTextureURLs converts file-based texture references to CSS url() values
// that the dashboard can use directly as background-image.
func ResolveTextureURLs(themeID string, textures map[string]ThemeTexture) map[string]ThemeTexture {
	resolved := make(map[string]ThemeTexture, len(textures))
	for name, tex := range textures {
		switch tex.Kind {
		case "file":
			// Convert local file reference to servable URL.
			resolved[name] = ThemeTexture{
				Kind:  "css",
				Value: fmt.Sprintf("url(/api/themes/%s/textures/%s)", themeID, tex.Value),
				Tile:  tex.Tile,
			}
		case "url":
			// Convert raw URL to CSS url() expression.
			resolved[name] = ThemeTexture{
				Kind:  "css",
				Value: fmt.Sprintf("url(%s)", tex.Value),
				Tile:  tex.Tile,
			}
		default:
			// "css" kind — already a CSS expression, pass through.
			resolved[name] = tex
		}
	}
	return resolved
}
