package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestReviewSprintRetryReturnsExistingJob(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	old, runner := taskStore, dashAgentRunner
	retryKey := t.TempDir()
	taskStore = dashboard.NewTaskStore(t.TempDir() + "/tasks.json")
	gate := make(chan struct{})
	started := make(chan struct{})
	actionName := "review_sprint_wait" + t.TempDir()
	engine.RegisterAction(actionName, func(ctx *btcore.BTContext[engine.Blackboard]) int { close(started); <-gate; return 1 })
	dashAgentRunner = &agent.RunDeps{ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: actionName}
	}}
	defer func() {
		close(gate)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			sprintState.Lock()
			running := sprintState.Running
			sprintState.Unlock()
			if !running {
				break
			}
			time.Sleep(time.Millisecond)
		}
		taskStore = old
		dashAgentRunner = runner
	}()
	if err := taskStore.Create(dashboard.Task{ID: "task", Title: "task", Status: "approved", Assignee: "reviewer"}); err != nil {
		t.Fatal(err)
	}
	request := func() map[string]any {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/sprint/execute", strings.NewReader(`{}`))
		r.Header.Set("Idempotency-Key", retryKey)
		handleSprintExecute(w, r)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := request()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sprint did not start")
	}
	second := request()
	if first["job_id"] == nil || second["job_id"] != first["job_id"] {
		t.Errorf("retry first=%v second=%v", first, second)
	}
}
