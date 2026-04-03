package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// mockPaymentHandler is a test double for PaymentHandler.
type mockPaymentHandler struct {
	header string
	err    error
	calls  atomic.Int32
}

func (m *mockPaymentHandler) HandlePayment(_ []byte) (string, error) {
	m.calls.Add(1)
	return m.header, m.err
}

// mockHTTPSequence returns a sequence of responses, one per call.
type mockHTTPSequence struct {
	responses []mockHTTP
	idx       atomic.Int32
}

func (m *mockHTTPSequence) Do(_ *http.Request) (*http.Response, error) {
	i := int(m.idx.Add(1) - 1)
	if i >= len(m.responses) {
		return nil, fmt.Errorf("no more mock responses (called %d times)", i+1)
	}
	r := m.responses[i]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.statusCode,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

// headerCapturingHTTP wraps a mock sequence and captures headers from each request.
type headerCapturingHTTP struct {
	inner   *mockHTTPSequence
	headers []http.Header
}

func (h *headerCapturingHTTP) Do(req *http.Request) (*http.Response, error) {
	h.headers = append(h.headers, req.Header.Clone())
	return h.inner.Do(req)
}

func TestClient_Complete_402PaymentRetry(t *testing.T) {
	seq := &mockHTTPSequence{
		responses: []mockHTTP{
			{statusCode: 402, body: `{"amount":0.50,"recipient":"0xabc","chain_id":8453}`},
			{statusCode: 200, body: `{"id":"cmpl-1","model":"gpt-4","choices":[{"message":{"role":"assistant","content":"paid response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`},
		},
	}
	capture := &headerCapturingHTTP{inner: seq}

	handler := &mockPaymentHandler{header: "x402 amount=0.500000 recipient=0xabc auth=deadbeef"}

	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, capture)
	client.SetPaymentHandler(handler)

	resp, err := client.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected success after payment retry, got: %v", err)
	}
	if resp.Content != "paid response" {
		t.Errorf("content = %q, want %q", resp.Content, "paid response")
	}
	if handler.calls.Load() != 1 {
		t.Errorf("payment handler called %d times, want 1", handler.calls.Load())
	}

	// Verify the retry request had the X-Payment header.
	if len(capture.headers) < 2 {
		t.Fatalf("expected 2 requests, got %d", len(capture.headers))
	}
	xPayment := capture.headers[1].Get("X-Payment")
	if xPayment == "" {
		t.Error("retry request missing X-Payment header")
	}

	// Verify the first request did NOT have X-Payment.
	if capture.headers[0].Get("X-Payment") != "" {
		t.Error("initial request should not have X-Payment header")
	}
}

func TestClient_Complete_402NoHandler(t *testing.T) {
	mock := &mockHTTP{
		statusCode: 402,
		body:       `{"amount":0.50,"recipient":"0xabc","chain_id":8453}`,
	}
	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, mock)
	// No payment handler set.

	_, err := client.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 402 with no handler")
	}
	// Should fall through to parseErrorResponse which returns ErrCreditExhausted.
	if !strings.Contains(err.Error(), "credit") {
		t.Errorf("error = %q, want credit-related error", err)
	}
}

func TestClient_Complete_402PaymentFails(t *testing.T) {
	mock := &mockHTTP{
		statusCode: 402,
		body:       `{"amount":0.50,"recipient":"0xabc","chain_id":8453}`,
	}
	handler := &mockPaymentHandler{err: fmt.Errorf("insufficient balance")}

	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, mock)
	client.SetPaymentHandler(handler)

	_, err := client.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when payment fails")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Errorf("error = %q, want 'insufficient balance'", err)
	}
}

func TestClient_Complete_402OnlyOneRetry(t *testing.T) {
	// Both responses are 402 -- should only retry once, then return error.
	seq := &mockHTTPSequence{
		responses: []mockHTTP{
			{statusCode: 402, body: `{"amount":0.50,"recipient":"0xabc","chain_id":8453}`},
			{statusCode: 402, body: `{"amount":0.50,"recipient":"0xabc","chain_id":8453}`},
		},
	}
	handler := &mockPaymentHandler{header: "x402 amount=0.500000 recipient=0xabc auth=deadbeef"}

	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, seq)
	client.SetPaymentHandler(handler)

	_, err := client.Complete(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when retry also returns 402")
	}
	// Payment handler should only be called once (for the first 402).
	if handler.calls.Load() != 1 {
		t.Errorf("payment handler called %d times, want exactly 1", handler.calls.Load())
	}
}

func TestClient_Stream_402PaymentRetry(t *testing.T) {
	seq := &mockHTTPSequence{
		responses: []mockHTTP{
			{statusCode: 402, body: `{"amount":0.25,"recipient":"0xdef","chain_id":8453}`},
			{statusCode: 200, body: "data: {\"choices\":[{\"delta\":{\"content\":\"streamed\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"},
		},
	}
	handler := &mockPaymentHandler{header: "x402 amount=0.250000 recipient=0xdef auth=cafe"}

	client, _ := NewClientWithHTTP(&Provider{
		Name: "test", URL: "http://mock.test", Format: FormatOpenAI,
	}, seq)
	client.SetPaymentHandler(handler)

	chunks, errs := client.Stream(context.Background(), &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	var content string
	for chunk := range chunks {
		content += chunk.Delta
	}
	for err := range errs {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if content != "streamed" {
		t.Errorf("streamed content = %q, want %q", content, "streamed")
	}
	if handler.calls.Load() != 1 {
		t.Errorf("payment handler called %d times, want 1", handler.calls.Load())
	}
}
