package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roboticus/internal/channel"
	"roboticus/internal/pipeline"
)

type webhookStubRunner struct {
	inputs []pipeline.Input
}

func (r *webhookStubRunner) Run(_ context.Context, _ pipeline.Config, input pipeline.Input) (*pipeline.Outcome, error) {
	r.inputs = append(r.inputs, input)
	return &pipeline.Outcome{SessionID: "s1", Content: "ok"}, nil
}

type stubBatchParser struct {
	msgs []channel.InboundMessage
	err  error
}

func (s stubBatchParser) ProcessWebhookBatch(_ []byte) ([]channel.InboundMessage, error) {
	return s.msgs, s.err
}

type stubWhatsAppWebhook struct {
	stubBatchParser
	verifyOK  bool
	sigOK     bool
	challenge string
}

func (s stubWhatsAppWebhook) VerifyWebhook(_, _, challenge string) (string, bool) {
	if !s.verifyOK {
		return "", false
	}
	if s.challenge != "" {
		return s.challenge, true
	}
	return challenge, true
}

func (s stubWhatsAppWebhook) ValidateWebhookSignature(_ []byte, _ string) bool {
	return s.sigOK
}

func TestWebhookTelegram_UsesAdapterNormalizedMessage(t *testing.T) {
	runner := &webhookStubRunner{}
	parser := stubBatchParser{
		msgs: []channel.InboundMessage{{
			Platform: "telegram",
			SenderID: "99",
			ChatID:   "111",
			Content:  "hello",
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/telegram", strings.NewReader(`{"ignored":true}`))
	rec := httptest.NewRecorder()

	WebhookTelegram(runner, parser).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("inputs = %d, want 1", len(runner.inputs))
	}
	if got := runner.inputs[0]; got.Platform != "telegram" || got.SenderID != "99" || got.ChatID != "111" || got.Content != "hello" {
		t.Fatalf("unexpected input: %+v", got)
	}
	if runner.inputs[0].Claim == nil || !runner.inputs[0].Claim.SenderInAllowlist {
		t.Fatalf("webhook input missing accepted-channel claim: %+v", runner.inputs[0].Claim)
	}
}

func TestWebhookWhatsApp_UsesBatchParserAndSkipsEmptyContent(t *testing.T) {
	runner := &webhookStubRunner{}
	parser := stubWhatsAppWebhook{
		stubBatchParser: stubBatchParser{
			msgs: []channel.InboundMessage{
				{Platform: "whatsapp", SenderID: "+1", ChatID: "+1", Content: ""},
				{Platform: "whatsapp", SenderID: "+2", ChatID: "+2", Content: "hello"},
			},
		},
		verifyOK: true,
		sigOK:    true,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/whatsapp", strings.NewReader(`{"entry":[]}`))
	req.Header.Set("X-Hub-Signature-256", "sig")
	rec := httptest.NewRecorder()

	WebhookWhatsApp(runner, parser, parser).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("inputs = %d, want 1", len(runner.inputs))
	}
	if got := runner.inputs[0]; got.SenderID != "+2" || got.ChatID != "+2" || got.Content != "hello" {
		t.Fatalf("unexpected input: %+v", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["processed"] != float64(1) {
		t.Fatalf("processed = %v", body["processed"])
	}
}

func TestWebhookWhatsAppVerify_UsesVerifier(t *testing.T) {
	verifier := stubWhatsAppWebhook{
		verifyOK:  true,
		challenge: "abc123",
	}
	req := httptest.NewRequest(http.MethodGet, "/api/webhooks/whatsapp?hub.mode=subscribe&hub.verify_token=t&hub.challenge=abc123", nil)
	rec := httptest.NewRecorder()

	WebhookWhatsAppVerify(verifier).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "abc123" {
		t.Fatalf("challenge = %q", rec.Body.String())
	}
}
