package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

type a2aExecutionOwnerKey struct{}

type executionRPCRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func executionRPCFixture(t *testing.T, handler func(http.ResponseWriter, *http.Request, executionRPCRequest)) string {
	t.Helper()
	var rpcURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-card.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(&protocol.AgentCard{Name: "fixture", SupportedInterfaces: []*protocol.AgentInterface{protocol.NewAgentInterface(rpcURL, protocol.TransportProtocolJSONRPC)}})
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		var req executionRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode RPC: %v", err)
			http.Error(w, "bad fixture request", http.StatusBadRequest)
			return
		}
		handler(w, r, req)
	})
	server := httptest.NewServer(mux)
	rpcURL = server.URL + "/rpc"
	t.Cleanup(server.Close)
	return server.URL
}

func writeExecutionRPC(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func TestA2AUnknownSendCannotReplay(t *testing.T) {
	for _, failure := range []string{"503", "lost response", "malformed response", "redirect"} {
		t.Run(failure, func(t *testing.T) {
			var operations atomic.Int64
			url := executionRPCFixture(t, func(w http.ResponseWriter, r *http.Request, req executionRPCRequest) {
				_, _ = io.Copy(io.Discard, r.Body)
				operations.Add(1) // operation ran before its response became unusable
				switch failure {
				case "503":
					http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				case "lost response":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				case "malformed response":
					writeExecutionRPC(w, req.ID, map[string]any{"broken": true})
				case "redirect":
					w.Header().Set("Location", "/rpc")
					w.WriteHeader(http.StatusTemporaryRedirect)
				}
			})
			c := NewBTAgentClient()
			c.Timeout = time.Second
			err := (&reliability.RetryPolicy{MaxRetries: 3, RetryUnknown: true}).ExecuteContext(context.Background(), func() error { _, err := c.SendTask(context.Background(), url, "fixture operation"); return err })
			if operations.Load() != 1 || !reliability.IsExecutionUncertainError(err) {
				t.Fatalf("operations=%d err=%v", operations.Load(), err)
			}
		})
	}
}

func TestA2AActiveTaskPollsWithoutResending(t *testing.T) {
	for _, mode := range []string{"completed", "deadline", "wrong task", "poll failure"} {
		t.Run(mode, func(t *testing.T) {
			var sends, polls atomic.Int64
			url := executionRPCFixture(t, func(w http.ResponseWriter, r *http.Request, req executionRPCRequest) {
				task := &protocol.Task{ID: "owned-task", ContextID: "owned-context", Status: protocol.TaskStatus{State: protocol.TaskStateWorking}}
				if req.Method == "SendMessage" {
					sends.Add(1)
					writeExecutionRPC(w, req.ID, protocol.StreamResponse{Event: task})
					return
				}
				if req.Method != "GetTask" {
					t.Errorf("unexpected method %s", req.Method)
					return
				}
				polls.Add(1)
				var params protocol.GetTaskRequest
				_ = json.Unmarshal(req.Params, &params)
				if params.ID != task.ID {
					t.Errorf("poll ID=%s", params.ID)
				}
				switch mode {
				case "completed":
					task.Status.State = protocol.TaskStateCompleted
					task.Artifacts = []*protocol.Artifact{{Parts: protocol.ContentParts{protocol.NewTextPart("completed fixture output")}}}
				case "wrong task":
					task.ID = "someone-else"
				case "poll failure":
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				writeExecutionRPC(w, req.ID, task)
			})
			c := NewBTAgentClient()
			c.Timeout = 350 * time.Millisecond
			out, err := c.SendTask(context.Background(), url, "fixture work")
			if sends.Load() != 1 || polls.Load() < 1 {
				t.Fatalf("sends=%d polls=%d err=%v", sends.Load(), polls.Load(), err)
			}
			if mode == "completed" {
				if err != nil || out != "completed fixture output" {
					t.Fatalf("out=%q err=%v", out, err)
				}
			} else if !reliability.IsExecutionUncertainError(err) {
				t.Fatalf("want uncertain: %v", err)
			}
		})
	}
}

func TestA2ACardResolutionSharesCallerBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	c := NewBTAgentClient()
	c.Timeout = 30 * time.Millisecond
	start := time.Now()
	_, err := c.SendTask(context.Background(), server.URL, "fixture")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second || reliability.IsExecutionUncertainError(err) {
		t.Fatalf("card budget err=%v elapsed=%v", err, time.Since(start))
	}
}

func TestA2ASettledStateAndMetadataDoNotTriggerTransportRetry(t *testing.T) {
	for _, state := range []protocol.TaskState{protocol.TaskStateFailed, protocol.TaskStateCanceled, protocol.TaskStateRejected, protocol.TaskStateInputRequired, protocol.TaskStateAuthRequired} {
		t.Run(string(state), func(t *testing.T) {
			var calls atomic.Int64
			url := executionRPCFixture(t, func(w http.ResponseWriter, _ *http.Request, req executionRPCRequest) {
				calls.Add(1)
				writeExecutionRPC(w, req.ID, protocol.StreamResponse{Event: &protocol.Task{ID: "t", Status: protocol.TaskStatus{State: state, Message: protocol.NewMessage(protocol.MessageRoleAgent, protocol.NewTextPart("network timeout requires attention"))}}})
			})
			c := NewBTAgentClient()
			err := (&reliability.RetryPolicy{MaxRetries: 3, RetryUnknown: true}).ExecuteContext(context.Background(), func() error { _, err := c.SendTask(context.Background(), url, "fixture"); return err })
			if calls.Load() != 1 || err == nil || reliability.IsExecutionUncertainError(err) {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
	for _, metadata := range []any{"invalid", map[string]any{"error_kind": "persistence"}, map[string]any{"outcome": "success", "error_kind": "mystery", "error": "problem"}} {
		task := &protocol.Task{ID: "t", Status: protocol.TaskStatus{State: protocol.TaskStateCompleted}, Metadata: map[string]any{executionMetadataKey: metadata}, Artifacts: []*protocol.Artifact{{Parts: protocol.ContentParts{protocol.NewTextPart("output")}}}}
		_, err := interpretSendResult(task)
		if !reliability.IsExecutionUncertainError(err) {
			t.Fatalf("metadata=%v err=%v", metadata, err)
		}
	}
}

func a2aExecutionServer(t *testing.T, action engine.ActionFunc) (*Server, string) {
	t.Helper()
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(agent.Definition{Name: "fixture", Tree: "fixture-tree"}); err != nil {
		t.Fatal(err)
	}
	name := "A2AExecutionFixture" + strings.ReplaceAll(t.Name(), "/", "_") + strconv.FormatInt(time.Now().UnixNano(), 10)
	engine.RegisterAction(name, action)
	server, err := NewServer(reg, nil, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	server.APIKey = "fixture-key"
	server.Executor.TreeMap = map[string]*evolution.SerializableNode{"fixture": {Type: "Action", Name: name}}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	server.BaseURL = httpServer.URL
	if err := server.RefreshCards(); err != nil {
		t.Fatal(err)
	}
	return server, httpServer.URL
}

func TestA2AHistoryFailurePreservesCompletedWorkThroughSDK(t *testing.T) {
	var operations atomic.Int64
	server, url := a2aExecutionServer(t, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		operations.Add(1)
		ctx.Blackboard.Result = "## Result\nFixture completed successfully with an attributable output."
		return 1
	})
	dir := t.TempDir()
	hist, err := agent.NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", filepath.Join(dir, "fixture.jsonl")); err != nil {
		t.Fatal(err)
	}
	server.Executor.History = hist
	c := &BTAgentClient{APIKey: "fixture-key", PlatformURL: url, Timeout: time.Second}
	var output string
	err = (&reliability.RetryPolicy{MaxRetries: 3, RetryUnknown: true}).ExecuteContext(context.Background(), func() error {
		var err error
		output, err = c.SendTask(context.Background(), url+"/agents/fixture", "fixture side effect")
		return err
	})
	if operations.Load() != 1 || !strings.Contains(output, "Fixture completed") || !reliability.IsExecutionPersistenceError(err) {
		t.Fatalf("operations=%d output=%q err=%v", operations.Load(), output, err)
	}
	if len(hist.List("fixture", 0)) != 0 {
		t.Fatal("failed record entered history cache")
	}
}

func TestA2ACancelHistoryFailureRemainsVisible(t *testing.T) {
	dir := t.TempDir()
	hist, err := agent.NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", filepath.Join(dir, "fixture.jsonl")); err != nil {
		t.Fatal(err)
	}
	expectedHistoryErr := hist.Record(agent.RunRecord{AgentName: "fixture", Outcome: "cancelled"})
	if expectedHistoryErr == nil {
		t.Fatal("history fault was not injected")
	}
	executor := &BTAgentExecutor{History: hist}
	execCtx := &a2asrv.ExecutorContext{TaskID: "t", ContextID: "fixture"}
	for event, err := range executor.Cancel(context.Background(), execCtx) {
		if err != nil {
			t.Fatal(err)
		}
		status := event.(*protocol.TaskStatusUpdateEvent)
		task := &protocol.Task{ID: "t", Status: status.Status, Metadata: status.Metadata}
		_, err := interpretSendResult(task)
		if !reliability.IsExecutionStoppedError(err) || !strings.Contains(err.Error(), expectedHistoryErr.Error()) || reliability.IsExecutionPersistenceError(err) {
			t.Fatalf("cancel disposition: %v", err)
		}
	}
}

type terminalAuctionTransport struct {
	calls      atomic.Int64
	diagnostic error
}

func (f *terminalAuctionTransport) SendTask(_ context.Context, _ string, text string) (string, error) {
	if ann, ok := parseAnnouncement(text); ok {
		raw, _ := json.Marshal(Bid{TaskID: ann.TaskID, BidderName: "fixture", Cost: 0, Confidence: 1})
		return string(raw), nil
	}
	f.calls.Add(1)
	return "completed child output", f.diagnostic
}
func TestAuctionTerminalDispatchPreservesAwardAndBreakerDisposition(t *testing.T) {
	for _, diagnostic := range []error{&reliability.ExecutionPersistenceError{Err: errors.New("disk failure")}, &reliability.ExecutionUncertainError{Err: errors.New("lost response")}} {
		f := &terminalAuctionTransport{diagnostic: diagnostic}
		auction := NewAuctioneer(f)
		result, err := auction.RunAuction(context.Background(), TaskAnnouncement{TaskID: "t", Description: "fixture"}, map[string]string{"fixture": "http://fixture"})
		if f.calls.Load() != 1 || result.Award.WinnerName != "fixture" || result.Result != "completed child output" || !errors.Is(err, diagnostic) || errors.Is(err, ErrWinnerDispatchExhausted) {
			t.Fatalf("calls=%d result=%+v err=%v", f.calls.Load(), result, err)
		}
		failures := auction.winnerBreaker("fixture").FailureCount()
		if reliability.IsExecutionPersistenceError(diagnostic) && failures != 0 {
			t.Fatalf("healthy child charged breaker: %d", failures)
		}
	}
}

func TestA2AAsyncRequestEndsButExplicitCancelStopsCooperativeTree(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server, url := a2aExecutionServer(t, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		close(started)
		<-ctx.Done()
		close(stopped)
		ctx.Blackboard.Result = "fixture cooperative cancellation"
		return -1
	})
	card := server.cardCacheSnapshot()["fixture"]
	client, err := a2aclient.NewFromCard(context.Background(), card, a2aclient.WithJSONRPCTransport(&http.Client{Transport: &platformKeyTransport{key: "fixture-key", platformURL: url, sourceURL: url}}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := client.SendMessage(ctx, &protocol.SendMessageRequest{Message: protocol.NewMessage(protocol.MessageRoleUser, protocol.NewTextPart("fixture")), Config: &protocol.SendMessageConfig{ReturnImmediately: true}})
	if err != nil {
		t.Fatal(err)
	}
	task, ok := response.(*protocol.Task)
	if !ok || task.ID == "" {
		t.Fatalf("response=%+v", response)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("task did not start")
	}
	select {
	case <-stopped:
		t.Fatal("HTTP completion canceled asynchronous owner")
	default:
	}
	canceled, err := client.CancelTask(ctx, &protocol.CancelTaskRequest{ID: task.ID})
	if err != nil || canceled.Status.State != protocol.TaskStateCanceled {
		t.Fatalf("cancel=%+v err=%v", canceled, err)
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("explicit SDK cancellation did not stop cooperative action")
	}
}

func TestA2ASDKPreservesNestedStopEvidenceAndHonestWinnerHealth(t *testing.T) {
	old, legacy := engine.AuctionDelegateWithContextFn, engine.AuctionDelegateFn
	defer func() { engine.AuctionDelegateWithContextFn = old; engine.AuctionDelegateFn = legacy }()
	for _, kind := range []string{"persistence", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			var operations atomic.Int64
			var diagnostic error = &reliability.ExecutionPersistenceError{Err: errors.New("nested child record failed")}
			if kind == "uncertain" {
				diagnostic = &reliability.ExecutionUncertainError{Err: errors.New("nested child response lost")}
			}
			engine.AuctionDelegateWithContextFn = func(context.Context, string, map[string]any) (string, bool, error) {
				operations.Add(1)
				return "nested child evidence", true, diagnostic
			}
			_, url := a2aExecutionServer(t, engine.GetAction("AuctionDelegate"))
			c := &BTAgentClient{APIKey: "fixture-key", PlatformURL: url, Timeout: time.Second}
			output, err := c.SendTask(context.Background(), url+"/agents/fixture", "allocate fixture")
			if operations.Load() != 1 || output != "nested child evidence" || !reliability.IsExecutionTerminalError(err) {
				t.Fatalf("operations=%d output=%q err=%v", operations.Load(), output, err)
			}
			if kind == "uncertain" && !reliability.IsExecutionUncertainError(err) {
				t.Fatalf("lost uncertainty: %v", err)
			}
			if kind == "persistence" && (!reliability.IsExecutionStoppedError(err) || reliability.IsExecutionPersistenceError(err) || reliability.ExecutionStopOutcome(err) != "aborted") {
				t.Fatalf("lost persistence stop: %v", err)
			}
			f := &terminalAuctionTransport{diagnostic: err}
			auction := NewAuctioneer(f)
			result, err := auction.RunAuction(context.Background(), TaskAnnouncement{TaskID: "t", Description: "fixture"}, map[string]string{"fixture": "http://fixture"})
			cb := auction.winnerBreaker("fixture")
			if f.calls.Load() != 1 || result.Award.WinnerName != "fixture" || !reliability.IsExecutionTerminalError(err) || cb.FailureCount() != 1 || cb.SuccessCount() != 0 {
				t.Fatalf("calls=%d award=%+v err=%v failures=%d successes=%d", f.calls.Load(), result.Award, err, cb.FailureCount(), cb.SuccessCount())
			}
		})
	}
}

type executionTransportFunc func(context.Context, string, string) (string, error)

func (f executionTransportFunc) SendTask(ctx context.Context, url, text string) (string, error) {
	return f(ctx, url, text)
}

func TestAuctionDelegateProductionHookKeepsCallerCancellation(t *testing.T) {
	old := newAuctionCollector
	defer func() { newAuctionCollector = old }()
	key := a2aExecutionOwnerKey{}
	newAuctionCollector = func() BidCollector {
		return executionTransportFunc(func(ctx context.Context, _ string, _ string) (string, error) {
			if ctx.Value(key) != "owner" {
				t.Error("auction caller context lost")
			}
			<-ctx.Done()
			return "", ctx.Err()
		})
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), key, "owner"), 30*time.Millisecond)
	defer cancel()
	_, awarded, err := AuctionDelegateWithContext(ctx, "fixture", map[string]any{"auction_candidates": map[string]string{"fixture": "http://fixture"}})
	if awarded || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation became fallback: awarded=%v err=%v", awarded, err)
	}
}

