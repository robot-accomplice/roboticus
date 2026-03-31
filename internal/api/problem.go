package api

import (
	"encoding/json"
	"net/http"
)

// ProblemDetails implements RFC 9457 Problem Details for HTTP APIs.
// All API error responses use this format with Content-Type: application/problem+json.
type ProblemDetails struct {
	Type     string `json:"type"`               // URI reference identifying the problem type
	Title    string `json:"title"`              // Short human-readable summary
	Status   int    `json:"status"`             // HTTP status code
	Detail   string `json:"detail,omitempty"`   // Occurrence-specific explanation
	Instance string `json:"instance,omitempty"` // URI identifying this occurrence
}

// Common problem type URIs.
const (
	ProblemTypeBlank        = "about:blank"
	ProblemTypeInjection    = "urn:goboticus:problem:injection-blocked"
	ProblemTypeRateLimited  = "urn:goboticus:problem:rate-limited"
	ProblemTypeUnauthorized = "urn:goboticus:problem:unauthorized"
)

// WriteProblem writes an RFC 9457 problem+json response.
func WriteProblem(w http.ResponseWriter, status int, detail string) {
	WriteProblemWithType(w, status, ProblemTypeBlank, detail)
}

// WriteProblemWithType writes an RFC 9457 response with a specific problem type URI.
func WriteProblemWithType(w http.ResponseWriter, status int, problemType, detail string) {
	pd := ProblemDetails{
		Type:   problemType,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(pd)
}

// StatusTitle returns the canonical HTTP reason phrase for a status code.
func StatusTitle(code int) string {
	text := http.StatusText(code)
	if text == "" {
		return "Unknown Error"
	}
	return text
}
