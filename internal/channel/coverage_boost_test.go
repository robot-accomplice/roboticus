package channel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// adapter.go -- SanitizePlatform, SanitizeInbound
// ---------------------------------------------------------------------------

func TestSanitizePlatform_StripControl(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"telegram", "telegram"},
		{"tel\x00egram", "telegram"},
		{"dis\x01cord", "discord"},
		{"\x02\x03\x04hello", "hello"},
		{"line1\nline2", "line1\nline2"}, // newlines preserved
		{"", ""},
	}
	for _, tt := range tests {
		got := SanitizePlatform(tt.input)
		if got != tt.want {
			t.Errorf("SanitizePlatform(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizePlatform_Truncation(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := SanitizePlatform(long)
	if len(got) > maxPlatformBytes {
		t.Errorf("len = %d, should be <= %d", len(got), maxPlatformBytes)
	}
}

func TestSanitizePlatform_UTF8TruncationBoundary(t *testing.T) {
	// Multi-byte runes: each is 2 bytes, total 80 bytes. Truncation at 64 must not split a rune.
	runes := strings.Repeat("\u00e9", 40)
	got := SanitizePlatform(runes)
	if len(got) > maxPlatformBytes {
		t.Errorf("len = %d, should be <= %d", len(got), maxPlatformBytes)
	}
}

func TestSanitizeInbound_NilSafe(t *testing.T) {
	msg := &InboundMessage{Platform: "te\x00st"}
	SanitizeInbound(msg)
	if msg.Platform != "test" {
		t.Errorf("Platform = %q, want %q", msg.Platform, "test")
	}
	SanitizeInbound(nil) // must not panic
}

// ---------------------------------------------------------------------------
// delivery.go -- DeliveryWorker, MarkDelivered, drain
// ---------------------------------------------------------------------------

type fakeAdapter struct {
	sendErr  error
	mu       sync.Mutex
	sent     []OutboundMessage
	platform string
}

func (f *fakeAdapter) PlatformName() string { return f.platform }
func (f *fakeAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	return nil, nil
}
func (f *fakeAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	f.mu.Lock()
	f.sent = append(f.sent, msg)
	f.mu.Unlock()
	return f.sendErr
}
func (f *fakeAdapter) sentMessages() []OutboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]OutboundMessage, len(f.sent))
	copy(cp, f.sent)
	return cp
}

func TestDeliveryQueue_MarkDelivered(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("telegram", "user1", "hello")
	items := dq.DrainReady()
	if len(items) != 1 {
		t.Fatal("expected 1 item")
	}
	dq.MarkDelivered(items[0])
	if items[0].Status != DeliveryDelivered {
		t.Errorf("status = %d, want %d", items[0].Status, DeliveryDelivered)
	}
}

