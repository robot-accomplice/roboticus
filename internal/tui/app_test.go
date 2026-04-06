package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// NewModel
// ---------------------------------------------------------------------------

func TestNewModel(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	if m.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want %q", m.baseURL, "http://localhost:8080")
	}
	if m.focused != PanelInput {
		t.Errorf("focused = %d, want PanelInput (%d)", m.focused, PanelInput)
	}
	if m.status != StatusConnecting {
		t.Errorf("status = %d, want StatusConnecting", m.status)
	}
	if len(m.messages) != 0 {
		t.Errorf("messages should be empty, got %d", len(m.messages))
	}
	if len(m.logs) != 0 {
		t.Errorf("logs should be empty, got %d", len(m.logs))
	}
}

func TestNewModel_WithAPIKey(t *testing.T) {
	m := NewModel("http://localhost:8080", "secret123")
	if m.apiKey != "secret123" {
		t.Errorf("apiKey = %q, want %q", m.apiKey, "secret123")
	}
}

// ---------------------------------------------------------------------------
// FocusedPanel constants
// ---------------------------------------------------------------------------

func TestFocusedPanelValues(t *testing.T) {
	if PanelInput != 0 {
		t.Errorf("PanelInput = %d, want 0", PanelInput)
	}
	if PanelChat != 1 {
		t.Errorf("PanelChat = %d, want 1", PanelChat)
	}
	if PanelLogs != 2 {
		t.Errorf("PanelLogs = %d, want 2", PanelLogs)
	}
}

// ---------------------------------------------------------------------------
// ConnectionStatus constants
// ---------------------------------------------------------------------------

func TestConnectionStatusValues(t *testing.T) {
	if StatusConnecting != 0 {
		t.Errorf("StatusConnecting = %d, want 0", StatusConnecting)
	}
	if StatusConnected != 1 {
		t.Errorf("StatusConnected = %d, want 1", StatusConnected)
	}
	if StatusDisconnected != 2 {
		t.Errorf("StatusDisconnected = %d, want 2", StatusDisconnected)
	}
}

// ---------------------------------------------------------------------------
// visibleMessages
// ---------------------------------------------------------------------------

