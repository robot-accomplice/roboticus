package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMetricsCostsCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "db error"})
	}))
	defer cleanup()

	err := metricsCostsCmd.RunE(metricsCostsCmd, nil)
	if err == nil {
		t.Fatal("expected error for costs failure")
	}
}

func TestMetricsCacheCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "cache error"})
	}))
	defer cleanup()

	err := metricsCacheCmd.RunE(metricsCacheCmd, nil)
	if err == nil {
		t.Fatal("expected error for cache failure")
	}
}

func TestMetricsCapacityCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "capacity error"})
	}))
	defer cleanup()

	err := metricsCapacityCmd.RunE(metricsCapacityCmd, nil)
	if err == nil {
		t.Fatal("expected error for capacity failure")
	}
}
