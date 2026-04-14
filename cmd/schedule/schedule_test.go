package schedule

import (
	"roboticus/cmd/internal/testhelp"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCronCreateCmd_WithMockServer(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/cron/jobs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "daily-backup" {
			t.Errorf("unexpected name: %v", body["name"])
		}
		if body["schedule_expr"] != "0 3 * * *" {
			t.Errorf("unexpected schedule: %v", body["schedule_expr"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "cron-42"})
	}))
	defer cleanup()

	err := cronCreateCmd.RunE(cronCreateCmd, []string{"daily-backup", "0 3 * * *"})
	if err != nil {
		t.Fatalf("cron create: %v", err)
	}
}

func TestCronDeleteCmd_WithMockServer(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/cron/jobs/cron-42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer cleanup()

	err := cronDeleteCmd.RunE(cronDeleteCmd, []string{"cron-42"})
	if err != nil {
		t.Fatalf("cron delete: %v", err)
	}
}

func TestCronRunCmd_WithMockServer(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/cron/jobs/cron-42/run" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "triggered"})
	}))
	defer cleanup()

	err := cronRunCmd.RunE(cronRunCmd, []string{"cron-42"})
	if err != nil {
		t.Fatalf("cron run: %v", err)
	}
}

func TestCronListCmd_WithDisabledJob(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"jobs": []any{
			map[string]any{"id": "1", "name": "active-job", "schedule_expr": "*/5 * * * *", "enabled": true},
			map[string]any{"id": "2", "name": "paused-job", "schedule_expr": "0 0 * * *", "enabled": false},
		},
	}))
	defer cleanup()

	err := cronListCmd.RunE(cronListCmd, nil)
	if err != nil {
		t.Fatalf("cron list with disabled job: %v", err)
	}
}

func TestCronListCmd_ServerError(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "db locked"})
	}))
	defer cleanup()

	err := cronListCmd.RunE(cronListCmd, nil)
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}
