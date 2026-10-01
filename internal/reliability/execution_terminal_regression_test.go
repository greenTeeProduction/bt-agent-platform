package reliability

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRouterCompletedPersistenceDoesNotReplay(t *testing.T) {
	calls := 0
	diagnostic := &ExecutionPersistenceError{Err: errors.New("fixture history timeout")}
	first := NewLocalExecutor("completed", func(context.Context, string, string) (*AgentResult, error) {
		calls++
		return &AgentResult{Outcome: "success", Output: "completed fixture side effect"}, diagnostic
	})
	fallback := NewLocalExecutor("fallback", func(context.Context, string, string) (*AgentResult, error) {
		calls++
		return &AgentResult{Success: true}, nil
	})
	router := NewAgentRouter(first)
	router.SetLocal(fallback)
	result, err := router.Execute(context.Background(), "fixture", "task")
	if calls != 1 || !IsExecutionPersistenceError(err) || result == nil || result.Outcome != "success" {
		t.Fatalf("completed work replayed/lost: calls=%d result=%+v err=%v", calls, result, err)
	}
}

func TestRouterAmbiguousRemoteDoesNotReplay(t *testing.T) {
	for _, probe := range []string{"408", "timeout"} {
		t.Run(probe, func(t *testing.T) {
			var calls atomic.Int64
			gate := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					return
				}
				calls.Add(1)
				if probe == "timeout" {
					select {
					case <-r.Context().Done():
					case <-gate:
					}
					return
				}
				w.WriteHeader(http.StatusRequestTimeout)
			}))
			defer server.Close()
			defer close(gate)
			remote := NewRemoteExecutor(RemoteExecutorConfig{Name: "ambiguous", BaseURL: server.URL, Timeout: 100 * time.Millisecond})
			fallback := NewLocalExecutor("fallback", func(context.Context, string, string) (*AgentResult, error) {
				calls.Add(1)
				return &AgentResult{Success: true}, nil
			})
			router := NewAgentRouter(remote)
			router.SetLocal(fallback)
			policy := DefaultRetryPolicy()
			policy.RetryUnknown = true
			err := policy.ExecuteContext(context.Background(), func() error { _, err := router.Execute(context.Background(), "fixture", "task"); return err })
			if calls.Load() != 1 || !IsExecutionUncertainError(err) {
				t.Fatalf("ambiguous remote work replayed: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestRemoteAmbiguousResponsesNeverReplay(t *testing.T) {
	for _, probe := range []string{"500", "lost", "invalid-json", "missing", "null", "wrong-agent", "wrong-task", "redirect", "truncated", "invalid-kind"} {
		t.Run(probe, func(t *testing.T) {
			var dispatched, redirected, local atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					return
				}
				if r.URL.Path == "/redirected" {
					redirected.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				dispatched.Add(1)
				switch probe {
				case "500":
					w.WriteHeader(http.StatusInternalServerError)
				case "lost":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				case "invalid-json":
					_, _ = w.Write([]byte("not json"))
				case "missing":
					_, _ = w.Write([]byte(`{}`))
				case "null":
					_, _ = w.Write([]byte(`{"agent":"fixture","task":"task","output":"done","duration":0,"success":null}`))
				case "wrong-agent":
					_ = json.NewEncoder(w).Encode(AgentResult{Agent: "another", Task: "task", Success: true})
				case "wrong-task":
					_ = json.NewEncoder(w).Encode(AgentResult{Agent: "fixture", Task: "another", Success: true})
				case "redirect":
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "truncated":
					w.Header().Set("Content-Length", "1000")
					_, _ = w.Write([]byte(`{"agent":"fixture"}`))
				case "invalid-kind":
					_ = json.NewEncoder(w).Encode(AgentResult{Agent: "fixture", Task: "task", Outcome: "success", ErrorKind: "future-kind"})
				}
			}))
			defer server.Close()
			remote := NewRemoteExecutor(RemoteExecutorConfig{Name: "ambiguous", BaseURL: server.URL, Timeout: time.Second})
			router := NewAgentRouter(remote)
			router.SetLocal(NewLocalExecutor("local", func(context.Context, string, string) (*AgentResult, error) {
				local.Add(1)
				return &AgentResult{Success: true}, nil
			}))
			policy := DefaultRetryPolicy()
			policy.RetryUnknown = true
			err := policy.ExecuteContext(context.Background(), func() error { _, err := router.Execute(context.Background(), "fixture", "task"); return err })
			if !IsExecutionUncertainError(err) || dispatched.Load() != 1 || redirected.Load() != 0 || local.Load() != 0 {
				t.Fatalf("ambiguous execution replayed/lost: err=%v dispatched=%d redirect=%d local=%d", err, dispatched.Load(), redirected.Load(), local.Load())
			}
		})
	}
}

