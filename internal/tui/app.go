package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FocusedPanel indicates which panel has keyboard focus.
type FocusedPanel int

const (
	PanelInput FocusedPanel = iota
	PanelChat
	PanelLogs
)

// ChatMessage is a single message in the conversation.
type ChatMessage struct {
	Role      string
	Content   string
	Timestamp time.Time
}

// ConnectionStatus tracks the WebSocket connection state.
type ConnectionStatus int

const (
	StatusConnecting ConnectionStatus = iota
	StatusConnected
	StatusDisconnected
)

// Model is the bubbletea application model for the TUI.
type Model struct {
	// State.
	focused   FocusedPanel
	input     string
	cursor    int
	messages  []ChatMessage
	logs      []string
	sessionID string
	status    ConnectionStatus
	streaming bool
	width     int
	height    int

	// Scroll offsets.
	chatScroll int
	logScroll  int

	// Config.
	baseURL string
	apiKey  string
}

// NewModel creates a new TUI model.
func NewModel(baseURL, apiKey string) Model {
	return Model{
		focused: PanelInput,
		status:  StatusConnecting,
		baseURL: baseURL,
		apiKey:  apiKey,
	}
}

// Init starts the TUI with initial commands.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("roboticus"),
		m.createSession(),
	)
}

// Update handles messages and key events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case sessionCreatedMsg:
		m.sessionID = msg.sessionID
		m.status = StatusConnected
		m.addLog("Session created: " + msg.sessionID[:8])
		return m, nil

	case agentReplyMsg:
		m.messages = append(m.messages, ChatMessage{
			Role:      "assistant",
			Content:   msg.content,
			Timestamp: time.Now(),
		})
		m.streaming = false
		m.chatScroll = max(0, len(m.messages)-visibleMessages(m.height))
		return m, nil

	case errorMsg:
		m.addLog("ERROR: " + msg.err.Error())
		m.streaming = false
		return m, nil
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.focused = (m.focused + 1) % 3
		return m, nil

	case "shift+tab":
		m.focused = (m.focused + 2) % 3
		return m, nil

	case "enter":
		if m.focused == PanelInput && strings.TrimSpace(m.input) != "" {
			content := m.input
			m.input = ""
			m.cursor = 0
			m.messages = append(m.messages, ChatMessage{
				Role:      "user",
				Content:   content,
				Timestamp: time.Now(),
			})
			m.streaming = true
			m.chatScroll = max(0, len(m.messages)-visibleMessages(m.height))
			return m, m.sendMessage(content)
		}
		return m, nil

	case "backspace":
		if m.focused == PanelInput && m.cursor > 0 {
			m.input = m.input[:m.cursor-1] + m.input[m.cursor:]
			m.cursor--
		}
		return m, nil

	case "left":
		if m.focused == PanelInput && m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "right":
		if m.focused == PanelInput && m.cursor < len(m.input) {
			m.cursor++
		}
		return m, nil

	case "up":
		switch m.focused {
		case PanelChat:
			if m.chatScroll > 0 {
				m.chatScroll--
			}
		case PanelLogs:
			if m.logScroll > 0 {
				m.logScroll--
			}
		}
		return m, nil

	case "down":
		switch m.focused {
		case PanelChat:
			m.chatScroll++
		case PanelLogs:
			m.logScroll++
		}
		return m, nil

	default:
		if m.focused == PanelInput && len(msg.String()) == 1 {
			m.input = m.input[:m.cursor] + msg.String() + m.input[m.cursor:]
			m.cursor++
		}
		return m, nil
	}
}

