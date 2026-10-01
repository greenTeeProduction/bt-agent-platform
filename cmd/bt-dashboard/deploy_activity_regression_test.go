package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestDashboardDriftGuardRetainsDetachedSprint(t *testing.T) {
	isolateSprintPersistence(t)
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "drift-owned", Title: "drift", Assignee: "owned", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	started, stop := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	t.Cleanup(func() { release(); dashWorkerPool.Shutdown() })
	name := fmt.Sprintf("DriftOwnedAction%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		close(started)
		<-stop
		ctx.Blackboard.Result = `{"status":"ok","result":"Completed local owned drift fixture with no provider invocation."}`
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	rr := httptest.NewRecorder()
	inFlightMiddleware(http.HandlerFunc(handleSprintExecute)).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/sprint/execute", strings.NewReader(`{}`)))
	if rr.Code != 200 {
		t.Fatalf("admission: %d %s", rr.Code, rr.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("action did not start")
	}
	if dashRequestsInFlight.Load() != 0 {
		t.Fatal("HTTP request did not finish")
	}
	if !dashAnyInFlight() {
		t.Fatal("guard reports idle while detached sprint action is running")
	}
	release()
	dashWorkerPool.Shutdown()
	waitSprintFixture(t)
	if dashAnyInFlight() {
		t.Fatal("completed cleanup remains in flight")
	}
}

func waitDashboardIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for dashAnyInFlight() {
		if time.Now().After(deadline) {
			t.Fatal("dashboard execution ownership did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDashboardRestartSealRejectsAdmissionUntilFailedHandoff(t *testing.T) {
	old := dashActivity
	dashActivity = &dashboardActivityGate{}
	t.Cleanup(func() { dashActivity = old })
	finish, ready := dashActivity.beginRestart()
	if !ready {
		t.Fatal("idle dashboard cannot seal")
	}
	defer finish(false)
	var calls int
	handler := inFlightMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rr.Code != 503 || calls != 0 || rr.Header().Get("X-BT-Execution-Admitted") != "false" {
		t.Fatalf("sealed HTTP admitted: %d %d %v", rr.Code, calls, rr.Header())
	}
	if _, err := dashActivity.acquire(); err == nil {
		t.Fatal("background admitted during restart handoff")
	}
	finish(false)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rr.Code != 200 || calls != 1 {
		t.Fatal("failed handoff did not reopen admission")
	}
	finish, ready = dashActivity.beginRestart()
	if !ready {
		t.Fatal("completed request retained ownership")
	}
	finish(true)
	finish(false)
	if _, err := dashActivity.acquire(); err == nil {
		t.Fatal("accepted asynchronous restart reopened before exit")
	}
}

func TestDashboardRestartSealCannotRaceOwnedAdmission(t *testing.T) {
	for range 200 {
		gate := &dashboardActivityGate{}
		start := make(chan struct{})
		admitted := make(chan func(), 1)
		sealed := make(chan func(bool), 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			release, err := gate.acquire()
			if err == nil {
				admitted <- release
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			finish, ready := gate.beginRestart()
			if ready {
				sealed <- finish
			}
		}()
		close(start)
		wg.Wait()
		if len(admitted)+len(sealed) != 1 {
			t.Fatal("work admission and restart seal overlapped or neither acquired")
		}
		select {
		case release := <-admitted:
			release()
		default:
		}
		select {
		case finish := <-sealed:
			finish(false)
		default:
		}
	}
}

func TestDashboardDriftGuardRetainsCanceledFallbackExecution(t *testing.T) {
	isolateExecutionAdmission(t)
	dashWorkerPool.Shutdown()
	dashWorkerPool = nil
	dashConcurrencyLimiter = nil
	started, stop := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	t.Cleanup(func() { release(); waitDashboardIdle(t) })
	name := fmt.Sprintf("DriftCanceledAction%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		close(started)
		<-stop
		ctx.Blackboard.Result = "Completed local held cancellation fixture."
		return 1
	})
	dashAgentRunner = &agent.RunDeps{ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		inFlightMiddleware(http.HandlerFunc(handleAgentExecute)).ServeHTTP(rr, admissionRequest(ctx, "/api/agents/execute"))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("action did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled HTTP did not return")
	}
	if rr.Code != 408 || dashRequestsInFlight.Load() != 0 || !dashAnyInFlight() {
		t.Fatal("HTTP cancellation hid still-running fallback work")
	}
	if finish, ready := dashActivity.beginRestart(); ready {
		finish(false)
		t.Fatal("restart sealed while action still running")
	}
	release()
	waitDashboardIdle(t)
}

func TestDashboardRestartResponseMatchesAllRouteSchemas(t *testing.T) {
	rr := httptest.NewRecorder()
	writeDashboardRestarting(rr)
	for _, route := range api.DashboardRoutes() {
		if violations := api.ValidateResponse(&route, rr.Code, rr.Body.Bytes()); len(violations) != 0 {
			t.Fatalf("%s %s restart response: %v", route.Method, route.Path, violations)
		}
	}
}

func TestDashboardDriftGuardRetainsDetachedPipeline(t *testing.T) {
	root := isolatePipelinePaths(t)
	fixture := "name: drift-owned\nsteps:\n  - id: owned\n    kind: agent\n    agent: fixture\n    input: local drift fixture\n"
	if err := os.WriteFile(filepath.Join(root, "drift-owned.yaml"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	started, stop := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	t.Cleanup(func() { release(); waitDashboardIdle(t) })
	name := fmt.Sprintf("DriftPipelineAction%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		close(started)
		<-stop
		ctx.Blackboard.Result = "Completed local owned pipeline fixture without provider invocation."
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/run", strings.NewReader(`{"pipeline_name":"drift-owned"}`))
	req.Header.Set("X-API-Key", "drift-fixture-key")
	inFlightMiddleware(dashboardMux("drift-fixture-key")).ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("pipeline admission: %d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("pipeline action did not start")
	}
	if dashRequestsInFlight.Load() != 0 || !dashAnyInFlight() {
		t.Fatal("completed HTTP hid detached pipeline work")
	}
	if finish, ready := dashActivity.beginRestart(); ready {
		finish(false)
		t.Fatal("restart sealed during detached pipeline execution")
	}
	release()
	waitAndRemoveFixturePipeline(t, accepted.RunID)
	waitDashboardIdle(t)
}