func TestVisibleMessages(t *testing.T) {
	tests := []struct {
		height int
		want   int
	}{
		{0, 3},   // clamp to minimum 3
		{5, 3},   // 5-8 = -3, clamp to 3
		{8, 3},   // 8-8 = 0, clamp to 3
		{10, 3},  // 10-8 = 2, clamp to 3
		{11, 3},  // 11-8 = 3, exactly 3
		{12, 4},  // 12-8 = 4
		{20, 12}, // 20-8 = 12
		{50, 42}, // 50-8 = 42
	}
	for _, tt := range tests {
		got := visibleMessages(tt.height)
		if got != tt.want {
			t.Errorf("visibleMessages(%d) = %d, want %d", tt.height, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// addLog
// ---------------------------------------------------------------------------

func TestModel_AddLog(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.addLog("test log message")
	if len(m.logs) != 1 {
		t.Fatalf("logs count = %d, want 1", len(m.logs))
	}
	if !strings.Contains(m.logs[0], "test log message") {
		t.Errorf("log entry = %q, want it to contain %q", m.logs[0], "test log message")
	}
	// Verify timestamp prefix format [HH:MM:SS].
	if !strings.HasPrefix(m.logs[0], "[") {
		t.Errorf("log entry should start with '[', got %q", m.logs[0])
	}
}

func TestModel_AddLog_Multiple(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.addLog("first")
	m.addLog("second")
	m.addLog("third")
	if len(m.logs) != 3 {
		t.Fatalf("logs count = %d, want 3", len(m.logs))
	}
	if !strings.Contains(m.logs[2], "third") {
		t.Errorf("third log = %q", m.logs[2])
	}
}

// ---------------------------------------------------------------------------
// View — zero dimensions
// ---------------------------------------------------------------------------

func TestModel_View_ZeroDimensions(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	view := m.View()
	if view != "Initializing..." {
		t.Errorf("View with zero dims = %q, want %q", view, "Initializing...")
	}
}

// ---------------------------------------------------------------------------
// View — with dimensions set
// ---------------------------------------------------------------------------

func TestModel_View_WithDimensions(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 120
	m.height = 40
	view := m.View()
	if view == "Initializing..." {
		t.Error("View should render full layout when dimensions are set")
	}
	if view == "" {
		t.Error("View should not be empty")
	}
}

func TestModel_View_StatusBar_Connecting(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	view := m.View()
	if !strings.Contains(view, "CONNECTING") {
		t.Error("status bar should show CONNECTING for initial state")
	}
}

func TestModel_View_StatusBar_Connected(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	m.status = StatusConnected
	m.sessionID = "abcdef1234567890"
	view := m.View()
	if !strings.Contains(view, "CONNECTED") {
		t.Error("status bar should show CONNECTED")
	}
	if !strings.Contains(view, "session:abcdef12") {
		t.Errorf("status bar should show truncated session ID, got:\n%s", view)
	}
}

func TestModel_View_StatusBar_Disconnected(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	m.status = StatusDisconnected
	view := m.View()
	if !strings.Contains(view, "DISCONNECTED") {
		t.Error("status bar should show DISCONNECTED")
	}
}

func TestModel_View_StreamingIndicator(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	m.streaming = true
	view := m.View()
	if !strings.Contains(view, "thinking...") {
		t.Error("view should show thinking indicator when streaming")
	}
}

func TestModel_View_ChatMessages(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	m.messages = []ChatMessage{
		{Role: "user", Content: "hello world", Timestamp: time.Now()},
		{Role: "assistant", Content: "hi there", Timestamp: time.Now()},
	}
	view := m.View()
	if !strings.Contains(view, "you: hello world") {
		t.Error("view should show user messages with 'you' role label")
	}
	if !strings.Contains(view, "assistant: hi there") {
		t.Error("view should show assistant messages")
	}
}

func TestModel_View_LogPanel(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 200 // wide enough for the log panel to not truncate
	m.height = 30
	m.addLog("conn ok")
	view := m.View()
	if !strings.Contains(view, "conn ok") {
		t.Errorf("view should render log messages, got:\n%s", view)
	}
}

func TestModel_View_InputCursor(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	m.focused = PanelInput
	m.input = "hello"
	m.cursor = 5
	view := m.View()
	// When cursor is at end, a block cursor char is appended.
	if !strings.Contains(view, "\u2588") {
		t.Error("view should show block cursor character when input panel is focused")
	}
}

func TestModel_View_SmallHeight(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 80
	m.height = 5 // very small -- chatHeight should clamp to 3
	view := m.View()
	if view == "" || view == "Initializing..." {
		t.Error("view should render even with small height")
	}
}

// ---------------------------------------------------------------------------
// Update — WindowSizeMsg
// ---------------------------------------------------------------------------

func TestUpdate_WindowSizeMsg(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := asModel(t, updated)
	if model.width != 120 || model.height != 40 {
		t.Errorf("dimensions = %dx%d, want 120x40", model.width, model.height)
	}
	if cmd != nil {
		t.Error("WindowSizeMsg should not produce a command")
	}
}

// ---------------------------------------------------------------------------
// Update — sessionCreatedMsg
// ---------------------------------------------------------------------------

func TestUpdate_SessionCreatedMsg(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	updated, cmd := m.Update(sessionCreatedMsg{sessionID: "abcdef1234567890"})
	model := asModel(t, updated)
	if model.sessionID != "abcdef1234567890" {
		t.Errorf("sessionID = %q", model.sessionID)
	}
	if model.status != StatusConnected {
		t.Errorf("status = %d, want StatusConnected", model.status)
	}
	if len(model.logs) == 0 {
		t.Error("should have logged session creation")
	}
	if !strings.Contains(model.logs[0], "abcdef12") {
		t.Errorf("log should contain truncated session ID, got %q", model.logs[0])
	}
	if cmd != nil {
		t.Error("sessionCreatedMsg should not produce a command")
	}
}

// ---------------------------------------------------------------------------
// Update — agentReplyMsg
// ---------------------------------------------------------------------------

func TestUpdate_AgentReplyMsg(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.streaming = true
	m.height = 30
	updated, _ := m.Update(agentReplyMsg{content: "I can help with that"})
	model := asModel(t, updated)
	if len(model.messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(model.messages))
	}
	if model.messages[0].Role != "assistant" {
		t.Errorf("role = %q, want assistant", model.messages[0].Role)
	}
	if model.messages[0].Content != "I can help with that" {
		t.Errorf("content = %q", model.messages[0].Content)
	}
	if model.streaming {
		t.Error("streaming should be false after reply")
	}
}

// ---------------------------------------------------------------------------
// Update — errorMsg
// ---------------------------------------------------------------------------

func TestUpdate_ErrorMsg(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.streaming = true
	updated, _ := m.Update(errorMsg{err: fmt.Errorf("network timeout")})
	model := asModel(t, updated)
	if model.streaming {
		t.Error("streaming should be false after error")
	}
	if len(model.logs) == 0 {
		t.Fatal("should log the error")
	}
	if !strings.Contains(model.logs[0], "ERROR: network timeout") {
		t.Errorf("log = %q", model.logs[0])
	}
}

// ---------------------------------------------------------------------------
// Update — unknown msg (no-op)
// ---------------------------------------------------------------------------

func TestUpdate_UnknownMsg(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	type customMsg struct{}
	updated, cmd := m.Update(customMsg{})
	model := asModel(t, updated)
	if model.focused != PanelInput {
		t.Error("unknown message should not change model state")
	}
	if cmd != nil {
		t.Error("unknown message should not produce a command")
	}
}

// ---------------------------------------------------------------------------
// Key handling — tab / shift+tab panel cycling
// ---------------------------------------------------------------------------

func TestHandleKey_Tab(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	// Input -> Chat -> Logs -> Input
	steps := []FocusedPanel{PanelChat, PanelLogs, PanelInput}
	for i, expected := range steps {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = asModel(t, updated)
		if m.focused != expected {
			t.Errorf("step %d: focused = %d, want %d", i, m.focused, expected)
		}
	}
}

func TestHandleKey_ShiftTab(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	// Input -> Logs -> Chat -> Input
	steps := []FocusedPanel{PanelLogs, PanelChat, PanelInput}
	for i, expected := range steps {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		m = asModel(t, updated)
		if m.focused != expected {
			t.Errorf("step %d: focused = %d, want %d", i, m.focused, expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Key handling — ctrl+c quit
// ---------------------------------------------------------------------------

func TestHandleKey_CtrlC(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should produce a quit command")
	}
	// Execute the command and check for tea.Quit behavior.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c cmd should produce QuitMsg, got %T", msg)
	}
}

// ---------------------------------------------------------------------------
// Key handling — text input
// ---------------------------------------------------------------------------

func TestHandleKey_TypingCharacters(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput

	for _, ch := range "hello" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = asModel(t, updated)
	}
	if m.input != "hello" {
		t.Errorf("input = %q, want %q", m.input, "hello")
	}
	if m.cursor != 5 {
		t.Errorf("cursor = %d, want 5", m.cursor)
	}
}

func TestHandleKey_TypingIgnoredInOtherPanels(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelChat
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model := asModel(t, updated)
	if model.input != "" {
		t.Errorf("typing in Chat panel should not modify input, got %q", model.input)
	}
}

// ---------------------------------------------------------------------------
// Key handling — backspace
// ---------------------------------------------------------------------------

func TestHandleKey_Backspace(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "hello"
	m.cursor = 5

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model := asModel(t, updated)
	if model.input != "hell" {
		t.Errorf("input after backspace = %q, want %q", model.input, "hell")
	}
	if model.cursor != 4 {
		t.Errorf("cursor = %d, want 4", model.cursor)
	}
}

func TestHandleKey_Backspace_AtStart(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "hello"
	m.cursor = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model := asModel(t, updated)
	if model.input != "hello" {
		t.Errorf("input should be unchanged, got %q", model.input)
	}
	if model.cursor != 0 {
		t.Errorf("cursor should stay at 0, got %d", model.cursor)
	}
}

func TestHandleKey_Backspace_MidString(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "hello"
	m.cursor = 3 // between 'l' and 'l'

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model := asModel(t, updated)
	if model.input != "helo" {
		t.Errorf("input = %q, want %q", model.input, "helo")
	}
	if model.cursor != 2 {
		t.Errorf("cursor = %d, want 2", model.cursor)
	}
}

// ---------------------------------------------------------------------------
// Key handling — left/right cursor movement
// ---------------------------------------------------------------------------

func TestHandleKey_LeftRight(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "abc"
	m.cursor = 3

	// Left twice.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = asModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = asModel(t, updated)
	if m.cursor != 1 {
		t.Errorf("cursor after 2 lefts = %d, want 1", m.cursor)
	}

	// Right once.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = asModel(t, updated)
	if m.cursor != 2 {
		t.Errorf("cursor after right = %d, want 2", m.cursor)
	}
}

func TestHandleKey_Left_AtBoundary(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "a"
	m.cursor = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model := asModel(t, updated)
	if model.cursor != 0 {
		t.Errorf("cursor should stay at 0, got %d", model.cursor)
	}
}

func TestHandleKey_Right_AtBoundary(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "ab"
	m.cursor = 2

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	model := asModel(t, updated)
	if model.cursor != 2 {
		t.Errorf("cursor should stay at 2, got %d", model.cursor)
	}
}

// ---------------------------------------------------------------------------
// Key handling — up/down scrolling
// ---------------------------------------------------------------------------

func TestHandleKey_UpDown_ChatPanel(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelChat
	m.chatScroll = 5

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = asModel(t, updated)
	if m.chatScroll != 4 {
		t.Errorf("chatScroll after up = %d, want 4", m.chatScroll)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = asModel(t, updated)
	if m.chatScroll != 5 {
		t.Errorf("chatScroll after down = %d, want 5", m.chatScroll)
	}
}

func TestHandleKey_Up_ChatAtZero(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelChat
	m.chatScroll = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	model := asModel(t, updated)
	if model.chatScroll != 0 {
		t.Errorf("chatScroll should stay at 0, got %d", model.chatScroll)
	}
}

func TestHandleKey_UpDown_LogPanel(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelLogs
	m.logScroll = 3

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = asModel(t, updated)
	if m.logScroll != 2 {
		t.Errorf("logScroll after up = %d, want 2", m.logScroll)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = asModel(t, updated)
	if m.logScroll != 3 {
		t.Errorf("logScroll after down = %d, want 3", m.logScroll)
	}
}

func TestHandleKey_Up_LogAtZero(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelLogs
	m.logScroll = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	model := asModel(t, updated)
	if model.logScroll != 0 {
		t.Errorf("logScroll should stay at 0, got %d", model.logScroll)
	}
}

func TestHandleKey_UpDown_InputPanel_NoOp(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	model := asModel(t, updated)
	// Up/down in input panel should not change scroll values.
	if model.chatScroll != 0 || model.logScroll != 0 {
		t.Error("up/down in input panel should not change scroll offsets")
	}
}

// ---------------------------------------------------------------------------
// Key handling — enter sends message
// ---------------------------------------------------------------------------

func TestHandleKey_Enter_SendsMessage(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "test message"
	m.cursor = 12
	m.height = 30

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := asModel(t, updated)
	if model.input != "" {
		t.Errorf("input should be cleared, got %q", model.input)
	}
	if model.cursor != 0 {
		t.Errorf("cursor should be 0, got %d", model.cursor)
	}
	if !model.streaming {
		t.Error("streaming should be true after sending")
	}
	if len(model.messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(model.messages))
	}
	if model.messages[0].Role != "user" {
		t.Errorf("message role = %q, want user", model.messages[0].Role)
	}
	if model.messages[0].Content != "test message" {
		t.Errorf("message content = %q", model.messages[0].Content)
	}
	if cmd == nil {
		t.Error("enter should produce a send command")
	}
}

func TestHandleKey_Enter_EmptyInput(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = ""

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := asModel(t, updated)
	if len(model.messages) != 0 {
		t.Error("empty input should not send a message")
	}
	if cmd != nil {
		t.Error("empty input should not produce a command")
	}
}

func TestHandleKey_Enter_WhitespaceOnly(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelInput
	m.input = "   "

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := asModel(t, updated)
	if len(model.messages) != 0 {
		t.Error("whitespace-only input should not send a message")
	}
	if cmd != nil {
		t.Error("whitespace-only input should not produce a command")
	}
}

func TestHandleKey_Enter_NotInputPanel(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.focused = PanelChat
	m.input = "some text"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := asModel(t, updated)
	if len(model.messages) != 0 {
		t.Error("enter in chat panel should not send a message")
	}
	if cmd != nil {
		t.Error("enter in chat panel should not produce a command")
	}
}

// ---------------------------------------------------------------------------
// Init produces commands
// ---------------------------------------------------------------------------

func TestModel_Init(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a command (batch of window title + session creation)")
	}
}

// ---------------------------------------------------------------------------
// tuiAPIPost — integration via httptest
// ---------------------------------------------------------------------------

func TestTuiAPIPost_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["key"] != "value" {
			t.Errorf("request body key = %v", body["key"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "sess-123"})
	}))
	defer srv.Close()

	data, err := tuiAPIPost(srv.URL, map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("tuiAPIPost: %v", err)
	}
	if data["id"] != "sess-123" {
		t.Errorf("response id = %v", data["id"])
	}
}

func TestTuiAPIPost_ConnectionError(t *testing.T) {
	_, err := tuiAPIPost("http://127.0.0.1:1", map[string]any{})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !strings.Contains(err.Error(), "connection failed") {
		t.Errorf("error = %v, want 'connection failed'", err)
	}
}

func TestTuiAPIPost_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := tuiAPIPost(srv.URL, map[string]any{})
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error = %v, want 'invalid JSON'", err)
	}
}

