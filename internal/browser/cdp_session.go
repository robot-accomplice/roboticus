package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// CdpTarget represents a CDP debugging target.
type CdpTarget struct {
	ID                 string `json:"id"`
	URL                string `json:"url"`
	Title              string `json:"title"`
	Type               string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// CdpSession wraps a WebSocket connection to a CDP target.
type CdpSession struct {
	conn      *websocket.Conn
	commandID atomic.Int64
	mu        sync.Mutex
	pending   map[int64]chan json.RawMessage
	timeout   time.Duration
	cancel    context.CancelFunc
}

// CdpCommand is a JSON-RPC message sent to CDP.
type cdpCommand struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// cdpResponse is a JSON-RPC response from CDP.
type cdpResponse struct {
	ID     int64            `json:"id"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *cdpError        `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ConnectCdp connects to a CDP target's WebSocket URL.
func ConnectCdp(ctx context.Context, wsURL string, timeout time.Duration) (*CdpSession, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp connect: %w", err)
	}
	conn.SetReadLimit(10 * 1024 * 1024) // 10MB for screenshots

	readCtx, cancel := context.WithCancel(ctx)
	s := &CdpSession{
		conn:    conn,
		pending: make(map[int64]chan json.RawMessage),
		timeout: timeout,
		cancel:  cancel,
	}

	go s.readLoop(readCtx)
	return s, nil
}

// Close shuts down the CDP session.
func (s *CdpSession) Close() {
	s.cancel()
	s.conn.Close(websocket.StatusNormalClosure, "done")
}

// SendCommand sends a CDP method call and waits for the response.
func (s *CdpSession) SendCommand(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.commandID.Add(1)
	ch := make(chan json.RawMessage, 1)

	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	cmd := cdpCommand{ID: id, Method: method, Params: params}
	data, _ := json.Marshal(cmd)

	if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("cdp send: %w", err)
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(s.timeout):
		return nil, fmt.Errorf("cdp command %q timed out", method)
	}
}

func (s *CdpSession) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := s.conn.Read(ctx)
		if err != nil {
			return
		}

		var resp cdpResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}

		if resp.ID > 0 {
			s.mu.Lock()
			ch, ok := s.pending[resp.ID]
			s.mu.Unlock()
			if ok {
				if resp.Error != nil {
					ch <- json.RawMessage(fmt.Sprintf(`{"error":"%s"}`, resp.Error.Message))
				} else {
					ch <- resp.Result
				}
			}
		}
	}
}

// FindPageTarget discovers the first "page" type target from the CDP HTTP API.
func FindPageTarget(baseURL string) (*CdpTarget, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/json/list")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var targets []CdpTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}

	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("no page target found")
}
