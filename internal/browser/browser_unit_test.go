package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- isRecoverable ---

func TestIsRecoverable(t *testing.T) {
	tests := []struct {
		errMsg string
		want   bool
	}{
		{"websocket closed", true},
		{"broken pipe", true},
		{"connection refused", true},
		{"timeout", false},
		{"", false},
		{"some random error", false},
		{"WEBSOCKET CLOSED", true},       // Rust parity: case-insensitive matching
		{"CDP read error", true},         // Rust parity: CDP disconnect signature
		{"cdp send failed", true},        // Rust parity: CDP send failure
		{"connection reset", true},       // Rust parity: TCP reset
		{"browser not started", false},   // precondition, not recoverable
		{"browser not running", false},   // precondition, not recoverable
		{"URL scheme is blocked", false}, // security denial, not recoverable
	}
	for _, tt := range tests {
		if got := isRecoverable(tt.errMsg); got != tt.want {
			t.Errorf("isRecoverable(%q) = %v, want %v", tt.errMsg, got, tt.want)
		}
	}
}

// --- isIdempotent ---

func TestIsIdempotent(t *testing.T) {
	idempotent := []ActionKind{
		ActionNavigate, ActionScreenshot, ActionPDF, ActionEvaluate,
		ActionGetCookies, ActionReadPage, ActionGoBack, ActionGoForward, ActionReload,
	}
	for _, k := range idempotent {
		if !isIdempotent(k) {
			t.Errorf("isIdempotent(%q) should be true", k)
		}
	}

	nonIdempotent := []ActionKind{
		ActionClick, ActionType, ActionClearCookies,
	}
	for _, k := range nonIdempotent {
		if isIdempotent(k) {
			t.Errorf("isIdempotent(%q) should be false", k)
		}
	}

	// Unknown action kinds should not be idempotent.
	if isIdempotent(ActionKind("unknown_action")) {
		t.Error("unknown action kind should not be idempotent")
	}
}

// --- NewBrowserWithHTTP config defaults ---

func TestNewBrowserWithHTTP_AppliesDefaults(t *testing.T) {
	b := NewBrowserWithHTTP(BrowserConfig{}, nil)
	if b.cfg.CDPPort != DefaultCDPPort {
		t.Errorf("CDPPort = %d, want %d", b.cfg.CDPPort, DefaultCDPPort)
	}
	if b.cfg.TimeoutSeconds != DefaultTimeoutSeconds {
		t.Errorf("TimeoutSeconds = %d, want %d", b.cfg.TimeoutSeconds, DefaultTimeoutSeconds)
	}
	if b.client == nil {
		t.Error("client should not be nil when no HTTP client provided")
	}
}

func TestNewBrowserWithHTTP_PreservesCustomValues(t *testing.T) {
	b := NewBrowserWithHTTP(BrowserConfig{
		CDPPort:        1234,
		TimeoutSeconds: 60,
		Headless:       true,
		Enabled:        true,
		ExecutablePath: "/usr/bin/fake-chrome",
	}, nil)
	if b.cfg.CDPPort != 1234 {
		t.Errorf("CDPPort = %d, want 1234", b.cfg.CDPPort)
	}
	if b.cfg.TimeoutSeconds != 60 {
		t.Errorf("TimeoutSeconds = %d, want 60", b.cfg.TimeoutSeconds)
	}
	if !b.cfg.Headless {
		t.Error("Headless should be true")
	}
	if b.cfg.ExecutablePath != "/usr/bin/fake-chrome" {
		t.Errorf("ExecutablePath = %q", b.cfg.ExecutablePath)
	}
}

type mockDoer struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func TestNewBrowserWithHTTP_UsesInjectedClient(t *testing.T) {
	mock := &mockDoer{doFunc: func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("mock called")
	}}
	b := NewBrowserWithHTTP(BrowserConfig{}, mock)
	_, err := b.httpGet("http://localhost/test")
	if err == nil || err.Error() != "mock called" {
		t.Errorf("expected mock error, got: %v", err)
	}
}

// --- cdpURL ---

