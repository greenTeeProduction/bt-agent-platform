package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// TestDashboardDriftWatcherRebuildsItself pins — at the source level, the
// same audit style as cmd/bt-agent/main_test.go's requireBuildIdentityWiring
// — that bt-dashboard's deploy-drift watcher can actually rebuild its own
// binary, not just detect that it has drifted from repo HEAD.
//
// agent.DefaultRebuildTargets deliberately excludes bt-dashboard (its doc
// comment: "bt-dashboard and the MCP bin/bt-agent are intentionally excluded
// here — callers pass the set they own"), so passing it unmodified as
// Targets means an AutoRebuild-enabled bt-dashboard WARNs on its own drift
// but the rebuild it triggers only ever swaps bt-agent/bt-agent-cli/
// bt-gardener — never itself. main.go must instead pass a target list that
// includes bt-dashboard's own binary.
func TestDashboardDriftWatcherRebuildsItself(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "agent.DashboardRebuildTargets(repoDir)") {
		t.Errorf("main.go's deploy-drift watcher must pass agent.DashboardRebuildTargets(repoDir) as " +
			"Targets so bt-dashboard rebuilds its own binary on drift; found agent.DefaultRebuildTargets " +
			"(or equivalent) which intentionally excludes bt-dashboard")
	}
}

// TestDashboardDriftWatcherWiresRebuildBackoff pins — the same audit style as
// TestDashboardDriftWatcherRebuildsItself above — that bt-dashboard's
// deploy-drift watcher sets a RebuildBackoff guard so a broken HEAD cannot
// retry-storm `go build` every watcher tick (ADR-045 milestone 4, currently
// unwired per arc42 §Deploy Drift, 2026-07-12).
func TestDashboardDriftWatcherWiresRebuildBackoff(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "Backoff:") {
		t.Error("main.go's deploy-drift watcher must wire a RebuildBackoff (Backoff:); not found")
	}
}

// TestHandleScalability_ReflectsInjectedQueueAndRouter pins milestone 3/5 of the
// horizontal-scaling adoption program: the /api/scalability endpoint must surface
// the injected TaskQueue depth and AgentRouter executor health instead of the
// hardcoded 0/nil placeholders that leave Queue and Router omitted from the JSON.
func TestHandleScalability_ReflectsInjectedQueueAndRouter(t *testing.T) {
	// Preserve and restore the package globals this test mutates.
	origQueue := dashTaskQueue
	origRouter := dashAgentRouter
	t.Cleanup(func() {
		dashTaskQueue = origQueue
		dashAgentRouter = origRouter
	})

	// Inject a task queue carrying two pending items.
	q := reliability.NewTaskQueue(filepath.Join(t.TempDir(), "queue.json"))
	q.Enqueue("task-a")
	q.Enqueue("task-b")
	dashTaskQueue = q

	// Inject an agent router holding a single healthy local executor.
	local := reliability.NewLocalExecutor("local-test", func(agent, task string) (*reliability.AgentResult, error) {
		return &reliability.AgentResult{Agent: agent, Success: true}, nil
	})
	dashAgentRouter = reliability.NewAgentRouter(local)

	req := httptest.NewRequest(http.MethodGet, "/api/scalability", nil)
	rr := httptest.NewRecorder()
	handleScalability(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var status reliability.ScalabilityStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	if status.Queue == nil {
		t.Fatalf("Queue is nil; endpoint still ignores the injected TaskQueue. body=%s", rr.Body.String())
	}
	if status.Queue.Pending != 2 {
		t.Errorf("Queue.Pending = %d, want 2 (injected queue depth)", status.Queue.Pending)
	}

	if status.Router == nil {
		t.Fatalf("Router is nil; endpoint still ignores the injected AgentRouter. body=%s", rr.Body.String())
	}
	if status.Router.Total != 1 {
		t.Errorf("Router.Total = %d, want 1 (injected executor count)", status.Router.Total)
	}
	if status.Router.Healthy != 1 {
		t.Errorf("Router.Healthy = %d, want 1 (single healthy local executor)", status.Router.Healthy)
	}
}

// TestHandleDLQReplay_RequeuesInsteadOfDropping pins milestone 4/5 of the
// drop-safe-DLQ program. The dashboard runs in a separate process from bt-agent
// and has no tree runner of its own, so it cannot actually execute a replayed
// task. The old handler called dlq.Replay(id), which REMOVES the entry from the
// queue and persists the removal — a cross-process silent drop: bt-agent's
// executor never sees the entry again and the task is lost.
//
// The fixed handler must instead reload the DLQ from disk and mark the entry for
// retry (RequeuedAt) without removing it, so the entry survives on disk and
// bt-agent's executor picks it up on its next scan.
func TestHandleDLQReplay_RequeuesInsteadOfDropping(t *testing.T) {
	origDLQ := dlq
	t.Cleanup(func() { dlq = origDLQ })

	dlqPath := filepath.Join(t.TempDir(), "dead_letter_queue.json")
	dlq = reliability.NewDeadLetterQueue(dlqPath)
	dlq.Push(reliability.DeadLetterEntry{
		ID:    "dead-1",
		Task:  "rebuild-index",
		Agent: "indexer",
		Error: "boom",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/dlq/replay?id=dead-1", nil)
	rr := httptest.NewRecorder()
	handleDLQReplay(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Reload from disk to assert the cross-process contract: the entry must
	// survive the replay rather than being silently dropped.
	entries := reliability.NewDeadLetterQueue(dlqPath).List()
	if len(entries) != 1 {
		t.Fatalf("entries on disk after replay = %d, want 1 (dashboard silently dropped the entry)", len(entries))
	}
	if entries[0].ID != "dead-1" {
		t.Fatalf("surviving entry ID = %q, want dead-1", entries[0].ID)
	}

	// The surviving entry must be flagged for retry so bt-agent's executor
	// requeues it. A zero RequeuedAt means it was left untouched (never picked up).
	if entries[0].RequeuedAt.IsZero() {
		t.Errorf("RequeuedAt is zero; entry was not flagged for retry")
	}
}
