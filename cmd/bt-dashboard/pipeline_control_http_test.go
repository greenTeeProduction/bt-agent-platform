package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestPipelineHTTPRetainsWaitingAndFailedOwnership(t *testing.T) {
	for _, fixture := range []string{"agent-wait", "approval-timeout", "ordinary-failure", "input-write-failure"} {
		t.Run(fixture, func(t *testing.T) {
			isolateExecutionAdmission(t)
			if fixture == "input-write-failure" {
				root := t.TempDir()
				manager := blackboard.DefaultManager()
				if err := manager.EnablePersistence(root); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, "session")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "session"), []byte("blocked fixture directory"), 0600); err != nil {
					t.Fatal(err)
				}
				dashAgentRunner.Blackboards = manager
			}
			var calls atomic.Int64
			oldHook := engine.AuctionDelegateWithContextFn
			t.Cleanup(func() { engine.AuctionDelegateWithContextFn = oldHook })
			engine.AuctionDelegateWithContextFn = func(context.Context, string, map[string]any) (string, bool, error) {
				calls.Add(1)
				return "owned fixture evidence", true, &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("human input required")}
			}
			dashAgentRunner.ResolveTree = func(string) *evolution.SerializableNode {
				if fixture == "ordinary-failure" {
					return &evolution.SerializableNode{Type: "AlwaysFail"}
				}
				return &evolution.SerializableNode{Type: "Action", Name: "AuctionDelegate"}
			}
			oldStore, oldPolicy := hitl.DefaultStore, hitl.GetPolicy()
			t.Cleanup(func() { hitl.DefaultStore = oldStore; hitl.SetPolicy(oldPolicy) })
			if _, err := hitl.InitStore(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Minute})
			kind := "agent"
			timeout := ""
			if fixture == "approval-timeout" {
				kind = "approval"
				timeout = "    timeout: 30ms\n"
			}
			yaml := "name: control-fixture\nsteps:\n  - id: nested\n    kind: subworkflow\n    steps:\n      - id: owned\n        kind: " + kind + "\n        agent: fixture\n        input: fixture task\n"
			if timeout != "" {
				yaml += "        timeout: 30ms\n"
			}
			yaml += "        on_failure: retry\n      - id: must-not-run\n        kind: agent\n        agent: later\n        input: unapproved operation\n"
			root := agent.WorkflowsDir()
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "control-fixture.yaml"), []byte(yaml), 0600); err != nil {
				t.Fatal(err)
			}
			validator := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})
			run := validator(http.HandlerFunc(handlePipelineRun))
			status := validator(http.HandlerFunc(handlePipelineStatus))
			started := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/pipelines/run", bytes.NewBufferString(`{"pipeline_name":"control-fixture","input":"fixture"}`))
			run.ServeHTTP(started, req)
			if started.Code != http.StatusAccepted {
				t.Fatalf("start=%d %s", started.Code, started.Body.String())
			}
			var accepted struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal(started.Body.Bytes(), &accepted); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pipelineRunsMu.Lock(); delete(pipelineRuns, accepted.RunID); pipelineRunsMu.Unlock() })
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			for {
				rr := httptest.NewRecorder()
				status.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pipelines/status?id="+accepted.RunID, nil))
				if rr.Code != http.StatusOK {
					t.Fatalf("status=%d %s", rr.Code, rr.Body.String())
				}
				var response struct {
					Status, Outcome, Error string
					ErrorKind              string `json:"error_kind"`
					Steps                  []dashboard.StepResult
				}
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Status != "running" {
					if fixture == "input-write-failure" {
						if response.Status != "failed" || response.Outcome != "failure" || response.Error == "" || len(response.Steps) != 0 || calls.Load() != 0 || response.ErrorKind != "" {
							t.Fatalf("failed input admission acknowledged: calls=%d response=%+v", calls.Load(), response)
						}
						break
					}
					wantStatus, wantOutcome := "waiting", "input-required"
					if fixture == "approval-timeout" {
						wantStatus, wantOutcome = "failed", "timeout"
					}
					if fixture == "ordinary-failure" {
						wantStatus, wantOutcome = "failed", "failure"
					}
					if response.Status != wantStatus || response.Outcome != wantOutcome || len(response.Steps) != 1 || len(response.Steps[0].Steps) != 1 || response.Error == "" {
						t.Fatalf("response=%+v", response)
					}
					if fixture != "ordinary-failure" && response.ErrorKind != reliability.ExecutionStoppedKind {
						t.Fatalf("diagnostic lost: %+v", response)
					}
					if fixture == "agent-wait" && calls.Load() != 1 {
						t.Fatalf("operations=%d", calls.Load())
					}
					if fixture == "approval-timeout" && (calls.Load() != 0 || response.Steps[0].Steps[0].HitlRequestID == "") {
						t.Fatalf("operations=%d response=%+v", calls.Load(), response)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("pipeline did not settle")
				case <-time.After(5 * time.Millisecond):
				}
			}
		})
	}
}
