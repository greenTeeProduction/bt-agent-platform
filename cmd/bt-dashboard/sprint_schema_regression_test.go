package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/dashboard"
)

func TestSprintStatusRealCountersSurviveEnforcedValidation(t *testing.T) {
	isolatePipelinePaths(t)
	oldStore := taskStore
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	t.Cleanup(func() { taskStore = oldStore })
	sprintState.Lock()
	oldDeadline := sprintState.Deadline
	oldProgress := sprintState.Progress
	oldRunning, oldJob, oldStart, oldTask := sprintState.Running, sprintState.JobID, sprintState.StartedAt, sprintState.CurrentTask
	sprintState.Unlock()
	t.Cleanup(func() {
		sprintState.Lock()
		defer sprintState.Unlock()
		sprintState.Deadline = oldDeadline
		sprintState.Progress = oldProgress
		sprintState.Running, sprintState.JobID, sprintState.StartedAt, sprintState.CurrentTask = oldRunning, oldJob, oldStart, oldTask
	})
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("sprint-fixture-key"))
	for _, id := range []string{"healthy", "failed", "pending"} {
		if err := taskStore.Create(dashboard.Task{ID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	for id, status := range map[string]string{"healthy": "completed", "failed": "failed", "pending": "approved"} {
		if err := taskStore.UpdateStatus(id, status); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range []string{"idle", "running", "finished"} {
		t.Run(fixture, func(t *testing.T) {
			sprintState.Lock()
			sprintState.Running = fixture == "running"
			sprintState.JobID = "fixture-job"
			sprintState.CurrentTask = "fixture-task"
			sprintState.StartedAt = time.Time{}
			sprintState.Deadline = time.Time{}
			sprintState.Progress = ""
			if fixture != "idle" {
				sprintState.StartedAt = time.Now().Add(-time.Second)
				sprintState.Progress = "done"
				if fixture == "running" {
					sprintState.Progress = "running"
				}
			}
			sprintState.Unlock()
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/sprint/status", nil)
			req.Header.Set("X-API-Key", "sprint-fixture-key")
			handler.ServeHTTP(rr, req)
			var result struct {
				Running   bool    `json:"running"`
				Job       string  `json:"job_id"`
				Progress  string  `json:"progress"`
				Started   string  `json:"started_at"`
				Elapsed   float64 `json:"elapsed"`
				Completed int     `json:"tasks_completed"`
				Total     int     `json:"tasks_total"`
				Task      string  `json:"current_task"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if rr.Code != 200 || result.Running != (fixture == "running") || result.Job != "fixture-job" || result.Task != "fixture-task" || result.Completed != 1 || result.Total != 3 {
				t.Fatalf("actual sprint observation rejected/replaced: status=%d body=%s", rr.Code, rr.Body.String())
			}
			wantProgress := map[string]string{"idle": "idle", "running": "running", "finished": "done"}[fixture]
			if result.Progress != wantProgress {
				t.Fatalf("progress evidence missing: %+v", result)
			}
			if fixture == "idle" {
				if result.Started != "" {
					t.Fatalf("unstarted sprint has timestamp: %+v", result)
				}
			} else if _, err := time.Parse(time.RFC3339Nano, result.Started); err != nil {
				t.Fatalf("invalid start evidence: %v", err)
			}
			if fixture == "idle" && result.Elapsed != 0 || fixture != "idle" && result.Elapsed < 1 {
				t.Fatalf("elapsed seconds do not represent initialized start: %s elapsed=%v", fixture, result.Elapsed)
			}
		})
	}
}
