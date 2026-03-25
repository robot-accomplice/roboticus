package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ApprovalEntry is a JSON-serializable approval request (avoids agent import).
type ApprovalEntry struct {
	ID        string `json:"id"`
	ToolName  string `json:"tool_name"`
	ToolInput string `json:"tool_input"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// ApprovalService is the interface for approval operations (avoids importing agent).
type ApprovalService interface {
	ListAllJSON() []map[string]any
	ListPendingJSON() []map[string]any
	GetJSON(id string) map[string]any
	Approve(id, operator string) error
	Deny(id, operator, reason string) error
}

// ListApprovals returns all approval requests.
func ListApprovals(svc ApprovalService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		var reqs []map[string]any
		if status == "pending" {
			reqs = svc.ListPendingJSON()
		} else {
			reqs = svc.ListAllJSON()
		}
		if reqs == nil {
			reqs = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"approvals": reqs})
	}
}

// GetApproval returns a single approval request by ID.
func GetApproval(svc ApprovalService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		req := svc.GetJSON(id)
		if req == nil {
			writeError(w, http.StatusNotFound, "approval not found")
			return
		}
		writeJSON(w, http.StatusOK, req)
	}
}

// ApproveRequest approves a pending approval.
func ApproveRequest(svc ApprovalService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var body struct {
			Operator string `json:"operator"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Operator == "" {
			body.Operator = "api"
		}

		if err := svc.Approve(id, body.Operator); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
	}
}

// DenyRequest denies a pending approval.
func DenyRequest(svc ApprovalService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var body struct {
			Operator string `json:"operator"`
			Reason   string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Operator == "" {
			body.Operator = "api"
		}

		if err := svc.Deny(id, body.Operator, body.Reason); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
	}
}
