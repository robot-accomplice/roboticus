package channels

import (
	"encoding/json"
	"net/http"
	"roboticus/cmd/internal/testhelp"
	"testing"
)

func TestChannelsTestCmd_WithMockServer(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/channels/slack/test" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}))
	defer cleanup()

	err := channelsTestCmd.RunE(channelsTestCmd, []string{"slack"})
	if err != nil {
		t.Fatalf("channels test: %v", err)
	}
}

func TestChannelsListCmd_NonArrayChannels(t *testing.T) {
	// When response doesn't have channels as an array, falls back to cmdutil.PrintJSON.
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"channels": "not-an-array",
	}))
	defer cleanup()

	err := channelsListCmd.RunE(channelsListCmd, nil)
	if err != nil {
		t.Fatalf("channels list non-array: %v", err)
	}
}

func TestChannelsDeadLetterCmd_EmptyQueue(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"entries": []any{},
	}))
	defer cleanup()

	err := channelsDeadLetterCmd.RunE(channelsDeadLetterCmd, nil)
	if err != nil {
		t.Fatalf("channels dead-letter empty: %v", err)
	}
}

func TestChannelsDeadLetterCmd_NonArrayEntries(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"raw": "data",
	}))
	defer cleanup()

	err := channelsDeadLetterCmd.RunE(channelsDeadLetterCmd, nil)
	if err != nil {
		t.Fatalf("channels dead-letter non-array: %v", err)
	}
}

func TestChannelsTestCmd_ServerError(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "channel unavailable"})
	}))
	defer cleanup()

	err := channelsTestCmd.RunE(channelsTestCmd, []string{"discord"})
	if err == nil {
		t.Fatal("expected error for 502 response")
	}
}
