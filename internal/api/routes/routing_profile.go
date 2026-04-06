package routes

import (
	"encoding/json"
	"math"
	"net/http"

	"roboticus/internal/db"
)

// routingProfile holds the three normalized weights for metascore routing.
type routingProfile struct {
	Correctness float64 `json:"correctness"`
	Cost        float64 `json:"cost"`
	Speed       float64 `json:"speed"`
}

var defaultProfile = routingProfile{Correctness: 0.5, Cost: 0.25, Speed: 0.25}

const routingProfileKey = "routing_profile"

// GetRoutingProfile returns the persisted routing profile weights.
func GetRoutingProfile(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row := store.QueryRowContext(r.Context(),
			`SELECT value FROM runtime_settings WHERE key = ?`, routingProfileKey)

		var raw string
		if err := row.Scan(&raw); err != nil {
			// No saved profile — return defaults.
			writeJSON(w, http.StatusOK, defaultProfile)
			return
		}

		var profile routingProfile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			writeJSON(w, http.StatusOK, defaultProfile)
			return
		}
		writeJSON(w, http.StatusOK, profile)
	}
}

// PutRoutingProfile validates, normalizes, and persists routing profile weights.
func PutRoutingProfile(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var profile routingProfile
		if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Validate non-negative.
		if profile.Correctness < 0 || profile.Cost < 0 || profile.Speed < 0 {
			writeError(w, http.StatusBadRequest, "weights must be non-negative")
			return
		}

		// Normalize to sum=1.0.
		sum := profile.Correctness + profile.Cost + profile.Speed
		if sum == 0 {
			writeError(w, http.StatusBadRequest, "at least one weight must be positive")
			return
		}
		profile.Correctness = math.Round(profile.Correctness/sum*1000) / 1000
		profile.Cost = math.Round(profile.Cost/sum*1000) / 1000
		profile.Speed = math.Round(profile.Speed/sum*1000) / 1000

		// Re-normalize rounding residual onto the largest weight.
		residual := 1.0 - (profile.Correctness + profile.Cost + profile.Speed)
		if residual != 0 {
			if profile.Correctness >= profile.Cost && profile.Correctness >= profile.Speed {
				profile.Correctness += residual
			} else if profile.Cost >= profile.Speed {
				profile.Cost += residual
			} else {
				profile.Speed += residual
			}
		}

		data, err := json.Marshal(profile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal profile")
			return
		}

		_, err = store.ExecContext(r.Context(),
			`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, datetime('now'))
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
			routingProfileKey, string(data))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, profile)
	}
}
