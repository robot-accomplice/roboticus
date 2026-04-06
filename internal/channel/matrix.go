package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// MatrixConfig holds Matrix homeserver connection settings.
type MatrixConfig struct {
	HomeserverURL string   `json:"homeserver_url" mapstructure:"homeserver_url"`
	AccessToken   string   `json:"access_token" mapstructure:"access_token"`
	DeviceID      string   `json:"device_id" mapstructure:"device_id"`
	AllowedRooms  []string `json:"allowed_rooms" mapstructure:"allowed_rooms"`
	AutoJoin      bool     `json:"auto_join" mapstructure:"auto_join"`
	E2EEEnabled   bool     `json:"e2ee_enabled" mapstructure:"e2ee_enabled"`
	SyncTimeoutMs int      `json:"sync_timeout_ms" mapstructure:"sync_timeout_ms"`
}

const (
	defaultMatrixSyncTimeout = 30000 // 30s long-poll
	matrixAPIPrefix          = "/_matrix/client/v3"
)

// MatrixAdapter implements the Adapter interface for Matrix.
type MatrixAdapter struct {
	cfg       MatrixConfig
	client    core.HTTPDoer
	userID    string
	syncToken string
	inbound   chan InboundMessage
	Crypto    *MatrixCrypto
}

// NewMatrixAdapter creates a Matrix channel adapter.
func NewMatrixAdapter(cfg MatrixConfig) (*MatrixAdapter, error) {
	return NewMatrixAdapterWithHTTP(cfg, nil)
}

// NewMatrixAdapterWithHTTP creates a Matrix adapter with an injected HTTP client.
func NewMatrixAdapterWithHTTP(cfg MatrixConfig, httpClient core.HTTPDoer) (*MatrixAdapter, error) {
	if cfg.HomeserverURL == "" {
		return nil, fmt.Errorf("matrix: homeserver_url required")
	}
	if cfg.AccessToken == "" {
		return nil, fmt.Errorf("matrix: access_token required")
	}
	if cfg.SyncTimeoutMs <= 0 {
		cfg.SyncTimeoutMs = defaultMatrixSyncTimeout
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: time.Duration(cfg.SyncTimeoutMs+5000) * time.Millisecond,
		}
	}

	adapter := &MatrixAdapter{
		cfg:     cfg,
		client:  httpClient,
		inbound: make(chan InboundMessage, 64),
	}

	// Initialize E2EE crypto layer if enabled.
	if cfg.E2EEEnabled {
		adapter.Crypto = NewMatrixCrypto()
		log.Info().Msg("Matrix E2EE crypto initialized")
	}

	// Resolve own user ID.
	userID, err := adapter.whoami()
	if err != nil {
		return nil, fmt.Errorf("matrix: whoami failed: %w", err)
	}
	adapter.userID = userID
	log.Info().Str("user_id", userID).Msg("Matrix adapter connected")

	return adapter, nil
}

func (m *MatrixAdapter) PlatformName() string { return "matrix" }

func (m *MatrixAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	// Run a sync cycle to fetch new events.
	if err := m.syncOnce(ctx); err != nil {
		return nil, err
	}
	select {
	case msg := <-m.inbound:
		return &msg, nil
	default:
		return nil, nil
	}
}

