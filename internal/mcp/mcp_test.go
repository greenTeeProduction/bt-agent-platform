package mcp

import (
	"bytes"
	"encoding/json"
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