// View renders the TUI.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Layout: 75% left (chat + input), 25% right (logs), status bar at bottom.
	leftWidth := m.width * 3 / 4
	rightWidth := m.width - leftWidth - 1
	chatHeight := m.height - 5 // room for input and status bar
	if chatHeight < 3 {
		chatHeight = 3
	}

	// Chat panel.
	chatStyle := lipgloss.NewStyle().
		Width(leftWidth - 2).
		Height(chatHeight).
		Border(lipgloss.RoundedBorder())
	if m.focused == PanelChat {
		chatStyle = chatStyle.BorderForeground(lipgloss.Color("5"))
	}

	var chatLines []string
	for _, msg := range m.messages {
		ts := msg.Timestamp.Format("15:04")
		role := msg.Role
		if role == "user" {
			role = "you"
		}
		chatLines = append(chatLines, fmt.Sprintf("[%s] %s: %s", ts, role, msg.Content))
	}
	if m.streaming {
		chatLines = append(chatLines, "⠿ thinking...")
	}
	chatContent := strings.Join(chatLines, "\n")
	chatPanel := chatStyle.Render(chatContent)

	// Logs panel.
	logStyle := lipgloss.NewStyle().
		Width(rightWidth - 2).
		Height(chatHeight).
		Border(lipgloss.RoundedBorder())
	if m.focused == PanelLogs {
		logStyle = logStyle.BorderForeground(lipgloss.Color("5"))
	}
	logContent := strings.Join(m.logs, "\n")
	logPanel := logStyle.Render(logContent)

	// Input panel.
	inputStyle := lipgloss.NewStyle().
		Width(m.width - 4).
		Border(lipgloss.RoundedBorder())
	if m.focused == PanelInput {
		inputStyle = inputStyle.BorderForeground(lipgloss.Color("5"))
	}
	inputContent := m.input
	if m.focused == PanelInput {
		// Show cursor.
		if m.cursor < len(inputContent) {
			inputContent = inputContent[:m.cursor] + "█" + inputContent[m.cursor+1:]
		} else {
			inputContent += "█"
		}
	}
	inputPanel := inputStyle.Render(inputContent)

	// Status bar.
	var statusText string
	switch m.status {
	case StatusConnected:
		statusText = "CONNECTED"
	case StatusDisconnected:
		statusText = "DISCONNECTED"
	default:
		statusText = "CONNECTING"
	}
	sessionText := ""
	if m.sessionID != "" && len(m.sessionID) >= 8 {
		sessionText = " | session:" + m.sessionID[:8]
	}
	statusBar := lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("0")).
		Foreground(lipgloss.Color("7")).
		Render(fmt.Sprintf(" %s%s | Tab=switch panels | Ctrl+C=quit", statusText, sessionText))

	// Compose.
	top := lipgloss.JoinHorizontal(lipgloss.Top, chatPanel, logPanel)
	return lipgloss.JoinVertical(lipgloss.Left, top, inputPanel, statusBar)
}

func (m *Model) addLog(msg string) {
	ts := time.Now().Format("15:04:05")
	m.logs = append(m.logs, fmt.Sprintf("[%s] %s", ts, msg))
}

func visibleMessages(height int) int {
	n := height - 8
	if n < 3 {
		return 3
	}
	return n
}

// --- Tea messages ---

type sessionCreatedMsg struct{ sessionID string }
type agentReplyMsg struct{ content string }
type errorMsg struct{ err error }

// --- Commands ---

func (m Model) createSession() tea.Cmd {
	return func() tea.Msg {
		data, err := tuiAPIPost(m.baseURL+"/api/sessions", map[string]any{"agent_id": "default"})
		if err != nil {
			return errorMsg{err}
		}
		id, _ := data["id"].(string)
		if id == "" {
			return errorMsg{fmt.Errorf("no session id returned")}
		}
		return sessionCreatedMsg{id}
	}
}

func (m Model) sendMessage(content string) tea.Cmd {
	return func() tea.Msg {
		data, err := tuiAPIPost(m.baseURL+"/api/agent/message", map[string]any{
			"content":    content,
			"session_id": m.sessionID,
			"agent_id":   "default",
		})
		if err != nil {
			return errorMsg{err}
		}
		reply, _ := data["content"].(string)
		if reply == "" {
			reply = "(empty response)"
		}
		return agentReplyMsg{reply}
	}
}