func TestRemoteExplicitAdmissionRejectionCanFallBack(t *testing.T) {
	var requests, local atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		requests.Add(1)
		w.Header().Set(ExecutionAdmissionHeader, "false")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	remote := NewRemoteExecutor(RemoteExecutorConfig{Name: "rejected", BaseURL: server.URL, Timeout: time.Second})
	router := NewAgentRouter(remote)
	router.SetLocal(NewLocalExecutor("local", func(context.Context, string, string) (*AgentResult, error) {
		local.Add(1)
		return &AgentResult{Success: true}, nil
	}))
	result, err := router.Execute(context.Background(), "fixture", "task")
	if err != nil || result == nil || !result.Success || requests.Load() != 1 || local.Load() != 1 {
		t.Fatalf("safe fallback lost: result=%+v err=%v requests=%d local=%d", result, err, requests.Load(), local.Load())
	}
}

func TestRemoteCompletedPersistenceDiagnosticRemainsTerminal(t *testing.T) {
	var requests, local atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(AgentResult{Agent: "fixture", Task: "task", Output: "completed evidence", Outcome: "success", QualityScore: .9, Error: "fixture history timeout", ErrorKind: ExecutionPersistenceKind})
	}))
	defer server.Close()
	router := NewAgentRouter(NewRemoteExecutor(RemoteExecutorConfig{Name: "completed", BaseURL: server.URL, Timeout: time.Second}))
	router.SetLocal(NewLocalExecutor("local", func(context.Context, string, string) (*AgentResult, error) {
		local.Add(1)
		return &AgentResult{Success: true}, nil
	}))
	policy := DefaultRetryPolicy()
	policy.RetryUnknown = true
	var result *AgentResult
	err := policy.ExecuteContext(context.Background(), func() error {
		var err error
		result, err = router.Execute(context.Background(), "fixture", "task")
		return err
	})
	if !IsExecutionPersistenceError(err) || result == nil || result.Outcome != "success" || result.Output != "completed evidence" || result.QualityScore != .9 || requests.Load() != 1 || local.Load() != 0 {
		t.Fatalf("completed diagnostic lost/replayed: result=%+v err=%v remote=%d local=%d", result, err, requests.Load(), local.Load())
	}
	if status := router.ExecutorHealthStatus(); len(status) != 1 || status[0].ConsecutiveFailures != 0 {
		t.Fatal("record failure marked completed executor unhealthy")
	}
}

func TestLegacyRetryDoesNotReplayTerminalExecution(t *testing.T) {
	for _, diagnostic := range []error{&ExecutionPersistenceError{Err: errors.New("record timeout")}, &ExecutionUncertainError{Err: context.DeadlineExceeded}} {
		calls := 0
		err := RetryWithBackoff(3, time.Millisecond, time.Millisecond, func() error { calls++; return diagnostic })
		if calls != 1 || !errors.Is(err, diagnostic) {
			t.Fatalf("terminal work repeated: calls=%d err=%v", calls, err)
		}
	}
}

