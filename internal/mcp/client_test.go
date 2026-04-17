package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockTransport is a test double for the Transport interface.
type mockTransport struct {
	mu        sync.Mutex
	sent      []json.RawMessage
	responses []json.RawMessage
	sendErr   error
	recvErr   error
	closeErr  error
	closed    bool
}

type queuedTransport struct {
	mu      sync.Mutex
	sent    []json.RawMessage
	recv    chan json.RawMessage
	closeCh chan struct{}
	closed  bool
}

func newQueuedTransport() *queuedTransport {
	return &queuedTransport{
		recv:    make(chan json.RawMessage, 16),
		closeCh: make(chan struct{}),
	}
}

func (q *queuedTransport) Send(_ context.Context, msg json.RawMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sent = append(q.sent, msg)
	return nil
}

func (q *queuedTransport) Receive(_ context.Context) (json.RawMessage, error) {
	select {
	case msg := <-q.recv:
		return msg, nil
	case <-q.closeCh:
		return nil, fmt.Errorf("closed")
	}
}

func (q *queuedTransport) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		close(q.closeCh)
		q.closed = true
	}
	return nil
}

func (q *queuedTransport) sentID(t *testing.T, index int) int64 {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.sent) <= index {
		t.Fatalf("sent %d messages, want index %d", len(q.sent), index)
	}
	var req jsonRPCRequest
	if err := json.Unmarshal(q.sent[index], &req); err != nil {
		t.Fatalf("unmarshal sent request: %v", err)
	}
	return req.ID
}

func (m *mockTransport) Send(_ context.Context, msg json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockTransport) Receive(_ context.Context) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("no more responses")
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

func (m *mockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeErr
}

// helper to build a JSON-RPC success response with an arbitrary ID.
func makeResponse(id int64, result any) json.RawMessage {
	resultBytes, _ := json.Marshal(result)
	resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, id, string(resultBytes))
	return json.RawMessage(resp)
}

// helper to build a JSON-RPC error response.
func makeErrorResponse(id int64, code int, message string) json.RawMessage {
	resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":%d,"message":%q}}`, id, code, message)
	return json.RawMessage(resp)
}

func TestJsonRPCRequest_Marshal(t *testing.T) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]any{"protocolVersion": "2024-11-05"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", parsed["jsonrpc"])
	}
	if parsed["method"] != "initialize" {
		t.Errorf("method = %v", parsed["method"])
	}
}

func TestJsonRPCResponse_Unmarshal(t *testing.T) {
	data := `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"test","version":"0.1.0"}}}`
	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("id = %d", resp.ID)
	}
	if resp.Error != nil {
		t.Error("should not have error")
	}
	if resp.Result == nil {
		t.Error("result should not be nil")
	}
}

func TestJsonRPCResponse_Error(t *testing.T) {
	data := `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`
	var resp jsonRPCResponse
	_ = json.Unmarshal([]byte(data), &resp)
	if resp.Error == nil {
		t.Fatal("should have error")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("code = %d", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid Request" {
		t.Errorf("message = %s", resp.Error.Message)
	}
}

func TestToolDescriptor_JSON(t *testing.T) {
	td := ToolDescriptor{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}
	data, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed ToolDescriptor
	_ = json.Unmarshal(data, &parsed)
	if parsed.Name != "read_file" {
		t.Errorf("name = %s", parsed.Name)
	}
}

func TestServerStatus_JSON(t *testing.T) {
	s := ServerStatus{
		Name:          "test-server",
		Connected:     true,
		ToolCount:     5,
		ServerName:    "test",
		ServerVersion: "1.0",
	}
	data, _ := json.Marshal(s)
	var parsed ServerStatus
	_ = json.Unmarshal(data, &parsed)
	if !parsed.Connected {
		t.Error("should be connected")
	}
	if parsed.ToolCount != 5 {
		t.Errorf("tool_count = %d", parsed.ToolCount)
	}
}

// --- Connection.call tests using mockTransport ---

func TestConnection_Call_Success(t *testing.T) {
	// The atomic nextID is shared across tests; capture the next expected value.
	expectedID := nextID.Load() + 1

	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, map[string]string{"status": "ok"}),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	result, err := conn.call(context.Background(), "test/method", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed["status"] != "ok" {
		t.Errorf("result status = %s", parsed["status"])
	}

	// Verify a message was sent.
	if len(mt.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(mt.sent))
	}

	var sentReq jsonRPCRequest
	if err := json.Unmarshal(mt.sent[0], &sentReq); err != nil {
		t.Fatalf("unmarshal sent: %v", err)
	}
	if sentReq.JSONRPC != "2.0" {
		t.Errorf("sent jsonrpc = %s", sentReq.JSONRPC)
	}
	if sentReq.Method != "test/method" {
		t.Errorf("sent method = %s", sentReq.Method)
	}
}

