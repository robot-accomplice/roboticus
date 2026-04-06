package routes

import (
	"encoding/json"
	"net/http"
	"sync"

	"roboticus/internal/db"
)

// ThemeManifest describes a UI theme.
type ThemeManifest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	Swatch      string            `json:"swatch"`    // primary color hex
	Variables   map[string]string `json:"variables"` // CSS custom properties
	Source      string            `json:"source"`    // "builtin", "catalog", "custom"
}

var (
	catalogMu       sync.RWMutex
	installedThemes = make(map[string]bool)
)

var builtinThemes = []ThemeManifest{
	{ID: "default", Name: "Default", Description: "Standard dark theme", Author: "roboticus", Swatch: "#33ff33",
		Variables: map[string]string{"--bg": "#0a0a0a", "--surface": "#1a1a2e", "--accent": "#33ff33", "--text": "#e0e0e0"}, Source: "builtin"},
	{ID: "parchment", Name: "Parchment", Description: "Warm paper-like theme", Author: "roboticus", Swatch: "#8b6914",
		Variables: map[string]string{"--bg": "#f5e6c8", "--surface": "#ede0c8", "--accent": "#8b6914", "--text": "#3e2723"}, Source: "builtin"},
	{ID: "midnight-ocean", Name: "Midnight Ocean", Description: "Deep blue ocean theme", Author: "roboticus", Swatch: "#00bcd4",
		Variables: map[string]string{"--bg": "#0d1b2a", "--surface": "#1b2838", "--accent": "#00bcd4", "--text": "#b0bec5"}, Source: "builtin"},
	{ID: "solarized-dark", Name: "Solarized Dark", Description: "Ethan Schoonover's Solarized", Author: "roboticus", Swatch: "#b58900",
		Variables: map[string]string{"--bg": "#002b36", "--surface": "#073642", "--accent": "#b58900", "--text": "#839496"}, Source: "builtin"},
	{ID: "nord", Name: "Nord", Description: "Arctic color palette", Author: "roboticus", Swatch: "#88c0d0",
		Variables: map[string]string{"--bg": "#2e3440", "--surface": "#3b4252", "--accent": "#88c0d0", "--text": "#d8dee9"}, Source: "builtin"},
}

// GetThemesList returns themes as a flat array (used by the dashboard's /api/themes endpoint).
func GetThemesList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, builtinThemes)
	}
}

// GetThemeCatalog returns all available themes.
func GetThemeCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalogMu.RLock()
		defer catalogMu.RUnlock()

		themes := make([]map[string]any, 0, len(builtinThemes))
		for _, t := range builtinThemes {
			themes = append(themes, map[string]any{
				"id": t.ID, "name": t.Name, "description": t.Description,
				"author": t.Author, "swatch": t.Swatch, "source": t.Source,
				"installed": installedThemes[t.ID] || t.Source == "builtin",
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"themes": themes})
	}
}

// GetActiveTheme returns the currently active theme.
func GetActiveTheme(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var themeID string
		row := store.QueryRowContext(r.Context(),
			`SELECT value FROM identity WHERE key = 'active_theme'`)
		if row.Scan(&themeID) != nil {
			themeID = "default"
		}

		for _, t := range builtinThemes {
			if t.ID == themeID {
				writeJSON(w, http.StatusOK, t)
				return
			}
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
		valid := false
		for _, theme := range builtinThemes {
			if theme.ID == req.ThemeID {
				valid = true
				break
			}
		}
		if !valid {
			writeError(w, http.StatusBadRequest, "unknown theme_id")
			return
		}

		if _, err := store.ExecContext(r.Context(),
			`INSERT OR REPLACE INTO identity (key, value) VALUES ('active_theme', ?)`, req.ThemeID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "theme_id": req.ThemeID})
	}
}
