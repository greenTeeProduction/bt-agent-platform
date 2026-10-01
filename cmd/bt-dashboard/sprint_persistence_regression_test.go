package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/startup"
	btcore "github.com/rvitorper/go-bt/core"
)

func isolateSprintPersistence(t *testing.T) {
	t.Helper()
	isolatePipelinePaths(t)
	oldStore, oldCB := taskStore, getDashCBStore
	currentWorkflowMu.Lock()
	oldWorkflow := currentWorkflow
	currentWorkflow = nil
	currentWorkflowMu.Unlock()
	fixtureCB := agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{})
	getDashCBStore = func() *agent.AgentCircuitBreakerStore { return fixtureCB }
	sprintState.Lock()
	oldDeadline := sprintState.Deadline
	oldRunning, oldJob, oldStart, oldProgress := sprintState.Running, sprintState.JobID, sprintState.StartedAt, sprintState.Progress
	oldCurrent := sprintState.CurrentTask
	oldError, oldKind, oldDiagnostics, oldUncertain := sprintState.Error, sprintState.ErrorKind, sprintState.Diagnostics, sprintState.Uncertain
	sprintState.Running, sprintState.Uncertain = false, false
	sprintState.Error, sprintState.ErrorKind, sprintState.JobID, sprintState.Progress = "", "", "", ""
	sprintState.Deadline = time.Time{}
	sprintState.Diagnostics = nil
	sprintState.Unlock()
	t.Cleanup(func() {
		waitSprintFixture(t)
		taskStore, getDashCBStore = oldStore, oldCB
		currentWorkflowMu.Lock()
		currentWorkflow = oldWorkflow
		currentWorkflowMu.Unlock()
		sprintState.Lock()
		defer sprintState.Unlock()
		sprintState.Running, sprintState.JobID, sprintState.StartedAt, sprintState.Progress = oldRunning, oldJob, oldStart, oldProgress
		sprintState.Deadline = oldDeadline
		sprintState.CurrentTask = oldCurrent
		sprintState.Error, sprintState.ErrorKind, sprintState.Diagnostics, sprintState.Uncertain = oldError, oldKind, oldDiagnostics, oldUncertain
	})
}