func TestConnection_Call_SendError(t *testing.T) {
	mt := &mockTransport{
		sendErr: fmt.Errorf("send failed"),
	}
	conn := &Connection{Name: "test", transport: mt}

	_, err := conn.call(context.Background(), "test/method", nil)
	if err == nil {
		t.Fatal("expected error from send failure")
	}
}

func TestConnection_Call_ReceiveError(t *testing.T) {
	mt := &mockTransport{
		recvErr: fmt.Errorf("recv failed"),
	}
	conn := &Connection{Name: "test", transport: mt}

	_, err := conn.call(context.Background(), "test/method", nil)
	if err == nil {
		t.Fatal("expected error from receive failure")
	}
}

func TestConnection_Call_InvalidResponseJSON(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`not valid json`),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	_, err := conn.call(context.Background(), "test/method", nil)
	if err == nil {
		t.Fatal("expected error from invalid JSON response")
	}
}

func TestConnection_Call_RPCError(t *testing.T) {
	expectedID := nextID.Load() + 1
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeErrorResponse(expectedID, -32601, "method not found"),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	_, err := conn.call(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected RPC error")
	}
	if got := err.Error(); got != "mcp rpc error -32601: method not found" {
		t.Errorf("error = %q", got)
	}
}

func TestConnection_Call_ContextTimeoutDoesNotCloseConnection(t *testing.T) {
	qt := newQueuedTransport()
	conn := &Connection{Name: "test", transport: qt}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := conn.call(ctx, "test/method", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("call should return promptly after timeout, took %v", elapsed)
	}
	if qt.closed {
		t.Fatal("per-call timeout should not close transport")
	}
	if conn.receiverErr() != nil {
		t.Fatalf("timeout should not poison the connection, got receiver error %v", conn.receiverErr())
	}
}

