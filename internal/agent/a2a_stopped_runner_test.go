package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/a2aproject/a2a-go/v2/a2a"
	transport "github.com/nico/go-bt-evolve/internal/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestSDKStoppedTaskCannotReplayThroughRunOnce(t *testing.T) {
	for _, fixture := range []struct {
		state   protocol.TaskState
		outcome string
	}{
		{protocol.TaskStateFailed, "failure"},
		{protocol.TaskStateCanceled, "cancelled"},
		{protocol.TaskStateRejected, "rejected"},
		{protocol.TaskStateInputRequired, "input-required"},
		{protocol.TaskStateAuthRequired, "auth-required"},
	} {
		t.Run(string(fixture.state), func(t *testing.T) {
			var url string
			var calls atomic.Int64
			const evidence = "network timeout requires owner attention"
			mux := http.NewServeMux()
			mux.HandleFunc("/.well-known/agent-card.json", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(&protocol.AgentCard{Name: "fixture", SupportedInterfaces: []*protocol.AgentInterface{protocol.NewAgentInterface(url+"/rpc", protocol.TransportProtocolJSONRPC)}})
			})
			mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture request", http.StatusBadRequest)
					return
				}
				if req.Method != "SendMessage" {
					t.Errorf("unexpected RPC %s", req.Method)
				}
				calls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": protocol.StreamResponse{Event: &protocol.Task{ID: "settled-owner", ContextID: "owner-context", Status: protocol.TaskStatus{State: fixture.state, Message: protocol.NewMessage(protocol.MessageRoleAgent, protocol.NewTextPart(evidence))}}}})
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			url = server.URL
			old := engine.AuctionDelegateWithContextFn
			defer func() { engine.AuctionDelegateWithContextFn = old }()
			client := transport.NewBTAgentClient()
			engine.AuctionDelegateWithContextFn = func(ctx context.Context, task string, _ map[string]any) (string, bool, error) {
				output, err := client.SendTask(ctx, url, task)
				return output, true, err
			}
			tree := &evolution.SerializableNode{Type: "Selector", Children: []evolution.SerializableNode{
				{Type: "Retry", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "AuctionDelegate"}}},
				{Type: "Action", Name: "AuctionDelegate"},
			}}
			deps := &agent.RunDeps{ResolveTree: func(string) *evolution.SerializableNode { return tree }}
			agentName := "sdk-stopped-" + fixture.outcome
			before := engine.GetSLOMetrics(agentName, agentName).Snapshot()
			var result *agent.RunResult
			err := (&reliability.RetryPolicy{MaxRetries: 3, Base: time.Millisecond, MaxDelay: time.Millisecond, RetryUnknown: true}).ExecuteContext(t.Context(), func() error {
				var err error
				result, err = deps.RunOnce(t.Context(), agentName, "allocate fixture", agent.RunOptions{DisableBlackboard: true})
				return err
			})
			after := engine.GetSLOMetrics(agentName, agentName).Snapshot()
			if reliability.IsPausedOutcome(fixture.outcome) {
				if after.DeferredCalls-before.DeferredCalls != 1 || after.TotalCalls != before.TotalCalls {
					t.Fatalf("pause SLO before=%+v after=%+v", before, after)
				}
			} else if after.FailedCalls-before.FailedCalls != 1 {
				t.Fatalf("failed disposition SLO before=%+v after=%+v", before, after)
			}
			if calls.Load() != 1 || !reliability.IsExecutionStoppedError(err) || result == nil || result.Outcome != fixture.outcome || result.Output != evidence {
				t.Fatalf("calls=%d result=%+v err=%v", calls.Load(), result, err)
			}
		})
	}
}