func TestCdpURL(t *testing.T) {
	tests := []struct {
		port int
		path string
		want string
	}{
		{9222, "/json/version", "http://127.0.0.1:9222/json/version"},
		{9222, "/json/list", "http://127.0.0.1:9222/json/list"},
		{8080, "", "http://127.0.0.1:8080"},
		{9222, "/json/new?about:blank", "http://127.0.0.1:9222/json/new?about:blank"},
	}
	for _, tt := range tests {
		b := NewBrowser(BrowserConfig{CDPPort: tt.port})
		got := b.cdpURL(tt.path)
		if got != tt.want {
			t.Errorf("cdpURL(%q) with port %d = %q, want %q", tt.path, tt.port, got, tt.want)
		}
	}
}

// --- CDPPort accessor ---

func TestBrowser_CDPPort(t *testing.T) {
	b := NewBrowser(BrowserConfig{CDPPort: 5555})
	if b.CDPPort() != 5555 {
		t.Errorf("CDPPort() = %d, want 5555", b.CDPPort())
	}
}

// --- Execute with not-running browser returns error for all action kinds ---

func TestExecute_NotRunning_AllActions(t *testing.T) {
	b := NewBrowser(BrowserConfig{})
	actions := []BrowserAction{
		{Kind: ActionNavigate, URL: "https://example.com"},
		{Kind: ActionClick, Selector: "#btn"},
		{Kind: ActionType, Selector: "#input", Text: "hello"},
		{Kind: ActionScreenshot},
		{Kind: ActionPDF},
		{Kind: ActionEvaluate, Script: "1+1"},
		{Kind: ActionGetCookies},
		{Kind: ActionClearCookies},
		{Kind: ActionReadPage},
		{Kind: ActionGoBack},
		{Kind: ActionGoForward},
		{Kind: ActionReload},
	}
	for _, a := range actions {
		result := b.Execute(context.Background(), &a)
		if result.Success {
			t.Errorf("action %q should fail when browser not running", a.Kind)
		}
		if result.Error != "browser not running" {
			t.Errorf("action %q: error = %q, want %q", a.Kind, result.Error, "browser not running")
		}
		if result.Action != a.Kind {
			t.Errorf("action kind = %q, want %q", result.Action, a.Kind)
		}
	}
}

// --- executeAction with nil session ---

func TestExecuteAction_NilSession_Navigate(t *testing.T) {
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionNavigate, URL: "http://example.com"})
	if r.Success || r.Error != "no CDP session" {
		t.Errorf("navigate with nil session: success=%v, error=%q", r.Success, r.Error)
	}
}

func TestExecuteAction_NilSession_Evaluate(t *testing.T) {
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionEvaluate, Script: "1+1"})
	if r.Success || r.Error != "no CDP session" {
		t.Errorf("evaluate with nil session: success=%v, error=%q", r.Success, r.Error)
	}
}

func TestExecuteAction_NilSession_Screenshot(t *testing.T) {
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionScreenshot})
	if r.Success || r.Error != "no CDP session" {
		t.Errorf("screenshot with nil session: success=%v, error=%q", r.Success, r.Error)
	}
}

func TestExecuteAction_NilSession_ReadPage(t *testing.T) {
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionReadPage})
	if r.Success || r.Error != "no CDP session" {
		t.Errorf("read_page with nil session: success=%v, error=%q", r.Success, r.Error)
	}
}

func TestExecuteAction_NilSession_GetCookies(t *testing.T) {
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionGetCookies})
	if r.Success || r.Error != "no CDP session" {
		t.Errorf("get_cookies with nil session: success=%v, error=%q", r.Success, r.Error)
	}
}

func TestExecuteAction_NilSession_SimpleCommands(t *testing.T) {
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	for _, kind := range []ActionKind{ActionGoBack, ActionGoForward, ActionReload, ActionClearCookies} {
		r := b.executeAction(context.Background(), &BrowserAction{Kind: kind})
		if r.Success || r.Error != "no CDP session" {
			t.Errorf("%s with nil session: success=%v, error=%q", kind, r.Success, r.Error)
		}
	}
}

func TestExecuteAction_NilSession_Click(t *testing.T) {
	// Click dispatches to cdpEvaluate, which checks for nil session.
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionClick, Selector: "#btn"})
	if r.Success || r.Error != "no CDP session" {
		t.Errorf("click with nil session: success=%v, error=%q", r.Success, r.Error)
	}
}

