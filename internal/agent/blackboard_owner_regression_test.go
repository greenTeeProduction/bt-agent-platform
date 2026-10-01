package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestBlackboardOwnerFailurePreventsAdmissionAndMemoryFallback(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	root := BlackboardDir()
	if err := os.WriteFile(root, []byte("blocked fixture directory"), 0600); err != nil {
		t.Fatal(err)
	}
	var called atomic.Int64
	action := "BlackboardOwnerNoAdmissionFixture"
	engine.RegisterAction(action, func(*btcore.BTContext[engine.Blackboard]) int { called.Add(1); return 1 })
	deps := &RunDeps{ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: action}
	}}
	manager, err := deps.BoardManager()
	if manager != nil || err == nil {
		t.Fatalf("unavailable persistent owner published: manager=%p err=%v", manager, err)
	}
	result, runErr := deps.RunOnce(context.Background(), "fixture", "owned input", RunOptions{SkipSLORecording: true})
	if called.Load() != 0 || runErr == nil || result == nil || result.Outcome != "failure" || result.RunID != "" {
		t.Fatalf("work admitted after initialization failed: calls=%d result=%+v err=%v", called.Load(), result, runErr)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if manager, err := deps.BoardManager(); manager != nil || err == nil {
		t.Fatal("failed owner silently retargeted/reinitialized")
	}
	fresh := &RunDeps{}
	if manager, err := fresh.BoardManager(); err != nil || manager == nil {
		t.Fatalf("replacement owner did not recover after repair: %v", err)
	}
}

func TestConcurrentBlackboardOwnerRetainsAllWritersAndPinnedRoot(t *testing.T) {
	first := t.TempDir()
	t.Setenv("BT_AGENT_HOME", first)
	deps := &RunDeps{}
	const writers = 24
	results := make(chan *blackboard.Manager, writers)
	failures := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			manager, err := deps.BoardManager()
			if err == nil {
				err = manager.Set(blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "shared"}, fmt.Sprintf("writer-%d", i), "owned value", "", "text")
			}
			results <- manager
			if err != nil {
				failures <- err
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var owner *blackboard.Manager
	for manager := range results {
		if owner == nil {
			owner = manager
		}
		if manager != owner {
			t.Fatal("concurrent runs received different owners")
		}
	}
	second := t.TempDir()
	t.Setenv("BT_AGENT_HOME", second)
	manager, err := deps.BoardManager()
	if err != nil || manager != owner {
		t.Fatal("initialized owner changed after home override")
	}
	disk, err := blackboard.NewPersistentManager(filepath.Join(first, "blackboard"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := disk.List(blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "shared"}, "", writers)
	if err != nil || len(entries) != writers {
		t.Fatalf("concurrent persistence lost evidence: count=%d err=%v", len(entries), err)
	}
	if _, err := os.Stat(filepath.Join(second, "blackboard")); !os.IsNotExist(err) {
		t.Fatalf("owner redirected into later root: %v", err)
	}
}

func TestCompletedPromotionFailureKeepsHealthyEvidenceAndStopsReplay(t *testing.T) {
	for _, fixture := range []string{"write-failure", "caller-canceled"} {
		t.Run(fixture, func(t *testing.T) {
			t.Setenv("BT_AGENT_HOME", t.TempDir())
			root := t.TempDir()
			manager, err := blackboard.NewPersistentManager(root)
			if err != nil {
				t.Fatal(err)
			}
			history, err := NewHistory(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int64
			name := "PromotionOwnedFixture" + fixture
			engine.RegisterAction(name, func(treeCtx *btcore.BTContext[engine.Blackboard]) int {
				calls.Add(1)
				treeCtx.Blackboard.Result = "Fixture action has produced a complete, attributable result. This observation identifies the work and can be read independently of its stored run summary."
				treeCtx.Blackboard.Outcome = "success"
				if fixture == "caller-canceled" {
					cancel()
				} else {
					if err := os.Remove(filepath.Join(root, "agent")); err != nil {
						t.Error(err)
						return -1
					}
					if err := os.WriteFile(filepath.Join(root, "agent"), []byte("blocked after execution"), 0600); err != nil {
						t.Error(err)
						return -1
					}
				}
				return 1
			})
			deps := &RunDeps{Blackboards: manager, History: history, ResolveTree: func(string) *evolution.SerializableNode {
				return &evolution.SerializableNode{Type: "Action", Name: name}
			}}
			var result *RunResult
			err = (&reliability.RetryPolicy{MaxRetries: 3, RetryUnknown: true}).ExecuteContext(context.Background(), func() error {
				var runErr error
				result, runErr = deps.RunOnce(ctx, "fixture", "input", RunOptions{RecordHistory: true, SkipSLORecording: true})
				return runErr
			})
			if calls.Load() != 1 || result == nil || result.Outcome != "success" || result.Output != "Fixture action has produced a complete, attributable result. This observation identifies the work and can be read independently of its stored run summary." || !reliability.IsExecutionPersistenceError(err) {
				t.Fatalf("completed evidence/replay guard lost: calls=%d result=%+v err=%v", calls.Load(), result, err)
			}
			records := history.List("fixture", 0)
			if len(records) != 1 || records[0].Outcome != "success" || records[0].Output != result.Output || !strings.Contains(records[0].Error, "promote completed run") {
				t.Fatalf("promotion diagnostic not attributed in history: %+v", records)
			}
		})
	}
}
