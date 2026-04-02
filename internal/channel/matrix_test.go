package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMatrixAdapter_PlatformName(t *testing.T) {
	m := &MatrixAdapter{cfg: MatrixConfig{HomeserverURL: "http://localhost"}}
	if m.PlatformName() != "matrix" {
		t.Errorf("platform = %s", m.PlatformName())
	}
}

func TestMatrixAdapter_Send(t *testing.T) {
	var sentBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentBody)
		json.NewEncoder(w).Encode(map[string]string{"event_id": "$test"})
	}))
	defer server.Close()

	m := &MatrixAdapter{
		cfg: MatrixConfig{
			HomeserverURL: server.URL,
			AccessToken:   "test-token",
		},
		client: server.Client(),
	}

	err := m.Send(context.Background(), OutboundMessage{
		Content:     "hello matrix",
		RecipientID: "!room:example.com",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sentBody["body"] != "hello matrix" {
		t.Errorf("body = %v", sentBody["body"])
	}
	if sentBody["msgtype"] != "m.text" {
		t.Errorf("msgtype = %v", sentBody["msgtype"])
	}
}

func TestMatrixAdapter_Whoami(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_matrix/client/v3/account/whoami" {
			json.NewEncoder(w).Encode(map[string]string{"user_id": "@bot:example.com"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	m := &MatrixAdapter{
		cfg:    MatrixConfig{HomeserverURL: server.URL, AccessToken: "tok"},
		client: server.Client(),
	}
	userID, err := m.whoami()
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if userID != "@bot:example.com" {
		t.Errorf("user_id = %s", userID)
	}
}

func TestMatrixAdapter_SyncOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"next_batch": "s12345",
			"rooms": map[string]any{
				"join": map[string]any{
					"!room1:example.com": map[string]any{
						"timeline": map[string]any{
							"events": []map[string]any{
								{
									"type":             "m.room.message",
									"event_id":         "$evt1",
									"sender":           "@user:example.com",
									"origin_server_ts": 1700000000000,
									"content":          map[string]any{"msgtype": "m.text", "body": "hello bot"},
								},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	m := &MatrixAdapter{
		cfg:     MatrixConfig{HomeserverURL: server.URL, AccessToken: "tok", SyncTimeoutMs: 1000},
		client:  server.Client(),
		userID:  "@bot:example.com",
		inbound: make(chan InboundMessage, 64),
	}

	err := m.syncOnce(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if m.syncToken != "s12345" {
		t.Errorf("sync token = %s", m.syncToken)
	}

	select {
	case msg := <-m.inbound:
		if msg.Content != "hello bot" {
			t.Errorf("content = %s", msg.Content)
		}
		if msg.SenderID != "@user:example.com" {
			t.Errorf("sender = %s", msg.SenderID)
		}
	default:
		t.Error("should have received an inbound message")
	}
}