// ---------------------------------------------------------------------------
// createSession command — integration
// ---------------------------------------------------------------------------

func TestCreateSession_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "session-abcdef12"})
	}))
	defer srv.Close()

	m := NewModel(srv.URL, "")
	cmd := m.createSession()
	msg := cmd()
	if created, ok := msg.(sessionCreatedMsg); ok {
		if created.sessionID != "session-abcdef12" {
			t.Errorf("sessionID = %q", created.sessionID)
		}
	} else {
		t.Errorf("expected sessionCreatedMsg, got %T", msg)
	}
}

func TestCreateSession_NoID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer srv.Close()

	m := NewModel(srv.URL, "")
	cmd := m.createSession()
	msg := cmd()
	if errMsg, ok := msg.(errorMsg); ok {
		if !strings.Contains(errMsg.err.Error(), "no session id") {
			t.Errorf("error = %v", errMsg.err)
		}
	} else {
		t.Errorf("expected errorMsg, got %T", msg)
	}
}

func TestCreateSession_ConnectionFailure(t *testing.T) {
	m := NewModel("http://127.0.0.1:1", "")
	cmd := m.createSession()
	msg := cmd()
	if _, ok := msg.(errorMsg); !ok {
		t.Errorf("expected errorMsg, got %T", msg)
	}
}