func TestDeliveryWorker_DrainSuccess(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("test", "user1", "hello")

	adapter := &fakeAdapter{platform: "test"}
	adapters := map[string]Adapter{"test": adapter}

	dw := NewDeliveryWorker(dq, adapters, 100*time.Millisecond)
	if dw == nil {
		t.Fatal("worker should not be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go dw.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()

	msgs := adapter.sentMessages()
	if len(msgs) == 0 {
		t.Fatal("worker should have sent the message")
	}
	if msgs[0].Content != "hello" {
		t.Errorf("content = %q", msgs[0].Content)
	}
}

func TestDeliveryWorker_UnknownChannel(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("nonexistent", "user1", "hi")

	dw := NewDeliveryWorker(dq, map[string]Adapter{}, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go dw.Run(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()

	if dq.PendingCount() == 0 && len(dq.DeadLetters()) == 0 {
		t.Fatal("item should be requeued or dead-lettered for unknown channel")
	}
}

func TestDeliveryWorker_SendError(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("test", "user1", "msg")

	adapter := &fakeAdapter{platform: "test", sendErr: fmt.Errorf("connection timeout")}
	dw := NewDeliveryWorker(dq, map[string]Adapter{"test": adapter}, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go dw.Run(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()

	if dq.PendingCount() == 0 {
		t.Fatal("transient error should requeue")
	}
}

func TestReplayDeadLetter_NotFound(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	if dq.ReplayDeadLetter("nonexistent-id") {
		t.Fatal("replay of non-existent ID should return false")
	}
}

// ---------------------------------------------------------------------------
// email.go -- smtpDomain, isSenderAllowed, PushMessage, Recv
// ---------------------------------------------------------------------------

func TestEmailSmtpDomain(t *testing.T) {
	e := NewEmailAdapter(EmailConfig{FromAddress: "bot@example.com"})
	if got := e.smtpDomain(); got != "example.com" {
		t.Errorf("smtpDomain = %q, want %q", got, "example.com")
	}

	e2 := NewEmailAdapter(EmailConfig{FromAddress: "noatsign", SMTPHost: "smtp.example.com"})
	if got := e2.smtpDomain(); got != "smtp.example.com" {
		t.Errorf("smtpDomain = %q, want %q", got, "smtp.example.com")
	}
}

func TestEmailIsSenderAllowed(t *testing.T) {
	e := NewEmailAdapter(EmailConfig{AllowedSenders: []string{"alice@example.com", "bob@example.com"}})
	if !e.isSenderAllowed("Alice@Example.com") {
		t.Error("should be case-insensitive match")
	}
	if e.isSenderAllowed("eve@evil.com") {
		t.Error("should reject unlisted sender")
	}

	e2 := NewEmailAdapter(EmailConfig{})
	if e2.isSenderAllowed("anyone@anywhere.com") {
		t.Error("empty allowlist with DenyOnEmpty should reject")
	}
}

func TestEmailPushAndRecv(t *testing.T) {
	e := NewEmailAdapter(EmailConfig{FromAddress: "bot@example.com"})
	ctx := context.Background()

	msg, _ := e.Recv(ctx)
	if msg != nil {
		t.Fatal("expected nil from empty buffer")
	}

	e.PushMessage(InboundMessage{ID: "1", Content: "hello"})
	msg, _ = e.Recv(ctx)
	if msg == nil || msg.Content != "hello" {
		t.Fatal("expected 'hello'")
	}

	msg, _ = e.Recv(ctx)
	if msg != nil {
		t.Fatal("expected nil after drain")
	}
}

func TestEmailStartIMAPPoller_NoHost(t *testing.T) {
	e := NewEmailAdapter(EmailConfig{})
	err := e.StartIMAPPoller(context.Background())
	if err != nil {
		t.Fatalf("no IMAP host should return nil: %v", err)
	}
}

// ---------------------------------------------------------------------------
// whatsapp.go -- NewWhatsAppAdapter, VerifyWebhook, ValidateWebhookSignature,
//   ProcessWebhook, isSenderAllowed, Send, Recv
// ---------------------------------------------------------------------------

func TestNewWhatsAppAdapter_Defaults(t *testing.T) {
	a := NewWhatsAppAdapter(WhatsAppConfig{Token: "tok", PhoneNumberID: "123"})
	if a.PlatformName() != "whatsapp" {
		t.Fatalf("expected whatsapp, got %s", a.PlatformName())
	}
	if a.cfg.APIVersion != "v21.0" {
		t.Fatalf("expected v21.0, got %s", a.cfg.APIVersion)
	}
	if !a.cfg.DenyOnEmpty {
		t.Fatal("expected DenyOnEmpty=true when AllowedNumbers is empty")
	}
	url := a.apiURL("messages")
	if !strings.Contains(url, "v21.0") || !strings.Contains(url, "123") {
		t.Fatalf("apiURL = %s", url)
	}
}

func TestWhatsAppVerifyWebhook(t *testing.T) {
	a := NewWhatsAppAdapter(WhatsAppConfig{VerifyToken: "my-token"})

	challenge, ok := a.VerifyWebhook("subscribe", "my-token", "challenge123")
	if !ok || challenge != "challenge123" {
		t.Fatal("should pass verification")
	}
	if _, ok := a.VerifyWebhook("subscribe", "wrong-token", "ch"); ok {
		t.Fatal("should fail with wrong token")
	}
	if _, ok := a.VerifyWebhook("not-subscribe", "my-token", "ch"); ok {
		t.Fatal("should fail with wrong mode")
	}
}

func TestWhatsAppValidateWebhookSignature(t *testing.T) {
	a := NewWhatsAppAdapter(WhatsAppConfig{AppSecret: "secret123"})
	body := []byte(`{"test":"data"}`)
	mac := hmac.New(sha256.New, []byte("secret123"))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !a.ValidateWebhookSignature(body, validSig) {
		t.Fatal("valid signature should pass")
	}
	if a.ValidateWebhookSignature(body, "sha256=invalid") {
		t.Fatal("invalid signature should fail")
	}

	a2 := NewWhatsAppAdapter(WhatsAppConfig{})
	if !a2.ValidateWebhookSignature(body, "anything") {
		t.Fatal("no secret should always pass")
	}
}

func TestWhatsAppProcessWebhook_AllTypes(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		payload string
		wantNil bool
		wantID  string
		wantErr bool
	}{
		{
			name:    "text message",
			allowed: []string{"+15551234567"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.1","from":"+15551234567","timestamp":"1700000000","type":"text","text":{"body":"hello"}}]}}]}]}`,
			wantID:  "wamid.1",
		},
		{
			name:    "image message",
			allowed: []string{"+15551234567"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.2","from":"+15551234567","timestamp":"1700000000","type":"image","image":{"id":"img1","caption":"photo"}}]}}]}]}`,
			wantID:  "wamid.2",
		},
		{
			name:    "video message",
			allowed: []string{"+15551234567"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.3","from":"+15551234567","timestamp":"1700000000","type":"video","video":{"id":"vid1","caption":"clip"}}]}}]}]}`,
			wantID:  "wamid.3",
		},
		{
			name:    "audio message",
			allowed: []string{"+15551234567"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.4","from":"+15551234567","timestamp":"1700000000","type":"audio","audio":{"id":"aud1"}}]}}]}]}`,
			wantID:  "wamid.4",
		},
		{
			name:    "document message",
			allowed: []string{"+15551234567"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.5","from":"+15551234567","timestamp":"1700000000","type":"document","document":{"id":"doc1","filename":"report.pdf"}}]}}]}]}`,
			wantID:  "wamid.5",
		},
		{
			name:    "sender not allowed",
			allowed: []string{"+15551111111"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.6","from":"+15559999999","timestamp":"1700000000","type":"text","text":{"body":"hi"}}]}}]}]}`,
			wantNil: true,
		},
		{
			name:    "empty text ignored",
			allowed: []string{"+15551234567"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.7","from":"+15551234567","timestamp":"1700000000","type":"text","text":{"body":""}}]}}]}]}`,
			wantNil: true,
		},
		{
			name:    "no messages",
			allowed: []string{"+15551234567"},
			payload: `{"entry":[{"changes":[{"value":{"messages":[]}}]}]}`,
			wantNil: true,
		},
		{
			name:    "invalid JSON",
			allowed: []string{"+15551234567"},
			payload: `{invalid`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewWhatsAppAdapter(WhatsAppConfig{
				Token:          "tok",
				PhoneNumberID:  "123",
				AllowedNumbers: tt.allowed,
			})
			msg, err := a.ProcessWebhook([]byte(tt.payload))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
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
				t.Fatal("expected message")
			}
			if msg.ID != tt.wantID {
				t.Fatalf("ID = %q, want %q", msg.ID, tt.wantID)
			}
			if msg.Platform != "whatsapp" {
				t.Fatalf("platform = %s", msg.Platform)
			}
			if got := msg.Metadata["is_group"]; got != false {
				t.Fatalf("is_group = %v, want false", got)
			}
			if got := msg.Metadata["sender_phone"]; got != msg.SenderID {
				t.Fatalf("sender_phone = %v, want %s", got, msg.SenderID)
			}
		})
	}
}

func TestWhatsAppRecv_Empty(t *testing.T) {
	a := NewWhatsAppAdapter(WhatsAppConfig{Token: "tok", PhoneNumberID: "123"})
	msg, _ := a.Recv(context.Background())
	if msg != nil {
		t.Fatal("expected nil from empty buffer")
	}
}

func TestWhatsAppRecv_AfterWebhook(t *testing.T) {
	a := NewWhatsAppAdapter(WhatsAppConfig{
		Token:          "tok",
		PhoneNumberID:  "123",
		AllowedNumbers: []string{"+15551234567"},
	})
	payload := `{"entry":[{"changes":[{"value":{"messages":[{"id":"w1","from":"+15551234567","timestamp":"1700000000","type":"text","text":{"body":"test"}}]}}]}]}`
	_, _ = a.ProcessWebhook([]byte(payload))
	msg, _ := a.Recv(context.Background())
	if msg == nil || msg.Content != "test" {
		t.Fatal("expected message with content 'test'")
	}
}

func TestWhatsAppIsSenderAllowed_Direct(t *testing.T) {
	a := NewWhatsAppAdapter(WhatsAppConfig{AllowedNumbers: []string{"+15551234567"}})
	if !a.isSenderAllowed("+15551234567") {
		t.Error("listed number should be allowed")
	}
	if a.isSenderAllowed("+15559999999") {
		t.Error("unlisted number should be denied")
	}
}

func TestWhatsAppSend_InvalidE164(t *testing.T) {
	a := NewWhatsAppAdapter(WhatsAppConfig{Token: "tok", PhoneNumberID: "123"})
	err := a.Send(context.Background(), OutboundMessage{RecipientID: "not-e164", Content: "hello"})
	if err == nil {
		t.Fatal("should error for invalid E.164 number")
	}
}

// ---------------------------------------------------------------------------
// voice.go -- Transcribe, Synthesize, Stats
// ---------------------------------------------------------------------------

func TestVoiceAdapter_TranscribeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello world","language":"en","duration":1.5}`))
	}))
	defer srv.Close()

	v := NewVoiceAdapter(VoiceConfig{APIBaseURL: srv.URL, APIKey: "test-key"})
	result, err := v.Transcribe(context.Background(), []byte("fake-audio"), AudioWav)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "hello world" {
		t.Errorf("text = %q", result.Text)
	}
	tc, sc := v.Stats()
	if tc != 1 || sc != 0 {
		t.Errorf("stats = %d, %d", tc, sc)
	}
}

func TestVoiceAdapter_TranscribeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad audio"}`))
	}))
	defer srv.Close()

	v := NewVoiceAdapter(VoiceConfig{APIBaseURL: srv.URL})
	_, err := v.Transcribe(context.Background(), []byte("bad"), AudioWav)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVoiceAdapter_SynthesizeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("fake-audio-data"))
	}))
	defer srv.Close()

	v := NewVoiceAdapter(VoiceConfig{APIBaseURL: srv.URL, APIKey: "test-key"})
	audio, err := v.Synthesize(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if string(audio) != "fake-audio-data" {
		t.Errorf("audio = %q", string(audio))
	}
	tc, sc := v.Stats()
	if tc != 0 || sc != 1 {
		t.Errorf("stats = %d, %d", tc, sc)
	}
}

