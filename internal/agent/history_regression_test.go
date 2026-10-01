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

func TestHistoryConfinesIdentifiersAndSymlinksToOwner(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "history")
	outside := filepath.Join(base, "outside.jsonl")
	original := []byte("{\"task\":\"private outside evidence\"}\n")
	if err := os.WriteFile(outside, original, 0600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "../outside", outside, "nested/agent", `nested\agent`, "bad\x00name"} {
		if err := h.Record(RunRecord{AgentName: name, Outcome: "success"}); err == nil {
			t.Errorf("unsafe identifier acknowledged: %q", name)
		}
		if len(h.List(name, 0)) != 0 {
			t.Errorf("unsafe record cached: %q", name)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := h.Record(RunRecord{AgentName: "linked"}); err == nil {
		t.Fatal("outward append acknowledged")
	}
	loaded, err := NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.List("linked", 0)) != 0 {
		t.Fatal("outward history read disclosed records")
	}
	if actual, err := os.ReadFile(outside); err != nil || string(actual) != string(original) {
		t.Fatal("outside history changed")
	}
	if err := h.Record(RunRecord{AgentName: "domain:arc42", Task: "valid local identifier"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "domain:arc42.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("new history exposes private records: %o", info.Mode().Perm())
	}
}
