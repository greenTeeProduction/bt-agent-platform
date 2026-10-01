package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	btcore "github.com/rvitorper/go-bt/core"
)

func TestSprintRejectsClosedExecutionPoolBeforeClaiming(t *testing.T) {
	isolateSprintPersistence(t)
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "capacity-fixture", Title: "capacity", Assignee: "capacity-agent", TreeID: "capacity", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	name := fmt.Sprintf("SprintCapacityProbe%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		calls.Add(1)
		ctx.Blackboard.Result = `{"status":"ok","result":"Local capacity fixture completed despite a closed worker pool; no provider was invoked."}`
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	dashWorkerPool.Shutdown()
	rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute")
	waitSprintFixture(t)
	stored, _ := taskStore.Get("capacity-fixture")
	if rr.Code != 503 || calls.Load() != 0 || stored.Status != "approved" {
		t.Fatalf("closed execution pool did not stop sprint admission: status=%d actions=%d task_status=%s body=%s", rr.Code, calls.Load(), stored.Status, rr.Body.String())
	}
}

func TestSprintAdmissionDeadlineDoesNotClaimOrBlockStatus(t *testing.T) {
	for _, blocked := range []string{"limiter", "serialization", "queue"} {
		t.Run(blocked, func(t *testing.T) {
			isolateSprintPersistence(t)
			taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
			if err := taskStore.Create(dashboard.Task{ID: "waiting", Status: "approved"}); err != nil {
				t.Fatal(err)
			}
			switch blocked {
			case "limiter":
				dashConcurrencyLimiter.Acquire()
				defer dashConcurrencyLimiter.Release()
			case "serialization":
				sprintAdmission.Acquire()
				defer sprintAdmission.Release()
			case "queue":
				gate := make(chan struct{})
				started := make(chan struct{})
				if !dashWorkerPool.Submit(func() { close(started); <-gate }) {
					t.Fatal("fixture pool closed")
				}
				<-started
				for range 100 {
					if !dashWorkerPool.Submit(func() {}) {
						t.Fatal("fixture queue rejected")
					}
				}
				defer close(gate)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "/api/sprint/execute", strings.NewReader(`{}`)).WithContext(ctx)
			handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(handleSprintExecute))
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() { rr := httptest.NewRecorder(); handler.ServeHTTP(rr, req); result <- rr }()
			// Observe the specific capacity waiter before proving status remains readable.
			observeDeadline := time.Now().Add(time.Second)
			for {
				active, waiting, _ := dashConcurrencyLimiter.Stats()
				_, serializedWaiting, _ := sprintAdmission.Stats()
				if blocked == "limiter" && waiting > 0 || blocked == "serialization" && serializedWaiting > 0 || blocked == "queue" && active > 0 {
					break
				}
				if time.Now().After(observeDeadline) {
					t.Fatal("admission did not reach its capacity wait")
				}
				time.Sleep(time.Millisecond)
			}
			status := sprintFixtureHTTP(t, http.MethodGet, "/api/sprint/status")
			if status.Code != 200 {
				t.Fatalf("waiting admission blocked status: %d", status.Code)
			}
			select {
			case rr := <-result:
				if rr.Code != 408 || rr.Header().Get(reliability.ExecutionAdmissionHeader) != "false" {
					t.Fatalf("canceled admission disposition lost: %d %s", rr.Code, rr.Body.String())
				}
			case <-time.After(time.Second):
				t.Fatal("admission ignored caller deadline")
			}
			stored, _ := taskStore.Get("waiting")
			if stored.Status != "approved" || stored.Output != "" {
				t.Fatalf("canceled waiter claimed work: %+v", stored)
			}
			active, waiting, _ := dashConcurrencyLimiter.Stats()
			expected := 0
			if blocked == "limiter" {
				expected = 1
			}
			if active != expected || waiting != 0 {
				t.Fatalf("rejected admission leaked another owner's capacity: %d/%d", active, waiting)
			}
		})
	}
}