func TestVoiceAdapter_SynthesizeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`error`))
	}))
	defer srv.Close()

	v := NewVoiceAdapter(VoiceConfig{APIBaseURL: srv.URL})
	_, err := v.Synthesize(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVoiceAdapter_ConfigDefaults(t *testing.T) {
	v := NewVoiceAdapter(VoiceConfig{})
	if v.cfg.STTModel != "whisper-large-v3" {
		t.Errorf("STTModel = %q", v.cfg.STTModel)
	}
	if v.cfg.TTSModel != "tts-1" {
		t.Errorf("TTSModel = %q", v.cfg.TTSModel)
	}
	if v.cfg.TTSVoice != "alloy" {
		t.Errorf("TTSVoice = %q", v.cfg.TTSVoice)
	}
	if v.cfg.SampleRate != 16000 {
		t.Errorf("SampleRate = %d", v.cfg.SampleRate)
	}
}

// ---------------------------------------------------------------------------
// router.go -- SendReply, Adapters, GetAdapter, DeliveryQueue
// ---------------------------------------------------------------------------

func TestRouter_SendReplySuccess(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	r := NewRouter(dq)
	adapter := &fakeAdapter{platform: "test"}
	r.Register(adapter)

	err := r.SendReply(context.Background(), "test", "user1", "hello **world**")
	if err != nil {
		t.Fatalf("SendReply: %v", err)
	}
	if len(adapter.sent) != 1 {
		t.Fatalf("expected 1 sent, got %d", len(adapter.sent))
	}
}

func TestRouter_SendReply_UnknownChannel(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	r := NewRouter(dq)
	err := r.SendReply(context.Background(), "nonexistent", "user1", "msg")
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
}

func TestRouter_Adapters_Returns(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	r := NewRouter(dq)
	r.Register(&fakeAdapter{platform: "p1"})
	r.Register(&fakeAdapter{platform: "p2"})

	adapters := r.Adapters()
	if len(adapters) != 2 {
		t.Fatalf("expected 2 adapters, got %d", len(adapters))
	}
}

func TestRouter_GetAdapter_Found(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	r := NewRouter(dq)
	fa := &fakeAdapter{platform: "test"}
	r.Register(fa)

	if r.GetAdapter("test") != fa {
		t.Fatal("GetAdapter should return registered adapter")
	}
	if r.GetAdapter("nonexistent") != nil {
		t.Fatal("GetAdapter for unknown should return nil")
	}
}

func TestRouter_DeliveryQueueAccess(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	r := NewRouter(dq)
	if r.DeliveryQueue() != dq {
		t.Fatal("DeliveryQueue() should return the queue")
	}
}

func TestRouter_PollAll_WithError(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	r := NewRouter(dq)
	r.Register(&errorAdapter{platform: "err"})

	msgs := r.PollAll(context.Background())
	if len(msgs) != 0 {
		t.Fatal("should return no messages on error")
	}
	statuses := r.Status()
	found := false
	for _, s := range statuses {
		if s.Name == "err" && s.LastError != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected error to be recorded in status")
	}
}

type errorAdapter struct {
	platform string
}

func (e *errorAdapter) PlatformName() string { return e.platform }
func (e *errorAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	return nil, fmt.Errorf("recv failed")
}
func (e *errorAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	return nil
}

// ---------------------------------------------------------------------------
// a2a.go -- DecryptInbound, CleanupExpired, evictOldestSession, PlatformName, Send
// ---------------------------------------------------------------------------

func TestA2AAdapter_DecryptInbound(t *testing.T) {
	a, err := NewA2AAdapter(A2AConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if a.PlatformName() != "a2a" {
		t.Fatal("wrong platform name")
	}

	peer, _ := NewA2AAdapter(A2AConfig{})
	if err := a.EstablishSession("peer1", peer.PublicKeyHex(), "nonce1"); err != nil {
		t.Fatal(err)
	}

	a.mu.Lock()
	session := a.sessions["peer1"]
	ct, err := a.encrypt(session.sessionKey, []byte("secret message"))
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := a.DecryptInbound("peer1", ct); err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	msg, _ := a.Recv(context.Background())
	if msg == nil || msg.Content != "secret message" {
		t.Fatal("expected decrypted message")
	}
}

func TestA2AAdapter_DecryptInbound_NoSession(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{})
	if err := a.DecryptInbound("unknown-peer", []byte("data")); err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestA2AAdapter_Send_NoSession(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{})
	err := a.Send(context.Background(), OutboundMessage{RecipientID: "no-peer", Content: "hi"})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestA2AAdapter_Send_TooLarge(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{MaxMessageSize: 10})
	peer, _ := NewA2AAdapter(A2AConfig{})
	_ = a.EstablishSession("peer1", peer.PublicKeyHex(), "nonce1")

	err := a.Send(context.Background(), OutboundMessage{
		RecipientID: "peer1",
		Content:     strings.Repeat("x", 100),
	})
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func TestA2AAdapter_Send_Success(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{})
	peer, _ := NewA2AAdapter(A2AConfig{})
	_ = a.EstablishSession("peer1", peer.PublicKeyHex(), "nonce1")

	err := a.Send(context.Background(), OutboundMessage{
		RecipientID: "peer1",
		Content:     "hello peer",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestA2AAdapter_CleanupExpired(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{SessionTimeout: 1, NonceTTL: 1})
	peer, _ := NewA2AAdapter(A2AConfig{})
	_ = a.EstablishSession("peer1", peer.PublicKeyHex(), "nonce1")

	a.mu.Lock()
	a.sessions["peer1"].lastActive = time.Now().Add(-10 * time.Second)
	a.seenNonces["nonce1"] = time.Now().Add(-10 * time.Second)
	a.mu.Unlock()

	a.CleanupExpired()

	a.mu.Lock()
	if len(a.sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(a.sessions))
	}
	if len(a.seenNonces) != 0 {
		t.Errorf("expected 0 nonces, got %d", len(a.seenNonces))
	}
	a.mu.Unlock()
}

func TestA2AAdapter_EvictOldestSession(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{MaxSessions: 2})
	peer1, _ := NewA2AAdapter(A2AConfig{})
	peer2, _ := NewA2AAdapter(A2AConfig{})
	peer3, _ := NewA2AAdapter(A2AConfig{})

	_ = a.EstablishSession("p1", peer1.PublicKeyHex(), "n1")
	time.Sleep(5 * time.Millisecond)
	_ = a.EstablishSession("p2", peer2.PublicKeyHex(), "n2")
	time.Sleep(5 * time.Millisecond)
	_ = a.EstablishSession("p3", peer3.PublicKeyHex(), "n3")

	a.mu.Lock()
	_, hasP1 := a.sessions["p1"]
	_, hasP3 := a.sessions["p3"]
	a.mu.Unlock()

	if hasP1 {
		t.Error("p1 should have been evicted")
	}
	if !hasP3 {
		t.Error("p3 should be present")
	}
}

func TestA2AAdapter_RateLimit(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{RateLimitPerPeer: 2})
	peer, _ := NewA2AAdapter(A2AConfig{})

	_ = a.EstablishSession("p1", peer.PublicKeyHex(), "n1")
	_ = a.EstablishSession("p1", peer.PublicKeyHex(), "n2")
	err := a.EstablishSession("p1", peer.PublicKeyHex(), "n3")
	if err == nil {
		t.Fatal("expected rate limit error")
	}
}

