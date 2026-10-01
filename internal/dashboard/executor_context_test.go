package dashboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestExecutorPreCanceledContextDoesNotExecuteOrRecordFailure(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	called := false
	executor := &AgentExecutor{Timeout: time.Second, Runner: &agent.RunDeps{ResolveTree: func(string) *evolution.SerializableNode { called = true; return nil }}, CBStore: agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := executor.RunTaskResultWithContext(ctx, "pre-canceled-context-probe", "task", "")
	if result != nil || !errors.Is(err, context.Canceled) || called {
		t.Fatalf("pre-canceled work executed: %v %v called=%v", result, err, called)
	}
	if _, err := os.Stat(agent.CircuitBreakersFile()); !os.IsNotExist(err) {
		t.Fatal("pre-execution cancellation recorded a breaker failure")
	}
}

func TestExecutorPropagatesCallerDeadlineToTree(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	engine.RegisterAction("DashboardCallerDeadlineProbe", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		<-ctx.Done()
		ctx.Blackboard.Result = ctx.Context.Err().Error()
		return -1
	})
	executor := &AgentExecutor{Timeout: time.Minute, Runner: &agent.RunDeps{ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: "DashboardCallerDeadlineProbe"}
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	result, err := executor.RunTaskResultWithContext(ctx, "caller-deadline-probe", "task", "")
	if time.Since(start) > time.Second || result == nil || result.Outcome == "success" {
		t.Fatalf("caller deadline lost: result=%v err=%v", result, err)
	}
}

func TestHermesFallbackPreservesExitErrorAndCallerDeadline(t *testing.T) {
	for _, probe := range []string{"exit", "deadline", "child"} {
		t.Run(probe, func(t *testing.T) {
			base := t.TempDir()
			script := "#!/bin/sh\nprintf 'partial evidence'\nexit 7\n"
			if probe == "deadline" {
				script = "#!/bin/sh\nprintf 'partial evidence'\nexec /bin/sleep 10\n"
			}
			pidFile := filepath.Join(base, "child.pid")
			if probe == "child" {
				script = "#!/bin/sh\nprintf 'partial evidence'\n/bin/sleep 10 &\nchild=$!\nprintf '%s' \"$child\" > \"$BT_TEST_HERMES_CHILD_PID\"\nwait \"$child\"\n"
				t.Setenv("BT_TEST_HERMES_CHILD_PID", pidFile)
			}
			if err := os.WriteFile(filepath.Join(base, "hermes"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", base+string(os.PathListSeparator)+os.Getenv("PATH"))
			executor := &AgentExecutor{Timeout: time.Minute}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			start := time.Now()
			result, err := executor.RunTaskResultWithContext(ctx, "fake-hermes-context-probe", "task", "tree")
			if err == nil || result == nil || !strings.Contains(result.Output, "partial evidence") || time.Since(start) > 2*time.Second {
				t.Fatalf("fallback lost failure/evidence/deadline: %v %v", result, err)
			}
			if probe != "exit" && (!errors.Is(err, context.DeadlineExceeded) || result.Outcome != "timeout") {
				t.Fatalf("fallback lost caller deadline: %v %v", result, err)
			}
			if probe == "child" {
				data, err := os.ReadFile(pidFile)
				if err != nil {
					t.Fatal("fake command never started its child", err)
				}
				pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil || pid <= 0 {
					t.Fatal("invalid child pid")
				}
				deadline := time.Now().Add(time.Second)
				for {
					stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
					if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
					if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X") {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("cancellation left the owned child running")
					}
					time.Sleep(time.Millisecond)
				}
			}
		})
	}
}
