package routes

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
)

// WriteThemeManifest writes a theme manifest JSON to its directory.
// Delegates to core.WriteThemeManifest to keep filesystem mutation out of the route layer.
func WriteThemeManifest(themeID string, manifest ThemeManifest) error {
	data, err := core.MarshalThemeManifest(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return core.WriteThemeManifest(themeID, data)
}

// DownloadThemeTextures materializes texture files for a theme.
// Delegates to core.DownloadThemeTextures for filesystem operations.
func DownloadThemeTextures(themeID string, manifest *ThemeManifest) error {
	// Convert routes.ThemeTexture → core.ThemeTextureEntry.
	coreTextures := make(map[string]core.ThemeTextureEntry, len(manifest.Textures))
	for name, tex := range manifest.Textures {
		coreTextures[name] = core.ThemeTextureEntry{Kind: tex.Kind, Value: tex.Value, Tile: tex.Tile}
	}

	updated, err := core.DownloadThemeTextures(themeID, coreTextures)
	if err != nil {
		return err
	}

	// Write back updated texture entries.
	for name, tex := range updated {
		manifest.Textures[name] = ThemeTexture{Kind: tex.Kind, Value: tex.Value, Tile: tex.Tile}
	}
	return nil
}

// ServeThemeTexture serves a texture file from a theme's textures directory.
// Route: GET /api/themes/{id}/textures/{filename}
func ServeThemeTexture() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		themeID := chi.URLParam(r, "id")
		filename := chi.URLParam(r, "filename")

		if strings.Contains(filename, "..") || strings.Contains(themeID, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		path := filepath.Join(core.ThemeAssetDir(themeID), "textures", filename)
		if _, err := os.Stat(path); err == nil {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.ServeFile(w, r, path)
			return
		}

		data, err := core.ReadBundledThemeAsset(filepath.ToSlash(filepath.Join(themeID, filename)))
		if err == nil {
			if ctype := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); ctype != "" {
				w.Header().Set("Content-Type", ctype)
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.NotFound(w, r)
	}
}

// ResolveTextureURLs converts file-based texture references to CSS url() values
// that the dashboard can use directly as background-image.
func ResolveTextureURLs(themeID string, textures map[string]ThemeTexture) map[string]ThemeTexture {
	resolved := make(map[string]ThemeTexture, len(textures))
	for name, tex := range textures {
		switch tex.Kind {
		case "file":
			resolved[name] = ThemeTexture{
				Kind:  "css",
				Value: fmt.Sprintf("url(/api/themes/%s/textures/%s)", themeID, tex.Value),
				Tile:  tex.Tile,
			}
		case "bundled":
			resolved[name] = ThemeTexture{
				Kind:  "css",
				Value: fmt.Sprintf("url(/api/themes/%s/textures/%s)", themeID, filepath.Base(tex.Value)),
				Tile:  tex.Tile,
			}
		case "url":
			resolved[name] = ThemeTexture{
				Kind:  "css",
				Value: fmt.Sprintf("url(%s)", tex.Value),
				Tile:  tex.Tile,
			}
		default:
			resolved[name] = tex
		}
	}
	return resolved
}