func TestA2AAdapter_NonceReplay(t *testing.T) {
	a, _ := NewA2AAdapter(A2AConfig{})
	peer, _ := NewA2AAdapter(A2AConfig{})
	_ = a.EstablishSession("p1", peer.PublicKeyHex(), "nonce-once")
	if err := a.EstablishSession("p1", peer.PublicKeyHex(), "nonce-once"); err == nil {
		t.Fatal("expected nonce replay error")
	}
}

// ---------------------------------------------------------------------------
// dkim.go -- Verify with signature present (DNS lookup will fail = temperror)
// ---------------------------------------------------------------------------

func TestDKIMVerifier_SignaturePresent_DNSFail(t *testing.T) {
	v := NewDKIMVerifier(true)
	raw := "DKIM-Signature: v=1; a=rsa-sha256; d=nonexistent-domain-test.invalid; s=selector1; b=abc123base64;\nFrom: test@example.com\n"
	result := v.Verify(raw)
	if !result.HasSignature {
		t.Fatal("should detect signature")
	}
	if result.Status != "temperror" {
		t.Errorf("status = %q, want temperror", result.Status)
	}
}

func TestExtractDKIMSignature_IncompleteTags(t *testing.T) {
	sig := extractDKIMSignature("DKIM-Signature: v=1; a=rsa-sha256; d=example.com;\n")
	if sig != nil {
		t.Fatal("expected nil when required tags (s, b) missing")
	}
}

