package dashboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestWorkflowMetadataFailureStopsAdmissionAndReplay(t *testing.T) {
	for _, phase := range []string{"input", "completed-output", "failed-output"} {
		for _, policy := range []string{"retry", "skip"} {
			t.Run(phase+"/"+policy, func(t *testing.T) {
				root := t.TempDir()
				manager := blackboard.DefaultManager()
				if err := manager.EnablePersistence(root); err != nil {
					t.Fatal(err)
				}
				inject := func() {
					if err := os.Rename(filepath.Join(root, "session"), filepath.Join(root, "session-evidence")); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, "session"), []byte("blocked fixture directory"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if phase == "input" {
					inject()
				}
				operations := 0
				fixtureErr := errors.New("ordinary execution failure")
				runner := &Runner{RunID: "owned-fixture", Blackboards: manager, RunAgent: func(context.Context, string, string, string) (string, string, error) {
					operations++
					inject()
					if phase == "failed-output" {
						return "failure", "failed fixture evidence", fixtureErr
					}
					return "success", "completed fixture evidence", nil
				}}
				result, err := runner.Run(t.Context(), Pipeline{Name: "metadata", Steps: []Step{{ID: "work", Kind: StepAgent, OnFailure: policy}, {ID: "later", Kind: StepAgent}}}, "input evidence")
				if phase == "input" {
					if operations != 0 || err == nil || result.Outcome != "failure" || len(result.Steps) != 0 || reliability.IsExecutionTerminalError(err) {
						t.Fatalf("failed input write acknowledged: operations=%d result=%+v err=%v", operations, result, err)
					}
					return
				}
				if operations != 1 || !reliability.IsExecutionTerminalError(err) || len(result.Steps) != 1 {
					t.Fatalf("admitted work replayed/acknowledged: operations=%d result=%+v err=%v", operations, result, err)
				}
				if phase == "completed-output" {
					if !reliability.IsExecutionPersistenceError(err) || result.Outcome != "aborted" || result.Steps[0].Outcome != "success" || result.Steps[0].Output != "completed fixture evidence" {
						t.Fatalf("completed evidence lost: result=%+v err=%v", result, err)
					}
				} else if !errors.Is(err, fixtureErr) || !reliability.IsExecutionStoppedError(err) || result.Steps[0].Output != "failed fixture evidence" {
					t.Fatalf("failed disposition lost: result=%+v err=%v", result, err)
				}
			})
		}
	}
}

func TestWorkflowSecondOutputMirrorFailureRetainsCommittedFirstMirror(t *testing.T) {
	manager := blackboard.NewManager(map[blackboard.ScopeKind]blackboard.Limits{blackboard.ScopeSession: {MaxEntries: 2, MaxTotalBytes: 1000}})
	if err := manager.EnablePersistence(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	operations := 0
	runner := &Runner{RunID: "mirror", Blackboards: manager, RunAgent: func(context.Context, string, string, string) (string, string, error) {
		operations++
		return "success", "completed evidence", nil
	}}
	result, err := runner.Run(t.Context(), Pipeline{Name: "mirror", Steps: []Step{{ID: "work", Kind: StepAgent, OnFailure: "retry"}, {ID: "later", Kind: StepAgent}}}, "input")
	scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: "mirror"}
	entry, readErr := manager.Get(scope, "steps/work/output")
	if operations != 1 || !reliability.IsExecutionPersistenceError(err) || result.Outcome != "aborted" || readErr != nil || entry.Value != "completed evidence" {
		t.Fatalf("first mirror evidence lost/replayed: operations=%d result=%+v err=%v entry=%+v readErr=%v", operations, result, err, entry, readErr)
	}
	if _, err := manager.Get(scope, "prev/output"); err == nil {
		t.Fatal("failed second mirror was acknowledged")
	}
}

func TestWorkflowMetadataLockUsesCallerDeadline(t *testing.T) {
	for _, phase := range []string{"input", "completed-output"} {
		t.Run(phase, func(t *testing.T) { testWorkflowMetadataLockDeadline(t, phase) })
	}
}

func testWorkflowMetadataLockDeadline(t *testing.T, phase string) {
	root := t.TempDir()
	manager := blackboard.DefaultManager()
	if err := manager.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: "locked"}
	if err := manager.Set(scope, "seed", "seed", "", "text"); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "session", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("scope fixture files=%v err=%v", files, err)
	}
	var unlock func()
	acquire := func() {
		var err error
		unlock, err = reliability.AcquireFileLockWithContext(t.Context(), files[0])
		if err != nil {
			t.Fatal(err)
		}
	}
	if phase == "input" {
		acquire()
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	operations := 0
	runner := &Runner{RunID: "locked", Blackboards: manager, RunAgent: func(context.Context, string, string, string) (string, string, error) {
		operations++
		acquire()
		return "success", "completed evidence", nil
	}}
	budget := 20 * time.Millisecond
	if phase == "completed-output" {
		// Leave setup time for the real input commit before the action. A
		// premature deadline would legitimately reject admission and test a
		// different boundary. Still much shorter than the default lock budget.
		budget = 250 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	start := time.Now()
	result, err := runner.Run(ctx, Pipeline{Name: "bounded", Steps: []Step{{ID: "work", Kind: StepAgent, OnFailure: "retry"}, {ID: "later", Kind: StepAgent}}}, "input")
	wantOperations, wantOutcome := 0, "failure"
	if phase == "completed-output" {
		wantOperations, wantOutcome = 1, "aborted"
		if !reliability.IsExecutionPersistenceError(err) || result.Steps[0].Outcome != "success" || result.Steps[0].Output != "completed evidence" {
			t.Fatalf("completed evidence lost: result=%+v err=%v", result, err)
		}
	}
	if operations != wantOperations || !errors.Is(err, context.DeadlineExceeded) || result.Outcome != wantOutcome || time.Since(start) > time.Second {
		t.Fatalf("failed admission ignored deadline: operations=%d elapsed=%s result=%+v err=%v", operations, time.Since(start), result, err)
	}
}
