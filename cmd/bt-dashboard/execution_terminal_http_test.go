package main

import (
	"context"
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
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestRemoteDashboardHistoryFailureNeverReplays(t *testing.T) {
	called := isolateExecutionAdmission(t)
	historyRoot := t.TempDir()
	history, err := agent.NewHistory(historyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", filepath.Join(historyRoot, "admission-probe.jsonl")); err != nil {
		t.Fatal(err)
	}
	dashAgentRunner.History = history
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(handleAgentExecute))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	router := reliability.NewAgentRouter(reliability.NewRemoteExecutor(reliability.RemoteExecutorConfig{Name: "dashboard", BaseURL: server.URL, Timeout: time.Second}))
	var fallback atomic.Int64
	router.SetLocal(reliability.NewLocalExecutor("fallback", func(context.Context, string, string) (*reliability.AgentResult, error) {
		fallback.Add(1)
		return &reliability.AgentResult{Success: true}, nil
	}))
	policy := reliability.DefaultRetryPolicy()
	policy.RetryUnknown = true
	var result *reliability.AgentResult
	err = policy.ExecuteContext(context.Background(), func() error {
		var err error
		result, err = router.Execute(context.Background(), "admission-probe", "fixture task")
		return err
	})
	if !reliability.IsExecutionPersistenceError(err) || result == nil || result.Outcome != "success" || result.ErrorKind != reliability.ExecutionPersistenceKind || result.QualityScore <= 0 || called.Load() != 1 || fallback.Load() != 0 {
		t.Fatalf("completed dashboard run lost/replayed: result=%+v err=%v calls=%d fallback=%d", result, err, called.Load(), fallback.Load())
	}
}

func TestRemoteDashboardStoppedDispositionNeverReplays(t *testing.T) {
	for _, outcome := range []string{"input-required", "auth-required", "failure", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			isolateExecutionAdmission(t)
			old := engine.AuctionDelegateWithContextFn
			t.Cleanup(func() { engine.AuctionDelegateWithContextFn = old })
			var operations atomic.Int64
			engine.AuctionDelegateWithContextFn = func(context.Context, string, map[string]any) (string, bool, error) {
				operations.Add(1)
				return "owned fixture evidence", true, &reliability.ExecutionStoppedError{Outcome: outcome, Err: errors.New("network timeout requires owner decision")}
			}
			dashAgentRunner.ResolveTree = func(string) *evolution.SerializableNode {
				return &evolution.SerializableNode{Type: "Action", Name: "AuctionDelegate"}
			}
			handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(handleAgentExecute))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/health" {
					w.WriteHeader(http.StatusOK)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			router := reliability.NewAgentRouter(reliability.NewRemoteExecutor(reliability.RemoteExecutorConfig{Name: "dashboard", BaseURL: server.URL, Timeout: time.Second}))
			var fallback atomic.Int64
			router.SetLocal(reliability.NewLocalExecutor("fallback", func(context.Context, string, string) (*reliability.AgentResult, error) {
				fallback.Add(1)
				return &reliability.AgentResult{Success: true}, nil
			}))
			policy := reliability.DefaultRetryPolicy()
			policy.RetryUnknown = true
			var result *reliability.AgentResult
			err := policy.ExecuteContext(t.Context(), func() error {
				var err error
				result, err = router.Execute(t.Context(), "admission-probe", "fixture task")
				return err
			})
			if operations.Load() != 1 || fallback.Load() != 0 || !reliability.IsExecutionStoppedError(err) || result == nil || result.Outcome != outcome || result.Success || result.ErrorKind != reliability.ExecutionStoppedKind || result.Output != "owned fixture evidence" {
				t.Fatalf("operations=%d fallback=%d result=%+v err=%v", operations.Load(), fallback.Load(), result, err)
			}
		})
	}
}
