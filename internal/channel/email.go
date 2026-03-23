package channel

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// EmailConfig holds email adapter configuration.
type EmailConfig struct {
	FromAddress    string   `mapstructure:"from_address"`
	SMTPHost       string   `mapstructure:"smtp_host"`
	SMTPPort       int      `mapstructure:"smtp_port"`
	IMAPHost       string   `mapstructure:"imap_host"`
	IMAPPort       int      `mapstructure:"imap_port"`
	Username       string   `mapstructure:"username"`
	Password       string   `mapstructure:"password"`
	AllowedSenders []string `mapstructure:"allowed_senders"`
	DenyOnEmpty    bool     `mapstructure:"deny_on_empty"`
	PollInterval   int      `mapstructure:"poll_interval"` // seconds, default 30
}

// EmailAdapter implements Adapter for email (SMTP send, IMAP receive).
type EmailAdapter struct {
	cfg           EmailConfig
	mu            sync.Mutex
	messageBuffer []InboundMessage
	threads       map[string]string // chatID → last Message-ID for threading
}

// NewEmailAdapter creates an email channel adapter.
func NewEmailAdapter(cfg EmailConfig) *EmailAdapter {
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}
	if cfg.IMAPPort == 0 {
		cfg.IMAPPort = 993
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 30
	}
	if len(cfg.AllowedSenders) == 0 {
		cfg.DenyOnEmpty = true
	}
	return &EmailAdapter{
		cfg:     cfg,
		threads: make(map[string]string),
	}
}

func (e *EmailAdapter) PlatformName() string { return "email" }

// PushMessage adds an inbound email from the IMAP poller.
func (e *EmailAdapter) PushMessage(msg InboundMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.messageBuffer = append(e.messageBuffer, msg)
}

// Recv returns the next buffered inbound email.
func (e *EmailAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.messageBuffer) == 0 {
		return nil, nil
	}
	msg := e.messageBuffer[0]
	e.messageBuffer = e.messageBuffer[1:]
	return &msg, nil
}

// Send sends an email via SMTP with threading headers.
func (e *EmailAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	messageID := fmt.Sprintf("<%s@%s>", uuid.New().String(), e.smtpDomain())

	// Build email with threading headers.
	var headers strings.Builder
	headers.WriteString(fmt.Sprintf("From: %s\r\n", e.cfg.FromAddress))
	headers.WriteString(fmt.Sprintf("To: %s\r\n", msg.RecipientID))
	headers.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	headers.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z)))

	subject := "Re: Conversation"
	if s, ok := msg.Metadata["subject"].(string); ok && s != "" {
		subject = s
	}
	headers.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))

	// Thread continuation.
	e.mu.Lock()
	if lastMsgID, ok := e.threads[msg.RecipientID]; ok {
		headers.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", lastMsgID))
		headers.WriteString(fmt.Sprintf("References: %s\r\n", lastMsgID))
	}
	e.threads[msg.RecipientID] = messageID
	e.mu.Unlock()

	headers.WriteString("MIME-Version: 1.0\r\n")
	headers.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	headers.WriteString("\r\n")

	fullMsg := headers.String() + msg.Content

	addr := fmt.Sprintf("%s:%d", e.cfg.SMTPHost, e.cfg.SMTPPort)
	auth := smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.SMTPHost)

	err := smtp.SendMail(addr, auth, e.cfg.FromAddress, []string{msg.RecipientID}, []byte(fullMsg))
	if err != nil {
		return fmt.Errorf("email smtp send: %w", err)
	}

	log.Debug().Str("to", msg.RecipientID).Msg("email sent")
	return nil
}

func (e *EmailAdapter) smtpDomain() string {
	parts := strings.SplitN(e.cfg.FromAddress, "@", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return e.cfg.SMTPHost
}

func (e *EmailAdapter) isSenderAllowed(sender string) bool {
	if len(e.cfg.AllowedSenders) == 0 {
		return !e.cfg.DenyOnEmpty
	}
	lower := strings.ToLower(sender)
	for _, allowed := range e.cfg.AllowedSenders {
		if strings.ToLower(allowed) == lower {
			return true
		}
	}
	return false
}