func TestConnection_Call_TimeoutDropsLateResponseAndAllowsNextCall(t *testing.T) {
	qt := newQueuedTransport()
	conn := &Connection{Name: "test", transport: qt}

	ctx1, cancel1 := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel1()
	_, err := conn.call(ctx1, "slow/method", nil)
	if err == nil {
		t.Fatal("expected timeout on first call")
	}

	firstID := qt.sentID(t, 0)
	qt.recv <- makeResponse(firstID, map[string]string{"status": "late"})

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	resultCh := make(chan struct {
		result json.RawMessage
		err    error
	}, 1)
	go func() {
		result, err := conn.call(ctx2, "next/method", nil)
		resultCh <- struct {
			result json.RawMessage
			err    error
		}{result: result, err: err}
	}()

	deadline := time.Now().Add(time.Second)
	var secondID int64
	for time.Now().Before(deadline) {
		qt.mu.Lock()
		if len(qt.sent) >= 2 {
			var req jsonRPCRequest
			_ = json.Unmarshal(qt.sent[1], &req)
			secondID = req.ID
			qt.mu.Unlock()
			break
		}
		qt.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if secondID == 0 {
		t.Fatal("second request was not sent")
	}

	qt.recv <- makeResponse(secondID, map[string]string{"status": "ok"})

	res := <-resultCh
	if res.err != nil {
		t.Fatalf("second call failed: %v", res.err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(res.result, &parsed); err != nil {
		t.Fatalf("unmarshal second result: %v", err)
	}
	if parsed["status"] != "ok" {
		t.Fatalf("status = %q, want ok", parsed["status"])
	}
	if qt.closed {
		t.Fatal("late response should not force connection closure")
	}
}

// --- Connection.initialize tests ---

func TestConnection_Initialize_Success(t *testing.T) {
	// initialize calls call() which increments nextID; then sends a notification (another send).
	expectedID := nextID.Load() + 1

	initResult := map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]string{
			"name":    "mock-server",
			"version": "1.2.3",
		},
	}
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, initResult),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.initialize(context.Background())
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	if conn.ServerName != "mock-server" {
		t.Errorf("ServerName = %q", conn.ServerName)
	}
	if conn.ServerVersion != "1.2.3" {
		t.Errorf("ServerVersion = %q", conn.ServerVersion)
	}

	// Should have sent 2 messages: the initialize request + the notifications/initialized notification.
	if len(mt.sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(mt.sent))
	}
}

func TestConnection_Initialize_ErrorResponse(t *testing.T) {
	expectedID := nextID.Load() + 1
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeErrorResponse(expectedID, -32600, "bad request"),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.initialize(context.Background())
	if err == nil {
		t.Fatal("expected initialize error")
	}
}

func TestConnection_Initialize_MalformedServerInfo(t *testing.T) {
	// If serverInfo is missing or malformed, initialize should still succeed
	// (it ignores unmarshal errors for the info struct).
	expectedID := nextID.Load() + 1

	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, map[string]string{"protocolVersion": "2024-11-05"}),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.initialize(context.Background())
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	// ServerName/Version should remain zero-valued since serverInfo wasn't present.
	if conn.ServerName != "" {
		t.Errorf("ServerName = %q, want empty", conn.ServerName)
	}
}

// --- Connection.listTools tests ---

func TestConnection_ListTools_Success(t *testing.T) {
	expectedID := nextID.Load() + 1

	toolsResult := map[string]any{
		"tools": []map[string]any{
			{
				"name":        "read_file",
				"description": "Read a file",
				"inputSchema": map[string]any{"type": "object"},
			},
			{
				"name":        "write_file",
				"description": "Write a file",
				"inputSchema": map[string]any{"type": "object"},
			},
		},
	}
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, toolsResult),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}

	if len(conn.Tools) != 2 {
		t.Fatalf("tool count = %d, want 2", len(conn.Tools))
	}
	if conn.Tools[0].Name != "read_file" {
		t.Errorf("tool[0].Name = %s", conn.Tools[0].Name)
	}
	if conn.Tools[1].Name != "write_file" {
		t.Errorf("tool[1].Name = %s", conn.Tools[1].Name)
	}
}

func TestConnection_ListTools_Empty(t *testing.T) {
	expectedID := nextID.Load() + 1

	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, map[string]any{"tools": []any{}}),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}
	if len(conn.Tools) != 0 {
		t.Errorf("tool count = %d, want 0", len(conn.Tools))
	}
}

func TestConnection_ListTools_Error(t *testing.T) {
	expectedID := nextID.Load() + 1
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeErrorResponse(expectedID, -32000, "internal error"),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.listTools(context.Background())
	if err == nil {
		t.Fatal("expected listTools error")
	}
}

func TestConnection_ListTools_BadJSON(t *testing.T) {
	expectedID := nextID.Load() + 1
	// Return a result that is valid JSON-RPC but unparseable tools list.
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, "not an object"),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.listTools(context.Background())
	if err == nil {
		t.Fatal("expected unmarshal error from bad tools result")
	}
}

