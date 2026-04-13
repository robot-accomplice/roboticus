package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"roboticus/internal/db"
)

// ThemeTexture describes a CSS texture or pattern overlay.
type ThemeTexture struct {
	Kind  string `json:"kind"`  // "css" or "url"
	Value string `json:"value"` // CSS gradient/pattern string or URL
}

// ThemeManifest describes a UI theme.
type ThemeManifest struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Author      string                  `json:"author"`
	Swatch      string                  `json:"swatch"`              // primary color hex
	Variables   map[string]string       `json:"variables"`           // CSS custom properties
	Source      string                  `json:"source"`              // "builtin", "catalog", "custom"
	Textures    map[string]ThemeTexture `json:"textures,omitempty"`  // CSS gradients/patterns
	Fonts       []string                `json:"fonts,omitempty"`     // Google Fonts URLs
	Thumbnail   string                  `json:"thumbnail,omitempty"` // preview image data URI or URL
	Version     string                  `json:"version,omitempty"`   // semver
}

var (
	catalogMu       sync.RWMutex
	installedThemes = make(map[string]bool)
)

var builtinThemes = []ThemeManifest{
	{ID: "ai-purple", Name: "AI Purple", Description: "Default — purple accent with lavender body text", Author: "roboticus", Swatch: "#6366f1",
		Variables: map[string]string{
			"--bg": "#0a0a0a", "--surface": "#1a1a2e", "--accent": "#6366f1",
			"--text": "#e0e0e0", "--highlight": "#818cf8", "--secondary": "#a78bfa",
		},
		Textures: map[string]ThemeTexture{
			"noise": {Kind: "css", Value: "url(data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='4' height='4'%3E%3Crect width='4' height='4' fill='%230a0a0a'/%3E%3Crect width='1' height='1' fill='%231a1a2e' opacity='0.2'/%3E%3C/svg%3E)"},
		},
		Fonts:   []string{"https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;600&display=swap"},
		Version: "1.0.0", Source: "builtin"},
	{ID: "crt-green", Name: "CRT Green", Description: "Phosphor green CRT terminal emulation", Author: "roboticus", Swatch: "#33ff33",
		Variables: map[string]string{
			"--bg": "#0a0a0a", "--surface": "#0d1a0d", "--accent": "#33ff33",
			"--text": "#b8ffb8", "--highlight": "#66ff66", "--secondary": "#1a3a1a",
		},
		Textures: map[string]ThemeTexture{
			"scanlines": {Kind: "css", Value: "repeating-linear-gradient(0deg, transparent, transparent 1px, rgba(0,0,0,0.15) 1px, rgba(0,0,0,0.15) 2px)"},
			"glow":      {Kind: "css", Value: "radial-gradient(ellipse at 50% 50%, rgba(51,255,51,0.04) 0%, transparent 70%)"},
		},
		Fonts:   []string{"https://fonts.googleapis.com/css2?family=VT323&display=swap"},
		Version: "1.0.0", Source: "builtin"},
	{ID: "crt-orange", Name: "CRT Orange", Description: "Amber phosphor CRT terminal emulation", Author: "roboticus", Swatch: "#ffb347",
		Variables: map[string]string{
			"--bg": "#0a0800", "--surface": "#1a1408", "--accent": "#ffb347",
			"--text": "#ffd9a0", "--highlight": "#ffcc66", "--secondary": "#3a2a10",
		},
		Textures: map[string]ThemeTexture{
			"scanlines": {Kind: "css", Value: "repeating-linear-gradient(0deg, transparent, transparent 1px, rgba(0,0,0,0.15) 1px, rgba(0,0,0,0.15) 2px)"},
			"glow":      {Kind: "css", Value: "radial-gradient(ellipse at 50% 50%, rgba(255,179,71,0.04) 0%, transparent 70%)"},
		},
		Fonts:   []string{"https://fonts.googleapis.com/css2?family=VT323&display=swap"},
		Version: "1.0.0", Source: "builtin"},
	{ID: "psychedelic-freakout", Name: "Psychedelic Freakout", Description: "Mind-melting color cycling chaos — not for the faint of heart", Author: "roboticus", Swatch: "#ff00ff",
		Variables: map[string]string{
			"--bg": "#0a0014", "--surface": "#1a0028", "--surface-2": "#2a0042",
			"--accent": "#ff00ff", "--text": "#ff88ff", "--muted": "#cc66cc",
			"--border": "#660066", "--highlight": "#00ffff", "--secondary": "#ffff00",
		},
		Textures: map[string]ThemeTexture{
			"body": {Kind: "css", Value: "linear-gradient(135deg, rgba(255,0,255,0.05) 0%, rgba(0,255,255,0.05) 33%, rgba(255,255,0,0.05) 66%, rgba(255,0,128,0.05) 100%)"},
		},
		Fonts:   []string{"https://fonts.googleapis.com/css2?family=Bungee+Shade&display=swap"},
		Version: "1.0.0", Source: "builtin"},
}

