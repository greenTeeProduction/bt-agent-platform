package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
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
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
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

// mixedToolSharedLeaf copies Task into Result after a short sleep, widening
// the window in which a concurrent call to a DIFFERENT tool sharing the same
// *Blackboard can stomp this call's Task before it reads its own result
// back — the same widening trick cmd/bt-agent/tools_test.go's echoTaskLeaf
// uses to pin the (already-fixed) single-tool bt_run_task race.
type mixedToolSharedLeaf struct{}

func (mixedToolSharedLeaf) Run(ctx *btcore.BTContext[Blackboard]) int {
	time.Sleep(5 * time.Millisecond)
	ctx.Blackboard.Result = ctx.Blackboard.Task
	return 1
}

// TestServer_Run_MixedToolConcurrentCallsDoNotRaceOnSharedBlackboard is the
// engine-level regression guard for the mcpDeps shared-blackboard data race
// (Q1 Correctness / Q3 Reliability) left unfixed by the prior guard program.
// That program's only landed change (cmd/bt-agent/tools.go's deps.bbMu,
// ac6659c) wraps a single tool — bt_run_task — in a mutex around its whole
// Task-assign -> RunTask -> Result-read critical section. Every OTHER tool
// handler that reads/writes the same *engine.Blackboard through mcpDeps —
// bt_delegate_to_tree, bt_use_go_tree, bt_use_finance_tree,
// bt_use_research_tree, bt_use_domain_tree (all in cmd/bt-agent/tools.go) —
// still performs an equivalent critical section with NO lock at all, at
// HEAD: a lock taken ad hoc inside only one handler provides no protection
// whatsoever, because any other handler that forgot to opt in can freely
// interleave into the "protected" handler's critical section. That
// per-handler opt-in (easy to add a new tool and forget) is the actual gap:
// this test pins Server.RegisterBlackboardTool (added alongside this test)
// as the structural fix — a Server-wide lock applied at registration time,
// so no handler can forget it. cmd/bt-agent still needs a follow-up
// migration of its five unguarded tools onto the equivalent primitive; this
// test only proves the primitive itself closes the race when used.
//
// cmd/bt-agent cannot exercise this dispatch path directly (engine.Server's
// stdin/stdout fields aren't exported), so this test reproduces the
// identical shape locally, using the real engine.Server, engine.Blackboard,
// and engine.RunTask/BuildTree machinery, dispatched through the server's
// actual stdin/stdout Run() loop (this file's Run(), lines ~148-209: the
// 5-slot semaphore + goroutine dispatch), not the Invoke() shortcut.
//
// "task_alpha" and "task_beta" both perform the identical Task-assign ->
// RunTask -> Result-read cycle against one shared *Blackboard and tree, both
// registered via RegisterBlackboardTool. Firing a mixed burst of both
// through Run() must never let one call's task leak into a DIFFERENT call's
// echoed result. Run with -race for a hard proof: this must also report
// zero races on the shared Blackboard.
func TestServer_Run_MixedToolConcurrentCallsDoNotRaceOnSharedBlackboard(t *testing.T) {
	bb := &Blackboard{}
	var tree btcore.Command[Blackboard] = mixedToolSharedLeaf{}

	sharedCriticalSection := func(args json.RawMessage) *ToolResult {
		var params struct {
			Task string `json:"task"`
		}
		_ = json.Unmarshal(args, &params)
		bb.Task = params.Task
		result := RunTask(bb, tree)
		return &ToolResult{Content: []ContentItem{{Type: "text", Text: result}}}
	}

	srv := NewServer("mixed-tool-race-test")
	srv.RegisterBlackboardTool("task_alpha", "shared-blackboard critical section", nil, nil, sharedCriticalSection)
	srv.RegisterBlackboardTool("task_beta", "shared-blackboard critical section", nil, nil, sharedCriticalSection)

	// n must not exceed Run()'s 5-slot concurrency semaphore — any call
	// beyond that is rejected outright with a busy error before it ever
	// touches the shared Blackboard, which would fail this test for an
	// unrelated reason.
	const n = 5
	lines := make([]string, 0, n)
	want := make(map[int]string, n)
	for i := range n {
		id := i + 1
		task := fmt.Sprintf("mixed-race-task-%02d", i)
		tool := "task_alpha"
		if i%2 == 1 {
			tool = "task_beta"
		}
		lines = append(lines, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{"task":%q}}}`, id, tool, task))
		want[id] = task
	}
	input := strings.Join(lines, "\n") + "\n"

	var out bytes.Buffer
	srv.in = strings.NewReader(input)
	srv.out = &out

	if err := srv.Run(); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	type rpcResponse struct {
		ID     int         `json:"id"`
		Result *ToolResult `json:"result,omitempty"`
		Error  *RPCError   `json:"error,omitempty"`
	}
	responses := make(map[int]rpcResponse, n)
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("response line %q is not valid JSON-RPC: %v", line, err)
		}
		responses[resp.ID] = resp
	}

	for id, task := range want {
		resp, ok := responses[id]
		if !ok {
			t.Fatalf("no response for id=%d (task %q); server dropped the request", id, task)
		}
		if resp.Error != nil {
			t.Fatalf("id=%d (task %q) returned error %+v, want result", id, task, resp.Error)
		}
		if resp.Result == nil || len(resp.Result.Content) == 0 {
			t.Fatalf("id=%d (task %q) returned no content", id, task)
		}
		got := resp.Result.Content[0].Text
		if got != task {
			t.Errorf("id=%d: echoed result = %q, want %q — a concurrent call to the OTHER "+
				"tool (sharing the same *Blackboard, not honoring the lock) stomped this "+
				"call's task before it read its own result back; every tool touching a "+
				"shared Blackboard must hold the same lock for its whole critical section",
				id, got, task)
		}
	}
}
