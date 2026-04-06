package routes

import (
	"encoding/json"
	"math"
	"net/http"

	"roboticus/internal/db"
)

// routingProfile holds the six normalized weights for metascore routing,
// matching the MetascoreBreakdown dimensions in the Rust implementation.
type routingProfile struct {
	Efficacy     float64 `json:"efficacy"`
	Cost         float64 `json:"cost"`
	Availability float64 `json:"availability"`
	Locality     float64 `json:"locality"`
	Confidence   float64 `json:"confidence"`
	Speed        float64 `json:"speed"`
}

// Default weights match the Rust Metascore formula:
// 0.35*efficacy + 0.20*(1-cost) + 0.25*availability + 0.10*locality + 0.10*confidence + speed_bonus
var defaultProfile = routingProfile{
	Efficacy: 0.35, Cost: 0.20, Availability: 0.25,
	Locality: 0.10, Confidence: 0.10, Speed: 0.10,
}

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
		if profile.Efficacy < 0 || profile.Cost < 0 || profile.Availability < 0 ||
			profile.Locality < 0 || profile.Confidence < 0 || profile.Speed < 0 {
			writeError(w, http.StatusBadRequest, "weights must be non-negative")
			return
		}

		// Normalize to sum=1.0.
		sum := profile.Efficacy + profile.Cost + profile.Availability +
			profile.Locality + profile.Confidence + profile.Speed
		if sum == 0 {
			writeError(w, http.StatusBadRequest, "at least one weight must be positive")
			return
		}
		profile.Efficacy = math.Round(profile.Efficacy/sum*1000) / 1000
		profile.Cost = math.Round(profile.Cost/sum*1000) / 1000
		profile.Availability = math.Round(profile.Availability/sum*1000) / 1000
		profile.Locality = math.Round(profile.Locality/sum*1000) / 1000
		profile.Confidence = math.Round(profile.Confidence/sum*1000) / 1000
		profile.Speed = math.Round(profile.Speed/sum*1000) / 1000

		// Re-normalize rounding residual onto the largest weight.
		sum2 := profile.Efficacy + profile.Cost + profile.Availability +
			profile.Locality + profile.Confidence + profile.Speed
		residual := 1.0 - sum2
		if residual != 0 {
			fields := []*float64{
				&profile.Efficacy, &profile.Cost, &profile.Availability,
				&profile.Locality, &profile.Confidence, &profile.Speed,
			}
			maxVal := 0.0
			var maxPtr *float64
			for _, fp := range fields {
				if *fp >= maxVal {
					maxVal = *fp
					maxPtr = fp
				}
			}
			if maxPtr != nil {
				*maxPtr += residual
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
