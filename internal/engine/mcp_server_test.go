package engine

import (
	"encoding/json"
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