// --- Connection.CallTool tests ---

func TestConnection_CallTool_Success(t *testing.T) {
	expectedID := nextID.Load() + 1

	toolResult := map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": "file contents here"},
		},
		"isError": false,
	}
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, toolResult),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	result, err := conn.CallTool(context.Background(), "read_file", json.RawMessage(`{"path":"test.txt"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.Content != "file contents here" {
		t.Errorf("content = %q", result.Content)
	}
	if result.IsError {
		t.Error("should not be error")
	}

	// Verify the sent request includes the tool name and arguments.
	if len(mt.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(mt.sent))
	}
	var sentReq map[string]any
	_ = json.Unmarshal(mt.sent[0], &sentReq)
	params := sentReq["params"].(map[string]any)
	if params["name"] != "read_file" {
		t.Errorf("sent tool name = %v", params["name"])
	}
}

func TestConnection_CallTool_IsError(t *testing.T) {
	expectedID := nextID.Load() + 1

	toolResult := map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": "tool error: file not found"},
		},
		"isError": true,
	}
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, toolResult),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	result, err := conn.CallTool(context.Background(), "read_file", json.RawMessage(`{"path":"missing.txt"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("should be error")
	}
	if result.Content != "tool error: file not found" {
		t.Errorf("content = %q", result.Content)
	}
}

func TestConnection_CallTool_RPCError(t *testing.T) {
	expectedID := nextID.Load() + 1
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeErrorResponse(expectedID, -32000, "server error"),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	_, err := conn.CallTool(context.Background(), "bad_tool", nil)
	if err == nil {
		t.Fatal("expected error from RPC")
	}
}

func TestConnection_CallTool_NonStandardResult(t *testing.T) {
	// When the result doesn't match the expected content structure,
	// CallTool returns the raw result as content string.
	expectedID := nextID.Load() + 1
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, "plain string result"),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	result, err := conn.CallTool(context.Background(), "custom_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// Should fall back to raw result string.
	if result.Content == "" {
		t.Error("expected non-empty content from fallback")
	}
}

func TestConnection_CallTool_MultipleContentBlocks(t *testing.T) {
	expectedID := nextID.Load() + 1

	toolResult := map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": "part1"},
			{"type": "image", "text": "ignored"},
			{"type": "text", "text": "part2"},
		},
		"isError": false,
	}
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(expectedID, toolResult),
		},
	}
	conn := &Connection{Name: "test", transport: mt}

	result, err := conn.CallTool(context.Background(), "multi_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.Content != "part1part2" {
		t.Errorf("content = %q, want 'part1part2'", result.Content)
	}
}

// --- Connection.Close tests ---

func TestConnection_Close_WithTransport(t *testing.T) {
	mt := &mockTransport{}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !mt.closed {
		t.Error("transport should be closed")
	}
}

func TestConnection_Close_NilTransport(t *testing.T) {
	conn := &Connection{Name: "test"}
	err := conn.Close()
	if err != nil {
		t.Fatalf("Close with nil transport: %v", err)
	}
}

func TestConnection_Close_TransportError(t *testing.T) {
	mt := &mockTransport{closeErr: fmt.Errorf("close error")}
	conn := &Connection{Name: "test", transport: mt}

	err := conn.Close()
	if err == nil {
		t.Fatal("expected close error")
	}
}

// --- Atomic ID counter tests ---

func TestNextID_Increments(t *testing.T) {
	before := nextID.Load()
	mt := &mockTransport{
		responses: []json.RawMessage{
			makeResponse(before+1, "ok"),
		},
	}
	conn := &Connection{Name: "test", transport: mt}
	_, _ = conn.call(context.Background(), "test", nil)

	after := nextID.Load()
	if after != before+1 {
		t.Errorf("nextID went from %d to %d, expected increment by 1", before, after)
	}
}