// ---------------------------------------------------------------------------
// media.go -- DownloadWhatsAppMedia error paths, checkIP additional ranges
// ---------------------------------------------------------------------------

func TestDownloadWhatsAppMedia_EmptyParams(t *testing.T) {
	ms := NewMediaService()

	_, err := ms.DownloadWhatsAppMedia(context.Background(), "", "token")
	if err == nil {
		t.Fatal("expected error for empty media ID")
	}
	_, err = ms.DownloadWhatsAppMedia(context.Background(), "media123", "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestCheckIP_SpecificRanges(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.0.1", false},
		{"172.32.0.1", false},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		err := checkIP(ip)
		if tt.blocked && err == nil {
			t.Errorf("checkIP(%s) should be blocked", tt.ip)
		}
		if !tt.blocked && err != nil {
			t.Errorf("checkIP(%s) should be allowed: %v", tt.ip, err)
		}
	}
}

// ---------------------------------------------------------------------------
// web.go -- PlatformName
// ---------------------------------------------------------------------------

func TestWebAdapter_PlatformNameCoverage(t *testing.T) {
	a := NewWebAdapter(WebConfig{})
	if a.PlatformName() != "web" {
		t.Fatalf("expected web, got %s", a.PlatformName())
	}
}

// ---------------------------------------------------------------------------
// formatter.go -- WhatsApp headers, Signal detailed paths
// ---------------------------------------------------------------------------

func TestWhatsAppFormatter_HeaderConversion(t *testing.T) {
	f := &WhatsAppFormatter{}
	result := f.Format("# Main Title\n## Subtitle\nRegular line")
	if !strings.Contains(result, "*Main Title*") {
		t.Error("should convert # header to bold")
	}
	if !strings.Contains(result, "*Subtitle*") {
		t.Error("should convert ## header to bold")
	}
}

func TestSignalFormatter_StripHeadersNested(t *testing.T) {
	f := &SignalFormatter{}
	result := f.Format("# # Nested header")
	if strings.HasPrefix(result, "# ") {
		t.Error("should strip header markers")
	}
}

func TestSignalFormatter_ExtractsURLs(t *testing.T) {
	f := &SignalFormatter{}
	result := f.Format("[link](https://example.com)")
	if !strings.Contains(result, "https://example.com") {
		t.Error("should extract URL from link syntax")
	}
}

// ---------------------------------------------------------------------------
// discord.go -- isGuildAllowed edge, Send chunking, ConnectGateway empty token
// ---------------------------------------------------------------------------

func TestDiscordIsGuildAllowed_EmptyNoDeny(t *testing.T) {
	a := &DiscordAdapter{cfg: DiscordConfig{AllowedGuildIDs: []string{}, DenyOnEmpty: false}}
	if !a.isGuildAllowed("any-guild") {
		t.Fatal("should allow when empty without DenyOnEmpty")
	}
}