// ---------------------------------------------------------------------------
// sendMessage command — integration
// ---------------------------------------------------------------------------

func TestSendMessage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["content"] != "hello bot" {
			t.Errorf("content = %v", body["content"])
		}
		if body["session_id"] != "sess-1" {
			t.Errorf("session_id = %v", body["session_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"content": "Hello! How can I help?"})
	}))
	defer srv.Close()

	m := NewModel(srv.URL, "")
	m.sessionID = "sess-1"
	cmd := m.sendMessage("hello bot")
	msg := cmd()
	if reply, ok := msg.(agentReplyMsg); ok {
		if reply.content != "Hello! How can I help?" {
			t.Errorf("content = %q", reply.content)
		}
	} else {
		t.Errorf("expected agentReplyMsg, got %T", msg)
	}
}

func TestSendMessage_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"content": ""})
	}))
	defer srv.Close()

	m := NewModel(srv.URL, "")
	cmd := m.sendMessage("test")
	msg := cmd()
	if reply, ok := msg.(agentReplyMsg); ok {
		if reply.content != "(empty response)" {
			t.Errorf("content = %q, want %q", reply.content, "(empty response)")
		}
	} else {
		t.Errorf("expected agentReplyMsg, got %T", msg)
	}
}

func TestSendMessage_ConnectionFailure(t *testing.T) {
	m := NewModel("http://127.0.0.1:1", "")
	cmd := m.sendMessage("test")
	msg := cmd()
	if _, ok := msg.(errorMsg); !ok {
		t.Errorf("expected errorMsg, got %T", msg)
	}
}

