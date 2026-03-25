package channel

import (
	"bufio"
	"context"
	"crypto/tls"
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

// StartIMAPPoller begins polling for new emails via IMAP. Blocks until context is cancelled.
func (e *EmailAdapter) StartIMAPPoller(ctx context.Context) error {
	if e.cfg.IMAPHost == "" {
		log.Info().Msg("email: IMAP not configured, skipping poller")
		return nil
	}

	interval := time.Duration(e.cfg.PollInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Info().Str("host", e.cfg.IMAPHost).Int("port", e.cfg.IMAPPort).Dur("interval", interval).
		Msg("email: IMAP poller started")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := e.pollIMAP(ctx); err != nil {
				log.Warn().Err(err).Msg("email: IMAP poll error")
			}
		}
	}
}

// pollIMAP connects to the IMAP server, fetches unseen messages, and pushes them as inbound.
func (e *EmailAdapter) pollIMAP(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", e.cfg.IMAPHost, e.cfg.IMAPPort)

	// Connect via TLS.
	conn, err := imapDial(addr)
	if err != nil {
		return fmt.Errorf("imap connect: %w", err)
	}
	defer conn.Close()

	// Login.
	if err := imapCommand(conn, fmt.Sprintf("LOGIN %q %q", e.cfg.Username, e.cfg.Password)); err != nil {
		return fmt.Errorf("imap login: %w", err)
	}

	// Select INBOX.
	if err := imapCommand(conn, "SELECT INBOX"); err != nil {
		return fmt.Errorf("imap select: %w", err)
	}

	// Search unseen.
	uids, err := imapSearchUnseen(conn)
	if err != nil {
		return fmt.Errorf("imap search: %w", err)
	}

	for _, uid := range uids {
		from, subject, body, err := imapFetch(conn, uid)
		if err != nil {
			log.Warn().Err(err).Int("uid", uid).Msg("email: failed to fetch message")
			continue
		}

		if !e.isSenderAllowed(from) {
			log.Debug().Str("from", from).Msg("email: sender not in allowlist")
			continue
		}

		e.PushMessage(InboundMessage{
			ID:        fmt.Sprintf("email-%d", uid),
			Platform:  "email",
			SenderID:  from,
			ChatID:    from,
			Content:   body,
			Timestamp: time.Now(),
			Metadata:  map[string]any{"subject": subject, "uid": uid},
		})

		// Mark as seen.
		imapCommand(conn, fmt.Sprintf("STORE %d +FLAGS (\\Seen)", uid))
	}

	// Logout.
	imapCommand(conn, "LOGOUT")
	return nil
}

// --- IMAP helpers (simplified raw protocol, avoids external dependency) ---

type imapConn struct {
	conn    interface{ Close() error }
	scanner interface{ Scan() bool; Text() string }
	writer  interface{ WriteString(string) (int, error); Flush() error }
	tag     int
}

func imapDial(addr string) (*imapConn, error) {
	// Use crypto/tls for the connection.
	tlsConn, err := tls.Dial("tcp", addr, &tls.Config{})
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(tlsConn)
	writer := bufio.NewWriter(tlsConn)

	// Read greeting.
	if scanner.Scan() {
		log.Trace().Str("greeting", scanner.Text()).Msg("imap")
	}

	return &imapConn{
		conn:    tlsConn,
		scanner: scanner,
		writer:  writer,
	}, nil
}

func (c *imapConn) Close() error {
	if closer, ok := c.conn.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func imapCommand(c *imapConn, cmd string) error {
	c.tag++
	tag := fmt.Sprintf("A%03d", c.tag)
	line := fmt.Sprintf("%s %s\r\n", tag, cmd)
	c.writer.WriteString(line)
	c.writer.Flush()

	// Read until tagged response.
	for c.scanner.Scan() {
		text := c.scanner.Text()
		if strings.HasPrefix(text, tag+" OK") {
			return nil
		}
		if strings.HasPrefix(text, tag+" NO") || strings.HasPrefix(text, tag+" BAD") {
			return fmt.Errorf("imap: %s", text)
		}
	}
	return fmt.Errorf("imap: connection closed")
}

func imapSearchUnseen(c *imapConn) ([]int, error) {
	c.tag++
	tag := fmt.Sprintf("A%03d", c.tag)
	c.writer.WriteString(fmt.Sprintf("%s SEARCH UNSEEN\r\n", tag))
	c.writer.Flush()

	var uids []int
	for c.scanner.Scan() {
		text := c.scanner.Text()
		if strings.HasPrefix(text, "* SEARCH") {
			parts := strings.Fields(text)
			for _, p := range parts[2:] {
				var uid int
				if _, err := fmt.Sscanf(p, "%d", &uid); err == nil {
					uids = append(uids, uid)
				}
			}
		}
		if strings.HasPrefix(text, tag+" OK") {
			return uids, nil
		}
		if strings.HasPrefix(text, tag+" NO") || strings.HasPrefix(text, tag+" BAD") {
			return nil, fmt.Errorf("imap search: %s", text)
		}
	}
	return nil, fmt.Errorf("imap: connection closed during search")
}

func imapFetch(c *imapConn, uid int) (from, subject, body string, err error) {
	c.tag++
	tag := fmt.Sprintf("A%03d", c.tag)
	c.writer.WriteString(fmt.Sprintf("%s FETCH %d (BODY[HEADER.FIELDS (FROM SUBJECT)] BODY[TEXT])\r\n", tag, uid))
	c.writer.Flush()

	var inHeader, inBody bool
	var headerBuf, bodyBuf strings.Builder

	for c.scanner.Scan() {
		text := c.scanner.Text()
		if strings.HasPrefix(text, tag+" OK") {
			break
		}
		if strings.HasPrefix(text, tag+" NO") || strings.HasPrefix(text, tag+" BAD") {
			return "", "", "", fmt.Errorf("imap fetch: %s", text)
		}

		if strings.Contains(text, "HEADER.FIELDS") {
			inHeader = true
			inBody = false
			continue
		}
		if strings.Contains(text, "BODY[TEXT]") {
			inHeader = false
			inBody = true
			continue
		}
		if text == ")" {
			inHeader = false
			inBody = false
			continue
		}

		if inHeader {
			headerBuf.WriteString(text + "\n")
		}
		if inBody {
			bodyBuf.WriteString(text + "\n")
		}
	}

	// Parse headers.
	for _, line := range strings.Split(headerBuf.String(), "\n") {
		if strings.HasPrefix(strings.ToLower(line), "from:") {
			from = strings.TrimSpace(line[5:])
		}
		if strings.HasPrefix(strings.ToLower(line), "subject:") {
			subject = strings.TrimSpace(line[8:])
		}
	}

	body = strings.TrimSpace(bodyBuf.String())
	return from, subject, body, nil
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