func TestDiscordSend_ChunkingLargeMessage(t *testing.T) {
	var requestCount int
	mock := &discordMultiMockHTTP{handler: func(r *http.Request) (int, string) {
		requestCount++
		return 200, `{"id":"1"}`
	}}

	a := NewDiscordAdapterWithHTTP(DiscordConfig{Token: "tok"}, mock)
	bigMsg := strings.Repeat("x", discordMaxChunk+100)
	err := a.Send(context.Background(), OutboundMessage{Content: bigMsg, RecipientID: "ch1"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected 2 chunks, got %d requests", requestCount)
	}
}

type discordMultiMockHTTP struct {
	handler func(r *http.Request) (int, string)
}

func (m *discordMultiMockHTTP) Do(req *http.Request) (*http.Response, error) {
	status, body := m.handler(req)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func TestDiscordConnectGateway_EmptyToken(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{Token: "", GatewayEnabled: true})
	err := a.ConnectGateway(context.Background())
	if err != nil {
		t.Fatalf("empty token should return nil: %v", err)
	}
}

// ---------------------------------------------------------------------------
// telegram.go -- sendMessage fallback, Recv edge cases
// ---------------------------------------------------------------------------

func TestTelegramSendMessage_FallbackToPlain(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{
			{200, `{"ok":true}`},  // typing action
			{400, `{"ok":false}`}, // MarkdownV2 fails
			{200, `{"ok":true}`},  // plain text succeeds
		},
	}
	a := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "tok"}, mock)
	err := a.Send(context.Background(), OutboundMessage{
		Content:     "test with *markdown*",
		RecipientID: "123",
	})
	if err != nil {
		t.Fatalf("send with fallback should succeed: %v", err)
	}
}

func TestTelegramRecv_RateLimited429(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{{429, `{"ok":false}`}},
	}
	a := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "tok"}, mock)
	msg, err := a.Recv(context.Background())
	if err != nil {
		t.Fatalf("rate limited should not error: %v", err)
	}
	if msg != nil {
		t.Fatal("rate limited should return nil")
	}
}

func TestTelegramRecv_NonOKStatusCode(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{{500, `{"ok":false}`}},
	}
	a := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "tok"}, mock)
	_, err := a.Recv(context.Background())
	if err == nil {
		t.Fatal("500 should produce error")
	}
}

func TestTelegramRecv_OKFalseResponse(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{{200, `{"ok":false,"result":[]}`}},
	}
	a := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "tok"}, mock)
	_, err := a.Recv(context.Background())
	if err == nil {
		t.Fatal("ok=false should produce error")
	}
}

func TestTelegramRecv_MultipleUpdatesBuffered(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{
			{200, `{
				"ok": true,
				"result": [
					{"update_id": 1, "message": {"message_id": 1, "from": {"id": 100}, "chat": {"id": 200}, "text": "msg1", "date": 1700000000}},
					{"update_id": 2, "message": {"message_id": 2, "from": {"id": 100}, "chat": {"id": 200}, "text": "msg2", "date": 1700000001}}
				]
			}`},
		},
	}
	a := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "tok", AllowedChatIDs: []int64{200}}, mock)

	msg, _ := a.Recv(context.Background())
	if msg == nil || msg.Content != "msg1" {
		t.Fatal("expected msg1")
	}
	msg, _ = a.Recv(context.Background())
	if msg == nil || msg.Content != "msg2" {
		t.Fatal("expected msg2")
	}
}

func TestTelegramRecv_NoFromField(t *testing.T) {
	mock := &telegramMockHTTP{
		responses: []mockResponse{
			{200, `{"ok":true,"result":[{"update_id":1,"message":{"message_id":1,"chat":{"id":200},"text":"hello","date":1700000000}}]}`},
		},
	}
	a := NewTelegramAdapterWithHTTP(TelegramConfig{Token: "tok", AllowedChatIDs: []int64{200}}, mock)
	msg, err := a.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil {
		t.Fatal("expected message")
	}
	if msg.SenderID != "" {
		t.Errorf("SenderID = %q, want empty", msg.SenderID)
	}
}

// ---------------------------------------------------------------------------
// signal.go -- rpcCall error response, non-OK status, webhook edge cases
// ---------------------------------------------------------------------------

func TestSignalRPC_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-1,"message":"daemon error"},"id":1}`))
	}))
	defer srv.Close()

	a := NewSignalAdapterWithHTTP(SignalConfig{
		PhoneNumber:    "+15551234567",
		DaemonURL:      srv.URL,
		AllowedNumbers: []string{"+15559999999"},
	}, srv.Client())

	err := a.Send(context.Background(), OutboundMessage{RecipientID: "+15559999999", Content: "test"})
	if err == nil {
		t.Fatal("expected error from rpc error response")
	}
	if !strings.Contains(err.Error(), "daemon error") {
		t.Errorf("error = %q, should contain 'daemon error'", err.Error())
	}
}

func TestSignalRPC_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`internal error`))
	}))
	defer srv.Close()

	a := NewSignalAdapterWithHTTP(SignalConfig{
		PhoneNumber:    "+15551234567",
		DaemonURL:      srv.URL,
		AllowedNumbers: []string{"+15559999999"},
	}, srv.Client())

	err := a.Send(context.Background(), OutboundMessage{RecipientID: "+15559999999", Content: "test"})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestSignalProcessWebhook_InvalidJSON(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{PhoneNumber: "+15551234567"})
	_, err := a.ProcessWebhook([]byte(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSignalProcessWebhook_NoDataMessage(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{PhoneNumber: "+15551234567"})
	msg, err := a.ProcessWebhook([]byte(`{"envelope":{"sourceNumber":"+15559999999","timestamp":1700000000}}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg != nil {
		t.Fatal("expected nil for missing dataMessage")
	}
}

