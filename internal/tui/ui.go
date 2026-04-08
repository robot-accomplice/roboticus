package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChatModel is a standalone chat interface using bubbletea.
// It provides a simpler alternative to the full Model for embedding
// or testing chat interactions.
type ChatModel struct {
	messages []ChatMessage
	input    string
	width    int
	height   int
}

// NewChatModel creates a new ChatModel.
func NewChatModel() ChatModel {
	return ChatModel{}
}

// Init satisfies tea.Model.
func (m ChatModel) Init() tea.Cmd {
	return tea.SetWindowTitle("roboticus chat")
}

// Update handles messages for the chat model.
func (m ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleChatKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m ChatModel) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit

	case "enter":
		text := strings.TrimSpace(m.input)
		if text != "" {
			m.messages = append(m.messages, ChatMessage{
				Role:      "user",
				Content:   text,
				Timestamp: time.Now(),
			})
			m.input = ""
		}
		return m, nil

	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	default:
		if len(msg.String()) == 1 {
			m.input += msg.String()
		}
		return m, nil
	}
}

// View renders the chat model.
func (m ChatModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}

	chatHeight := h - 4
	if chatHeight < 3 {
		chatHeight = 3
	}

	// Render messages.
	var lines []string
	for _, msg := range m.messages {
		ts := msg.Timestamp.Format("15:04")
		role := msg.Role
		if role == "user" {
			role = "you"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", ts, role, msg.Content))
	}
	chatContent := strings.Join(lines, "\n")

	chatStyle := lipgloss.NewStyle().
		Width(w - 4).
		Height(chatHeight).
		Border(lipgloss.RoundedBorder())
	chatPanel := chatStyle.Render(chatContent)

	inputStyle := lipgloss.NewStyle().
		Width(w - 4).
		Border(lipgloss.NormalBorder())
	inputPanel := inputStyle.Render(m.input + "█")

	return lipgloss.JoinVertical(lipgloss.Left, chatPanel, inputPanel)
}

// Messages returns the current chat messages.
func (m ChatModel) Messages() []ChatMessage {
	return m.messages
}

// Input returns the current input text.
func (m ChatModel) Input() string {
	return m.input
}
