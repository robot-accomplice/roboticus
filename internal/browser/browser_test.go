package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewBrowser(t *testing.T) {
	b := NewBrowser(BrowserConfig{Enabled: false})
	if b == nil {
		t.Fatal("should not be nil")
	}
}

func TestBrowser_IsRunning_Initially(t *testing.T) {
	b := NewBrowser(BrowserConfig{})
	if b.IsRunning() {
		t.Error("should not be running initially")
	}
}

func TestBrowser_Stop_NotRunning(t *testing.T) {
	b := NewBrowser(BrowserConfig{})
	err := b.Stop()
	if err != nil {
		t.Errorf("stop when not running should not error: %v", err)
	}
}

func TestBrowser_Execute_NotRunning(t *testing.T) {
	b := NewBrowser(BrowserConfig{})
	result := b.Execute(context.Background(), &BrowserAction{
		Kind: ActionNavigate,
		URL:  "https://example.com",
	})
	if result.Error == "" {
		t.Error("execute on stopped browser should return error")
	}
}

func TestBrowser_CDPUrl(t *testing.T) {
	b := NewBrowser(BrowserConfig{CDPPort: DefaultCDPPort})
	url := b.cdpURL("/json/version")
	if url == "" {
		t.Error("should return non-empty URL")
	}
}

func TestBrowserConfig_Defaults(t *testing.T) {
	cfg := BrowserConfig{
		Enabled:  true,
		Headless: true,
		CDPPort:  DefaultCDPPort,
	}
	if cfg.CDPPort != 9222 {
		t.Errorf("default CDP port = %d", cfg.CDPPort)
	}
}

func TestActionKind_Values(t *testing.T) {
	kinds := []ActionKind{ActionNavigate, ActionClick, ActionType, ActionScreenshot, ActionEvaluate}
	seen := make(map[ActionKind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate action kind: %v", k)
		}
		seen[k] = true
	}
}

func TestActionResult_Error(t *testing.T) {
	r := ActionResult{Error: "something failed"}
	if r.Error == "" {
		t.Error("should have error")
	}
}

func TestActionResult_Success(t *testing.T) {
	r := ActionResult{Success: true, Data: json.RawMessage(`"screenshot data"`)}
	if !r.Success {
		t.Error("should be success")
	}
}
