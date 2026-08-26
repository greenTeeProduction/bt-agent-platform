package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// TestACPClientCircuitBreakerShortCircuitsRepeatedSubprocessFailures exercises
// milestone 4/4 of the Q3 Reliability program: a reliability.CircuitBreaker on
// ACPClient must trip after repeated subprocess failures and then short-circuit
// further GenerateCtx calls with a fast error instead of respawning the
// perpetually-failing subprocess on every call. Today (before the breaker is
// wired around Start()/Wait()) ACPClient has no failure memory, so it relaunches
// the crashing helper subprocess on every single call — this test fails because
// the subprocess invocation count keeps pace with the call count and no error
// ever mentions the circuit being open.
func TestACPClientCircuitBreakerShortCircuitsRepeatedSubprocessFailures(t *testing.T) {
	countFile := filepath.Join(t.TempDir(), "acp-crash-count")
	client := NewACPClient(ACPConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestACPHelperProcess", "--", "always-crash:" + countFile},
		CWD:     t.TempDir(),
		Timeout: 2 * time.Second,
	})

	const attempts = 10
	var lastErr error
	for i := range attempts {
		_, lastErr = client.Generate("trigger failure")
		if lastErr == nil {
			t.Fatalf("attempt %d: expected error from always-crashing ACP subprocess, got nil", i)
		}
	}

	invocations := countCrashInvocations(t, countFile)
	if invocations >= attempts {
		t.Fatalf("circuit breaker did not short-circuit: subprocess was launched %d times across %d "+
			"GenerateCtx calls; expected repeated subprocess failures to trip the breaker and stop "+
			"respawning the failing subprocess", invocations, attempts)
	}
	if !strings.Contains(strings.ToLower(lastErr.Error()), "circuit") {
		t.Fatalf("expected the final error to indicate the circuit breaker is open, got: %v", lastErr)
	}
}

// countCrashInvocations reports how many times the "always-crash" helper
// subprocess mode actually ran, by counting the marker bytes it appends to
// countFile on each launch before it exits.
func countCrashInvocations(t *testing.T, countFile string) int {
	t.Helper()
	data, err := os.ReadFile(countFile)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read crash invocation count file: %v", err)
	}
	return len(data)
}

// TestACPClientGenerateCtx_StderrBufferRace exercises GOAL1 of the
// NotebookLM ACP research: GenerateCtx (acp.go) hands cmd.Stderr a plain
// *bytes.Buffer. Because that buffer is not an *os.File, os/exec starts its
// own goroutine that keeps copying the subprocess's stderr pipe into the
// buffer for as long as the subprocess is alive. GenerateCtx's request()
// closure reads stderr.String() on its ctx.Done()/scanErr branches with no
// synchronization against that copy goroutine. This test drives an ACP
// helper subprocess that floods stderr continuously and never answers on
// stdout, so the client's context deadline fires while the subprocess is
// still alive and actively writing stderr — producing a concurrent
// Buffer.Write/Buffer.String data race that -race must catch.
func TestACPClientGenerateCtx_StderrBufferRace(t *testing.T) {
	client := NewACPClient(ACPConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestACPHelperProcess", "--", "stderr-flood"},
		CWD:     t.TempDir(),
		Timeout: 150 * time.Millisecond,
	})

	_, err := client.Generate("hello")
	if err == nil {
		t.Fatalf("expected GenerateCtx to time out against the stderr-flooding helper, got nil error")
	}
}

// TestACPHelperProcess is not a real test. It is a helper subprocess used by
// ACP client tests to emulate a newline-delimited JSON-RPC ACP server.
func TestACPHelperProcess(_ *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "stderr-flood" {
		// Never respond on stdout; keep writing to stderr until killed or
		// the deadline passes, so the parent's stderr.String() reads race
		// against this process's still-open stderr pipe.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			fmt.Fprintln(os.Stderr, "stderr flood line")
		}
		return
	}
	if strings.HasPrefix(mode, "always-crash:") {
		// Simulates a subprocess that fails on every launch (e.g. a broken
		// binary or crash-looping agent): record that we were invoked, then
		// exit immediately without ever answering an ACP request.
		countFile := strings.TrimPrefix(mode, "always-crash:")
		if f, err := os.OpenFile(countFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			_, _ = f.WriteString("x")
			_ = f.Close()
		}
		os.Exit(1)
	}
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