func TestRejectedRemoteThenTerminalLocalFallbackDoesNotReplay(t *testing.T) {
	for _, diagnostic := range []error{&ExecutionPersistenceError{Err: errors.New("history timeout")}, &ExecutionUncertainError{Err: context.DeadlineExceeded}} {
		var local, requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusOK)
				return
			}
			requests.Add(1)
			w.Header().Set(ExecutionAdmissionHeader, "false")
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		router := NewAgentRouter(NewRemoteExecutor(RemoteExecutorConfig{Name: "rejected", BaseURL: server.URL, Timeout: time.Second}))
		router.SetLocal(NewLocalExecutor("terminal", func(context.Context, string, string) (*AgentResult, error) {
			local.Add(1)
			return &AgentResult{Agent: "fixture", Task: "task", Outcome: "success", Output: "local evidence"}, diagnostic
		}))
		policy := DefaultRetryPolicy()
		policy.RetryUnknown = true
		var result *AgentResult
		err := policy.ExecuteContext(context.Background(), func() error {
			var err error
			result, err = router.Execute(context.Background(), "fixture", "task")
			return err
		})
		server.Close()
		if !errors.Is(err, diagnostic) || result == nil || result.Output != "local evidence" || local.Load() != 1 || requests.Load() != 1 {
			t.Fatalf("terminal fallback lost/replayed: result=%+v err=%v local=%d remote=%d", result, err, local.Load(), requests.Load())
		}
	}
}

func TestMixedExecutionDiagnosticsRetainUncertainty(t *testing.T) {
	mixed := errors.Join(&ExecutionPersistenceError{Err: errors.New("completed branch record timeout")}, &ExecutionUncertainError{Err: context.DeadlineExceeded})
	calls := 0
	router := NewAgentRouter(NewLocalExecutor("mixed", func(context.Context, string, string) (*AgentResult, error) {
		calls++
		return &AgentResult{Outcome: "partial"}, mixed
	}))
	router.SetLocal(NewLocalExecutor("must-not-run", func(context.Context, string, string) (*AgentResult, error) { calls++; return nil, nil }))
	_, err := router.Execute(context.Background(), "fixture", "task")
	if !IsExecutionUncertainError(err) || IsExecutionPersistenceError(err) || ExecutionErrorKind(err) != ExecutionUncertainKind || calls != 1 {
		t.Fatalf("joined uncertainty collapsed: err=%v kind=%q calls=%d", err, ExecutionErrorKind(err), calls)
	}
	if status := router.ExecutorHealthStatus(); len(status) != 1 || status[0].ConsecutiveFailures != 1 {
		t.Fatal("unknown branch counted as successful execution")
	}
}

func TestRemoteStoppedDispositionSurvivesRouterAndOuterRetry(t *testing.T) {
	for _, fixture := range []struct {
		name, outcome, kind, detail string
		success, valid              bool
	}{
		{"pause", "input-required", ExecutionStoppedKind, "timeout awaits input", false, true},
		{"failure", "failure", ExecutionStoppedKind, "network timeout settled failure", false, true},
		{"auth", "auth-required", ExecutionStoppedKind, "needs authentication", false, true},
		{"legacy aborted child", "aborted", ExecutionPersistenceKind, "child history failed", false, true},
		{"contradictory healthy", "success", ExecutionStoppedKind, "claimed stop", false, false},
		{"contradictory success flag", "input-required", ExecutionStoppedKind, "claimed stop", true, false},
		{"missing detail", "input-required", ExecutionStoppedKind, "", false, false},
		{"unknown outcome", "future", ExecutionStoppedKind, "claimed stop", false, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var requests, local atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					return
				}
				requests.Add(1)
				_ = json.NewEncoder(w).Encode(AgentResult{Agent: "fixture", Task: "task", Outcome: fixture.outcome, Success: fixture.success, Output: "received evidence", ErrorKind: fixture.kind, Error: fixture.detail})
			}))
			defer server.Close()
			router := NewAgentRouter(NewRemoteExecutor(RemoteExecutorConfig{Name: "fixture", BaseURL: server.URL, Timeout: time.Second}))
			router.SetLocal(NewLocalExecutor("local", func(context.Context, string, string) (*AgentResult, error) {
				local.Add(1)
				return &AgentResult{Success: true}, nil
			}))
			var result *AgentResult
			policy := DefaultRetryPolicy()
			policy.RetryUnknown = true
			err := policy.ExecuteContext(t.Context(), func() error { var err error; result, err = router.Execute(t.Context(), "fixture", "task"); return err })
			if requests.Load() != 1 || local.Load() != 0 || result == nil || result.Output != "received evidence" || IsExecutionStoppedError(err) != fixture.valid || IsExecutionUncertainError(err) == fixture.valid {
				t.Fatalf("requests=%d local=%d result=%+v err=%v", requests.Load(), local.Load(), result, err)
			}
		})
	}
}
