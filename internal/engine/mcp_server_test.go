package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestServer_HasToolAndInvoke exercises the exported tool-invocation seam:
// HasTool reports whether a tool was registered, and Invoke runs a registered
// tool's handler by name without going through the stdio JSON-RPC loop.
func TestServer_HasToolAndInvoke(t *testing.T) {
	srv := NewServer("t")
	srv.RegisterTool("echo", "echoes back", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "pong"}}}
	})

	// HasTool discriminates registered from unregistered tools.
	if !srv.HasTool("echo") {
		t.Fatal("HasTool(\"echo\") = false, want true")
	}
	if srv.HasTool("missing") {
		t.Fatal("HasTool(\"missing\") = true, want false")
	}

	// Invoke runs a registered handler and returns (result, true).
	result, ok := srv.Invoke("echo", json.RawMessage(`{}`))
	if !ok {
		t.Fatal("Invoke(\"echo\", ...) ok = false, want true")
	}
	if result == nil {
		t.Fatal("Invoke(\"echo\", ...) result = nil, want non-nil")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "pong" {
		t.Fatalf("Invoke(\"echo\", ...) result = %+v, want single text content \"pong\"", result)
	}

	// Invoke on an unregistered tool returns (nil, false).
	missingResult, ok := srv.Invoke("missing", json.RawMessage(`{}`))
	if ok {
		t.Fatal("Invoke(\"missing\", ...) ok = true, want false")
	}
	if missingResult != nil {
		t.Fatalf("Invoke(\"missing\", ...) result = %+v, want nil", missingResult)
	}
}

// TestServer_ToolPanicRecovery is the regression test for the tools/call
// dispatch goroutine lacking panic recovery: a tool handler that panics must
// not take down the daemon. The server must answer the panicking call with a
// JSON-RPC -32603 internal error naming the tool, and still answer the next
// request on the same connection.
//
// Without recovery in the dispatch goroutine (Run's `go func(d []byte)`), the
// panic propagates and crashes the whole process — which is exactly how a
// single bad tools/call kills the bt-agent daemon in production.
func TestServer_ToolPanicRecovery(t *testing.T) {
	srv := NewServer("panic-test")
	srv.RegisterTool("boom", "always panics", nil, nil, func(args json.RawMessage) *ToolResult {
		panic("deliberate test panic: tools/call dispatch must recover")
	})
	srv.RegisterTool("echo", "healthy tool", nil, nil, func(args json.RawMessage) *ToolResult {
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: "still alive"}}}
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	srv.in = strings.NewReader(input)
	srv.out = &out

	if err := srv.Run(); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	// tools/call handlers run concurrently, so responses may arrive in any
	// order — index them by request ID.
	responses := map[float64]Message{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("response line %q is not valid JSON-RPC: %v", line, err)
		}
		id, ok := msg.ID.(float64)
		if !ok {
			t.Fatalf("response line %q has non-numeric id %v", line, msg.ID)
		}
		responses[id] = msg
	}

	// The panicking call must yield a -32603 internal error naming the tool.
	boomResp, ok := responses[1]
	if !ok {
		t.Fatal("no response for id=1 (panicking tool call); server dropped the request")
	}
	if boomResp.Error == nil {
		t.Fatalf("response for panicking tool = %+v, want JSON-RPC error", boomResp)
	}
	if boomResp.Error.Code != -32603 {
		t.Errorf("panicking tool error code = %d, want -32603", boomResp.Error.Code)
	}
	if !strings.Contains(boomResp.Error.Message, "boom") {
		t.Errorf("panicking tool error message = %q, want it to name the tool \"boom\"", boomResp.Error.Message)
	}

	// The daemon must survive the panic and answer the next request.
	echoResp, ok := responses[2]
	if !ok {
		t.Fatal("no response for id=2 (healthy tool call after panic); server did not stay alive")
	}
	if echoResp.Error != nil {
		t.Fatalf("healthy tool call after panic returned error %+v, want result", echoResp.Error)
	}
}