func TestExecuteAction_NilSession_Type(t *testing.T) {
	// Type also dispatches to cdpEvaluate.
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionType, Selector: "#input", Text: "hello"})
	if r.Success || r.Error != "no CDP session" {
		t.Errorf("type with nil session: success=%v, error=%q", r.Success, r.Error)
	}
}

func TestExecuteAction_UnknownAction(t *testing.T) {
	b := &Browser{cfg: BrowserConfig{CDPPort: 9222}, running: true}
	r := b.executeAction(context.Background(), &BrowserAction{Kind: ActionKind("nonexistent")})
	if r.Success {
		t.Error("unknown action should not succeed")
	}
	if r.Error != "unknown action" {
		t.Errorf("error = %q, want %q", r.Error, "unknown action")
	}
}

// --- JSON round-trips for data types ---

func TestBrowserAction_JSON_RoundTrip(t *testing.T) {
	action := BrowserAction{
		Kind:     ActionNavigate,
		URL:      "https://example.com",
		Selector: "",
		Text:     "",
		Script:   "",
	}
	data, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	var decoded BrowserAction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != ActionNavigate {
		t.Errorf("Kind = %q, want %q", decoded.Kind, ActionNavigate)
	}
	if decoded.URL != "https://example.com" {
		t.Errorf("URL = %q", decoded.URL)
	}
}

