package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSignalAdapter_Defaults(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{PhoneNumber: "+15551234567"})
	if a.PlatformName() != "signal" {
		t.Fatalf("expected signal, got %s", a.PlatformName())
	}
	if a.cfg.DaemonURL != "http://localhost:8080" {
		t.Fatalf("expected default DaemonURL, got %s", a.cfg.DaemonURL)
	}
	if a.cfg.BufferSize != 256 {
		t.Fatalf("expected default BufferSize 256, got %d", a.cfg.BufferSize)
	}
	if !a.cfg.DenyOnEmpty {
		t.Fatal("expected DenyOnEmpty=true when AllowedNumbers is empty")
	}
}

func TestNewSignalAdapter_CustomConfig(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{
		PhoneNumber:    "+15551234567",
		DaemonURL:      "http://custom:9999",
		BufferSize:     10,
		AllowedNumbers: []string{"+15559999999"},
	})
	if a.cfg.DaemonURL != "http://custom:9999" {
		t.Fatalf("expected custom URL, got %s", a.cfg.DaemonURL)
	}
	if a.cfg.BufferSize != 10 {
		t.Fatalf("expected 10, got %d", a.cfg.BufferSize)
	}
	if a.cfg.DenyOnEmpty {
		t.Fatal("DenyOnEmpty should be false when AllowedNumbers provided")
	}
}

func TestSignalPushAndRecv(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{PhoneNumber: "+15551234567", BufferSize: 2})
	ctx := context.Background()

	// Empty returns nil.
	msg, err := a.Recv(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != nil {
		t.Fatal("expected nil from empty buffer")
	}

	// Push and recv.
	ok := a.PushMessage(InboundMessage{ID: "1", Content: "msg1"})
	if !ok {
		t.Fatal("expected push to succeed")
	}
	msg, err = a.Recv(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == nil || msg.Content != "msg1" {
		t.Fatal("expected msg1")
	}
}

func TestSignalPushMessage_BufferFull(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{PhoneNumber: "+15551234567", BufferSize: 1})
	a.PushMessage(InboundMessage{ID: "1", Content: "first"})
	ok := a.PushMessage(InboundMessage{ID: "2", Content: "second"})
	if ok {
		t.Fatal("expected push to fail when buffer is full")
	}
}

func TestSignalProcessWebhook(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		payload map[string]any
		wantNil bool
		wantID  string
		wantGrp bool
	}{
		{
			name:    "valid direct message",
			allowed: []string{"+15559999999"},
			payload: map[string]any{
				"envelope": map[string]any{
					"sourceNumber": "+15559999999",
					"timestamp":    1700000000000,
					"dataMessage":  map[string]any{"message": "hello signal"},
				},
			},
			wantID: "sig-1700000000000",
		},
		{
			name:    "group message",
			allowed: []string{"+15559999999"},
			payload: map[string]any{
				"envelope": map[string]any{
					"sourceNumber": "+15559999999",
					"timestamp":    1700000000001,
					"dataMessage": map[string]any{
						"message":   "group msg",
						"groupInfo": map[string]any{"groupId": "grp123"},
					},
				},
			},
			wantID:  "sig-1700000000001",
			wantGrp: true,
		},
		{
			name:    "empty message ignored",
			allowed: []string{"+15559999999"},
			payload: map[string]any{
				"envelope": map[string]any{
					"sourceNumber": "+15559999999",
					"timestamp":    1700000000002,
					"dataMessage":  map[string]any{"message": ""},
				},
			},
			wantNil: true,
		},
		{
			name:    "sender not allowed",
			allowed: []string{"+15551111111"},
			payload: map[string]any{
				"envelope": map[string]any{
					"sourceNumber": "+15559999999",
					"timestamp":    1700000000003,
					"dataMessage":  map[string]any{"message": "nope"},
				},
			},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewSignalAdapter(SignalConfig{
				PhoneNumber:    "+15551234567",
				AllowedNumbers: tt.allowed,
			})
			data, _ := json.Marshal(tt.payload)
			msg, err := a.ProcessWebhook(data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if msg != nil {
					t.Fatalf("expected nil, got %+v", msg)
				}
				return
			}
			if msg == nil {
				t.Fatal("expected message, got nil")
			}
			if msg.ID != tt.wantID {
				t.Fatalf("expected ID %q, got %q", tt.wantID, msg.ID)
			}
			if tt.wantGrp && msg.ChatID != "group:grp123" {
				t.Fatalf("expected group chat ID, got %q", msg.ChatID)
			}
			if got := msg.Metadata["is_group"]; got != tt.wantGrp {
				t.Fatalf("is_group = %v, want %v", got, tt.wantGrp)
			}
		})
	}
}

func TestSignalSend_RPC(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if m, ok := req["method"].(string); ok {
			gotMethod = m
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{},"id":1}`))
	}))
	defer srv.Close()

	a := NewSignalAdapter(SignalConfig{
		PhoneNumber:    "+15551234567",
		DaemonURL:      srv.URL,
		AllowedNumbers: []string{"+15559999999"},
	})

	err := a.Send(context.Background(), OutboundMessage{
		RecipientID: "+15559999999",
		Content:     "test message",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "send" {
		t.Fatalf("expected rpc method 'send', got %q", gotMethod)
	}
}

func TestSignalSend_InvalidNumber(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{PhoneNumber: "+15551234567"})
	err := a.Send(context.Background(), OutboundMessage{
		RecipientID: "not-a-number",
		Content:     "test",
	})
	if err == nil {
		t.Fatal("expected error for invalid phone number")
	}
}
