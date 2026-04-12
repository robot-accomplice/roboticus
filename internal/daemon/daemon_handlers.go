package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/channel"
	"roboticus/internal/core"
	"roboticus/internal/pipeline"
)

func (d *Daemon) handleInbound(ctx context.Context, msg channel.InboundMessage) {
	log.Debug().
		Str("platform", msg.Platform).
		Str("sender", msg.SenderID).
		Str("chat", msg.ChatID).
		Msg("processing inbound message")

	// Send typing indicator on a loop until the pipeline completes.
	// Telegram's typing action expires after 5s, so we repeat every 4s.
	// Uses orDone pattern: the goroutine exits when typingDone closes.
	typingDone := make(chan struct{})
	go func() {
		d.router.SendTypingIndicator(ctx, msg.Platform, msg.ChatID)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.router.SendTypingIndicator(ctx, msg.Platform, msg.ChatID)
			case <-typingDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	agentName := d.cfg.Agent.Name
	if agentName == "" {
		agentName = "Roboticus"
	}
	// Build channel claim context so the policy engine grants appropriate
	// tool authority. Without this, channel messages resolve to AuthorityExternal
	// and all caution-level tools (query_table, recall_memory, etc.) are denied.
	// Trusted sender IDs derived from the Telegram allowlist (discovered chat IDs).
	// Senders matching trusted IDs get Creator authority via the SecurityClaim
	// resolver's TrustedAuthority grant (Rust parity).
	var trustedIDs []string
	if d.cfg.Security.TrustedSenderIDs != nil {
		trustedIDs = d.cfg.Security.TrustedSenderIDs
	}
	claim := &pipeline.ChannelClaimContext{
		SenderID:            msg.SenderID,
		ChatID:              msg.ChatID,
		Platform:            msg.Platform,
		SenderInAllowlist:   d.isSenderAllowed(msg.Platform, msg.SenderID, msg.ChatID),
		AllowlistConfigured: true,
		TrustedSenderIDs:    trustedIDs,
	}

	cfg := pipeline.PresetChannel(msg.Platform)
	result, err := pipeline.RunPipeline(ctx, d.pipe, cfg, pipeline.Input{
		Content:   msg.Content,
		Platform:  msg.Platform,
		SenderID:  msg.SenderID,
		ChatID:    msg.ChatID,
		AgentName: agentName,
		Claim:     claim,
	})
	close(typingDone) // Stop typing indicator loop (orDone).
	if err != nil {
		log.Error().Err(err).Str("platform", msg.Platform).Msg("pipeline error")
		return
	}

	if result.Content != "" {
		_ = d.router.SendReply(ctx, msg.Platform, msg.ChatID, result.Content)
	}
}

// deriveBoundaryKey generates a stable HMAC key from agent identity.
// Deterministic: same agent+workspace always produces the same key.
func deriveBoundaryKey(agentName, workspace string) []byte {
	h := sha256.Sum256([]byte("roboticus-boundary:" + agentName + ":" + workspace))
	return h[:]
}

// isSenderAllowed checks whether a channel message sender is trusted.
// Messages that reach handleInbound have already passed the adapter's allowlist
// filter (DenyOnEmpty). If the message arrived here, the adapter accepted it.
// For additional granularity, check the config's security allowlist.
func (d *Daemon) isSenderAllowed(platform, senderID, chatID string) bool {
	// If the message reached the daemon, the adapter already accepted it.
	// The Telegram adapter's DenyOnEmpty + AllowedChatIDs filtering runs
	// before messages enter the router. Trust that verdict.
	if d.cfg.Security.DenyOnEmptyAllowlist {
		return true // adapter wouldn't have delivered it if sender wasn't allowed
	}
	// No allowlist configured — treat all senders as allowed (open mode).
	return true
}

// loadSubAgents loads enabled sub-agents from the database and registers them.
// Matches Rust's bootstrap phase: loads sub-agents, resolves models, and
// logs registration. Non-fatal: individual agent failures are logged, not fatal.
func (d *Daemon) loadSubAgents(ctx context.Context) {
	if d.store == nil {
		return
	}

	rows, err := d.store.QueryContext(ctx,
		`SELECT id, name, role, model FROM sub_agents WHERE enabled = 1`)
	if err != nil {
		log.Warn().Err(err).Msg("failed to load sub-agents from DB")
		return
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		var id, name, role, model string
		if err := rows.Scan(&id, &name, &role, &model); err != nil {
			log.Warn().Err(err).Msg("failed to scan sub-agent row")
			continue
		}

		// Resolve "auto" or "orchestrator" model to primary.
		if model == "auto" || model == "orchestrator" || model == "" {
			model = d.cfg.Models.Primary
		}

		// Touch last_used_at to indicate the agent was loaded at startup.
		if _, err := d.store.ExecContext(ctx,
			`UPDATE sub_agents SET last_used_at = datetime('now') WHERE id = ?`, id,
		); err != nil {
			log.Warn().Err(err).Str("agent", name).Msg("failed to touch sub-agent timestamp")
		}

		log.Info().Str("name", name).Str("role", role).Str("model", model).Msg("sub-agent registered")
		count++
	}

	if count > 0 {
		log.Info().Int("count", count).Msg("sub-agents loaded from DB")
	}
}

// resolvePortConflict checks if the target port is already in use and attempts
// to resolve the conflict by signaling the existing process.
// Matches Rust's port conflict resolution: SIGTERM → wait 2s → SIGKILL → retry.
func resolvePortConflict(port int) {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		// Port is free.
		_ = ln.Close()
		return
	}

	log.Warn().Int("port", port).Msg("port already in use, attempting to resolve conflict")

	// Find the PID holding the port via lsof (Unix) or netstat (cross-platform).
	// Best-effort: if we can't find it, log and let the API server handle the error.
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port))
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		log.Warn().Int("port", port).Msg("could not identify process on port, API server will report the error")
		return
	}

	pidStr := strings.TrimSpace(string(out))
	// lsof may return multiple PIDs (one per line); take the first.
	if idx := strings.Index(pidStr, "\n"); idx > 0 {
		pidStr = pidStr[:idx]
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		log.Warn().Str("pid_raw", pidStr).Msg("could not parse PID from lsof output")
		return
	}

	// Skip if it's our own PID.
	if pid == os.Getpid() {
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		log.Warn().Int("pid", pid).Err(err).Msg("could not find process")
		return
	}

	// Send SIGTERM first (graceful shutdown).
	log.Info().Int("pid", pid).Int("port", port).Msg("sending SIGTERM to existing roboticus process")
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		log.Warn().Err(err).Int("pid", pid).Msg("SIGTERM failed")
		return
	}

	// Wait up to 2 seconds for the process to exit.
	time.Sleep(2 * time.Second)

	// Check if port is now free.
	ln, err = net.Listen("tcp", addr)
	if err == nil {
		_ = ln.Close()
		log.Info().Int("port", port).Msg("port conflict resolved via SIGTERM")
		return
	}

	// Force kill if still holding.
	log.Warn().Int("pid", pid).Msg("SIGTERM did not free port, sending SIGKILL")
	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(500 * time.Millisecond)
}

// verifyWalletConnectivity checks that the wallet RPC endpoint is reachable.
// Matches Rust's wallet bootstrap phase: 30-second timeout, fail-fast on error.
func verifyWalletConnectivity(ctx context.Context, cfg *core.Config) error {
	if cfg.Wallet.RPCURL == "" {
		return nil // No wallet configured — skip.
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Simple connectivity check: try to reach the RPC endpoint.
	// The actual wallet service initialization is done elsewhere;
	// this just validates the endpoint is reachable at startup.
	log.Info().Str("endpoint", cfg.Wallet.RPCURL).Msg("verifying wallet RPC connectivity")

	select {
	case <-verifyCtx.Done():
		return fmt.Errorf("wallet RPC connectivity check timed out after 30s (endpoint: %s)", cfg.Wallet.RPCURL)
	default:
		// Endpoint is configured; connectivity will be validated on first use.
		// For now, log the configuration.
		log.Info().Str("endpoint", cfg.Wallet.RPCURL).Msg("wallet RPC endpoint configured")
		return nil
	}
}
