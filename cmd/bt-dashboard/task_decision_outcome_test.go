package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/hitl"
)

func TestTaskDecisionHTTPReportsPartialCommitAndRetryRecovers(t *testing.T) {
	previousTasks, previousAudit := taskStore, hitl.DefaultStore
	t.Cleanup(func() { taskStore = previousTasks; hitl.DefaultStore = previousAudit })
	root := t.TempDir()
	audit, err := hitl.InitStore(root)
	if err != nil {
		t.Fatal(err)
	}
	req := hitl.NewRequest("gate", "WorkflowApproval", "task", "", "", "", map[string]any{"task_id": "task"})
	if err := audit.Create(req); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "hitl", "requests.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "task"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(handleTaskApprove))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, newMutationTestRequest("/api/tasks/approve?id=task"))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("failure status=%d body=%s", response.Code, response.Body.String())
	}
	if len(taskStore.Approved()) != 0 {
		t.Fatal("HTTP partial approval admitted work")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, newMutationTestRequest("/api/tasks/approve?id=task"))
	if response.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
	}
	if len(taskStore.Approved()) != 1 {
		t.Fatal("reconciled approval not admitted")
	}
}