// asModel extracts the Model from a tea.Model, handling both value and pointer receivers.
func asModel(t *testing.T, tm tea.Model) Model {
	t.Helper()
	switch v := tm.(type) {
	case Model:
		return v
	case *Model:
		return *v
	default:
		t.Fatalf("unexpected tea.Model type: %T", tm)
		return Model{}
	}
}

// ---------------------------------------------------------------------------
// ChatMessage struct
// ---------------------------------------------------------------------------

func TestChatMessage_Fields(t *testing.T) {
	now := time.Now()
	msg := ChatMessage{Role: "user", Content: "hello", Timestamp: now}
	if msg.Role != "user" || msg.Content != "hello" || msg.Timestamp != now {
		t.Errorf("ChatMessage fields not set correctly: %+v", msg)
	}
}

// ---------------------------------------------------------------------------
// Complex scenario: full conversation flow
// ---------------------------------------------------------------------------

func TestFullConversationFlow(t *testing.T) {
	m := NewModel("http://localhost:8080", "key")
	m.width = 100
	m.height = 30

	// 1. Receive session.
	updated, _ := m.Update(sessionCreatedMsg{sessionID: "session-12345678"})
	m = asModel(t, updated)
	if m.status != StatusConnected {
		t.Fatal("should be connected")
	}

	// 2. Type a message.
	m.focused = PanelInput
	for _, ch := range "hi" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = asModel(t, updated)
	}
	if m.input != "hi" {
		t.Fatalf("input = %q, want %q", m.input, "hi")
	}

	// 3. Press enter (sends message). We can't easily run the cmd here
	//    since it would try to POST, but we verify the model state.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, updated)
	if m.input != "" {
		t.Errorf("input should be cleared")
	}
	if !m.streaming {
		t.Error("should be streaming")
	}
	if len(m.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(m.messages))
	}
	if cmd == nil {
		t.Error("should have a send command")
	}

	// 4. Receive reply.
	updated, _ = m.Update(agentReplyMsg{content: "Hello!"})
	m = asModel(t, updated)
	if m.streaming {
		t.Error("streaming should be off after reply")
	}
	if len(m.messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(m.messages))
	}

	// 5. View should contain both messages.
	view := m.View()
	if !strings.Contains(view, "you: hi") {
		t.Error("view should contain user message")
	}
	if !strings.Contains(view, "assistant: Hello!") {
		t.Error("view should contain assistant reply")
	}

	// 6. Tab to chat panel and scroll.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = asModel(t, updated)
	if m.focused != PanelChat {
		t.Error("should be on chat panel")
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestModel_View_SessionIDShort(t *testing.T) {
	// sessionID shorter than 8 chars should not panic.
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	m.status = StatusConnected
	m.sessionID = "abc" // too short for [:8]
	// View checks len(m.sessionID) >= 8, so this should not include session text.
	view := m.View()
	if strings.Contains(view, "session:") {
		t.Error("short session ID should not be displayed")
	}
}

func TestModel_View_CursorMidInput(t *testing.T) {
	m := NewModel("http://localhost:8080", "")
	m.width = 100
	m.height = 30
	m.focused = PanelInput
	m.input = "abcdef"
	m.cursor = 3
	view := m.View()
	// Cursor replaces the character at position 3 ('d') with block char.
	if !strings.Contains(view, "\u2588") {
		t.Error("view should contain block cursor character")
	}
}
