package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testGateway() *Gateway {
	tools := func() []GatewayToolDescriptor {
		return []GatewayToolDescriptor{
			{
				Name:        "echo",
				Description: "Echoes the input",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
			},
		}
	}

	callTool := func(_ context.Context, name string, input json.RawMessage) (*ToolCallResult, error) {
		if name == "echo" {
			var params struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return nil, err
			}
			return &ToolCallResult{Content: params.Text, IsError: false}, nil
		}
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	return NewGateway(tools, callTool)
}

func postJSONRPC(t *testing.T, gw *Gateway, method string, params any, id int) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	return w
}

func TestGateway_Initialize(t *testing.T) {
	gw := testGateway()
	w := postJSONRPC(t, gw, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test-client", "version": "0.1"},
	}, 1)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp gatewayResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result map[string]any
	_ = json.Unmarshal(resultBytes, &result)

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("missing serverInfo")
	}
	if serverInfo["name"] != "goboticus" {
		t.Errorf("serverInfo.name = %v", serverInfo["name"])
	}
}

func TestGateway_ToolsList(t *testing.T) {
	gw := testGateway()
	w := postJSONRPC(t, gw, "tools/list", nil, 2)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp gatewayResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(result.Tools))
	}
	if result.Tools[0].Name != "echo" {
		t.Errorf("tool name = %s", result.Tools[0].Name)
	}
}

func TestGateway_ToolsCall(t *testing.T) {
	gw := testGateway()
	w := postJSONRPC(t, gw, "tools/call", map[string]any{
		"name":      "echo",
		"arguments": map[string]string{"text": "hello"},
	}, 3)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp gatewayResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Errorf("unexpected content: %+v", result.Content)
	}
	if result.IsError {
		t.Error("should not be error")
	}
}

func TestGateway_ToolsCall_UnknownTool(t *testing.T) {
	gw := testGateway()
	w := postJSONRPC(t, gw, "tools/call", map[string]any{
		"name":      "nonexistent",
		"arguments": map[string]string{},
	}, 4)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp gatewayResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestGateway_ToolsCall_MissingName(t *testing.T) {
	gw := testGateway()
	w := postJSONRPC(t, gw, "tools/call", map[string]any{
		"arguments": map[string]string{},
	}, 5)

	var resp gatewayResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for missing tool name")
	}
}

func TestGateway_NotificationInitialized(t *testing.T) {
	gw := testGateway()
	body := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestGateway_UnknownMethod(t *testing.T) {
	gw := testGateway()
	w := postJSONRPC(t, gw, "completions/complete", nil, 6)

	var resp gatewayResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestGateway_InvalidJSON(t *testing.T) {
	gw := testGateway()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, req)

	var resp gatewayResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestGateway_Delete(t *testing.T) {
	gw := testGateway()
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGateway_MethodNotAllowed(t *testing.T) {
	gw := testGateway()
	req := httptest.NewRequest(http.MethodPut, "/mcp", nil)
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestGateway_SSE(t *testing.T) {
	gw := testGateway()
	srv := httptest.NewServer(gw)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("content-type = %s", ct)
	}

	// Cancel quickly; we just verify the SSE connection is established.
	cancel()
	_, _ = io.ReadAll(resp.Body)
}

func TestGateway_ToolsListEmpty(t *testing.T) {
	gw := NewGateway(func() []GatewayToolDescriptor { return nil }, nil)
	w := postJSONRPC(t, gw, "tools/list", nil, 10)

	var resp gatewayResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	resultBytes, _ := json.Marshal(resp.Result)
	var result struct {
		Tools []any `json:"tools"`
	}
	_ = json.Unmarshal(resultBytes, &result)
	if len(result.Tools) != 0 {
		t.Errorf("expected empty tools list, got %d", len(result.Tools))
	}
}

func TestGateway_ConcurrentRequests(t *testing.T) {
	gw := testGateway()
	srv := httptest.NewServer(gw)
	defer srv.Close()

	const n = 10
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		go func(id int) {
			body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list"}`, id)
			resp, err := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(body))
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("status %d for id %d", resp.StatusCode, id)
				return
			}
			errs <- nil
		}(i)
	}

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
}
