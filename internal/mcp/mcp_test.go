package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// testServer creates a server that writes to a buffer instead of stdout.
func testServer() (*Server, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	s := &Server{
		name:    "test-server",
		handler: make(map[string]ToolHandler),
		in:      nil, // not used in handleMessage
		out:     buf,
	}
	return s, buf
}

func readMessages(t *testing.T, buf *bytes.Buffer) []Message {
	t.Helper()
	var msgs []Message
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal response: %v\nline: %s", err, line)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func TestInitialize(t *testing.T) {
	s, buf := testServer()

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 response, got %d", len(msgs))
	}

	result, ok := msgs[0].Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion: %v", result["protocolVersion"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("serverInfo not found")
	}
	if serverInfo["name"] != "test-server" {
		t.Errorf("server name: %v", serverInfo["name"])
	}
}

func TestToolsList(t *testing.T) {
	s, buf := testServer()

	s.RegisterTool("test_tool", "A test tool",
		map[string]Property{
			"input": {Type: "string", Description: "the input"},
		},
		[]string{"input"},
		func(args json.RawMessage) *ToolResult {
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: "ok"}},
			}
		})

	req := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 response, got %d", len(msgs))
	}

	result, ok := msgs[0].Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatal("tools is not an array")
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]interface{})
	if tool["name"] != "test_tool" {
		t.Errorf("tool name: %v", tool["name"])
	}
}

func TestToolsCall_Success(t *testing.T) {
	s, buf := testServer()

	s.RegisterTool("greet", "Greet someone",
		map[string]Property{
			"name": {Type: "string", Description: "who to greet"},
		},
		[]string{"name"},
		func(args json.RawMessage) *ToolResult {
			var params struct {
				Name string `json:"name"`
			}
			json.Unmarshal(args, &params)
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: "Hello, " + params.Name + "!"}},
			}
		})

	req := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet","arguments":{"name":"Nico"}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 response, got %d", len(msgs))
	}

	result, ok := msgs[0].Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	content, ok := result["content"].([]interface{})
	if !ok {
		t.Fatal("content is not an array")
	}
	item := content[0].(map[string]interface{})
	if item["text"] != "Hello, Nico!" {
		t.Errorf("unexpected text: %v", item["text"])
	}
}

func TestToolsCall_UnknownTool(t *testing.T) {
	s, buf := testServer()

	req := []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 error response, got %d", len(msgs))
	}

	if msgs[0].Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if msgs[0].Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", msgs[0].Error.Code)
	}
}

func TestToolsCall_BadParams(t *testing.T) {
	s, buf := testServer()

	s.RegisterTool("needs_args", "requires args",
		map[string]Property{
			"required_field": {Type: "string", Description: "needed"},
		},
		[]string{"required_field"},
		func(args json.RawMessage) *ToolResult {
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: "ok"}},
			}
		})

	// Missing "name" in params
	req := []byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"arguments":{}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(msgs))
	}
	if msgs[0].Error == nil {
		t.Fatal("expected error for bad params")
	}
	if msgs[0].Error.Code != -32602 {
		t.Errorf("expected code -32602, got %d", msgs[0].Error.Code)
	}
}

