package routes

import (
	"context"
	"encoding/json"
	"net/http"

	"goboticus/internal/browser"
)

// BrowserStatus returns the browser's running state and CDP port.
func BrowserStatus(b *browser.Browser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"running":  b.IsRunning(),
			"cdp_port": b.CDPPort(),
		})
	}
}

// BrowserStart launches the browser process.
func BrowserStart(b *browser.Browser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := b.Start(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
	}
}

// BrowserStop terminates the browser process.
func BrowserStop(b *browser.Browser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := b.Stop(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
	}
}

// browserActionRequest is the JSON body for the action endpoint.
type browserActionRequest struct {
	Action   browser.ActionKind `json:"action"`
	URL      string             `json:"url,omitempty"`
	Selector string             `json:"selector,omitempty"`
	Text     string             `json:"text,omitempty"`
	Script   string             `json:"script,omitempty"`
}

// BrowserAction executes a browser action via CDP.
func BrowserAction(b *browser.Browser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req browserActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if req.Action == "" {
			writeError(w, http.StatusBadRequest, "action is required")
			return
		}

		action := &browser.BrowserAction{
			Kind:     req.Action,
			URL:      req.URL,
			Selector: req.Selector,
			Text:     req.Text,
			Script:   req.Script,
		}

		ctx := r.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		result := b.Execute(ctx, action)

		status := http.StatusOK
		if !result.Success {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, result)
	}
}
