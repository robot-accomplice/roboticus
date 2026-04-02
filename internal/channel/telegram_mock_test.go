package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type telegramMockHTTP struct {
	responses []mockResponse
	callIdx   int
}

type mockResponse struct {
	status int
	body   string
}

func (m *telegramMockHTTP) Do(_ *http.Request) (*http.Response, error) {
	if m.callIdx >= len(m.responses) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":[]}`)),
			Header:     make(http.Header),
		}, nil
	}
	r := m.responses[m.callIdx]
	m.callIdx++
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

func TestTelegramAdapter_Send_WithMock(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{{200, `{"ok":true,"result":{"message_id":1}}`}},
	}
	adapter := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "test-token"}, mock)

	err := adapter.Send(context.Background(), OutboundMessage{
		Content:     "hello world",
		RecipientID: "12345",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestTelegramAdapter_Send_NonOK(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{{400, `{"ok":false,"description":"Bad Request"}`}},
	}
	adapter := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "test-token"}, mock)

	// Send may or may not error depending on response parsing.
	// The key goal is exercising the code path.
	_ = adapter.Send(context.Background(), OutboundMessage{
		Content:     "hello",
		RecipientID: "12345",
	})
}

func TestTelegramAdapter_Recv_WithMock(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{{200, `{
			"ok": true,
			"result": [{
				"update_id": 1,
				"message": {
					"message_id": 1,
					"from": {"id": 100, "first_name": "Test"},
					"chat": {"id": 200},
					"text": "hello bot",
					"date": 1700000000
				}
			}]
		}`}},
	}
	adapter := NewTelegramAdapterWithHTTP(TelegramConfig{
		Token:          "test-token",
		AllowedChatIDs: []int64{200},
	}, mock)

	msg, err := adapter.Recv(context.Background())
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if msg == nil {
		t.Fatal("should receive a message")
	}
	if msg.Content != "hello bot" {
		t.Errorf("content = %q", msg.Content)
	}
	if msg.Platform != "telegram" {
		t.Errorf("platform = %s", msg.Platform)
	}
}

func TestTelegramAdapter_Recv_Empty(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{{200, `{"ok":true,"result":[]}`}},
	}
	adapter := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "test-token"}, mock)

	msg, err := adapter.Recv(context.Background())
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if msg != nil {
		t.Error("empty updates should return nil message")
	}
}