func TestA2AContradictorySettledMetadataIsUncertain(t *testing.T) {
	for _, fixture := range []struct {
		state   protocol.TaskState
		outcome string
		valid   bool
	}{
		{protocol.TaskStateCompleted, "failure", false},
		{protocol.TaskStateCompleted, "input-required", false},
		{protocol.TaskStateCompleted, "success", true},
		{protocol.TaskStateCompleted, string(protocol.TaskStateCompleted), true},
		{protocol.TaskStateInputRequired, "success", false},
		{protocol.TaskStateInputRequired, "pending_approval", true},
		{protocol.TaskStateInputRequired, string(protocol.TaskStateInputRequired), true},
		{protocol.TaskStateFailed, "goap_fusion_rate_limited", true},
	} {
		t.Run(string(fixture.state)+"/"+fixture.outcome, func(t *testing.T) {
			task := &protocol.Task{ID: "owner", Status: protocol.TaskStatus{State: fixture.state}, Metadata: map[string]any{executionMetadataKey: map[string]any{"outcome": fixture.outcome, "error_kind": "", "error": ""}}, Artifacts: []*protocol.Artifact{{Parts: protocol.ContentParts{protocol.NewTextPart("received evidence")}}}}
			output, err := interpretSendResult(task)
			if output != "received evidence" || reliability.IsExecutionUncertainError(err) == fixture.valid {
				t.Fatalf("output=%q err=%v valid=%v", output, err, fixture.valid)
			}
			if fixture.valid && fixture.state != protocol.TaskStateCompleted && !reliability.IsExecutionStoppedError(err) {
				t.Fatalf("lost known disposition: %v", err)
			}
		})
	}
}