func TestSignalProcessWebhook_SourceFallback(t *testing.T) {
	a := NewSignalAdapter(SignalConfig{
		PhoneNumber:    "+15551234567",
		AllowedNumbers: []string{"uuid-source"},
	})
	msg, err := a.ProcessWebhook([]byte(`{"envelope":{"source":"uuid-source","timestamp":1700000000,"dataMessage":{"message":"hi"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || msg.SenderID != "uuid-source" {
		t.Fatalf("expected sender 'uuid-source', got %v", msg)
	}
}

// ---------------------------------------------------------------------------
// matrix.go -- contains helper
// ---------------------------------------------------------------------------

func TestContains_Helper(t *testing.T) {
	if !contains([]string{"a", "b", "c"}, "b") {
		t.Error("should find 'b'")
	}
	if contains([]string{"a", "b", "c"}, "d") {
		t.Error("should not find 'd'")
	}
	if contains(nil, "a") {
		t.Error("nil slice should return false")
	}
}

// ---------------------------------------------------------------------------
// media.go -- DownloadWhatsAppMedia with mock (metadata + download steps)
// ---------------------------------------------------------------------------

func TestDownloadWhatsAppMedia_MetadataFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`server error`))
	}))
	defer srv.Close()

	ms := &MediaService{
		maxFileSize: defaultMaxFileSize,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		validateURL: func(s string) error { return nil },
	}
	_, err := ms.DownloadWhatsAppMedia(context.Background(), "media123", "token")
	// Will fail trying to reach the WhatsApp Graph API.
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Delivery queue: heap ordering
// ---------------------------------------------------------------------------

func TestDeliveryHeap_Ordering(t *testing.T) {
	dq := NewDeliveryQueue(nil)

	// Enqueue 3 items. All should be immediately ready.
	dq.Enqueue("a", "u1", "msg1")
	dq.Enqueue("b", "u2", "msg2")
	dq.Enqueue("c", "u3", "msg3")

	items := dq.DrainReady()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// Signal rate limit on Send
// ---------------------------------------------------------------------------

func TestSignalSend_RateLimitExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{},"id":1}`))
	}))
	defer srv.Close()

	a := NewSignalAdapterWithHTTP(SignalConfig{
		PhoneNumber:    "+15551234567",
		DaemonURL:      srv.URL,
		AllowedNumbers: []string{"+15559999999"},
	}, srv.Client())

	// Exhaust the rate limiter (30 per minute by default).
	for i := 0; i < 30; i++ {
		_ = a.Send(context.Background(), OutboundMessage{
			RecipientID: "+15559999999",
			Content:     "test",
		})
	}

	// 31st should hit rate limit.
	err := a.Send(context.Background(), OutboundMessage{
		RecipientID: "+15559999999",
		Content:     "one more",
	})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %q, expected 'rate limit'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Discord getGatewayURL with mock
// ---------------------------------------------------------------------------

func TestDiscord_GetGatewayURL(t *testing.T) {
	mock := &discordMultiMockHTTP{handler: func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "/gateway/bot") {
			return 200, `{"url":"wss://gateway.discord.gg"}`
		}
		return 200, `{}`
	}}
	a := NewDiscordAdapterWithHTTP(DiscordConfig{Token: "tok"}, mock)
	url, err := a.getGatewayURL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if url != "wss://gateway.discord.gg" {
		t.Errorf("url = %q", url)
	}
}

func TestDiscord_GetGatewayURL_EmptyFallback(t *testing.T) {
	mock := &discordMultiMockHTTP{handler: func(r *http.Request) (int, string) {
		return 200, `{"url":""}`
	}}
	a := NewDiscordAdapterWithHTTP(DiscordConfig{Token: "tok"}, mock)
	url, err := a.getGatewayURL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if url != "wss://gateway.discord.gg" {
		t.Errorf("expected fallback URL, got %q", url)
	}
}

// ---------------------------------------------------------------------------
// matrixEventContent UnmarshalJSON
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// matrix.go -- NewMatrixAdapterWithHTTP, Send, Recv, syncOnce, joinRoom
// ---------------------------------------------------------------------------

func TestNewMatrixAdapterWithHTTP_MissingConfig(t *testing.T) {
	_, err := NewMatrixAdapterWithHTTP(MatrixConfig{}, nil)
	if err == nil {
		t.Fatal("expected error for missing homeserver_url")
	}

	_, err = NewMatrixAdapterWithHTTP(MatrixConfig{HomeserverURL: "http://hs"}, nil)
	if err == nil {
		t.Fatal("expected error for missing access_token")
	}
}

