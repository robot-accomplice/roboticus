package routes

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"roboticus/internal/db"
)

// GenerateRecommendations returns a visible deep-analysis payload built from
// current prompt-performance metrics. It is not a silent alias for the normal
// recommendations endpoint.
func GenerateRecommendations(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		capture := &responseCapture{header: http.Header{}}
		GetRecommendations(store).ServeHTTP(capture, r)
		if capture.status >= 400 {
			for k, vals := range capture.header {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(capture.status)
			_, _ = w.Write(capture.body)
			return
		}

		var payload map[string]any
		if err := json.Unmarshal(capture.body, &payload); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build recommendation analysis")
			return
		}
		recs, _ := payload["recommendations"].([]any)
		payload["analysis_generated_at"] = time.Now().UTC().Format(time.RFC3339)
		payload["message"] = "Deep analysis generated from current prompt-performance metrics."
		payload["actions"] = recommendationActions(recs)
		payload["summary"] = map[string]any{
			"recommendation_count": len(recs),
			"period":               r.URL.Query().Get("period"),
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func recommendationActions(recs []any) []string {
	actions := make([]string, 0, len(recs))
	for _, item := range recs {
		rec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		action, _ := rec["action"].(string)
		if strings.TrimSpace(action) != "" {
			actions = append(actions, action)
		}
	}
	return actions
}

type responseCapture struct {
	header http.Header
	body   []byte
	status int
}

func (r *responseCapture) Header() http.Header { return r.header }

func (r *responseCapture) WriteHeader(status int) { r.status = status }

func (r *responseCapture) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body = append(r.body, p...)
	return len(p), nil
}
