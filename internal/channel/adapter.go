package channel

import (
	"context"
	"time"
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

const (
	MediaImage    MediaType = "image"
	MediaAudio    MediaType = "audio"
	MediaVideo    MediaType = "video"
	MediaDocument MediaType = "document"
)
