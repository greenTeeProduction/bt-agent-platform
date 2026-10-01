package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestExecutorReportsHistoryFailureWithoutTrippingExecutionBreaker(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	dir := t.TempDir()
	hist, err := agent.NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	name := "executor-history-failure"
	if err := os.Symlink("/dev/full", filepath.Join(dir, name+".jsonl")); err != nil {
		t.Fatal(err)
	}
	cb := agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{Threshold: 1, Cooldown: time.Minute})
	exec := &AgentExecutor{Timeout: time.Second, CBStore: cb, Runner: &agent.RunDeps{History: hist, ResolveTree: func(string) *evolution.SerializableNode { return &evolution.SerializableNode{Type: "AlwaysSucceed"} }}}
	result, err := exec.RunTaskResult(name, "do the thing", "test")
	if result == nil || result.Outcome != "success" || !reliability.IsExecutionPersistenceError(err) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if cb.Get(name).State() != agent.CircuitClosed || cb.Get(name).FailureCount() != 0 {
		t.Fatal("persistence diagnostic counted as failed execution")
	}
}

func TestWorkflowDoesNotReplayCompletedStepAfterPersistenceFailure(t *testing.T) {
	calls := 0
	diagnostic := &reliability.ExecutionPersistenceError{Err: os.ErrPermission}
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		calls++
		return "success", "completed output", diagnostic
	}}
	result, err := runner.Run(context.Background(), Pipeline{Name: "history", Steps: []Step{{ID: "one", Kind: StepAgent, OnFailure: "retry"}, {ID: "two", Kind: StepAgent}}}, "test")
	if !reliability.IsExecutionPersistenceError(err) || calls != 1 || result.Outcome != "aborted" || result.Steps[0].Outcome != "success" || result.Steps[0].Output != "completed output" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestWorkflowRetryPreservesCompletedPersistenceDiagnostic(t *testing.T) {
	calls := 0
	diagnostic := &reliability.ExecutionPersistenceError{Err: os.ErrPermission}
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		calls++
		if calls == 1 {
			return "failure", "failed", os.ErrInvalid
		}
		return "success", "completed retry", diagnostic
	}}
	result, err := runner.Run(context.Background(), Pipeline{Name: "retry-history", Steps: []Step{{ID: "one", Kind: StepAgent, OnFailure: "retry"}, {ID: "two", Kind: StepAgent}}}, "test")
	if !reliability.IsExecutionPersistenceError(err) || calls != 2 || result.Outcome != "aborted" || result.Steps[0].Outcome != "success" || result.Steps[0].Output != "completed retry" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestWorkflowParallelPropagatesCompletedPersistenceDiagnostic(t *testing.T) {
	diagnostic := &reliability.ExecutionPersistenceError{Err: os.ErrPermission}
	runner := &Runner{RunAgent: func(_ context.Context, name, _, _ string) (string, string, error) {
		if name == "must-not-run" {
			t.Error("continued beyond failed persistence")
		}
		return "success", "completed " + name, diagnostic
	}}
	result, err := runner.Run(context.Background(), Pipeline{Name: "parallel-history", Steps: []Step{
		{ID: "parallel", Kind: StepParallel, OnFailure: "retry", Steps: []Step{{ID: "a", Kind: StepAgent, Agent: "a"}, {ID: "b", Kind: StepAgent, Agent: "b"}}},
		{ID: "after", Kind: StepAgent, Agent: "must-not-run"},
	}}, "test")
	if !reliability.IsExecutionPersistenceError(err) || result.Outcome != "aborted" || len(result.Steps) != 1 || result.Steps[0].Outcome != "success" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