func (m *MatrixAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	roomID := msg.RecipientID
	txnID := fmt.Sprintf("roboticus-%d", time.Now().UnixNano())

	var body any
	eventType := "m.room.message"

	if m.Crypto != nil {
		// Encrypt the message using the Megolm session.
		encrypted, err := m.Crypto.EncryptMessage(roomID, msg.Content)
		if err != nil {
			return fmt.Errorf("matrix: encrypt message: %w", err)
		}
		body = encrypted
		eventType = "m.room.encrypted"
	} else {
		body = map[string]string{
			"msgtype": "m.text",
			"body":    msg.Content,
		}
	}

	sendURL := fmt.Sprintf("%s%s/rooms/%s/send/%s/%s",
		m.cfg.HomeserverURL, matrixAPIPrefix, roomID, eventType, txnID)

	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, sendURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("matrix: create send request: %w", err)
	}
	m.setAuth(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("matrix: send failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("matrix: send returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (m *MatrixAdapter) syncOnce(ctx context.Context) error {
	url := fmt.Sprintf("%s%s/sync?timeout=%d", m.cfg.HomeserverURL, matrixAPIPrefix, m.cfg.SyncTimeoutMs)
	if m.syncToken != "" {
		url += "&since=" + m.syncToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	m.setAuth(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("matrix: sync failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var syncResp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]struct {
				Timeline struct {
					Events []matrixEvent `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
			Invite map[string]struct{} `json:"invite"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		return fmt.Errorf("matrix: decode sync: %w", err)
	}

	m.syncToken = syncResp.NextBatch

	// Auto-join invited rooms.
	if m.cfg.AutoJoin {
		for roomID := range syncResp.Rooms.Invite {
			_ = m.joinRoom(ctx, roomID)
		}
	}

	// Process timeline events from joined rooms.
	for roomID, room := range syncResp.Rooms.Join {
		if len(m.cfg.AllowedRooms) > 0 && !contains(m.cfg.AllowedRooms, roomID) {
			continue
		}
		for _, event := range room.Timeline.Events {
			if event.Sender == m.userID {
				continue
			}

			var content string
			switch event.Type {
			case "m.room.message":
				content = event.Content.Body
			case "m.room.encrypted":
				if m.Crypto != nil {
					decrypted, err := m.Crypto.DecryptEvent(roomID, event.Content.Raw)
					if err != nil {
						log.Warn().Err(err).Str("room_id", roomID).Msg("Matrix: failed to decrypt event")
						continue
					}
					content = decrypted
				} else {
					log.Warn().Str("room_id", roomID).Msg("Matrix: received encrypted event but E2EE not enabled")
					continue
				}
			default:
				continue
			}

			if content == "" {
				continue
			}
			m.inbound <- InboundMessage{
				ID:        event.EventID,
				Platform:  "matrix",
				SenderID:  event.Sender,
				ChatID:    roomID,
				Content:   content,
				Timestamp: time.Unix(int64(event.OriginServerTS/1000), 0),
			}
		}
	}
	return nil
}

func (m *MatrixAdapter) joinRoom(ctx context.Context, roomID string) error {
	url := fmt.Sprintf("%s%s/rooms/%s/join", m.cfg.HomeserverURL, matrixAPIPrefix, roomID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	m.setAuth(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	log.Info().Str("room_id", roomID).Msg("Matrix auto-joined room")
	return nil
}

func (m *MatrixAdapter) whoami() (string, error) {
	url := m.cfg.HomeserverURL + matrixAPIPrefix + "/account/whoami"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	m.setAuth(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var result struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.UserID, nil
}

func (m *MatrixAdapter) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+m.cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")
}

type matrixEvent struct {
	Type           string             `json:"type"`
	EventID        string             `json:"event_id"`
	Sender         string             `json:"sender"`
	OriginServerTS int64              `json:"origin_server_ts"`
	Content        matrixEventContent `json:"content"`
}

type matrixEventContent struct {
	MsgType string         `json:"msgtype"`
	Body    string         `json:"body"`
	Raw     map[string]any `json:"-"` // populated by custom unmarshal
}

// UnmarshalJSON implements custom unmarshalling to capture the raw content map
// alongside the typed fields.
func (c *matrixEventContent) UnmarshalJSON(data []byte) error {
	// First unmarshal into raw map.
	if err := json.Unmarshal(data, &c.Raw); err != nil {
		return err
	}
	// Then unmarshal typed fields.
	type plain matrixEventContent
	return json.Unmarshal(data, (*plain)(c))
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
