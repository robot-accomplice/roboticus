// Discord WebSocket gateway implementation.
// Rust parity: crates/roboticus-channels/src/discord.rs gateway_loop/run_gateway_session.
// Handles: connect, Hello/Identify/Resume handshake, heartbeat, MESSAGE_CREATE dispatch,
// reconnection with backoff, graceful shutdown.

package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog/log"
)

const (
	gatewayVersion  = "10"
	gatewayEncoding = "json"
	// Discord gateway opcodes.
	opDispatch        = 0
	opHeartbeat       = 1
	opIdentify        = 2
	opResume          = 6
	opReconnect       = 7
	opInvalidSession  = 9
	opHello           = 10
	opHeartbeatAck    = 11
	// Discord intents: GUILDS | GUILD_MESSAGES | MESSAGE_CONTENT.
	defaultIntents = 1<<0 | 1<<9 | 1<<15
)

// gatewayAction tells the outer loop what to do after a session ends.
type gatewayAction int

const (
	gatewayReconnect gatewayAction = iota
	gatewayResume
	gatewayShutdown
)

// gatewayState holds mutable state shared between the event loop and heartbeat goroutine.
type gatewayState struct {
	mu                sync.Mutex
	heartbeatInterval time.Duration
	sequence          *int64  // nullable sequence number
	sessionID         string
	resumeGatewayURL  string
	heartbeatAcked    atomic.Bool
}

func newGatewayState() *gatewayState {
	gs := &gatewayState{heartbeatInterval: 41250 * time.Millisecond}
	gs.heartbeatAcked.Store(true)
	return gs
}

// RunGateway starts the Discord WebSocket gateway loop. Blocks until ctx is cancelled.
// Rust parity: gateway_loop() in discord.rs.
func (a *DiscordAdapter) RunGateway(ctx context.Context) error {
	if a.cfg.Token == "" {
		return fmt.Errorf("discord: gateway requires a bot token")
	}
	state := newGatewayState()
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Determine connection URL.
		url, err := a.gatewayURL(ctx, state)
		if err != nil {
			log.Warn().Err(err).Dur("backoff", backoff).Msg("discord: failed to fetch gateway URL")
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
			backoff = min(backoff*2, 60*time.Second)
			continue
		}

		action, err := a.runSession(ctx, url, state)
		switch {
		case err != nil:
			log.Error().Err(err).Dur("backoff", backoff).Msg("discord: gateway error, retrying")
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
			backoff = min(backoff*2, 60*time.Second)
		case action == gatewayReconnect:
			log.Info().Msg("discord: gateway reconnecting (fresh identify)")
			state.mu.Lock()
			state.sessionID = ""
			state.sequence = nil
			state.resumeGatewayURL = ""
			state.mu.Unlock()
			backoff = time.Second
		case action == gatewayResume:
			log.Info().Msg("discord: gateway reconnecting (resume)")
			backoff = time.Second
		case action == gatewayShutdown:
			log.Info().Msg("discord: gateway shutting down")
			return nil
		}
	}
}

func (a *DiscordAdapter) gatewayURL(ctx context.Context, state *gatewayState) (string, error) {
	state.mu.Lock()
	resumeURL := state.resumeGatewayURL
	state.mu.Unlock()

	if resumeURL != "" {
		return fmt.Sprintf("%s/?v=%s&encoding=%s", resumeURL, gatewayVersion, gatewayEncoding), nil
	}

	// GET /gateway/bot to discover the WebSocket URL.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discordAPIBase+"/gateway/bot", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bot "+a.cfg.Token)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discord: /gateway/bot returned %d", resp.StatusCode)
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/?v=%s&encoding=%s", body.URL, gatewayVersion, gatewayEncoding), nil
}