func TestAcceptedSprintDetachesHTTPAndRetainsCapacityUntilCleanup(t *testing.T) {
	isolateSprintPersistence(t)
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "accepted", Title: "accepted", Status: "approved", Assignee: "owned-sprint"}); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	started := make(chan struct{})
	actionContext := make(chan context.Context, 1)
	name := fmt.Sprintf("SprintOwnedContext%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		actionContext <- ctx.Context
		close(started)
		<-gate
		ctx.Blackboard.Result = `{"status":"ok","result":"Accepted local sprint retains a complete result independently of the HTTP caller disconnecting."}`
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rr := httptest.NewRecorder()
	handleSprintExecute(rr, httptest.NewRequest(http.MethodPost, "/api/sprint/execute", strings.NewReader(`{}`)).WithContext(ctx))
	if rr.Code != 200 {
		close(gate)
		t.Fatalf("admission: %d %s", rr.Code, rr.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		close(gate)
		t.Fatal("owned fixture never started")
	}
	cancel()
	treeContext := <-actionContext
	if treeContext.Err() != nil {
		close(gate)
		t.Fatal("successful asynchronous admission remained HTTP-owned")
	}
	active, _, _ := dashConcurrencyLimiter.Stats()
	if active != 1 {
		close(gate)
		t.Fatal("running sprint lost shared capacity")
	}
	sprintState.Lock()
	deadline := sprintState.Deadline
	sprintState.Unlock()
	if remaining := time.Until(deadline); remaining < 4*time.Minute || remaining > 5*time.Minute {
		close(gate)
		t.Fatalf("missing owned five-minute deadline: %v", remaining)
	}
	close(gate)
	waitSprintFixture(t)
	stored, _ := taskStore.Get("accepted")
	if stored.Status != "completed" || stored.Output == "" {
		t.Fatalf("caller disconnect lost observed completion: %+v", stored)
	}
	deadlineWait := time.Now().Add(time.Second)
	for dashConcurrencyLimiter.Available() != 1 && time.Now().Before(deadlineWait) {
		time.Sleep(time.Millisecond)
	}
	if dashConcurrencyLimiter.Available() != 1 {
		t.Fatal("completed sprint leaked capacity")
	}
}

func TestSprintBudgetDoesNotReleaseRunningWorkOrTickRemainingClaims(t *testing.T) {
	isolateSprintPersistence(t)
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	for _, id := range []string{"started", "remaining"} {
		if err := taskStore.Create(dashboard.Task{ID: id, Title: id, Assignee: "batch-budget-fixture", Status: "approved"}); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := taskStore.ClaimApproved()
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	gate := make(chan struct{})
	started := make(chan struct{})
	name := fmt.Sprintf("SprintBudgetFixture%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		calls.Add(1)
		close(started)
		<-gate
		ctx.Blackboard.Result = `{"status":"ok","result":"The admitted local action finished after its budget; its original output must still be preserved."}`
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	reservation, err := reserveSprint(context.Background(), taskStore, newAgentExecutor())
	if err != nil {
		close(gate)
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	sprintState.Lock()
	sprintState.Running = true
	sprintState.Unlock()
	reservation.once.Do(func() { reservation.decision <- &sprintBatch{ctx: ctx, cancel: cancel, tasks: claimed} })
	select {
	case <-started:
	case <-time.After(time.Second):
		close(gate)
		t.Fatal("budget fixture never started")
	}
	<-ctx.Done()
	active, _, _ := dashConcurrencyLimiter.Stats()
	sprintState.Lock()
	running := sprintState.Running
	sprintState.Unlock()
	if active != 1 || !running {
		close(gate)
		t.Fatal("expired budget released capacity while action was still running")
	}
	close(gate)
	waitSprintFixture(t)
	first, _ := taskStore.Get("started")
	remaining, _ := taskStore.Get("remaining")
	if calls.Load() != 1 || first.Status != "completed" || first.Output == "" || remaining.Status != "approved" || remaining.Outcome != "not_started" || remaining.RunID != "" {
		t.Fatalf("budget lost completion or dispatched unstarted work: calls=%d first=%+v remaining=%+v", calls.Load(), first, remaining)
	}
	rr := sprintFixtureHTTP(t, http.MethodGet, "/api/sprint/status")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"progress":"failed"`) || !strings.Contains(rr.Body.String(), `"outcome":"not_started"`) {
		t.Fatal(rr.Body.String())
	}
}