func TestNewMatrixAdapterWithHTTP_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/account/whoami") {
			_, _ = w.Write([]byte(`{"user_id":"@bot:matrix.org"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/sync") {
			_, _ = w.Write([]byte(`{"next_batch":"s1","rooms":{"join":{},"invite":{}}}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a, err := NewMatrixAdapterWithHTTP(MatrixConfig{
		HomeserverURL: srv.URL,
		AccessToken:   "test-token",
		DeviceID:      "DEVICE1",
	}, srv.Client())
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	if a.PlatformName() != "matrix" {
		t.Fatalf("platform = %s", a.PlatformName())
	}
	if a.userID != "@bot:matrix.org" {
		t.Errorf("userID = %q", a.userID)
	}
}

func TestMatrixAdapter_RecvAndSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/account/whoami") {
			_, _ = w.Write([]byte(`{"user_id":"@bot:test"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/sync") {
			resp := map[string]any{
				"next_batch": "s2",
				"rooms": map[string]any{
					"join": map[string]any{
						"!room1:test": map[string]any{
							"timeline": map[string]any{
								"events": []map[string]any{
									{
										"type":             "m.room.message",
										"event_id":         "$event1",
										"sender":           "@alice:test",
										"origin_server_ts": 1700000000000,
										"content":          map[string]any{"msgtype": "m.text", "body": "hello matrix"},
									},
									// Own message should be skipped.
									{
										"type":             "m.room.message",
										"event_id":         "$event2",
										"sender":           "@bot:test",
										"origin_server_ts": 1700000001000,
										"content":          map[string]any{"msgtype": "m.text", "body": "my own msg"},
									},
								},
							},
						},
					},
					"invite": map[string]any{},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a, err := NewMatrixAdapterWithHTTP(MatrixConfig{
		HomeserverURL: srv.URL,
		AccessToken:   "tok",
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	msg, err := a.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil {
		t.Fatal("expected message")
	}
	if msg.Content != "hello matrix" {
		t.Errorf("content = %q", msg.Content)
	}
	if msg.SenderID != "@alice:test" {
		t.Errorf("sender = %q", msg.SenderID)
	}
}

func TestMatrixAdapter_SendPlaintext(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/account/whoami") {
			_, _ = w.Write([]byte(`{"user_id":"@bot:test"}`))
			return
		}
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"event_id":"$sent1"}`))
	}))
	defer srv.Close()

	a, err := NewMatrixAdapterWithHTTP(MatrixConfig{
		HomeserverURL: srv.URL,
		AccessToken:   "tok",
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	err = a.Send(context.Background(), OutboundMessage{
		RecipientID: "!room1:test",
		Content:     "hello",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if !strings.Contains(gotPath, "/rooms/!room1:test/send/m.room.message") {
		t.Errorf("path = %s", gotPath)
	}
}

func TestMatrixAdapter_AutoJoinInvite(t *testing.T) {
	var joinedRoomID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/account/whoami") {
			_, _ = w.Write([]byte(`{"user_id":"@bot:test"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/join") {
			joinedRoomID = r.URL.Path
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if strings.Contains(r.URL.Path, "/sync") {
			resp := map[string]any{
				"next_batch": "s3",
				"rooms": map[string]any{
					"join":   map[string]any{},
					"invite": map[string]any{"!invited:test": map[string]any{}},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a, err := NewMatrixAdapterWithHTTP(MatrixConfig{
		HomeserverURL: srv.URL,
		AccessToken:   "tok",
		AutoJoin:      true,
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Recv(context.Background()) // triggers sync which processes invite
	if !strings.Contains(joinedRoomID, "!invited:test") {
		t.Errorf("expected auto-join of !invited:test, got %q", joinedRoomID)
	}
}

func TestMatrixAdapter_AllowedRoomsFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/account/whoami") {
			_, _ = w.Write([]byte(`{"user_id":"@bot:test"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/sync") {
			resp := map[string]any{
				"next_batch": "s4",
				"rooms": map[string]any{
					"join": map[string]any{
						"!allowed:test": map[string]any{
							"timeline": map[string]any{
								"events": []map[string]any{
									{
										"type": "m.room.message", "event_id": "$e1", "sender": "@alice:test",
										"origin_server_ts": 1700000000000,
										"content":          map[string]any{"msgtype": "m.text", "body": "allowed msg"},
									},
								},
							},
						},
						"!blocked:test": map[string]any{
							"timeline": map[string]any{
								"events": []map[string]any{
									{
										"type": "m.room.message", "event_id": "$e2", "sender": "@eve:test",
										"origin_server_ts": 1700000000000,
										"content":          map[string]any{"msgtype": "m.text", "body": "blocked msg"},
									},
								},
							},
						},
					},
					"invite": map[string]any{},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a, err := NewMatrixAdapterWithHTTP(MatrixConfig{
		HomeserverURL: srv.URL,
		AccessToken:   "tok",
		AllowedRooms:  []string{"!allowed:test"},
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	msg, _ := a.Recv(context.Background())
	if msg == nil {
		t.Fatal("expected message from allowed room")
	}
	if msg.Content != "allowed msg" {
		t.Errorf("content = %q", msg.Content)
	}
}

// ---------------------------------------------------------------------------
// discord.go -- runGateway / gatewaySession with cancelled context
// ---------------------------------------------------------------------------

func TestDiscordGatewaySession_Cancelled(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{Token: "tok", GatewayEnabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	err := a.gatewaySession(ctx, "wss://test")
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestDiscordRunGateway_Cancelled(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{Token: "tok", GatewayEnabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := a.runGateway(ctx, "wss://test")
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestMatrixEventContent_UnmarshalJSON(t *testing.T) {
	data := []byte(`{"msgtype":"m.text","body":"hello","custom_field":"value"}`)
	var c matrixEventContent
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.MsgType != "m.text" {
		t.Errorf("MsgType = %q", c.MsgType)
	}
	if c.Body != "hello" {
		t.Errorf("Body = %q", c.Body)
	}
	if c.Raw["custom_field"] != "value" {
		t.Error("Raw should capture extra fields")
	}
}
