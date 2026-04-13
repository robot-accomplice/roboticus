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
			"--theme-body-texture":    "repeating-linear-gradient(0deg, transparent, transparent 3px, rgba(139,94,60,0.03) 3px, rgba(139,94,60,0.03) 4px)",
			"--theme-separator":       "linear-gradient(90deg, transparent, #8b5e3c 20%, #c17f3a 50%, #8b5e3c 80%, transparent)",
			"--theme-separator-height": "2px",
			"--theme-scrollbar":       "rgba(193,127,58,0.3)",
			"--theme-card-border":     "linear-gradient(to bottom, #6b5540, #3f3326) 1",
		},
		Textures: map[string]ThemeTexture{
			"body":    {Kind: "css", Value: "repeating-linear-gradient(0deg, transparent, transparent 3px, rgba(139,94,60,0.03) 3px, rgba(139,94,60,0.03) 4px)"},
			"surface": {Kind: "css", Value: "radial-gradient(ellipse at 20% 50%, rgba(193,127,58,0.06) 0%, transparent 70%)"},
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
		Textures: map[string]ThemeTexture{
			"body": {Kind: "css", Value: "radial-gradient(ellipse at 50% 0%, rgba(13,148,136,0.08) 0%, transparent 60%)"},
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
		Textures: map[string]ThemeTexture{
			"body": {Kind: "css", Value: "url(data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='4' height='4'%3E%3Crect width='4' height='4' fill='%23002b36'/%3E%3Crect width='1' height='1' fill='%23073642' opacity='0.3'/%3E%3C/svg%3E)"},
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
			"body": {Kind: "css", Value: "repeating-linear-gradient(0deg, transparent, transparent 2px, rgba(0,0,0,0.15) 2px, rgba(0,0,0,0.15) 4px)"},
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
			"body": {Kind: "css", Value: "linear-gradient(180deg, rgba(122,162,247,0.03) 0%, transparent 30%), linear-gradient(0deg, rgba(158,206,106,0.02) 0%, transparent 20%)"},
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
