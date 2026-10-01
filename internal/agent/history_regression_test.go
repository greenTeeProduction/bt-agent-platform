package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestHistoryWriteFailureDoesNotAcknowledgeOrCache(t *testing.T) {
	dir := t.TempDir()
	h, err := NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", filepath.Join(dir, "agent.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := h.Record(RunRecord{AgentName: "agent", Outcome: "success"}); err == nil {
		t.Fatal("acknowledged a failed write")
	}
	if got := h.List("agent", 0); len(got) != 0 {
		t.Fatalf("cached uncommitted history: %+v", got)
	}
}

func TestHistoryStatsConcurrentWithRecording(t *testing.T) {
	h, err := NewHistory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Record(RunRecord{AgentName: "agent"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 300 {
				h.AllStats()
			}
		})
	}
	wg.Go(func() {
		for range 300 {
			if err := h.Record(RunRecord{AgentName: "agent"}); err != nil {
				t.Error(err)
			}
		}
	})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("stats and recording deadlocked")
	}
	if got := h.Stats("agent").TotalRuns; got != 301 {
		t.Fatalf("runs = %d, want 301", got)
	}
}

func TestRunHistoryFailureIsReportedWithExecutionResult(t *testing.T) {
	for _, mode := range []string{"runner", "scheduler"} {
		t.Run(mode, func(t *testing.T) {
			reg, err := NewRegistry(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			name := "history-failure"
			if _, err := reg.Create(Definition{Name: name, Tree: "domain:default", Version: "1.0.0"}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			hist, err := NewHistory(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/dev/full", filepath.Join(dir, name+".jsonl")); err != nil {
				t.Fatal(err)
			}
			if mode == "runner" {
				deps := RunDeps{Registry: reg, History: hist, ResolveTree: func(string) *evolution.SerializableNode {
					return &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "history-success"}
				}}
				result, err := deps.RunOnce(context.Background(), name, "run", RunOptions{RecordHistory: true})
				if err == nil || result == nil || result.Outcome != "success" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else {
				sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist})
				runner := func(RunContext) (string, string, *RunResult, error) { return "success", "completed", nil, nil }
				outcome, output, err := sched.RunNow(name, "run", runner, "30s")
				if err == nil || outcome != "success" || output != "completed" {
					t.Fatalf("outcome=%s output=%s err=%v", outcome, output, err)
				}
			}
			if len(hist.List(name, 0)) != 0 {
				t.Fatal("failed history write acknowledged in cache")
			}
		})
	}
}
