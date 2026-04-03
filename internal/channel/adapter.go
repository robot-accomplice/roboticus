package channel

import (
	"context"
	"time"
	"unicode/utf8"
)

// Adapter is the interface every channel implementation must satisfy.
// Adapters do exactly three things: parse inbound, call pipeline, format outbound.
type Adapter interface {
	PlatformName() string
	Recv(ctx context.Context) (*InboundMessage, error)
	Send(ctx context.Context, msg OutboundMessage) error
}

// InboundMessage is a normalized message received from any platform.
type InboundMessage struct {
	ID        string            `json:"id"`
	Platform  string            `json:"platform"`
	SenderID  string            `json:"sender_id"`
	ChatID    string            `json:"chat_id"`
	Content   string            `json:"content"`
	Timestamp time.Time         `json:"timestamp"`
	Media     []MediaAttachment `json:"media,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// OutboundMessage is a normalized message ready to send.
type OutboundMessage struct {
	Content     string         `json:"content"`
	RecipientID string         `json:"recipient_id"`
	Platform    string         `json:"platform"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// MediaAttachment represents multimodal content.
type MediaAttachment struct {
	Type        MediaType `json:"type"`
	SourceURL   string    `json:"source_url,omitempty"`
	LocalPath   string    `json:"local_path,omitempty"`
	Filename    string    `json:"filename,omitempty"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes,omitempty"`
	Caption     string    `json:"caption,omitempty"`
}

// MediaType classifies attachment content.
type MediaType string

const maxPlatformBytes = 64

// SanitizePlatform strips control characters (runes < 32 except newline) from s
// and truncates the result to 64 bytes, respecting UTF-8 boundaries.
func SanitizePlatform(s string) string {
	buf := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 32 && r != '\n' {
			continue
		}
		buf = append(buf, r)
	}
	out := string(buf)
	if len(out) > maxPlatformBytes {
		// Truncate to maxPlatformBytes without splitting a multi-byte rune.
		out = out[:maxPlatformBytes]
		for !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
	}
	return out
}

// SanitizeInbound sanitizes the Platform field of an inbound message.
// Adapters should call this before returning messages to the pipeline.
func SanitizeInbound(msg *InboundMessage) {
	if msg != nil {
		msg.Platform = SanitizePlatform(msg.Platform)
	}
}

const (
	MediaImage    MediaType = "image"
	MediaAudio    MediaType = "audio"
	MediaVideo    MediaType = "video"
	MediaDocument MediaType = "document"
)
