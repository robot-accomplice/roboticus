package llm

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// PaymentHandler handles x402 micropayment negotiation when an LLM provider
// returns HTTP 402. Implementations parse the payment requirements from the
// response body, validate them (including safety rails), sign the payment,
// and return the value for the X-Payment header.
type PaymentHandler interface {
	HandlePayment(body []byte) (paymentHeader string, err error)
}

// Client is a single-provider HTTP client that speaks the provider's native
// format. It implements Completer.
type Client struct {
	provider       *Provider
	httpClient     core.HTTPDoer
	apiKey         string
	paymentHandler PaymentHandler
}

// NewClient creates a Client for the given provider. It resolves the API key
// from the environment at construction time, returning an error if the key
// is required but missing.
// KeyResolver resolves API keys for providers. When set, the LLM client
// checks the resolver before falling back to environment variables.
var KeyResolver func(providerName string) string

func NewClient(p *Provider) (*Client, error) {
	var apiKey string
	if !p.IsLocal {
		// Priority: KeyResolver (keystore) → env var → error.
		if KeyResolver != nil {
			apiKey = KeyResolver(p.Name)
		}
		if apiKey == "" && p.APIKeyEnv != "" {
			apiKey = os.Getenv(p.APIKeyEnv)
		}
		// Non-local providers without a key will fail at request time,
		// not at construction — this allows the service to start even
		// when some providers are unconfigured.
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

// SetPaymentHandler configures an x402 micropayment handler. When set, HTTP 402
// responses from providers trigger automatic payment negotiation and retry.
func (c *Client) SetPaymentHandler(h PaymentHandler) {
	c.paymentHandler = h
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

	// x402 micropayment: if provider returns 402 and we have a handler, attempt
	// to pay and retry exactly once.
	if resp.StatusCode == http.StatusPaymentRequired && c.paymentHandler != nil {
		paymentHeader, payErr := c.handle402(resp)
		if payErr != nil {
			return nil, payErr
		}

		// Retry the same request with the X-Payment header.
		retryReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, core.WrapError(core.ErrNetwork, "failed to create payment retry request", err)
		}
		c.setHeaders(retryReq)
		retryReq.Header.Set("X-Payment", paymentHeader)

		resp, err = c.httpClient.Do(retryReq)
		if err != nil {
			return nil, core.WrapError(core.ErrNetwork, "payment retry request failed", err)
		}
		defer func() { _ = resp.Body.Close() }()
	}

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

		url := c.chatURL()
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
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

		// x402 micropayment: if provider returns 402 and we have a handler,
		// attempt to pay and retry exactly once.
		if resp.StatusCode == http.StatusPaymentRequired && c.paymentHandler != nil {
			paymentHeader, payErr := c.handle402(resp)
			if payErr != nil {
				errs <- payErr
				return
			}

			retryReq, retryErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if retryErr != nil {
				errs <- core.WrapError(core.ErrNetwork, "failed to create payment retry request", retryErr)
				return
			}
			c.setHeaders(retryReq)
			retryReq.Header.Set("X-Payment", paymentHeader)

			resp, err = c.httpClient.Do(retryReq)
			if err != nil {
				errs <- core.WrapError(core.ErrNetwork, "payment retry stream failed", err)
				return
			}
			defer func() { _ = resp.Body.Close() }()
		}

		if resp.StatusCode != http.StatusOK {
			errs <- c.parseErrorResponse(resp)
			return
		}

		c.readSSE(ctx, resp.Body, chunks, errs)
	}()

	return chunks, errs
}

// handle402 reads the 402 response body and delegates to the payment handler.
// It returns the X-Payment header value on success.
func (c *Client) handle402(resp *http.Response) (string, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", core.WrapError(core.ErrNetwork, "failed to read 402 body", err)
	}

	log.Info().Str("provider", c.provider.Name).Msg("received 402, attempting x402 payment")

	paymentHeader, err := c.paymentHandler.HandlePayment(respBody)
	if err != nil {
		return "", core.WrapError(core.ErrWallet, "x402 payment failed", err)
	}
	return paymentHeader, nil
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
// When AuthMode is "query", the API key is appended as a query parameter.
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
		case FormatOpenAIResponses:
			path = "/v1/responses"
		default:
			path = "/v1/chat/completions"
		}
	}
	url := strings.TrimRight(c.provider.URL, "/") + path

	// Query-parameter authentication: append API key to URL.
	if c.provider.AuthMode == "query" && c.apiKey != "" {
		if strings.Contains(url, "?") {
			url += "&api_key=" + c.apiKey
		} else {
			url += "?api_key=" + c.apiKey
		}
	}

	return url
}

// setHeaders adds authentication and content-type headers.
// When AuthMode is "query", authentication is handled in chatURL instead.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")

	if c.apiKey != "" && c.provider.AuthMode != "query" {
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
