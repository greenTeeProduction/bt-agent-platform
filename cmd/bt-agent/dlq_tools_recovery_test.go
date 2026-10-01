package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestDLQToolsRejectRestartedOriginalUncertainty(t *testing.T) {
	previous := engine.TaskDLQ
	t.Cleanup(func() { engine.TaskDLQ = previous })
	path := filepath.Join(t.TempDir(), "dlq.json")
	origin := reliability.NewDeadLetterQueue(path)
	executionErr := &reliability.ExecutionUncertainError{Err: errors.New("fixture lost original outcome")}
	entry := schedulerDeadLetter("fixture", "task", executionErr, 1, "fixture-revision")
	if err := origin.PushExecutionFailureWithError(entry, executionErr); err != nil {
		t.Fatal(err)
	}
	engine.TaskDLQ = reliability.NewDeadLetterQueue(path)
	actions := 0
	engine.TaskDLQ.SetReplayExecutor(func(reliability.DeadLetterEntry) error { actions++; return nil })
	server := engine.NewServer("fixture")
	registerMCPTools(server, &mcpDeps{})
	invoke := func(name string, args json.RawMessage) map[string]any {
		t.Helper()
		result, ok := server.Invoke(name, args)
		if !ok || result == nil || len(result.Content) == 0 {
			t.Fatal("tool unavailable")
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	args, err := json.Marshal(map[string]any{"id": entry.ID, "wait": true})
	if err != nil {
		t.Fatal(err)
	}
	out := invoke("bt_dlq_replay", args)
	if out["requeued"] != false || out["recovery_required"] != true || actions != 0 {
		t.Fatalf("original uncertainty replayed: %v actions=%d", out, actions)
	}
	listing := invoke("bt_dlq_list", json.RawMessage(`{}`))
	if listing["count"] != float64(1) {
		t.Fatalf("hold missing: %v", listing)
	}
	if err := os.WriteFile(path, []byte("{unreadable fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if out := invoke("bt_dlq_list", json.RawMessage(`{}`)); out["error"] != "dead letter storage unavailable" {
		t.Fatalf("failed storage listed successfully: %v", out)
	}
}

func TestDLQToolsPreserveCompletionPrecedence(t *testing.T) {
	previous := engine.TaskDLQ
	t.Cleanup(func() { engine.TaskDLQ = previous })
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "uncertain"}[uncertain], func(t *testing.T) {
			q := reliability.NewDeadLetterQueue("")
			engine.TaskDLQ = q
			if err := q.PushWithError(reliability.DeadLetterEntry{ID: "owned"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			q.SetReplayExecutor(func(reliability.DeadLetterEntry) error {
				calls++
				var diagnostic error = &reliability.ExecutionPersistenceError{Err: errors.New("fixture record failure")}
				if uncertain {
					diagnostic = errors.Join(diagnostic, &reliability.ExecutionUncertainError{Err: errors.New("fixture unknown action")})
				}
				return diagnostic
			})
			server := engine.NewServer("fixture")
			registerMCPTools(server, &mcpDeps{})
			result, ok := server.Invoke("bt_dlq_replay", json.RawMessage(`{"id":"owned","wait":true}`))
			if !ok || result == nil || len(result.Content) == 0 {
				t.Fatal("tool unavailable")
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
				t.Fatal(err)
			}
			if out["execution_completed"] == uncertain || out["recovery_required"] != true || calls != 1 {
				t.Fatalf("completion precedence lost: %v calls=%d", out, calls)
			}
			if _, err := q.RequeueWithError("owned"); !errors.Is(err, reliability.ErrReplayRecovery) {
				t.Fatal("terminal result requeued")
			}
		})
	}
}