var catalogThemes = []ThemeManifest{
	// ── Rust-parity catalog themes (exact match) ──────────────────────────
	{ID: "parchment", Name: "Parchment", Description: "Warm parchment tones with elegant serif typography feel", Author: "Roboticus", Swatch: "#d4a574",
		Variables: map[string]string{
			"--bg": "#2a2118", "--surface": "#352a1f", "--surface-2": "#3f3326",
			"--accent": "#c17f3a", "--text": "#f5e6c8", "--muted": "#b8a080",
			"--border": "#6b5540",
			"--theme-separator":       "linear-gradient(90deg, transparent, #8b5e3c 20%, #c17f3a 50%, #8b5e3c 80%, transparent)",
			"--theme-separator-height": "2px",
			"--theme-scrollbar":       "rgba(193,127,58,0.3)",
		},
		Textures: map[string]ThemeTexture{
			"body": {Kind: "css", Value: "url(data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAQAAAAEACAMAAABrrFhUAAAAGXRFWHRTb2Z0d2FyZQBBZG9iZSBJbWFnZVJlYWR5ccllPAAAAyNpVFh0WE1MOmNvbS5hZG9iZS54bXAAAAAAADw/eHBhY2tldCBiZWdpbj0i77u/IiBpZD0iVzVNME1wQ2VoaUh6cmVTek5UY3prYzlkIj8+IDx4OnhtcG1ldGEgeG1sbnM6eD0iYWRvYmU6bnM6bWV0YS8iIHg6eG1wdGs9IkFkb2JlIFhNUCBDb3JlIDUuNS1jMDE0IDc5LjE1MTQ4MSwgMjAxMy8wMy8xMy0xMjowOToxNSAgICAgICAgIj4gPHJkZjpSREYgeG1sbnM6cmRmPSJodHRwOi8vd3d3LnczLm9yZy8xOTk5LzAyLzIyLXJkZi1zeW50YXgtbnMjIj4gPHJkZjpEZXNjcmlwdGlvbiByZGY6YWJvdXQ9IiIgeG1sbnM6eG1wPSJodHRwOi8vbnMuYWRvYmUuY29tL3hhcC8xLjAvIiB4bWxuczp4bXBNTT0iaHR0cDovL25zLmFkb2JlLmNvbS94YXAvMS4wL21tLyIgeG1sbnM6c3RSZWY9Imh0dHA6Ly9ucy5hZG9iZS5jb20veGFwLzEuMC9zVHlwZS9SZXNvdXJjZVJlZiMiIHhtcDpDcmVhdG9yVG9vbD0iQWRvYmUgUGhvdG9zaG9wIENDIChNYWNpbnRvc2gpIiB4bXBNTTpJbnN0YW5jZUlEPSJ4bXAuaWlkOkI2NkEzNkJEOTk1OTExRTM4ODMyOTYwMkFBNDQxMTcwIiB4bXBNTTpEb2N1bWVudElEPSJ4bXAuZGlkOkI2NkEzNkJFOTk1OTExRTM4ODMyOTYwMkFBNDQxMTcwIj4gPHhtcE1NOkRlcml2ZWRGcm9tIHN0UmVmOmluc3RhbmNlSUQ9InhtcC5paWQ6MkYwRDVERUU5OTU4MTFFMzg4MzI5NjAyQUE0NDExNzAiIHN0UmVmOmRvY3VtZW50SUQ9InhtcC5kaWQ6QjY2QTM2QkM5OTU5MTFFMzg4MzI5NjAyQUE0NDExNzAiLz4gPC9yZGY6RGVzY3JpcHRpb24+IDwvcmRmOlJERj4gPC94OnhtcG1ldGE+IDw/eHBhY2tldCBlbmQ9InIiPz7kWqjbAAACeVBMVEUAAABNAABN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)"},
		},
		Version: "1.0.0", Source: "catalog"},
	{ID: "midnight-ocean", Name: "Midnight Ocean", Description: "Deep navy depths with teal accents and wave-inspired separators", Author: "Roboticus", Swatch: "#0d9488",
		Variables: map[string]string{
			"--bg": "#0a1628", "--surface": "#0e1f3a", "--surface-2": "#132848",
			"--accent": "#0d9488", "--text": "#c8e1f5", "--muted": "#6b8ab0",
			"--border": "#1e3a5f",
			"--theme-body-texture": "radial-gradient(ellipse at 50% 0%, rgba(13,148,136,0.08) 0%, transparent 60%)",
			"--theme-separator":    "linear-gradient(90deg, transparent, #0d9488 15%, #1e3a5f 50%, #0d9488 85%, transparent)",
			"--theme-separator-height": "2px",
			"--theme-scrollbar":    "rgba(13,148,136,0.3)",
		},
		Version: "1.0.0", Source: "catalog"},
	{ID: "solarized-dark", Name: "Solarized Dark", Description: "Ethan Schoonover's precision-engineered dark palette for low-fatigue reading", Author: "Roboticus", Swatch: "#268bd2",
		Variables: map[string]string{
			"--bg": "#002b36", "--surface": "#073642", "--surface-2": "#0a4050",
			"--accent": "#268bd2", "--text": "#93a1a1", "--muted": "#657b83",
			"--border": "#2aa198",
		},
		Version: "1.0.0", Source: "catalog"},
	{ID: "dracula", Name: "Dracula", Description: "The beloved dark theme with purple, pink, and green highlights", Author: "Roboticus", Swatch: "#bd93f9",
		Variables: map[string]string{
			"--bg": "#282a36", "--surface": "#2d303e", "--surface-2": "#343746",
			"--accent": "#bd93f9", "--text": "#f8f8f2", "--muted": "#6272a4",
			"--border": "#44475a",
			"--theme-scrollbar": "rgba(189,147,249,0.25)",
		},
		Version: "1.0.0", Source: "catalog"},
	{ID: "nord", Name: "Nord", Description: "Arctic blue-gray palette inspired by the cold beauty of the Nordic wilderness", Author: "Roboticus", Swatch: "#88c0d0",
		Variables: map[string]string{
			"--bg": "#2e3440", "--surface": "#3b4252", "--surface-2": "#434c5e",
			"--accent": "#88c0d0", "--text": "#eceff4", "--muted": "#81a1c1",
			"--border": "#4c566a",
		},
		Version: "1.0.0", Source: "catalog"},
	// ── Go-original catalog themes (beyond-parity) ───────────────────────
	{ID: "solarized-cyan", Name: "Solarized Cyan", Description: "Solarized variant with cyan accent and noise texture", Author: "Roboticus", Swatch: "#2aa198",
		Variables: map[string]string{
			"--bg": "#002b36", "--surface": "#073642", "--surface-2": "#0a4050",
			"--accent": "#2aa198", "--text": "#839496", "--muted": "#657b83",
			"--border": "#586e75", "--highlight": "#b58900", "--secondary": "#268bd2",
		},
		Fonts:   []string{"https://fonts.googleapis.com/css2?family=Source+Code+Pro:wght@400;600&display=swap"},
		Version: "1.0.0", Source: "catalog"},
	{ID: "cyberpunk", Name: "Cyberpunk", Description: "Neon-soaked dystopian interface with scanline overlay", Author: "Roboticus", Swatch: "#ff2a6d",
		Variables: map[string]string{
			"--bg": "#0d0221", "--surface": "#1a0a2e", "--surface-2": "#240e3e",
			"--accent": "#ff2a6d", "--text": "#05d9e8", "--muted": "#7b6b8a",
			"--border": "#3a1a5e", "--highlight": "#01ff70", "--secondary": "#05d9e8",
		},
		Textures: map[string]ThemeTexture{
			"body": {Kind: "css", Value: "url(data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAHElEQVR4nGKR4tJjgAEmBiTAyMEkgF0GEAAA//8T3gB2tig9wwAAAABJRU5ErkJggg==)"},
		},
		Fonts:   []string{"https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;700&display=swap"},
		Version: "1.0.0", Source: "catalog"},
	{ID: "minimal", Name: "Minimal", Description: "High-contrast grayscale with no distractions — accessibility focused", Author: "Roboticus", Swatch: "#ffffff",
		Variables: map[string]string{
			"--bg": "#1a1a1a", "--surface": "#2a2a2a", "--surface-2": "#333333",
			"--accent": "#ffffff", "--text": "#cccccc", "--muted": "#888888",
			"--border": "#444444", "--highlight": "#e0e0e0", "--secondary": "#999999",
		},
		Version: "1.0.0", Source: "catalog"},
	{ID: "tokyo-night", Name: "Tokyo Night", Description: "Neon-soaked night palette inspired by Tokyo's glowing skyline", Author: "Roboticus", Swatch: "#7aa2f7",
		Variables: map[string]string{
			"--bg": "#1a1b26", "--surface": "#24283b", "--surface-2": "#2a2e42",
			"--accent": "#7aa2f7", "--text": "#c0caf5", "--muted": "#565f89",
			"--border": "#3b4261", "--highlight": "#e0af68", "--secondary": "#9ece6a",
		},
		Textures: map[string]ThemeTexture{
			"body": {Kind: "css", Value: "url(data:image/png;base64,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)"},
		},
		Fonts:   []string{"https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500&display=swap"},
		Version: "1.0.0", Source: "catalog"},
}

