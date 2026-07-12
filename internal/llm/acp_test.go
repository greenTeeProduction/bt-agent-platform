package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
)

func TestACPClientGenerateTalksToACPServer(t *testing.T) {
	cmd := os.Args[0]
	args := []string{"-test.run=TestACPHelperProcess", "--", "normal"}
	client := NewACPClient(ACPConfig{
		Command: cmd,
		Args:    args,
		CWD:     t.TempDir(),
		Timeout: 5 * time.Second,
	})

	got, err := client.Generate("hello via acp")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if got != "ACP_RESPONSE: hello via acp" {
		t.Fatalf("unexpected ACP response: %q", got)
	}
}

func TestACPClientGenerateReturnsErrorWhenSessionMissing(t *testing.T) {
	client := NewACPClient(ACPConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestACPHelperProcess", "--", "missing-session"},
		CWD:     t.TempDir(),
		Timeout: 5 * time.Second,
	})

	_, err := client.Generate("hello")
	if err == nil || !strings.Contains(err.Error(), "sessionId") {
		t.Fatalf("expected missing sessionId error, got %v", err)
	}
}

func TestNewProviderCreatesACPClient(t *testing.T) {
	cfg := &config.Config{
		LLMProvider: "acp",
		ACPCommand:  os.Args[0],
		ACPArgs:     "-test.run=TestACPHelperProcess -- normal",
		ACPCwd:      t.TempDir(),
		LLMTimeout:  5,
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider(acp): %v", err)
	}
	client, ok := provider.(*ACPClient)
	if !ok {
		t.Fatalf("expected *ACPClient, got %T", provider)
	}
	got, err := client.Generate("provider prompt")
	if err != nil {
		t.Fatalf("Generate via provider: %v", err)
	}
	if got != "ACP_RESPONSE: provider prompt" {
		t.Fatalf("unexpected provider response: %q", got)
	}
}

func TestACPClientGenerateTalksToHermesACP(t *testing.T) {
	if os.Getenv("BT_LIVE_HERMES_ACP") != "1" {
		t.Skip("set BT_LIVE_HERMES_ACP=1 to run live Hermes ACP integration test")
	}
	client := NewACPClient(ACPConfig{
		Command: "hermes",
		Args:    []string{"acp", "--accept-hooks"},
		CWD:     t.TempDir(),
		Timeout: 120 * time.Second,
	})
	got, err := client.Generate("Reply with exactly: BT_ACP_OK")
	if err != nil {
		t.Fatalf("Generate via Hermes ACP: %v", err)
	}
	if !strings.Contains(got, "BT_ACP_OK") {
		t.Fatalf("expected Hermes ACP response to contain BT_ACP_OK, got %q", got)
	}
}

// panicReader is an io.Reader whose Read always panics, simulating a bug
// while parsing ACP subprocess stdout (e.g. a stdlib edge case or a future
// code change) inside the scanJSONLines goroutine.
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) {
	panic("acp_test: stdout reader exploded while scanning")
}

// An unrecovered panic inside a `go func(...)` goroutine crashes the entire
// process — it cannot be caught by the parent test's own recover(). This test
// re-execs the test binary so a crash is contained in the child process.
// Today (before reliability.SafeGo wraps the scanJSONLines goroutine launched
// at acp.go line 94) the child process crashes with an unhandled panic
// instead of exiting cleanly with the panic reported on scanErr.
const acpScanPanicSubprocessEnv = "BT_ACP_SCAN_PANIC_SUBPROCESS"

func TestStartScanJSONLines_ReaderPanicRecoveredOnScanErr(t *testing.T) {
	if os.Getenv(acpScanPanicSubprocessEnv) == "1" {
		messages := make(chan map[string]any, 1)
		scanErr := make(chan error, 1)
		startScanJSONLines(panicReader{}, messages, scanErr)

		select {
		case err := <-scanErr:
			if err == nil {
				os.Exit(3)
			}
			fmt.Println("scanErr received:", err)
			os.Exit(0)
		case <-time.After(5 * time.Second):
			fmt.Println("scanErr never received a value")
			os.Exit(4)
		}
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestStartScanJSONLines_ReaderPanicRecoveredOnScanErr")
	cmd.Env = append(os.Environ(), acpScanPanicSubprocessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("startScanJSONLines: a panicking stdout reader crashed the process instead of "+
			"being recovered and reported on scanErr via reliability.SafeGo; exit error=%v output=%s", err, out)
	}
	if !strings.Contains(string(out), "scanErr received:") {
		t.Fatalf("expected the recovered panic to be reported on scanErr, got output=%s", out)
	}
}

// TestACPHelperProcess is not a real test. It is a helper subprocess used by
// ACP client tests to emulate a newline-delimited JSON-RPC ACP server.
func TestACPHelperProcess(_ *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	r := bufio.NewScanner(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	for r.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(r.Bytes(), &msg); err != nil {
			fmt.Fprintf(os.Stderr, "bad json: %v\n", err)
			os.Exit(2)
		}
		id := msg["id"]
		switch msg["method"] {
		case "initialize":
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			if mode == "missing-session" {
				writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
			} else {
				writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"sessionId": "s-test"}})
			}
		case "session/prompt":
			params := msg["params"].(map[string]any)
			promptBlocks := params["prompt"].([]any)
			first := promptBlocks[0].(map[string]any)
			text := first["text"].(string)
			writeJSON(w, map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"text": "ACP_RESPONSE: " + text},
				}},
			})
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "result": nil})
		default:
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "unknown"}})
		}
	}
}

func writeJSON(w *bufio.Writer, msg map[string]any) {
	b, _ := json.Marshal(msg)
	_, _ = w.Write(append(b, '\n'))
	_ = w.Flush()
}

var _ = exec.Command
