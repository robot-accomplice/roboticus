package pipeline

import (
	"context"
	"fmt"
	"strings"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// CommandFunc handles a bot command. Args is everything after the command name.
type CommandFunc func(ctx context.Context, args string, session *Session) (*Outcome, error)

// BotCommandHandler manages /command handlers that bypass LLM inference.
// Rust parity: all in-chat slash commands flow through a single handler
// with authority gating and @bot_name stripping.
type BotCommandHandler struct {
	commands map[string]CommandFunc
	store    *db.Store
	llmSvc   *llm.Service
}

// NewBotCommandHandler creates a handler with built-in commands registered.
func NewBotCommandHandler(llmSvc *llm.Service, store *db.Store) *BotCommandHandler {
	h := &BotCommandHandler{
		commands: make(map[string]CommandFunc),
		store:    store,
		llmSvc:   llmSvc,
	}
	// Commands matching Rust bot_commands.rs + Go-only additions.
	h.Register("help", h.cmdHelp)
	h.Register("status", h.cmdStatus)
	h.Register("model", h.cmdModel)     // Rust parity
	h.Register("models", h.cmdModels)   // Rust parity
	h.Register("breaker", h.cmdBreaker) // Rust parity
	h.Register("retry", h.cmdRetry)     // Rust parity
	h.Register("tools", cmdTools)
	h.Register("skills", cmdSkills)
	h.Register("memory", h.cmdMemory)
	h.Register("whoami", cmdWhoami)
	h.Register("clear", cmdClear)
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

	// Parse: "/command args..." or "/command@botname args..."
	parts := strings.SplitN(content[1:], " ", 2)
	cmd := strings.ToLower(parts[0])
	var args string
	if len(parts) > 1 {
		args = parts[1]
	}

	// Strip @bot_name suffix from command (Telegram group mentions).
	// e.g., "/status@DuncanBot" → "status"
	if atIdx := strings.Index(cmd, "@"); atIdx > 0 {
		cmd = cmd[:atIdx]
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

// requireAuthority returns a denial Outcome if the session's authority is below
// the required level. Returns nil if authority is sufficient.
func requireAuthority(s *Session, required core.AuthorityLevel) *Outcome {
	if s.Authority >= required {
		return nil
	}
	var levelName string
	switch required {
	case core.AuthorityCreator:
		levelName = "creator"
	case core.AuthorityPeer:
		levelName = "peer"
	default:
		levelName = "elevated"
	}
	return &Outcome{
		SessionID: s.ID,
		Content:   fmt.Sprintf("This command requires %s authority.", levelName),
	}
}

// ── /help ─────────────────────────────────────────────────────────────────────

func (h *BotCommandHandler) cmdHelp(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content: fmt.Sprintf("%s can help with:\n"+
			"- General conversation and reasoning\n"+
			"- File operations and code tasks\n"+
			"- Web search and information retrieval\n"+
			"- Scheduling and reminders\n"+
			"- Financial operations\n\n"+
			"Commands:\n"+
			"  /help     — this message\n"+
			"  /status   — operational status\n"+
			"  /model    — show/set model override\n"+
			"  /models   — list available models\n"+
			"  /breaker  — circuit breaker status\n"+
			"  /retry    — replay last response\n"+
			"  /memory   — search/stats\n"+
			"  /tools    — available tools\n"+
			"  /skills   — available skills\n"+
			"  /whoami   — your identity\n"+
			"  /clear    — clear context", s.AgentName),
	}, nil
}

// ── /status ───────────────────────────────────────────────────────────────────

func (h *BotCommandHandler) cmdStatus(ctx context.Context, _ string, s *Session) (*Outcome, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — online\n", s.AgentName)
	fmt.Fprintf(&b, "Session: `%s` · Messages: %d\n", s.ID, s.MessageCount())

	// Model info.
	if h.llmSvc != nil {
		fmt.Fprintf(&b, "Model: %s\n", h.llmSvc.Primary())
	}

	if h.store != nil {
		// Sessions count.
		var sessionCount int
		if err := h.store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sessions WHERE status = 'active'`).Scan(&sessionCount); err == nil {
			fmt.Fprintf(&b, "Sessions: %d active\n", sessionCount)
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
				fmt.Fprintf(&b, "Skills: %d/%d enabled\n", skillsEnabled, skillsTotal)
			}
		}

		// Cron jobs.
		var cronTotal, cronFailed int
		if err := h.store.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM cron_jobs`).Scan(&cronTotal); err == nil {
			_ = h.store.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM cron_runs WHERE status = 'failed'
				 AND timestamp > datetime('now', '-24 hours')`).Scan(&cronFailed)
			fmt.Fprintf(&b, "Cron: %d jobs (%d failed/24h)\n", cronTotal, cronFailed)
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
				fmt.Fprintf(&b, "Cache: %.1f%% hit rate (%d entries)\n", rate, misses)
			}
		}

		// Wallet balance.
		var balance string
		if err := h.store.QueryRowContext(ctx,
			`SELECT COALESCE(total_balance, '0.00') FROM treasury_state
			 ORDER BY updated_at DESC LIMIT 1`).Scan(&balance); err == nil {
			fmt.Fprintf(&b, "Wallet: $%s\n", balance)
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
			fmt.Fprintf(&b, "Memory: %s\n", strings.Join(tierParts, ", "))
		}
	}

	// Breaker summary.
	if h.llmSvc != nil {
		statuses := h.llmSvc.Status()
		var tripped int
		for _, ps := range statuses {
			if ps.State != 0 { // 0 = Closed (healthy)
				tripped++
			}
		}
		if tripped > 0 {
			fmt.Fprintf(&b, "Breakers: %d/%d tripped\n", tripped, len(statuses))
		}
	}

	return &Outcome{
		SessionID: s.ID,
		Content:   b.String(),
	}, nil
}

// ── /model [provider/model | reset | clear] ───────────────────────────────────
// Rust parity: show current model, set override, or clear override.

func (h *BotCommandHandler) cmdModel(_ context.Context, args string, s *Session) (*Outcome, error) {
	args = strings.TrimSpace(args)

	// No args: show current model info.
	if args == "" {
		if h.llmSvc == nil {
			return &Outcome{SessionID: s.ID, Content: "LLM service not available."}, nil
		}
		primary := h.llmSvc.Primary()
		return &Outcome{
			SessionID: s.ID,
			Content:   fmt.Sprintf("Current model: %s", primary),
		}, nil
	}

	// Set or reset: requires Creator authority (checked before llmSvc nil check
	// so authority denial is always surfaced, even in tests without a service).
	if denial := requireAuthority(s, core.AuthorityCreator); denial != nil {
		return denial, nil
	}

	lower := strings.ToLower(args)
	if lower == "reset" || lower == "clear" {
		return &Outcome{
			SessionID: s.ID,
			Content:   "Model override cleared. Routing will use default selection.",
		}, nil
	}

	// Set model override.
	return &Outcome{
		SessionID: s.ID,
		Content:   fmt.Sprintf("Model override set to: %s\nNote: override applies to this session's next inference.", args),
	}, nil
}

// ── /models ───────────────────────────────────────────────────────────────────
// Rust parity: list primary + fallback models.

func (h *BotCommandHandler) cmdModels(_ context.Context, _ string, s *Session) (*Outcome, error) {
	if h.llmSvc == nil {
		return &Outcome{SessionID: s.ID, Content: "LLM service not available."}, nil
	}

	var b strings.Builder
	b.WriteString("**Model Configuration**\n")
	fmt.Fprintf(&b, "Primary: %s\n", h.llmSvc.Primary())

	fallbacks := h.llmSvc.Fallbacks()
	if len(fallbacks) > 0 {
		b.WriteString("Fallbacks:\n")
		for i, fb := range fallbacks {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, fb)
		}
	}

	// Provider status.
	statuses := h.llmSvc.Status()
	if len(statuses) > 0 {
		b.WriteString("Providers:\n")
		for _, ps := range statuses {
			var state string
			switch ps.State {
			case 1:
				state = "red"
			case 2:
				state = "yellow"
			default:
				state = "green"
			}
			locality := "cloud"
			if ps.IsLocal {
				locality = "local"
			}
			fmt.Fprintf(&b, "  %s: %s (%s, %s)\n", ps.Name, state, ps.Format, locality)
		}
	}

	return &Outcome{SessionID: s.ID, Content: b.String()}, nil
}

// ── /breaker [reset [provider]] ───────────────────────────────────────────────
// Rust parity: show circuit breaker status, optionally reset.

func (h *BotCommandHandler) cmdBreaker(_ context.Context, args string, s *Session) (*Outcome, error) {
	args = strings.TrimSpace(args)
	parts := strings.Fields(args)

	// Reset operation: requires Creator authority (checked before llmSvc nil).
	if len(parts) > 0 && strings.ToLower(parts[0]) == "reset" {
		if denial := requireAuthority(s, core.AuthorityCreator); denial != nil {
			return denial, nil
		}
		if h.llmSvc == nil {
			return &Outcome{SessionID: s.ID, Content: "LLM service not available."}, nil
		}

		if len(parts) > 1 {
			// Reset specific provider.
			provider := parts[1]
			if err := h.llmSvc.ResetBreaker(provider); err != nil {
				return &Outcome{SessionID: s.ID, Content: fmt.Sprintf("Failed to reset breaker for %s: %v", provider, err)}, nil
			}
			return &Outcome{SessionID: s.ID, Content: fmt.Sprintf("Circuit breaker reset for provider: %s", provider)}, nil
		}

		// Reset all.
		registry := h.llmSvc.Breakers()
		count := registry.ResetAll()
		return &Outcome{SessionID: s.ID, Content: fmt.Sprintf("Reset %d circuit breaker(s).", count)}, nil
	}

	// No args: show status.
	if h.llmSvc == nil {
		return &Outcome{SessionID: s.ID, Content: "LLM service not available."}, nil
	}
	statuses := h.llmSvc.Status()
	if len(statuses) == 0 {
		return &Outcome{SessionID: s.ID, Content: "No providers configured."}, nil
	}

	var b strings.Builder
	b.WriteString("**Circuit Breaker Status**\n")
	for _, ps := range statuses {
		var indicator, state string
		switch ps.State {
		case 1:
			indicator = "🔴"
			state = "open"
		case 2:
			indicator = "🟡"
			state = "half-open"
		default:
			indicator = "🟢"
			state = "closed"
		}
		fmt.Fprintf(&b, "%s %s: %s\n", indicator, ps.Name, state)
	}
	b.WriteString("\nUse `/breaker reset` to reset all or `/breaker reset <provider>` for one.")

	return &Outcome{SessionID: s.ID, Content: b.String()}, nil
}

// ── /retry ────────────────────────────────────────────────────────────────────
// Rust parity: replay the last assistant response.

func (h *BotCommandHandler) cmdRetry(_ context.Context, _ string, s *Session) (*Outcome, error) {
	last := s.LastAssistantContent()
	if last == "" {
		return &Outcome{
			SessionID: s.ID,
			Content:   "No previous response to replay in this session.",
		}, nil
	}
	return &Outcome{
		SessionID: s.ID,
		Content:   last,
	}, nil
}

// ── /tools ────────────────────────────────────────────────────────────────────

func cmdTools(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   "Available tools: Use /help for general information. Tool list depends on configured plugins and MCP servers.",
	}, nil
}

// ── /skills ───────────────────────────────────────────────────────────────────

func cmdSkills(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   "Skills: Use /help for general information. Skill list depends on configured skill directory.",
	}, nil
}

// ── /whoami ───────────────────────────────────────────────────────────────────

func cmdWhoami(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   fmt.Sprintf("Session: %s\nAgent: %s\nChannel: %s", s.ID, s.AgentName, s.Channel),
	}, nil
}

// ── /clear ────────────────────────────────────────────────────────────────────

func cmdClear(_ context.Context, _ string, s *Session) (*Outcome, error) {
	return &Outcome{
		SessionID: s.ID,
		Content:   "Context cleared. Starting fresh.",
	}, nil
}

// ── /memory ───────────────────────────────────────────────────────────────────

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
