package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

var admissionEndpoints = []struct {
	path    string
	handler http.HandlerFunc
}{
	{"/api/agents/execute", handleAgentExecute},
	{"/api/agents/run", handleAgentRun},
}

var admissionActionID atomic.Uint64

func isolateExecutionAdmission(t *testing.T) *atomic.Int64 {
	t.Helper()
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	oldRunner, oldPool, oldLimiter := dashAgentRunner, dashWorkerPool, dashConcurrencyLimiter
	t.Cleanup(func() { dashAgentRunner, dashWorkerPool, dashConcurrencyLimiter = oldRunner, oldPool, oldLimiter })
	called := &atomic.Int64{}
	dashAgentRunner = &agent.RunDeps{ResolveTree: func(string) *evolution.SerializableNode {
		called.Add(1)
		return &evolution.SerializableNode{Type: "AlwaysSucceed"}
	}}
	dashConcurrencyLimiter = reliability.NewConcurrencyLimiter(1)
	dashWorkerPool = reliability.NewWorkerPool(1)
	pool := dashWorkerPool
	t.Cleanup(pool.Shutdown)
	return called
}

func admissionRequest(ctx context.Context, path string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"agent":"admission-probe","task":"fixture task"}`))
	request.Header.Set("Content-Type", "application/json")
	return request.WithContext(ctx)
}

func serveAdmission(t *testing.T, handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	select {
	case <-done:
		return response
	case <-time.After(time.Second):
		t.Fatal("execution HTTP handler did not return")
		return nil
	}
}

func TestExecutionEndpointsRejectClosedPoolAndReleaseReservation(t *testing.T) {
	for _, endpoint := range admissionEndpoints {
		t.Run(endpoint.path, func(t *testing.T) {
			called := isolateExecutionAdmission(t)
			dashWorkerPool.Shutdown()
			handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(endpoint.handler)
			response := serveAdmission(t, handler, admissionRequest(context.Background(), endpoint.path))
			if response.Code != http.StatusServiceUnavailable || called.Load() != 0 {
				t.Fatalf("rejected task executed/acknowledged: %d %s calls=%d", response.Code, response.Body.String(), called.Load())
			}
			if response.Header().Get(reliability.ExecutionAdmissionHeader) != "false" {
				t.Fatal("closed-pool rejection lacks explicit admission evidence")
			}
			active, waiting, _ := dashConcurrencyLimiter.Stats()
			if active != 0 || waiting != 0 || dashConcurrencyLimiter.Available() != 1 {
				t.Fatal("closed pool rejection retained reservation")
			}
		})
	}
}

func TestExecutionEndpointsCancelWaitingReservationWithoutExecution(t *testing.T) {
	for _, endpoint := range admissionEndpoints {
		t.Run(endpoint.path, func(t *testing.T) {
			called := isolateExecutionAdmission(t)
			dashConcurrencyLimiter.Acquire()
			defer dashConcurrencyLimiter.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(endpoint.handler)
			response := serveAdmission(t, handler, admissionRequest(ctx, endpoint.path))
			if response.Code != http.StatusRequestTimeout || called.Load() != 0 {
				t.Fatalf("canceled waiter executed: %d %s calls=%d", response.Code, response.Body.String(), called.Load())
			}
			if response.Header().Get(reliability.ExecutionAdmissionHeader) != "" {
				t.Fatal("cancellation incorrectly asserts work was never admitted")
			}
			active, waiting, total := dashConcurrencyLimiter.Stats()
			if active != 1 || waiting != 0 || total != 1 {
				t.Fatal("cancellation changed an existing slot owner")
			}
		})
	}
}

func TestCanceledQueuedExecutionIsSkippedWhenPoolDrains(t *testing.T) {
	called := isolateExecutionAdmission(t)
	gate, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	dashWorkerPool.Submit(func() { close(started); <-gate })
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	response := serveAdmission(t, http.HandlerFunc(handleAgentExecute), admissionRequest(ctx, "/api/agents/execute"))
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("queued cancellation status=%d", response.Code)
	}
	release()
	dashWorkerPool.Shutdown()
	if called.Load() != 0 || dashConcurrencyLimiter.Available() != 1 {
		t.Fatal("canceled queued task executed or retained its reservation")
	}
}

func TestCanceledHTTPDoesNotReleaseRunningSlotOrReplayCompletedWork(t *testing.T) {
	for _, withPool := range []bool{true, false} {
		t.Run(map[bool]string{true: "pool", false: "fallback"}[withPool], func(t *testing.T) {
			isolateExecutionAdmission(t)
			pool := dashWorkerPool
			if !withPool {
				dashWorkerPool = nil
			}
			gate, started := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			t.Cleanup(func() {
				release()
				deadline := time.Now().Add(2 * time.Second)
				for dashConcurrencyLimiter.Available() != 1 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
			})
			var executed atomic.Int64
			actionName := fmt.Sprintf("DashboardUncooperativeAdmissionProbe_%d", admissionActionID.Add(1))
			engine.RegisterAction(actionName, func(ctx *btcore.BTContext[engine.Blackboard]) int {
				executed.Add(1)
				close(started)
				<-gate
				ctx.Blackboard.Result = "## Completion\nStatus: completed fixture side effect."
				return 1
			})
			history, err := agent.NewHistory(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			dashAgentRunner = &agent.RunDeps{History: history, ResolveTree: func(string) *evolution.SerializableNode {
				return &evolution.SerializableNode{Type: "Action", Name: actionName}
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { handleAgentExecute(response, admissionRequest(ctx, "/api/agents/execute")); close(done) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("execution did not start")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("canceled HTTP waiter did not return")
			}
			if response.Code != http.StatusRequestTimeout || dashConcurrencyLimiter.TryAcquire() {
				t.Fatal("HTTP cancellation released a slot still used by execution")
			}
			release()
			pool.Shutdown()
			deadline := time.Now().Add(time.Second)
			for dashConcurrencyLimiter.Available() != 1 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if executed.Load() != 1 || dashConcurrencyLimiter.Available() != 1 {
				t.Fatal("completed work replayed or its reservation leaked")
			}
			records := history.List("admission-probe", 10)
			if len(records) != 1 || records[0].Outcome != "success" {
				t.Fatalf("completed work lost/changed its recorded outcome: %+v", records)
			}
		})
	}
}

func TestExecutionPanicProducesTerminalResponseAndReleasesSlot(t *testing.T) {
	isolateExecutionAdmission(t)
	dashAgentRunner.ResolveTree = func(string) *evolution.SerializableNode { panic("fixture resolver panic") }
	response := serveAdmission(t, http.HandlerFunc(handleAgentExecute), admissionRequest(context.Background(), "/api/agents/execute"))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "panic") || dashConcurrencyLimiter.Available() != 1 {
		t.Fatalf("execution panic lost result/slot: %d %s", response.Code, response.Body.String())
	}
}