// GetThemesList returns themes as a flat array (used by the dashboard's /api/themes endpoint).
func GetThemesList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, builtinThemes)
	}
}

// GetThemeCatalog returns all available themes.
func installedThemeIDs(store *db.Store) map[string]bool {
	installed := make(map[string]bool)
	if store == nil {
		return installed
	}
	rows, err := db.NewRouteQueries(store).ListInstalledThemeIDs(context.Background())
	if err != nil {
		return installed
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			installed[id] = true
		}
	}
	return installed
}

func findThemeByID(store *db.Store, id string) (ThemeManifest, bool) {
	for _, t := range builtinThemes {
		if t.ID == id {
			return t, true
		}
	}
	installed := installedThemeIDs(store)
	for _, t := range catalogThemes {
		if t.ID == id && installed[id] {
			return t, true
		}
	}
	return ThemeManifest{}, false
}

func GetThemeCatalog(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalogMu.RLock()
		defer catalogMu.RUnlock()

		installed := installedThemeIDs(store)
		themes := make([]map[string]any, 0, len(builtinThemes)+len(catalogThemes))
		for _, t := range builtinThemes {
			entry := map[string]any{
				"id": t.ID, "name": t.Name, "description": t.Description,
				"author": t.Author, "swatch": t.Swatch, "source": t.Source,
				"installed": installedThemes[t.ID] || t.Source == "builtin" || installed[t.ID],
			}
			if len(t.Variables) > 0 {
				entry["variables"] = t.Variables
			}
			if len(t.Textures) > 0 {
				entry["textures"] = t.Textures
			}
			if len(t.Fonts) > 0 {
				entry["fonts"] = t.Fonts
			}
			themes = append(themes, entry)
		}
		for _, t := range catalogThemes {
			entry := map[string]any{
				"id": t.ID, "name": t.Name, "description": t.Description,
				"author": t.Author, "swatch": t.Swatch, "source": t.Source,
				"installed": installedThemes[t.ID] || installed[t.ID],
			}
			if len(t.Variables) > 0 {
				entry["variables"] = t.Variables
			}
			if len(t.Textures) > 0 {
				entry["textures"] = t.Textures
			}
			if len(t.Fonts) > 0 {
				entry["fonts"] = t.Fonts
			}
			themes = append(themes, entry)
		}
		writeJSON(w, http.StatusOK, map[string]any{"themes": themes})
	}
}

