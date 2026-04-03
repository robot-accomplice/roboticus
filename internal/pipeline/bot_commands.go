package pipeline

import (
	"context"
	"fmt"
	"strings"
)

// CommandFunc handles a bot command. Args is everything after the command name.
type CommandFunc func(ctx context.Context, args string, session *Session) (*Outcome, error)

// BotCommandHandler manages /command handlers that bypass LLM inference.
type BotCommandHandler struct {
	commands map[string]CommandFunc
}

// NewBotCommandHandler creates a handler with built-in commands registered.
func NewBotCommandHandler() *BotCommandHandler {
	h := &BotCommandHandler{commands: make(map[string]CommandFunc)}
	h.Register("help", cmdHelp)
	h.Register("status", cmdStatus)
	h.Register("tools", cmdTools)
	h.Register("skills", cmdSkills)
	h.Register("memory", cmdMemory)
	return h
}

// Register adds a command handler.
func (h *BotCommandHandler) Register(name string, fn CommandFunc) {
	h.commands[strings.ToLower(name)] = fn
}

// TryHandle attempts to handle content as a bot command.
// Returns (result, true) if matched, (nil, false) if not a command.
func (h *BotCommandHandler) TryHandle(ctx context.Context, content string, session *Session) (*Outcome, bool) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "/") {
		return nil, false
	}

	// Parse: "/command args..."
	parts := strings.SplitN(content[1:], " ", 2)
	cmd := strings.ToLower(parts[0])
	var args string
	if len(parts) > 1 {
		args = parts[1]
	}

	fn, ok := h.commands[cmd]
	if !ok {
		return nil, false
	}

	result, err := fn(ctx, args, session)
	if err != nil {
		return &Outcome{
			SessionID: session.ID,
			Content:   fmt.Sprintf("Command error: %v", err),
		}, true
	}
	return result, true
}

func cmdHelp(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   fmt.Sprintf("%s can help with:\n- General conversation and reasoning\n- File operations and code tasks\n- Web search and information retrieval\n- Scheduling and reminders\n- Financial operations\n\nCommands: /help, /status, /tools, /skills, /memory", s.AgentName),
	}, nil
}

func cmdStatus(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   fmt.Sprintf("Status: Agent %s is online. Session: %s, Messages: %d", s.AgentName, s.ID, s.MessageCount()),
	}, nil
}

func cmdTools(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   "Available tools: Use /help for general information. Tool list depends on configured plugins and MCP servers.",
	}, nil
}

func cmdSkills(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   "Skills: Use /help for general information. Skill list depends on configured skill directory.",
	}, nil
}

func cmdMemory(_ context.Context, args string, s *Session) (*Outcome, error) {
	if args == "" {
		return &Outcome{
			SessionID: s.ID,
			Content:   "Memory commands: /memory search <query>, /memory stats",
		}, nil
	}
	return &Outcome{
		SessionID: s.ID,
		Content:   fmt.Sprintf("Memory query: %s (not yet implemented)", args),
	}, nil
}
