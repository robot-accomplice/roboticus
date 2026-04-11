package pipeline

import (
	"context"
	"fmt"
	"strings"

	"roboticus/internal/db"
)

// CommandFunc handles a bot command. Args is everything after the command name.
type CommandFunc func(ctx context.Context, args string, session *Session) (*Outcome, error)

// BotCommandHandler manages /command handlers that bypass LLM inference.
type BotCommandHandler struct {
	commands map[string]CommandFunc
	store    *db.Store
}

// NewBotCommandHandler creates a handler with built-in commands registered.
// If store is non-nil, data-backed commands (like /memory) will query it.
func NewBotCommandHandler(store ...*db.Store) *BotCommandHandler {
	h := &BotCommandHandler{commands: make(map[string]CommandFunc)}
	if len(store) > 0 {
		h.store = store[0]
	}
	h.Register("help", cmdHelp)
	h.Register("status", h.cmdStatus)
	h.Register("tools", cmdTools)
	h.Register("skills", cmdSkills)
	h.Register("memory", h.cmdMemory)
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

// cmdStatus returns rich operational status matching the CLI `status` command.
// Rust parity: in-chat /status must show the same data as the CLI status output.
func (h *BotCommandHandler) cmdStatus(ctx context.Context, _ string, s *Session) (*Outcome, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**%s** — online\n", s.AgentName))
	b.WriteString(fmt.Sprintf("Session: `%s` · Messages: %d\n", s.ID, s.MessageCount()))

	if h.store != nil {
		// Sessions count.
		var sessionCount int
		if err := h.store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sessions WHERE status = 'active'`).Scan(&sessionCount); err == nil {
			b.WriteString(fmt.Sprintf("Sessions: %d active\n", sessionCount))
		}

		// Skills.
		var skillsEnabled, skillsTotal int
		rows, err := h.store.QueryContext(ctx, `SELECT enabled FROM skills`)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var enabled bool
				if rows.Scan(&enabled) == nil {
					skillsTotal++
					if enabled {
						skillsEnabled++
					}
				}
			}
			if skillsTotal > 0 {
				b.WriteString(fmt.Sprintf("Skills: %d/%d enabled\n", skillsEnabled, skillsTotal))
			}
		}

		// Cron jobs.
		var cronTotal, cronFailed int
		if err := h.store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM cron_jobs`).Scan(&cronTotal); err == nil {
			_ = h.store.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM cron_runs WHERE status = 'failed'
				 AND created_at > datetime('now', '-24 hours')`).Scan(&cronFailed)
			b.WriteString(fmt.Sprintf("Cron: %d jobs (%d failed/24h)\n", cronTotal, cronFailed))
		}

		// Cache hit rate.
		var hits, misses int64
		if err := h.store.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(hit_count), 0) FROM semantic_cache`).Scan(&hits); err == nil {
			_ = h.store.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM semantic_cache`).Scan(&misses)
			total := hits + misses
			if total > 0 {
				rate := float64(hits) / float64(total) * 100
				b.WriteString(fmt.Sprintf("Cache: %.1f%% hit rate (%d entries)\n", rate, misses))
			}
		}

		// Wallet balance.
		var balance string
		if err := h.store.QueryRowContext(ctx,
			`SELECT COALESCE(total_balance, '0.00') FROM treasury_state
			 ORDER BY updated_at DESC LIMIT 1`).Scan(&balance); err == nil {
			b.WriteString(fmt.Sprintf("Wallet: $%s\n", balance))
		}

		// Memory tier counts.
		tiers := []struct{ name, table string }{
			{"working", "working_memory"},
			{"episodic", "episodic_memory"},
			{"semantic", "semantic_memory"},
		}
		var tierParts []string
		for _, t := range tiers {
			var count int
			if err := h.store.QueryRowContext(ctx,
				fmt.Sprintf(`SELECT COUNT(*) FROM %s`, t.table)).Scan(&count); err == nil {
				tierParts = append(tierParts, fmt.Sprintf("%s=%d", t.name, count))
			}
		}
		if len(tierParts) > 0 {
			b.WriteString(fmt.Sprintf("Memory: %s\n", strings.Join(tierParts, ", ")))
		}
	}

	return &Outcome{
		SessionID: s.ID,
		Content:   b.String(),
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

func (h *BotCommandHandler) cmdMemory(ctx context.Context, args string, s *Session) (*Outcome, error) {
	if args == "" {
		return &Outcome{
			SessionID: s.ID,
			Content:   "Memory commands: /memory search <query>, /memory stats",
		}, nil
	}

	if h.store == nil {
		return &Outcome{
			SessionID: s.ID,
			Content:   "Memory subsystem unavailable (no database connection).",
		}, nil
	}

	parts := strings.SplitN(args, " ", 2)
	subCmd := strings.ToLower(parts[0])

	switch subCmd {
	case "stats":
		return h.cmdMemoryStats(ctx, s)
	case "search":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return &Outcome{
				SessionID: s.ID,
				Content:   "Usage: /memory search <query>",
			}, nil
		}
		return h.cmdMemorySearch(ctx, parts[1], s)
	default:
		return &Outcome{
			SessionID: s.ID,
			Content:   fmt.Sprintf("Unknown memory subcommand %q. Available: search, stats", subCmd),
		}, nil
	}
}

func (h *BotCommandHandler) cmdMemoryStats(ctx context.Context, s *Session) (*Outcome, error) {
	tables := []struct {
		name  string
		query string
	}{
		{"working_memory", "SELECT COUNT(*) FROM working_memory"},
		{"episodic_memory", "SELECT COUNT(*) FROM episodic_memory"},
		{"semantic_memory", "SELECT COUNT(*) FROM semantic_memory"},
		{"procedural_memory", "SELECT COUNT(*) FROM procedural_memory"},
		{"relationship_memory", "SELECT COUNT(*) FROM relationship_memory"},
	}

	var b strings.Builder
	b.WriteString("Memory Statistics:\n")
	for _, t := range tables {
		var count int64
		row := h.store.QueryRowContext(ctx, t.query)
		if err := row.Scan(&count); err != nil {
			fmt.Fprintf(&b, "  %s: error (%v)\n", t.name, err)
			continue
		}
		fmt.Fprintf(&b, "  %s: %d entries\n", t.name, count)
	}

	return &Outcome{
		SessionID: s.ID,
		Content:   b.String(),
	}, nil
}

func (h *BotCommandHandler) cmdMemorySearch(ctx context.Context, query string, s *Session) (*Outcome, error) {
	pattern := "%" + query + "%"
	var b strings.Builder
	fmt.Fprintf(&b, "Memory search results for %q:\n", query)

	// Search episodic memory.
	rows, err := h.store.QueryContext(ctx,
		`SELECT content FROM episodic_memory WHERE content LIKE ? LIMIT 5`, pattern)
	if err == nil {
		var found int
		for rows.Next() {
			var content string
			if err := rows.Scan(&content); err != nil {
				continue
			}
			found++
			// Truncate long entries for readability.
			if len(content) > 120 {
				content = content[:120] + "..."
			}
			fmt.Fprintf(&b, "\n[episodic %d] %s", found, content)
		}
		_ = rows.Close()
		if found == 0 {
			b.WriteString("\n  (no episodic matches)")
		}
	}

	// Search semantic memory.
	rows, err = h.store.QueryContext(ctx,
		`SELECT category, key, value FROM semantic_memory WHERE value LIKE ? OR key LIKE ? LIMIT 5`, pattern, pattern)
	if err == nil {
		var found int
		for rows.Next() {
			var cat, key, val string
			if err := rows.Scan(&cat, &key, &val); err != nil {
				continue
			}
			found++
			if len(val) > 80 {
				val = val[:80] + "..."
			}
			fmt.Fprintf(&b, "\n[semantic %d] %s/%s: %s", found, cat, key, val)
		}
		_ = rows.Close()
		if found == 0 {
			b.WriteString("\n  (no semantic matches)")
		}
	}

	return &Outcome{
		SessionID: s.ID,
		Content:   b.String(),
	}, nil
}
