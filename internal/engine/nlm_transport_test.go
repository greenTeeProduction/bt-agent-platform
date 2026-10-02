package engine

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func isolateNlmTransport(t *testing.T) {
	t.Helper()
	withNlmEconomy(t)
	oldCommand := nlmCommand
	nlmCircuitMu.Lock()
	oldOpen, oldCount, oldTime := nlmCircuitOpen, nlmFailCount, nlmOpenedAt
	nlmCircuitOpen, nlmFailCount = false, 0
	nlmCircuitMu.Unlock()
	t.Cleanup(func() {
		nlmCommand = oldCommand
		nlmCircuitMu.Lock()
		nlmCircuitOpen, nlmFailCount, nlmOpenedAt = oldOpen, oldCount, oldTime
		nlmCircuitMu.Unlock()
	})
}

func TestNlmGenerationAllowedUnderSolPolicyWithoutReplay(t *testing.T) {
	isolateNlmTransport(t)
	t.Setenv("BT_LLM_SOL_ONLY", "true")
	calls := 0
	nlmCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		calls++
		return exec.CommandContext(ctx, "sh", "-c", "printf 'connection lost after acceptance'; exit 1")
	}
	out := nlmRunContext(context.Background(), time.Second, "studio", "create", "nb-1", "--type", "audio")
	if calls != 1 || !strings.HasPrefix(out, "Error:") {
		t.Fatalf("calls=%d result=%q", calls, out)
	}
}

func TestNlmOpenCircuitReturnsImmediately(t *testing.T) {
	isolateNlmTransport(t)
	nlmCircuitOpen, nlmOpenedAt = true, time.Now()
	nlmCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		t.Fatal("open circuit executed command")
		return nil
	}
	start := time.Now()
	out := nlmRunContext(context.Background(), 100*time.Millisecond, "notebook", "list")
	if time.Since(start) > time.Second || !strings.Contains(out, "circuit open") {
		t.Fatalf("result=%q", out)
	}
}

func TestNlmDeadlineIncludesRetryBackoff(t *testing.T) {
	isolateNlmTransport(t)
	calls := 0
	nlmCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		calls++
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
	start := time.Now()
	out := nlmRunContext(context.Background(), 100*time.Millisecond, "notebook", "list")
	if calls != 1 || time.Since(start) > time.Second || !strings.Contains(out, "deadline exceeded") {
		t.Fatalf("calls=%d result=%q", calls, out)
	}
}

func TestNlmPreservesCompleteJSONAndRejectsErrorResponses(t *testing.T) {
	isolateNlmTransport(t)
	payload := `{"answer":"` + strings.Repeat("x", 12000) + `"}`
	nlmCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "%s", payload)
	}
	args := []string{"notebook", "query", "nb-1", "fixture question"}
	out := nlmRunContext(context.Background(), time.Second, args...)
	if out != payload || !json.Valid([]byte(out)) {
		t.Fatal("successful JSON was truncated")
	}
	cached, _, proceed := nlmPreflight(args)
	if proceed || cached != payload {
		t.Fatal("complete response was not cached")
	}
	payload = `{"status":"error","error":"expired"}`
	args[3] = "different fixture question"
	out = nlmRunContext(context.Background(), time.Second, args...)
	if !strings.HasPrefix(out, "Error:") {
		t.Fatalf("error JSON accepted: %s", out)
	}
	if _, _, proceed = nlmPreflight(args); !proceed {
		t.Fatal("failed response cached")
	}
}
