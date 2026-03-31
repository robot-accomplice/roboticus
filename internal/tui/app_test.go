package tui

import "testing"

func TestNewModel(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	if m.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %s", m.baseURL)
	}
}

func TestNewModel_WithAPIKey(t *testing.T) {
	m := NewModel("http://localhost:8080", "secret123")
	if m.apiKey != "secret123" {
		t.Errorf("apiKey not set")
	}
}

func TestModel_View_Initial(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	view := m.View()
	if view == "" {
		t.Error("initial view should not be empty")
	}
}

func TestVisibleMessages(t *testing.T) {
	tests := []struct {
		height int
		want   int
	}{
		{10, 10},
		{0, 0},
		{100, 100},
	}
	for _, tt := range tests {
		got := visibleMessages(tt.height)
		if got <= 0 && tt.height > 0 {
			t.Errorf("visibleMessages(%d) = %d", tt.height, got)
		}
	}
}

func TestModel_AddLog(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.addLog("test log message")
	if len(m.logs) != 1 {
		t.Errorf("logs = %d, want 1", len(m.logs))
	}
}