// runSession connects, handshakes, and reads events until disconnect.
// Rust parity: run_gateway_session().
func (a *DiscordAdapter) runSession(ctx context.Context, url string, state *gatewayState) (gatewayAction, error) {
	log.Debug().Str("url", url).Msg("discord: connecting to gateway")
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return gatewayReconnect, fmt.Errorf("ws connect: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20) // 1 MiB

	// 1. Read Hello (op 10).
	hello, err := readGatewayMessage(ctx, conn)
	if err != nil {
		return gatewayReconnect, fmt.Errorf("read Hello: %w", err)
	}
	if hello.Op != opHello {
		return gatewayReconnect, fmt.Errorf("expected op 10 Hello, got op %d", hello.Op)
	}
	interval := extractHeartbeatInterval(hello.D)
	state.mu.Lock()
	state.heartbeatInterval = time.Duration(interval) * time.Millisecond
	state.mu.Unlock()
	state.heartbeatAcked.Store(true)
	log.Debug().Int64("interval_ms", interval).Msg("discord: received Hello")

	// 2. Spawn heartbeat goroutine.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go heartbeatLoop(hbCtx, conn, state)

	// 3. Send Identify or Resume.
	state.mu.Lock()
	sessionID := state.sessionID
	seq := state.sequence
	state.mu.Unlock()

	var identifyPayload json.RawMessage
	if sessionID != "" && seq != nil {
		identifyPayload = buildResume(a.cfg.Token, sessionID, *seq)
		log.Info().Str("session", sessionID).Int64("seq", *seq).Msg("discord: resuming session")
	} else {
		identifyPayload = buildIdentify(a.cfg.Token, defaultIntents)
		log.Info().Msg("discord: identifying with gateway")
	}
	if err := conn.Write(ctx, websocket.MessageText, identifyPayload); err != nil {
		return gatewayReconnect, fmt.Errorf("send identify: %w", err)
	}

	// 4. Event loop.
	for {
		msg, err := readGatewayMessage(ctx, conn)
		if err != nil {
			if ctx.Err() != nil {
				return gatewayShutdown, nil
			}
			// Check for close status.
			status := websocket.CloseStatus(err)
			if status != -1 {
				log.Info().Int("code", int(status)).Msg("discord: WebSocket closed")
				if isFatalClose(int(status)) {
					return gatewayShutdown, nil
				}
				if isResumableClose(int(status)) {
					return gatewayResume, nil
				}
				return gatewayReconnect, nil
			}
			return gatewayReconnect, err
		}

		action, err := a.handleGatewayMessage(msg, state, conn, ctx)
		if err != nil {
			log.Warn().Err(err).Msg("discord: error handling gateway message")
			continue
		}
		if action != nil {
			return *action, nil
		}
	}
}

// handleGatewayMessage processes a single gateway message.
// Returns non-nil action if the session should end.
// Rust parity: handle_gateway_message().
func (a *DiscordAdapter) handleGatewayMessage(msg *gatewayMessage, state *gatewayState, conn *websocket.Conn, ctx context.Context) (*gatewayAction, error) {
	// Update sequence for all messages.
	if msg.S != nil {
		state.mu.Lock()
		state.sequence = msg.S
		state.mu.Unlock()
	}

	switch msg.Op {
	case opDispatch:
		return a.handleDispatch(msg, state)
	case opHeartbeat:
		// Server requests immediate heartbeat.
		state.mu.Lock()
		seq := state.sequence
		state.mu.Unlock()
		hb := buildHeartbeat(seq)
		_ = conn.Write(ctx, websocket.MessageText, hb)
		return nil, nil
	case opReconnect:
		action := gatewayResume
		return &action, nil
	case opInvalidSession:
		// d = true means resumable, d = false means fresh identify.
		resumable := false
		_ = json.Unmarshal(msg.D, &resumable)
		if resumable {
			action := gatewayResume
			return &action, nil
		}
		state.mu.Lock()
		state.sessionID = ""
		state.sequence = nil
		state.mu.Unlock()
		action := gatewayReconnect
		return &action, nil
	case opHeartbeatAck:
		state.heartbeatAcked.Store(true)
		return nil, nil
	default:
		return nil, nil
	}
}

func (a *DiscordAdapter) handleDispatch(msg *gatewayMessage, state *gatewayState) (*gatewayAction, error) {
	switch msg.T {
	case "READY":
		var ready struct {
			SessionID        string `json:"session_id"`
			ResumeGatewayURL string `json:"resume_gateway_url"`
		}
		_ = json.Unmarshal(msg.D, &ready)
		state.mu.Lock()
		state.sessionID = ready.SessionID
		state.resumeGatewayURL = ready.ResumeGatewayURL
		state.mu.Unlock()
		log.Info().Str("session", ready.SessionID).Msg("discord: READY")
		return nil, nil

	case "RESUMED":
		log.Info().Msg("discord: session RESUMED")
		return nil, nil

	case "MESSAGE_CREATE":
		a.handleMessageCreate(msg.D)
		return nil, nil

	default:
		return nil, nil
	}
}

func (a *DiscordAdapter) handleMessageCreate(data json.RawMessage) {
	var m struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Content   string `json:"content"`
		Author    struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	// Skip bot messages.
	if m.Author.Bot {
		return
	}
	// Guild allowlist check.
	if !a.isGuildAllowed(m.GuildID) {
		return
	}

	a.mu.Lock()
	a.messageBuffer = append(a.messageBuffer, InboundMessage{
		ID:        m.ID,
		Platform:  "discord",
		ChatID:    m.ChannelID,
		SenderID:  m.Author.ID,
		Content:   m.Content,
		Timestamp: time.Now(),
	})
	a.mu.Unlock()
}

// --- Gateway message types and helpers ---

type gatewayMessage struct {
	Op int              `json:"op"`
	D  json.RawMessage  `json:"d"`
	S  *int64           `json:"s,omitempty"`
	T  string           `json:"t,omitempty"`
}

func readGatewayMessage(ctx context.Context, conn *websocket.Conn) (*gatewayMessage, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var msg gatewayMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("invalid gateway JSON: %w", err)
	}
	return &msg, nil
}

