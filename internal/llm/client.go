package llm

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

type preparedRequest struct {
	body                 []byte
	messageNormalization ProviderMessageNormalizationResult
}

// MaxAutoPayUSDC is the safety rail for x402 micropayments. Any single payment
// request exceeding this amount (in USDC) is rejected without user confirmation.
const MaxAutoPayUSDC = 1.0

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
	defaultTimeout time.Duration
	apiKey         string
	paymentHandler PaymentHandler
}

// NewClient creates a Client for the given provider. It resolves the API key
// from the environment at construction time, returning an error if the key
// is required but missing.
// KeyResolver resolves API keys from the keystore by key name.
// This is the ONLY source for API keys — no environment variable fallback.
var KeyResolver func(keystoreKey string) string

func NewClient(p *Provider) (*Client, error) {
	var apiKey string
	if !p.IsLocal {
		// Keys come from the keystore only. No env var fallback.
		// Priority:
		// 1. Explicit api_key_ref (keystore reference or "keystore:name")
		// 2. Conventional keystore name: {provider}_api_key
		if KeyResolver != nil {
			if p.APIKeyRef != "" {
				ref := strings.TrimPrefix(p.APIKeyRef, "keystore:")
				apiKey = KeyResolver(ref)
			}
			if apiKey == "" {
				apiKey = KeyResolver(p.Name + "_api_key")
			}
			if apiKey == "" {
				apiKey = KeyResolver("provider_key:" + p.Name)
			}
		}
		// Non-local providers without a key will fail at request time,
		// not at construction — this allows the service to start even
		// when some providers are unconfigured.
	}

	providerTimeout := defaultProviderTimeout(p)

	return &Client{
		provider: p,
		httpClient: &http.Client{
			// Per-call deadlines are carried by the request context. A fixed
			// http.Client timeout would silently override longer baseline
			// model-call budgets.
			Timeout: 0,
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		defaultTimeout: providerTimeout,
		apiKey:         apiKey,
	}, nil
}

// NewClientWithHTTP creates a Client with an injected HTTP implementation.
// Use this in tests to provide a mock HTTPDoer.
func NewClientWithHTTP(p *Provider, httpClient core.HTTPDoer) (*Client, error) {
	var apiKey string
	// Keys from keystore only — no env var lookup.
	if KeyResolver != nil && !p.IsLocal {
		apiKey = KeyResolver(p.Name + "_api_key")
		if apiKey == "" {
			apiKey = KeyResolver("provider_key:" + p.Name)
		}
	}
	return &Client{
		provider:       p,
		httpClient:     httpClient,
		defaultTimeout: defaultProviderTimeout(p),
		apiKey:         apiKey,
	}, nil
}

// SetPaymentHandler configures an x402 micropayment handler. When set, HTTP 402
// responses from providers trigger automatic payment negotiation and retry.
func (c *Client) SetPaymentHandler(h PaymentHandler) {
	c.paymentHandler = h
}

// Complete sends a non-streaming request and returns the full response.
func (c *Client) Complete(ctx context.Context, req *Request) (*Response, error) {
	prepared, err := c.prepareRequest(req, false)
	if err != nil {
		return nil, err
	}
	resp, _, err := c.completePrepared(ctx, prepared, req.ModelCallTimeout)
	return resp, err
}

func defaultProviderTimeout(p *Provider) time.Duration {
	if p == nil {
		return 120 * time.Second
	}
	if p.TimeoutSecs > 0 {
		return time.Duration(p.TimeoutSecs) * time.Second
	}
	if p.IsLocal {
		return 300 * time.Second
	}
	return 120 * time.Second
}

func (c *Client) prepareRequest(req *Request, stream bool) (*preparedRequest, error) {
	req.Stream = stream
	var (
		body []byte
		err  error
		meta ProviderMessageNormalizationResult
	)
	switch c.provider.Format {
	case FormatOpenAIResponses:
		body, err = c.marshalOpenAIResponses(req)
	case FormatAnthropic:
		body, err = c.marshalAnthropic(req)
	case FormatGoogle:
		body, err = c.marshalGoogle(req)
	default:
		meta = NewToolMessageNormalizationFactory().NormalizeProviderMessages(ProviderMessageNormalizationInput{
			Format:   c.provider.Format,
			Messages: req.Messages,
		})
		if meta.Disposition == ToolMessageTransformFailed || meta.Disposition == ToolMessageNoQualifiedTransformer {
			return nil, core.NewError(core.ErrLLM, fmt.Sprintf("provider message normalization failed: %s", meta.Reason))
		}
		body, err = c.marshalOpenAICompatibleWithMessages(req, meta.Messages)
	}
	if err != nil {
		return nil, err
	}
	return &preparedRequest{body: body, messageNormalization: meta}, nil
}

func (c *Client) completePrepared(ctx context.Context, prepared *preparedRequest, timeout time.Duration) (*Response, []byte, error) {
	if timeout <= 0 {
		timeout = c.defaultTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	url := c.chatURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(prepared.body))
	if err != nil {
		return nil, nil, core.WrapError(core.ErrNetwork, "failed to create request", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, core.WrapError(core.ErrNetwork, "request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// x402 micropayment: if provider returns 402 and we have a handler, attempt
	// to pay and retry exactly once.
	if resp.StatusCode == http.StatusPaymentRequired && c.paymentHandler != nil {
		paymentHeader, payErr := c.handle402(resp)
		if payErr != nil {
			return nil, nil, payErr
		}

		// Retry the same request with the X-Payment header.
		retryReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(prepared.body))
		if err != nil {
			return nil, nil, core.WrapError(core.ErrNetwork, "failed to create payment retry request", err)
		}
		c.setHeaders(retryReq)
		retryReq.Header.Set("X-Payment", paymentHeader)

		resp, err = c.httpClient.Do(retryReq)
		if err != nil {
			return nil, nil, core.WrapError(core.ErrNetwork, "payment retry request failed", err)
		}
		defer func() { _ = resp.Body.Close() }()
	}

	rawBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, nil, core.WrapError(core.ErrNetwork, "failed to read response body", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, rawBody, c.parseErrorResponseBody(resp.StatusCode, rawBody)
	}

	parsed, err := c.unmarshalResponse(bytes.NewReader(rawBody))
	return parsed, rawBody, err
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

// Stream sends a streaming request and returns channels for chunks and errors.
func (c *Client) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		prepared, err := c.prepareRequest(req, true)
		if err != nil {
			errs <- err
			return
		}

		url := c.chatURL()
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(prepared.body))
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

		if resp.StatusCode == http.StatusPaymentRequired && c.paymentHandler != nil {
			paymentHeader, payErr := c.handle402(resp)
			if payErr != nil {
				errs <- payErr
				return
			}

			retryReq, retryErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(prepared.body))
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
		case FormatOpenAIResponses:
			path = "/v1/responses"
		default:
			// FormatOpenAI, FormatOllama, and all others use OpenAI-compatible endpoint.
			// Rust standardizes on /v1/chat/completions for all providers including Ollama.
			path = "/v1/chat/completions"
		}
	}
	url := strings.TrimRight(c.provider.URL, "/") + path

	// Query-parameter authentication: append API key to URL (RFC 3986 encoded).
	if c.provider.AuthMode == "query" && c.apiKey != "" {
		encoded := pctEncodeQueryValue(c.apiKey)
		if strings.Contains(url, "?") {
			url += "&api_key=" + encoded
		} else {
			url += "?api_key=" + encoded
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

// pctEncodeQueryValue encodes a string for use as a URL query parameter value
// following RFC 3986 unreserved characters. Matches Rust's percent-encode.
func pctEncodeQueryValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isUnreserved returns true for RFC 3986 unreserved characters: A-Z a-z 0-9 - _ . ~
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}