// InstallCatalogTheme installs a catalog theme by ID.
func InstallCatalogTheme(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		var theme ThemeManifest
		found := false
		for _, t := range catalogThemes {
			if t.ID == req.ID {
				theme = t
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "theme not found in catalog")
			return
		}
		content, err := json.Marshal(theme)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode theme")
			return
		}
		if err := db.NewRouteQueries(store).InstallTheme(r.Context(), req.ID, theme.Name, string(content)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "theme": theme})
	}
}

// UninstallCatalogTheme removes a catalog theme by ID.
func UninstallCatalogTheme(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		// Prevent uninstalling builtin themes.
		for _, t := range builtinThemes {
			if t.ID == req.ID {
				writeError(w, http.StatusBadRequest, "cannot uninstall builtin theme")
				return
			}
		}
		if err := db.NewRouteQueries(store).UninstallTheme(r.Context(), req.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		catalogMu.Lock()
		delete(installedThemes, req.ID)
		catalogMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": req.ID})
	}
}

// GetActiveTheme returns the currently active theme.
func GetActiveTheme(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var themeID string
		row := db.NewRouteQueries(store).GetIdentityValue(r.Context(), "active_theme")
		if row.Scan(&themeID) != nil {
			themeID = "ai-purple"
		}

		if t, ok := findThemeByID(store, themeID); ok {
			writeJSON(w, http.StatusOK, t)
			return
		}
		writeJSON(w, http.StatusOK, builtinThemes[0])
	}
}

// SetActiveTheme updates the active theme.
func SetActiveTheme(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ThemeID string `json:"theme_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.ThemeID == "" {
			writeError(w, http.StatusBadRequest, "theme_id required")
			return
		}
		if _, ok := findThemeByID(store, req.ThemeID); !ok {
			writeError(w, http.StatusBadRequest, "unknown theme_id")
			return
		}

		if err := db.NewRouteQueries(store).SetActiveThemeID(r.Context(), req.ThemeID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "theme_id": req.ThemeID})
	}
}