func TestBrowserAction_JSON_AllFields(t *testing.T) {
	action := BrowserAction{
		Kind:     ActionType,
		URL:      "http://test.com",
		Selector: "#name",
		Text:     "John",
		Script:   "alert(1)",
	}
	data, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	var decoded BrowserAction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != ActionType || decoded.Selector != "#name" || decoded.Text != "John" {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestBrowserAction_JSON_OmitsEmpty(t *testing.T) {
	action := BrowserAction{Kind: ActionScreenshot}
	data, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	// URL, Selector, Text, Script should be omitted.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["url"]; ok {
		t.Error("url should be omitted when empty")
	}
	if _, ok := m["selector"]; ok {
		t.Error("selector should be omitted when empty")
	}
}

func TestActionResult_JSON_RoundTrip(t *testing.T) {
	r := ActionResult{
		Success: true,
		Action:  ActionScreenshot,
		Data:    json.RawMessage(`{"data_base64":"abc"}`),
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ActionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Success || decoded.Action != ActionScreenshot {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestActionResult_JSON_WithError(t *testing.T) {
	r := ActionResult{
		Success: false,
		Action:  ActionNavigate,
		Error:   "browser not running",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ActionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Success {
		t.Error("should not be success")
	}
	if decoded.Error != "browser not running" {
		t.Errorf("error = %q", decoded.Error)
	}
}

func TestPageInfo_JSON(t *testing.T) {
	p := PageInfo{ID: "abc-123", URL: "https://example.com", Title: "Test Page"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PageInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "abc-123" || decoded.URL != "https://example.com" || decoded.Title != "Test Page" {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestScreenshotResult_JSON(t *testing.T) {
	s := ScreenshotResult{DataBase64: "iVBOR...", Format: "png", Width: 1920, Height: 1080}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ScreenshotResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Format != "png" || decoded.Width != 1920 || decoded.Height != 1080 {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestPageContent_JSON(t *testing.T) {
	pc := PageContent{URL: "https://example.com", Title: "Example", Text: "Hello World", HTMLLength: 500}
	data, err := json.Marshal(pc)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PageContent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Text != "Hello World" || decoded.HTMLLength != 500 {
		t.Errorf("decoded = %+v", decoded)
	}
}

// --- CdpTarget JSON parsing ---

func TestCdpTarget_JSON(t *testing.T) {
	raw := `{"id":"abc","url":"https://example.com","title":"Example","type":"page","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/page/abc"}`
	var target CdpTarget
	if err := json.Unmarshal([]byte(raw), &target); err != nil {
		t.Fatal(err)
	}
	if target.ID != "abc" {
		t.Errorf("ID = %q", target.ID)
	}
	if target.Type != "page" {
		t.Errorf("Type = %q", target.Type)
	}
	if target.WebSocketDebuggerURL != "ws://127.0.0.1:9222/devtools/page/abc" {
		t.Errorf("WebSocketDebuggerURL = %q", target.WebSocketDebuggerURL)
	}
}

// --- FindPageTarget with mock HTTP server ---

func TestFindPageTarget_Success(t *testing.T) {
	targets := []CdpTarget{
		{ID: "bg", URL: "", Title: "", Type: "background_page", WebSocketDebuggerURL: "ws://x"},
		{ID: "page1", URL: "https://example.com", Title: "Example", Type: "page", WebSocketDebuggerURL: "ws://127.0.0.1:9222/devtools/page/page1"},
	}
	data, _ := json.Marshal(targets)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	target, err := FindPageTarget(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if target.ID != "page1" {
		t.Errorf("target ID = %q, want page1", target.ID)
	}
	if target.Type != "page" {
		t.Errorf("target type = %q", target.Type)
	}
}

func TestFindPageTarget_NoPageTargets(t *testing.T) {
	targets := []CdpTarget{
		{ID: "bg", Type: "background_page", WebSocketDebuggerURL: "ws://x"},
		{ID: "sw", Type: "service_worker", WebSocketDebuggerURL: "ws://y"},
	}
	data, _ := json.Marshal(targets)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	_, err := FindPageTarget(srv.URL)
	if err == nil {
		t.Error("expected error when no page targets")
	}
}

func TestFindPageTarget_PageWithoutWSURL(t *testing.T) {
	// A page target with empty WebSocketDebuggerURL should be skipped.
	targets := []CdpTarget{
		{ID: "p", Type: "page", WebSocketDebuggerURL: ""},
	}
	data, _ := json.Marshal(targets)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	_, err := FindPageTarget(srv.URL)
	if err == nil {
		t.Error("expected error when page target has no ws URL")
	}
}

func TestFindPageTarget_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := FindPageTarget(srv.URL)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFindPageTarget_ConnectionRefused(t *testing.T) {
	_, err := FindPageTarget("http://127.0.0.1:1") // nothing listening
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- ListTargets with mock HTTP client ---

func TestListTargets_Success(t *testing.T) {
	respBody := `[
		{"id":"p1","url":"https://a.com","title":"A","type":"page"},
		{"id":"bg1","url":"","title":"","type":"background_page"},
		{"id":"p2","url":"https://b.com","title":"B","type":"page"}
	]`

	mock := &mockDoer{doFunc: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(respBody)),
		}, nil
	}}
	b := NewBrowserWithHTTP(BrowserConfig{CDPPort: 9222}, mock)
	b.running = true

	pages, err := b.ListTargets()
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(pages))
	}
	if pages[0].ID != "p1" || pages[1].ID != "p2" {
		t.Errorf("pages = %+v", pages)
	}
}

func TestListTargets_NoPages(t *testing.T) {
	respBody := `[{"id":"bg1","url":"","title":"","type":"background_page"}]`
	mock := &mockDoer{doFunc: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(respBody)),
		}, nil
	}}
	b := NewBrowserWithHTTP(BrowserConfig{CDPPort: 9222}, mock)
	b.running = true

	pages, err := b.ListTargets()
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 0 {
		t.Errorf("expected 0 pages, got %d", len(pages))
	}
}

func TestListTargets_HTTPError(t *testing.T) {
	mock := &mockDoer{doFunc: func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("connection refused")
	}}
	b := NewBrowserWithHTTP(BrowserConfig{CDPPort: 9222}, mock)
	b.running = true

	_, err := b.ListTargets()
	if err == nil {
		t.Error("expected error on HTTP failure")
	}
}

func TestListTargets_InvalidJSON(t *testing.T) {
	mock := &mockDoer{doFunc: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString("not json")),
		}, nil
	}}
	b := NewBrowserWithHTTP(BrowserConfig{CDPPort: 9222}, mock)
	b.running = true

	_, err := b.ListTargets()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// --- ActionKind constants ---

func TestActionKind_AllDistinct(t *testing.T) {
	all := []ActionKind{
		ActionNavigate, ActionClick, ActionType, ActionScreenshot,
		ActionPDF, ActionEvaluate, ActionGetCookies, ActionClearCookies,
		ActionReadPage, ActionGoBack, ActionGoForward, ActionReload,
	}
	seen := make(map[ActionKind]bool, len(all))
	for _, k := range all {
		if seen[k] {
			t.Errorf("duplicate ActionKind: %q", k)
		}
		seen[k] = true
	}
	if len(seen) != 12 {
		t.Errorf("expected 12 distinct action kinds, got %d", len(seen))
	}
}

func TestActionKind_StringValues(t *testing.T) {
	tests := map[ActionKind]string{
		ActionNavigate:     "navigate",
		ActionClick:        "click",
		ActionType:         "type",
		ActionScreenshot:   "screenshot",
		ActionPDF:          "pdf",
		ActionEvaluate:     "evaluate",
		ActionGetCookies:   "get_cookies",
		ActionClearCookies: "clear_cookies",
		ActionReadPage:     "read_page",
		ActionGoBack:       "go_back",
		ActionGoForward:    "go_forward",
		ActionReload:       "reload",
	}
	for kind, want := range tests {
		if string(kind) != want {
			t.Errorf("ActionKind %v = %q, want %q", kind, string(kind), want)
		}
	}
}

// --- BrowserConfig struct ---

func TestBrowserConfig_ZeroValueDefaults(t *testing.T) {
	var cfg BrowserConfig
	if cfg.Enabled {
		t.Error("default Enabled should be false")
	}
	if cfg.Headless {
		t.Error("default Headless should be false")
	}
	if cfg.CDPPort != 0 {
		t.Errorf("default CDPPort should be 0, got %d", cfg.CDPPort)
	}
}

// --- Start/Stop idempotency without real browser ---

func TestBrowser_StopIdempotent(t *testing.T) {
	b := NewBrowser(BrowserConfig{})
	// Stopping a non-running browser multiple times should be fine.
	for i := 0; i < 3; i++ {
		if err := b.Stop(); err != nil {
			t.Errorf("stop iteration %d: %v", i, err)
		}
	}
}

// --- httpGet constructs correct request ---

func TestHttpGet_ConstructsRequest(t *testing.T) {
	var gotMethod string
	var gotURL string
	mock := &mockDoer{doFunc: func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotURL = req.URL.String()
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString("{}")),
		}, nil
	}}
	b := NewBrowserWithHTTP(BrowserConfig{}, mock)
	_, _ = b.httpGet("http://127.0.0.1:9222/json/version")
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotURL != "http://127.0.0.1:9222/json/version" {
		t.Errorf("url = %q", gotURL)
	}
}