func TestUnknownMethod(t *testing.T) {
	s, buf := testServer()

	req := []byte(`{"jsonrpc":"2.0","id":6,"method":"some/unknown","params":{}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(msgs))
	}
	if msgs[0].Error == nil || msgs[0].Error.Code != -32601 {
		t.Error("expected method not found error")
	}
}

func TestParseError(t *testing.T) {
	s, buf := testServer()

	req := []byte(`not json at all`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(msgs))
	}
	if msgs[0].Error == nil || msgs[0].Error.Code != -32700 {
		t.Error("expected parse error")
	}
}

func TestNotification_Initialized(t *testing.T) {
	s, buf := testServer()

	req := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	s.handleMessage(req)

	// Should be no response for notifications
	if buf.Len() != 0 {
		t.Errorf("expected no response for notification, got: %s", buf.String())
	}
}

func TestRegisterMultipleTools(t *testing.T) {
	s, buf := testServer()

	for i := 0; i < 3; i++ {
		s.RegisterTool("tool_"+string(rune('a'+i)), "desc",
			map[string]Property{},
			nil,
			func(args json.RawMessage) *ToolResult {
				return &ToolResult{Content: []ContentItem{{Type: "text", Text: "ok"}}}
			})
	}

	req := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	result := msgs[0].Result.(map[string]interface{})
	tools := result["tools"].([]interface{})

	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}

func TestSetSecurity_DefaultOff(t *testing.T) {
	s, buf := testServer()

	s.RegisterTool("echo", "echo args",
		map[string]Property{
			"text": {Type: "string", Description: "text to echo"},
		},
		nil,
		func(args json.RawMessage) *ToolResult {
			// Security is off by default — args arrive unmodified
			// Return the text field value directly
			var p struct {
				Text string `json:"text"`
			}
			json.Unmarshal(args, &p)
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: p.Text}},
			}
		})

	// With security off, the raw args reach the handler unchanged
	req := []byte(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hello world"}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	result := msgs[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	item := content[0].(map[string]interface{})
	if item["text"] != "hello world" {
		t.Errorf("expected 'hello world', got %q", item["text"])
	}
}

func TestSetSecurity_SanitizeArgs(t *testing.T) {
	s, buf := testServer()
	s.SetSecurity(true, "") // sanitize on, no API key

	s.RegisterTool("echo", "echo args",
		map[string]Property{
			"text": {Type: "string", Description: "text to echo"},
		},
		nil,
		func(args json.RawMessage) *ToolResult {
			var p struct {
				Text string `json:"text"`
			}
			json.Unmarshal(args, &p)
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: p.Text}},
			}
		})

	// Send args with null byte (via \u0000) — should be stripped by sanitization
	req := []byte(`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hello\u0000world"}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	result := msgs[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	item := content[0].(map[string]interface{})
	// Null byte should be stripped — result is "helloworld"
	if strings.Contains(item["text"].(string), "\x00") {
		t.Error("expected null byte to be stripped when sanitize is on")
	}
	if item["text"] != "helloworld" {
		t.Errorf("expected 'helloworld', got %q", item["text"])
	}
}

func TestSetSecurity_ApiKeyRejected(t *testing.T) {
	s, buf := testServer()
	s.SetSecurity(true, "secret-key-123")

	s.RegisterTool("admin", "admin tool",
		map[string]Property{},
		nil,
		func(args json.RawMessage) *ToolResult {
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: "admin ok"}},
			}
		})

	// Request without bt_api_key
	req := []byte(`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"admin","arguments":{}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if msgs[0].Error == nil {
		t.Fatal("expected auth error for missing API key")
	}
	if msgs[0].Error.Code != -32001 {
		t.Errorf("expected error code -32001, got %d", msgs[0].Error.Code)
	}
}

func TestSetSecurity_ApiKeyAccepted(t *testing.T) {
	s, buf := testServer()
	s.SetSecurity(true, "secret-key-123")

	s.RegisterTool("admin", "admin tool",
		map[string]Property{},
		nil,
		func(args json.RawMessage) *ToolResult {
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: "admin ok"}},
			}
		})

	// Request with correct bt_api_key
	req := []byte(`{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"admin","arguments":{},"bt_api_key":"secret-key-123"}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if msgs[0].Error != nil {
		t.Fatalf("expected success, got error: %s", msgs[0].Error.Message)
	}
	result := msgs[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	item := content[0].(map[string]interface{})
	if item["text"] != "admin ok" {
		t.Errorf("expected 'admin ok', got %q", item["text"])
	}
}

func TestSetSecurity_SanitizeANSI(t *testing.T) {
	s, buf := testServer()
	s.SetSecurity(true, "") // sanitize on, no API key

	s.RegisterTool("echo", "echo",
		map[string]Property{
			"text": {Type: "string", Description: "text"},
		},
		nil,
		func(args json.RawMessage) *ToolResult {
			var p struct {
				Text string `json:"text"`
			}
			json.Unmarshal(args, &p)
			return &ToolResult{
				Content: []ContentItem{{Type: "text", Text: p.Text}},
			}
		})

	// ANSI escape should be stripped (use \u001b for ESC in JSON)
	req := []byte(`{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"echo","arguments":{"text":"normal\u001b[31mred\u001b[0mnormal"}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	result := msgs[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	item := content[0].(map[string]interface{})
	if strings.Contains(item["text"].(string), "\x1b") {
		t.Error("expected ANSI escape sequences to be stripped")
	}
	// Should be "normalrednormal" after ANSI stripping
	if item["text"] != "normalrednormal" {
		t.Errorf("expected 'normalrednormal', got %q", item["text"])
	}
}

func TestSetSecurity_NestedArgs(t *testing.T) {
	s, buf := testServer()
	s.SetSecurity(true, "")

	s.RegisterTool("nested", "nested args",
		map[string]Property{},
		nil,
		func(args json.RawMessage) *ToolResult {
			// Return a specific value to verify sanitization passed the correct data
			var p struct {
				Outer struct {
					Inner string `json:"inner"`
				} `json:"outer"`
				Arr []string `json:"arr"`
			}
			json.Unmarshal(args, &p)
			return &ToolResult{
				Content: []ContentItem{{
					Type: "text",
					Text: fmt.Sprintf("inner=%s arr0=%s arr1=%s", p.Outer.Inner, p.Arr[0], p.Arr[1]),
				}},
			}
		})

	// Nested map with null byte (via \u0000)
	req := []byte(`{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"nested","arguments":{"outer":{"inner":"val\u0000ue"},"arr":["a\u0000b","c"]}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	result := msgs[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	item := content[0].(map[string]interface{})
	text := item["text"].(string)
	// Null bytes stripped: inner="value", arr[0]="ab"
	if strings.Contains(text, "\x00") {
		t.Error("expected null bytes to be stripped from nested values")
	}
	if !strings.Contains(text, "inner=value") {
		t.Errorf("expected sanitized nested value, got %q", text)
	}
	if !strings.Contains(text, "arr0=ab") {
		t.Errorf("expected sanitized array value, got %q", text)
	}
}

func TestRateLimiting(t *testing.T) {
	s, buf := testServer()
	s.SetRateLimit(2, 3) // 2 tokens/sec, burst 3

	s.RegisterTool("ping", "pong", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	// First 3 requests (burst) should succeed
	for i := 1; i <= 3; i++ {
		buf.Reset()
		req := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"ping","arguments":{}}}`, i))
		s.handleMessage(req)
		msgs := readMessages(t, buf)
		if len(msgs) != 1 || msgs[0].Error != nil {
			t.Fatalf("request %d (burst) should succeed, got error: %+v", i, msgs)
		}
	}

	// 4th request exceeds burst, should be rate limited
	buf.Reset()
	req := []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ping","arguments":{}}}`)
	s.handleMessage(req)
	msgs := readMessages(t, buf)
	if msgs[0].Error == nil {
		t.Fatal("4th request should be rate limited")
	}
	if msgs[0].Error.Message != "Rate limit exceeded. Retry later." {
		t.Errorf("unexpected rate limit error: %s", msgs[0].Error.Message)
	}
}

func TestRateLimitingDisabled(t *testing.T) {
	s, buf := testServer()
	// No rate limit set — default should allow all requests

	s.RegisterTool("ping", "pong", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	// 10 requests should all succeed
	for i := 1; i <= 10; i++ {
		buf.Reset()
		req := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"ping","arguments":{}}}`, i))
		s.handleMessage(req)
		msgs := readMessages(t, buf)
		if len(msgs) != 1 || msgs[0].Error != nil {
			t.Fatalf("request %d should succeed without rate limiting, got: %+v", i, msgs)
		}
	}
}

func TestMaxMessageSize_RejectsOversized(t *testing.T) {
	// Use a pipe to feed stdin with oversized data.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	s := &Server{
		name:           "test-server",
		handler:        make(map[string]ToolHandler),
		in:             r,
		out:            io.Discard,
		maxMessageSize: 128, // cap at 128 bytes
	}

	s.RegisterTool("ping", "pong", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	// Write a valid small message first
	go func() {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
		// Then write an oversized message (200 bytes of JSON cruft)
		big := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ping","arguments":{"data":"%s"}}}`, strings.Repeat("x", 180))
		fmt.Fprintln(w, big)
		w.Close()
	}()

	err = s.Run()
	// Scanner should return bufio.ErrTooLong when hitting the oversized line
	if err == nil {
		t.Fatal("expected error for oversized message, got nil")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got: %v", err)
	}
}

func TestMaxMessageSize_AllowsNormalSized(t *testing.T) {
	// Messages under the limit should be processed normally.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	buf := &bytes.Buffer{}
	s := &Server{
		name:           "test-server",
		handler:        make(map[string]ToolHandler),
		in:             r,
		out:            buf,
		maxMessageSize: 1 << 20, // 1 MB
	}

	s.RegisterTool("ping", "pong", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	go func() {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ping","arguments":{}}}`)
		w.Close()
	}()

	err = s.Run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 responses (initialize + tools/call)
	msgs := readMessages(t, buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(msgs))
	}
	// First response: initialize result
	if msgs[0].Error != nil {
		t.Errorf("unexpected error on initialize: %v", msgs[0].Error)
	}
	// Second response: tools/call result
	if msgs[1].Error != nil {
		t.Errorf("unexpected error on tools/call: %v", msgs[1].Error)
	}
}
func TestTraceparent_Passthrough(t *testing.T) {
	s, buf := testServer()

	s.RegisterTool("ping", "pong", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	// Send a tools/call with a valid W3C traceparent in params
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{},"traceparent":"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if msgs[0].Error != nil {
		t.Fatalf("expected success with valid traceparent, got error: %+v", msgs[0].Error)
	}
	result := msgs[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	item := content[0].(map[string]interface{})
	if item["text"] != "pong" {
		t.Errorf("expected 'pong', got %q", item["text"])
	}
}

func TestTraceparent_InvalidDegradesGracefully(t *testing.T) {
	s, buf := testServer()

	s.RegisterTool("ping", "pong", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	// Send with an invalid traceparent (too short) — should still succeed, just start a new trace
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{},"traceparent":"garbage"}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if msgs[0].Error != nil {
		t.Fatalf("expected success even with invalid traceparent, got error: %+v", msgs[0].Error)
	}
}

func TestTraceparent_WithoutTraceparent(t *testing.T) {
	s, buf := testServer()

	s.RegisterTool("ping", "pong", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	// Send without any traceparent — default behavior, starts new trace
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}`)
	s.handleMessage(req)

	msgs := readMessages(t, buf)
	if msgs[0].Error != nil {
		t.Fatalf("expected success without traceparent, got error: %+v", msgs[0].Error)
	}
	result := msgs[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	item := content[0].(map[string]interface{})
	if item["text"] != "pong" {
		t.Errorf("expected 'pong', got %q", item["text"])
	}
}
