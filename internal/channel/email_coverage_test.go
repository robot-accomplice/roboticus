package channel

import (
	"testing"
)

func TestNewEmailAdapter(t *testing.T) {
	cfg := EmailConfig{
		SMTPHost:    "smtp.example.com",
		IMAPHost:    "imap.example.com",
		FromAddress: "bot@example.com",
		Password:    "secret",
	}
	adapter := NewEmailAdapter(cfg)
	if adapter == nil {
		t.Fatal("nil")
	}
	if adapter.PlatformName() != "email" {
		t.Errorf("platform = %s", adapter.PlatformName())
	}
}

func TestEmailAdapter_Defaults(t *testing.T) {
	adapter := NewEmailAdapter(EmailConfig{})
	if adapter.cfg.SMTPPort != DefaultSMTPPort {
		t.Errorf("smtp port = %d, want %d", adapter.cfg.SMTPPort, DefaultSMTPPort)
	}
	if adapter.cfg.IMAPPort != DefaultIMAPPort {
		t.Errorf("imap port = %d, want %d", adapter.cfg.IMAPPort, DefaultIMAPPort)
	}
	if adapter.cfg.PollInterval != DefaultPollIntervalSec {
		t.Errorf("poll = %d, want %d", adapter.cfg.PollInterval, DefaultPollIntervalSec)
	}
}

func TestEmailAdapter_CustomPorts(t *testing.T) {
	adapter := NewEmailAdapter(EmailConfig{
		SMTPPort: 465,
		IMAPPort: 143,
	})
	if adapter.cfg.SMTPPort != 465 {
		t.Errorf("smtp = %d", adapter.cfg.SMTPPort)
	}
	if adapter.cfg.IMAPPort != 143 {
		t.Errorf("imap = %d", adapter.cfg.IMAPPort)
	}
}
