package tui

import (
	"context"
	"fmt"
	"sync"

	"github.com/coder/websocket"
)

// WSClient manages a WebSocket connection to the roboticus server.
type WSClient struct {
	url  string
	conn *websocket.Conn
	done chan struct{}
	mu   sync.Mutex
}

// NewWSClient creates a new WebSocket client for the given URL.
func NewWSClient(url string) *WSClient {
	return &WSClient{
		url:  url,
		done: make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection.
func (wc *WSClient) Connect(ctx context.Context) error {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	if wc.conn != nil {
		return fmt.Errorf("ws: already connected")
	}

	conn, _, err := websocket.Dial(ctx, wc.url, nil)
	if err != nil {
		return fmt.Errorf("ws: dial %s: %w", wc.url, err)
	}

	wc.conn = conn
	return nil
}

// Send writes a message to the WebSocket.
func (wc *WSClient) Send(ctx context.Context, msg []byte) error {
	wc.mu.Lock()
	conn := wc.conn
	wc.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("ws: not connected")
	}

	return conn.Write(ctx, websocket.MessageText, msg)
}

// Receive reads the next message from the WebSocket.
func (wc *WSClient) Receive(ctx context.Context) ([]byte, error) {
	wc.mu.Lock()
	conn := wc.conn
	wc.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("ws: not connected")
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("ws: read: %w", err)
	}
	return data, nil
}

// Close shuts down the WebSocket connection.
func (wc *WSClient) Close() error {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	// Signal done regardless of connection state.
	select {
	case <-wc.done:
	default:
		close(wc.done)
	}

	if wc.conn == nil {
		return nil
	}

	err := wc.conn.Close(websocket.StatusNormalClosure, "closing")
	wc.conn = nil
	return err
}

// Done returns a channel that is closed when the client is shut down.
func (wc *WSClient) Done() <-chan struct{} {
	return wc.done
}
