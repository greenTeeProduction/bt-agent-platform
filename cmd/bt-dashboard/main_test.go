package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

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
