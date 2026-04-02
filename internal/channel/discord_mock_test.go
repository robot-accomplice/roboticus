package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type discordMockHTTP struct {
	status int
	body   string
}

func (m *discordMockHTTP) Do(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.status,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     make(http.Header),
	}, nil
}

func TestDiscordAdapter_Send_WithMock(t *testing.T) {
	mock := &discordMockHTTP{status: 200, body: `{"id":"1"}`}
	adapter := NewDiscordAdapterWithHTTP(DiscordConfig{Token: "test-token"}, mock)

	err := adapter.Send(context.Background(), OutboundMessage{
		Content:     "hello discord",
		RecipientID: "channel-123",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestDiscordAdapter_Send_Error(t *testing.T) {
	mock := &discordMockHTTP{status: 403, body: `{"message":"Missing Permissions"}`}
	adapter := NewDiscordAdapterWithHTTP(DiscordConfig{Token: "test-token"}, mock)

	err := adapter.Send(context.Background(), OutboundMessage{
		Content:     "hello",
		RecipientID: "channel-123",
	})
	if err == nil {
		t.Error("should error on 403")
	}
}