func waitSprintFixture(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		sprintState.Lock()
		running := sprintState.Running
		sprintState.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("local sprint fixture did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func sprintFixtureHTTP(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("sprint-persistence-fixture"))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
	req.Header.Set("X-API-Key", "sprint-persistence-fixture")
	handler.ServeHTTP(rr, req)
	return rr
}

func TestSprintTaskCommitFailureRetainsEvidenceAndRepairsWithoutExecution(t *testing.T) {
	isolateSprintPersistence(t)
	root := filepath.Join(t.TempDir(), "task-owner")
	path := filepath.Join(root, "tasks.json")
	taskStore = dashboard.NewTaskStore(path)
	wf := dashboard.NewWorkflow("fixture", nil, startup.NewDefaultCompany())
	wf.ID = "wf-fixture"
	wf.Tasks = []dashboard.WorkflowTask{{ID: "first", Status: dashboard.StatusApproved}, {ID: "second", Status: dashboard.StatusApproved}}
	currentWorkflowMu.Lock()
	currentWorkflow = wf
	currentWorkflowMu.Unlock()
	for _, id := range []string{"first", "second"} {
		if err := taskStore.Create(dashboard.Task{ID: wf.ID + "-" + id, Title: id, TreeID: "fixture", Assignee: "sprint-commit-fixture", Status: "approved"}); err != nil {
			t.Fatal(err)
		}
	}
	var called atomic.Int64
	var admitted []byte
	name := fmt.Sprintf("SprintCommitFixture%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		n := called.Add(1)
		ctx.Blackboard.Result = fmt.Sprintf(`{"status":"ok","result":"Fixture task %d completed with attributable output before its metadata was committed."}`, n)
		if n == 1 {
			var err error
			admitted, err = os.ReadFile(path)
			if err != nil {
				t.Error(err)
				return -1
			}
			if err := os.RemoveAll(root); err != nil {
				t.Error(err)
				return -1
			}
			if err := os.WriteFile(root, []byte("blocked after admitted work"), 0600); err != nil {
				t.Error(err)
				return -1
			}
		}
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	if rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute"); rr.Code != http.StatusOK {
		t.Fatalf("fixture not admitted: %d %s", rr.Code, rr.Body.String())
	}
	waitSprintFixture(t)
	status := sprintFixtureHTTP(t, http.MethodGet, "/api/sprint/status")
	var observation struct {
		Progress    string                 `json:"progress"`
		Error       string                 `json:"error"`
		ErrorKind   string                 `json:"error_kind"`
		Diagnostics []sprintTaskDiagnostic `json:"diagnostics"`
		Completed   int                    `json:"tasks_completed"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &observation); err != nil {
		t.Fatal(err)
	}
	if status.Code != 200 || observation.Progress != "failed" || observation.ErrorKind != "persistence" || observation.Error == "" || len(observation.Diagnostics) != 2 || called.Load() != 2 || observation.Completed != 0 {
		t.Fatalf("action/commit failure disappeared or prevented independent work: calls=%d status=%d body=%s", called.Load(), status.Code, status.Body.String())
	}
	for i, diagnostic := range observation.Diagnostics {
		if diagnostic.TaskID != wf.ID+"-"+[]string{"first", "second"}[i] || diagnostic.TaskCommitted || diagnostic.TaskStatus != "completed" || diagnostic.Outcome != "success" || diagnostic.RunID == "" || diagnostic.Output == "" || diagnostic.CommitError == "" {
			t.Fatalf("observed completion/attribution lost: %+v", diagnostic)
		}
		stored, _ := taskStore.Get(diagnostic.TaskID)
		if stored.Status != "in_progress" || stored.Output != "" || wf.Tasks[i].Status != dashboard.StatusInProgress {
			t.Fatalf("undurable completion published to task/workflow: %+v", stored)
		}
	}
	if rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute"); rr.Code != 503 || called.Load() != 2 {
		t.Fatalf("failed metadata repair replayed or acknowledged work: %d %s", rr.Code, rr.Body.String())
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, admitted, 0600); err != nil {
		t.Fatal(err)
	}
	if rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute"); rr.Code != 200 || !strings.Contains(rr.Body.String(), "no_approved_tasks") || called.Load() != 2 {
		t.Fatalf("repair did not commit metadata separately: %d %s", rr.Code, rr.Body.String())
	}
	fresh := dashboard.NewTaskStore(path)
	for i, diagnostic := range observation.Diagnostics {
		stored, _ := fresh.Get(diagnostic.TaskID)
		if stored.Status != "completed" || stored.Output != diagnostic.Output || stored.RunID != diagnostic.RunID || stored.Outcome != "success" || wf.Tasks[i].Status != dashboard.StatusCompleted {
			t.Fatalf("metadata repair lost original completion: %+v", stored)
		}
	}
	if claimed, err := fresh.ClaimApproved(); err != nil || len(claimed) != 0 {
		t.Fatal("repaired completion became replayable")
	}
}

func TestSprintHealthyRunPersistenceDiagnosticSurvivesTaskCommit(t *testing.T) {
	isolateSprintPersistence(t)
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "healthy", Title: "healthy", Assignee: "sprint-run-persistence", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manager, err := blackboard.NewPersistentManager(root)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	name := fmt.Sprintf("SprintPromotionFixture%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		calls.Add(1)
		ctx.Blackboard.Result = `{"status":"ok","result":"Healthy local fixture completed; promotion is deliberately unavailable after its action."}`
		if err := os.Remove(filepath.Join(root, "agent")); err != nil {
			t.Error(err)
			return -1
		}
		if err := os.WriteFile(filepath.Join(root, "agent"), []byte("blocked promotion"), 0600); err != nil {
			t.Error(err)
			return -1
		}
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: manager, ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	if rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute"); rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	waitSprintFixture(t)
	status := sprintFixtureHTTP(t, http.MethodGet, "/api/sprint/status")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"error_kind":"persistence"`) || !strings.Contains(status.Body.String(), `"task_committed":true`) || !strings.Contains(status.Body.String(), `"progress":"failed"`) || calls.Load() != 1 {
		t.Fatalf("healthy run diagnostic hidden after task commit: %s calls=%d", status.Body.String(), calls.Load())
	}
	stored, _ := taskStore.Get("healthy")
	if stored.Status != "completed" || stored.Outcome != "success" || stored.Output == "" || stored.ErrorKind != reliability.ExecutionPersistenceKind || stored.Error == "" {
		t.Fatalf("healthy work or diagnostic not committed: %+v", stored)
	}
}

func TestSprintPanicDoesNotReportDoneOrRequeueClaims(t *testing.T) {
	isolateSprintPersistence(t)
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "interrupted", Title: "interrupted", Assignee: "sprint-interrupted", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	claimed, err := taskStore.ClaimApproved()
	if err != nil {
		t.Fatal(err)
	}
	sprintState.Lock()
	sprintState.Running = true
	sprintState.Unlock()
	// A missing executor panics at the dispatch boundary, outside the engine's
	// own panic handler. No command/model is involved in this fixture.
	executeSprintTasksWithContext(context.Background(), taskStore, nil, claimed)
	status := sprintFixtureHTTP(t, http.MethodGet, "/api/sprint/status")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"progress":"failed"`) || !strings.Contains(status.Body.String(), `"error_kind":"uncertain"`) {
		t.Fatal(status.Body.String())
	}
	if rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute"); rr.Code != 503 {
		t.Fatalf("interrupted claims were automatically readmitted: %d %s", rr.Code, rr.Body.String())
	}
	stored, _ := taskStore.Get("interrupted")
	if stored.Status != "in_progress" || len(taskStore.Approved()) != 0 {
		t.Fatal("uncertain work became replayable")
	}
}

func TestSprintReconciliationRejectsChangedOwnerOrDecision(t *testing.T) {
	isolateSprintPersistence(t)
	store := dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := store.Create(dashboard.Task{ID: "owned", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimApproved(); err != nil {
		t.Fatal(err)
	}
	sprintState.Lock()
	sprintState.Diagnostics = []sprintTaskDiagnostic{{TaskID: "owned", store: store, result: dashboard.TaskExecutionResult{Status: "completed", Outcome: "success", Output: "retained"}}}
	sprintState.Unlock()
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "other.json"))
	if rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute"); rr.Code != 503 {
		t.Fatalf("retargeted metadata repair admitted: %d %s", rr.Code, rr.Body.String())
	}
	taskStore = store
	if err := store.UpdateStatus("owned", "rejected"); err != nil {
		t.Fatal(err)
	}
	if rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute"); rr.Code != 503 {
		t.Fatalf("new operator decision overwritten: %d %s", rr.Code, rr.Body.String())
	}
	stored, _ := store.Get("owned")
	sprintState.Lock()
	defer sprintState.Unlock()
	if stored.Status != "rejected" || stored.Output != "" || sprintState.Diagnostics[0].TaskCommitted {
		t.Fatal("conflicting repair published a completion")
	}
}

func TestSprintDoesNotTurnWaitOrContradictoryCarryoverIntoCompletion(t *testing.T) {
	for _, fixture := range []struct {
		outcome string
		err     error
		want    sprintDisposition
	}{
		{"success", &reliability.ExecutionPersistenceError{Err: errors.New("record")}, sprintCompleted},
		{"pending_approval", nil, sprintFailed},
		{"input-required", nil, sprintFailed},
		{agent.RateLimitCarryoverOutcome, &reliability.ExecutionUncertainError{Err: errors.New("lost")}, sprintFailed},
		{agent.RateLimitCarryoverOutcome, &reliability.ExecutionStoppedError{Outcome: "failure", Err: errors.New("joined sibling")}, sprintFailed},
	} {
		if got := sprintTaskDisposition(fixture.outcome, fixture.err); got != fixture.want {
			t.Fatalf("disposition lost observed completion/stop: %s err=%v got=%v", fixture.outcome, fixture.err, got)
		}
	}
}

func TestSprintRejectsAuthBeforeRepairingMetadata(t *testing.T) {
	isolateSprintPersistence(t)
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "owned", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := taskStore.ClaimApproved(); err != nil {
		t.Fatal(err)
	}
	sprintState.Lock()
	sprintState.Diagnostics = []sprintTaskDiagnostic{{TaskID: "owned", store: taskStore, result: dashboard.TaskExecutionResult{Status: "completed", Outcome: "success", Output: "retained"}}}
	sprintState.Unlock()
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("sprint-persistence-fixture"))
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/sprint/status"
		if method == http.MethodPost {
			path = "/api/sprint/execute"
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
		if rr.Code != 401 {
			t.Fatalf("unauthenticated sprint disclosed/admitted: %d %s", rr.Code, rr.Body.String())
		}
	}
	stored, _ := taskStore.Get("owned")
	if stored.Status != "in_progress" || stored.Output != "" {
		t.Fatal("unauthorized request repaired task metadata")
	}
}