func extractHeartbeatInterval(d json.RawMessage) int64 {
	var hello struct {
		HeartbeatInterval int64 `json:"heartbeat_interval"`
	}
	_ = json.Unmarshal(d, &hello)
	if hello.HeartbeatInterval <= 0 {
		return 41250
	}
	return hello.HeartbeatInterval
}

func buildIdentify(token string, intents int) json.RawMessage {
	payload := map[string]any{
		"op": opIdentify,
		"d": map[string]any{
			"token":   token,
			"intents": intents,
			"properties": map[string]string{
				"os":      "linux",
				"browser": "roboticus",
				"device":  "roboticus",
			},
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

func buildResume(token, sessionID string, seq int64) json.RawMessage {
	payload := map[string]any{
		"op": opResume,
		"d": map[string]any{
			"token":      token,
			"session_id": sessionID,
			"seq":        seq,
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

func buildHeartbeat(seq *int64) json.RawMessage {
	payload := map[string]any{"op": opHeartbeat, "d": seq}
	data, _ := json.Marshal(payload)
	return data
}

// heartbeatLoop sends periodic heartbeats.
// Rust parity: heartbeat_task().
func heartbeatLoop(ctx context.Context, conn *websocket.Conn, state *gatewayState) {
	for {
		state.mu.Lock()
		interval := state.heartbeatInterval
		state.mu.Unlock()

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return
		}

		if !state.heartbeatAcked.Load() {
			log.Warn().Msg("discord: heartbeat not ACKed, closing connection")
			_ = conn.Close(websocket.StatusGoingAway, "heartbeat timeout")
			return
		}

		state.heartbeatAcked.Store(false)
		state.mu.Lock()
		seq := state.sequence
		state.mu.Unlock()
		hb := buildHeartbeat(seq)
		if err := conn.Write(ctx, websocket.MessageText, hb); err != nil {
			log.Warn().Err(err).Msg("discord: heartbeat write failed")
			return
		}
	}
}

// isFatalClose returns true for Discord close codes that mean "don't reconnect".
func isFatalClose(code int) bool {
	switch code {
	case 4004: // Authentication failed
		return true
	case 4010: // Invalid shard
		return true
	case 4011: // Sharding required
		return true
	case 4013: // Invalid intents
		return true
	case 4014: // Disallowed intents
		return true
	default:
		return false
	}
}

// isResumableClose returns true for codes where we should resume rather than re-identify.
func isResumableClose(code int) bool {
	switch code {
	case 4000, 4001, 4002, 4003, 4005, 4007, 4008, 4009:
		return true
	default:
		return false
	}
}
