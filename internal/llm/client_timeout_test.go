package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type deadlineCaptureDoer struct {
	t        *testing.T
	min      time.Duration
	max      time.Duration
	requests int
}

func (d *deadlineCaptureDoer) Do(req *http.Request) (*http.Response, error) {
	d.requests++
	deadline, ok := req.Context().Deadline()
	if !ok {
		d.t.Fatal("expected per-call request deadline")
	}
	remaining := time.Until(deadline)
	if remaining < d.min || remaining > d.max {
		d.t.Fatalf("deadline remaining = %v, want between %v and %v", remaining, d.min, d.max)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"ok","model":"m","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		)),
		Header: make(http.Header),
	}, nil
}

func TestClientComplete_UsesRequestModelCallTimeoutPerCall(t *testing.T) {
	doer := &deadlineCaptureDoer{
		t:   t,
		min: 230 * time.Second,
		max: 240 * time.Second,
	}
	client, err := NewClientWithHTTP(&Provider{
		Name:        "moonshot",
		URL:         "https://api.moonshot.ai/v1",
		Format:      FormatOpenAI,
		TimeoutSecs: 120,
	}, doer)
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	resp, err := client.Complete(context.Background(), &Request{
		Model:            "kimi-k2.6",
		Messages:         []Message{{Role: "user", Content: "hello"}},
		ModelCallTimeout: 240 * time.Second,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if doer.requests != 1 {
		t.Fatalf("requests = %d, want 1", doer.requests)
	}
}
