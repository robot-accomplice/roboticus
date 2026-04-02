package llm

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/core"
)

// Client is a single-provider HTTP client that speaks the provider's native
// format. It implements Completer.
type Client struct {
	provider   *Provider
	httpClient core.HTTPDoer
	apiKey     string
}

// NewClient creates a Client for the given provider. It resolves the API key
// from the environment at construction time, returning an error if the key
// is required but missing.
func NewClient(p *Provider) (*Client, error) {
	var apiKey string
	if p.APIKeyEnv != "" && !p.IsLocal {
		apiKey = os.Getenv(p.APIKeyEnv)
		if apiKey == "" {
			return nil, core.NewError(core.ErrConfig,
				fmt.Sprintf("env var %s not set for provider %s", p.APIKeyEnv, p.Name))
		}
	}

	return &Client{
		provider: p,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		apiKey: apiKey,
	}, nil
}

// NewClientWithHTTP creates a Client with an injected HTTP implementation.
// Use this in tests to provide a mock HTTPDoer.
func NewClientWithHTTP(p *Provider, httpClient core.HTTPDoer) (*Client, error) {
	var apiKey string
	if p.APIKeyEnv != "" && !p.IsLocal {
		apiKey = os.Getenv(p.APIKeyEnv)
	}
	return &Client{
		provider:   p,
		httpClient: httpClient,
		apiKey:     apiKey,
	}, nil
}

// Complete sends a non-streaming request and returns the full response.
func (c *Client) Complete(ctx context.Context, req *Request) (*Response, error) {
	req.Stream = false
	body, err := c.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.chatURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, core.WrapError(core.ErrNetwork, "failed to create request", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, core.WrapError(core.ErrNetwork, "request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	return c.unmarshalResponse(resp.Body)
}

// Stream sends a streaming request and returns channels for chunks and errors.
func (c *Client) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		req.Stream = true
		body, err := c.marshalRequest(req)
		if err != nil {
			errs <- err
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL(), bytes.NewReader(body))
		if err != nil {
			errs <- core.WrapError(core.ErrNetwork, "failed to create request", err)
			return
		}
		c.setHeaders(httpReq)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			errs <- core.WrapError(core.ErrNetwork, "stream request failed", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			errs <- c.parseErrorResponse(resp)
			return
		}

		c.readSSE(ctx, resp.Body, chunks, errs)
	}()

	return chunks, errs
}

// readSSE parses an SSE stream into StreamChunks.
func (c *Client) readSSE(ctx context.Context, body io.Reader, chunks chan<- StreamChunk, errs chan<- error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		chunk, err := c.unmarshalStreamChunk([]byte(data))
		if err != nil {
			log.Warn().Err(err).Str("provider", c.provider.Name).Msg("failed to parse SSE chunk")
			continue
		}

		select {
		case chunks <- *chunk:
		case <-ctx.Done():
			return
		}
	}

	if err := scanner.Err(); err != nil {
		errs <- core.WrapError(core.ErrNetwork, "SSE stream read error", err)
	}
}

// chatURL returns the full URL for the chat completions endpoint.
func (c *Client) chatURL() string {
	path := c.provider.ChatPath
	if path == "" {
		switch c.provider.Format {
		case FormatAnthropic:
			path = "/v1/messages"
		case FormatGoogle:
			path = "/v1/models/"
		case FormatOllama:
			path = "/api/chat"
		default:
			path = "/v1/chat/completions"
		}
	}
	return strings.TrimRight(c.provider.URL, "/") + path
}

// setHeaders adds authentication and content-type headers.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")

	if c.apiKey != "" {
		authHeader := c.provider.AuthHeader
		if authHeader == "" {
			switch c.provider.Format {
			case FormatAnthropic:
				authHeader = "x-api-key"
			default:
				authHeader = "Authorization"
			}
		}
		if authHeader == "Authorization" {
			req.Header.Set(authHeader, "Bearer "+c.apiKey)
		} else {
			req.Header.Set(authHeader, c.apiKey)
		}
	}

	if c.provider.Format == FormatAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	for k, v := range c.provider.ExtraHeaders {
		req.Header.Set(k, v)
	}
}