func TestHttpGet_InvalidURL(t *testing.T) {
	b := NewBrowser(BrowserConfig{})
	_, err := b.httpGet("://bad-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// --- BrowserAction JSON from external input ---

func TestBrowserAction_UnmarshalFromExternal(t *testing.T) {
	tests := []struct {
		name string
		json string
		want BrowserAction
	}{
		{
			name: "navigate",
			json: `{"kind":"navigate","url":"https://example.com"}`,
			want: BrowserAction{Kind: ActionNavigate, URL: "https://example.com"},
		},
		{
			name: "click",
			json: `{"kind":"click","selector":"#btn"}`,
			want: BrowserAction{Kind: ActionClick, Selector: "#btn"},
		},
		{
			name: "type",
			json: `{"kind":"type","selector":"#input","text":"hello"}`,
			want: BrowserAction{Kind: ActionType, Selector: "#input", Text: "hello"},
		},
		{
			name: "evaluate",
			json: `{"kind":"evaluate","script":"document.title"}`,
			want: BrowserAction{Kind: ActionEvaluate, Script: "document.title"},
		},
		{
			name: "screenshot",
			json: `{"kind":"screenshot"}`,
			want: BrowserAction{Kind: ActionScreenshot},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got BrowserAction
			if err := json.Unmarshal([]byte(tt.json), &got); err != nil {
				t.Fatal(err)
			}
			if got.Kind != tt.want.Kind || got.URL != tt.want.URL || got.Selector != tt.want.Selector || got.Text != tt.want.Text || got.Script != tt.want.Script {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// --- Constants ---

func TestDefaultConstants(t *testing.T) {
	if DefaultCDPPort != 9222 {
		t.Errorf("DefaultCDPPort = %d", DefaultCDPPort)
	}
	if DefaultTimeoutSeconds != 30 {
		t.Errorf("DefaultTimeoutSeconds = %d", DefaultTimeoutSeconds)
	}
}
