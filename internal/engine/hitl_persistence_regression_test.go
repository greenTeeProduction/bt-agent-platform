package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

type hitlCountingChild struct{ runs int }

func (c *hitlCountingChild) Run(_ *btcore.BTContext[Blackboard]) int { c.runs++; return 1 }

func configureHITLGateFixture(t *testing.T) (string, *hitl.Store) {
	t.Helper()
	previous, policy := hitl.DefaultStore, hitl.GetPolicy()
	t.Cleanup(func() { hitl.DefaultStore = previous; hitl.SetPolicy(policy) })
	root := t.TempDir()
	store, err := hitl.InitStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: true, Timeout: time.Hour})
	return root, store
}

func TestHITLGateFailsClosedOnUnavailableApproval(t *testing.T) {
	for _, failure := range []string{"missing_store", "corrupt_store", "lock_deadline"} {
		t.Run(failure, func(t *testing.T) {
			root, _ := configureHITLGateFixture(t)
			parent := t.Context()
			path := filepath.Join(root, "hitl", "requests.json")
			switch failure {
			case "missing_store":
				hitl.DefaultStore = nil
			case "corrupt_store":
				if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			case "lock_deadline":
				release, err := reliability.AcquireFileLockWithContext(parent, path)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, 40*time.Millisecond)
				defer cancel()
			}
			child := &hitlCountingChild{}
			gate := &humanApprovalGateCmd{node: &evolution.SerializableNode{Name: "SharedGate"}, child: child}
			bb := &Blackboard{Task: "task", ChainState: map[string]any{chainKeyHITLStatus: string(hitl.StatusApproved)}}
			start := time.Now()
			code := gate.Run(btcore.NewBTContext(parent, bb))
			if code != -1 || child.runs != 0 || bb.Outcome != string(evolution.Failure) || hitlStatus(bb) != "" {
				t.Fatalf("gate=%d child=%d state=%+v", code, child.runs, bb)
			}
			if !strings.Contains(bb.Result, "HITL approval unavailable") {
				t.Fatal(bb.Result)
			}
			if time.Since(start) > time.Second {
				t.Fatal("gate ignored shorter deadline")
			}
		})
	}
}

func TestHITLGateDeduplicationBindsTaskAndAgent(t *testing.T) {
	_, store := configureHITLGateFixture(t)
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour})
	gate := &humanApprovalGateCmd{node: &evolution.SerializableNode{Name: "SharedGate"}}
	run := func(taskID, agentName string) *Blackboard {
		bb := &Blackboard{Task: "same display text", ChainState: map[string]any{"task_id": taskID, "agent_name": agentName}}
		if code := gate.Run(btcore.NewBTContext(t.Context(), bb)); code != 0 {
			t.Fatalf("gate=%d %s", code, bb.Result)
		}
		return bb
	}
	first, second, otherAgent := run("first", "agent"), run("second", "agent"), run("first", "other")
	id := func(bb *Blackboard) string { value, _ := bb.ChainState[chainKeyHITLRequestID].(string); return value }
	if id(first) == id(second) || id(first) == id(otherAgent) || id(second) == id(otherAgent) {
		t.Fatal("different work shared a request")
	}
	if got := id(run("first", "agent")); got != id(first) {
		t.Fatal("same work failed to reuse pending request")
	}
	if _, err := store.Approve(id(first), "reviewer", "ok"); err != nil {
		t.Fatal(err)
	}
	child := &hitlCountingChild{}
	gate.child = child
	second.ChainState[chainKeyHITLRequestID] = id(first)
	second.ChainState[gate.requestKey()] = id(first)
	if code := gate.Run(btcore.NewBTContext(t.Context(), second)); code != -1 || child.runs != 0 {
		t.Fatal("another task consumed approval")
	}
}

func TestNestedHITLGatesRequireIndependentApprovals(t *testing.T) {
	_, store := configureHITLGateFixture(t)
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour})
	child := &hitlCountingChild{}
	inner := &humanApprovalGateCmd{node: &evolution.SerializableNode{Name: "Inner", Type: "HumanApprovalGate"}, child: child}
	outer := &humanApprovalGateCmd{node: &evolution.SerializableNode{Name: "Outer", Type: "HumanApprovalGate"}, child: inner}
	bb := &Blackboard{Task: "task", ChainState: map[string]any{"task_id": "task", "agent_name": "agent"}}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := outer.Run(ctx); code != 0 {
		t.Fatalf("outer=%d", code)
	}
	outerID, _ := bb.ChainState[outer.requestKey()].(string)
	if _, err := store.Approve(outerID, "reviewer", ""); err != nil {
		t.Fatal(err)
	}
	if code := outer.Run(ctx); code != 0 || child.runs != 0 {
		t.Fatalf("inner bypassed approval: code=%d child=%d", code, child.runs)
	}
	innerID, _ := bb.ChainState[inner.requestKey()].(string)
	if innerID == "" || innerID == outerID {
		t.Fatal("nested gate reused outer approval")
	}
	if code := outer.Run(ctx); code != 0 || child.runs != 0 {
		t.Fatal("pending nested gate ran child")
	}
	if got, _ := bb.ChainState[outer.requestKey()].(string); got != outerID {
		t.Fatal("outer approval lost while nested gate pending")
	}
	if _, err := store.Approve(innerID, "reviewer", ""); err != nil {
		t.Fatal(err)
	}
	if code := outer.Run(ctx); code != 1 || child.runs != 1 {
		t.Fatalf("approved nested result=%d child=%d", code, child.runs)
	}
}

type hitlRunningChild struct{ runs int }

func (c *hitlRunningChild) Run(_ *btcore.BTContext[Blackboard]) int {
	c.runs++
	if c.runs == 1 {
		return 0
	}
	return 1
}

func TestPostHITLGateWaitsForChildCompletionBeforeReview(t *testing.T) {
	_, store := configureHITLGateFixture(t)
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour})
	child := &hitlRunningChild{}
	gate := &humanApprovalGateCmd{node: &evolution.SerializableNode{Name: "Post", Type: "HumanApprovalGate", Metadata: map[string]any{"phase": "post"}}, child: child}
	bb := &Blackboard{Task: "task", ChainState: map[string]any{}}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := gate.Run(ctx); code != 0 || gate.childDone(bb) || len(store.ListAll()) != 0 {
		t.Fatal("running child was marked completed or submitted for review")
	}
	if code := gate.Run(ctx); code != 0 || child.runs != 2 || !gate.childDone(bb) {
		t.Fatal("child did not complete before review")
	}
	if code := gate.Run(ctx); code != 0 || child.runs != 2 {
		t.Fatal("pending review replayed completed child")
	}
	reqID, _ := bb.ChainState[gate.requestKey()].(string)
	if _, err := store.Approve(reqID, "reviewer", ""); err != nil {
		t.Fatal(err)
	}
	if code := gate.Run(ctx); code != 1 || child.runs != 2 {
		t.Fatal("approved post review replayed child")
	}
}

func TestSandboxHITLGateDoesNotReadOrWriteOperationalApproval(t *testing.T) {
	root, _ := configureHITLGateFixture(t)
	path := filepath.Join(root, "hitl", "requests.json")
	before := "corrupt operational state must remain unchanged"
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	child := &hitlCountingChild{}
	gate := &humanApprovalGateCmd{node: &evolution.SerializableNode{Name: "Sandbox", Type: "HumanApprovalGate"}, child: child}
	bb := &Blackboard{Task: "benchmark", Sandbox: true, ChainState: map[string]any{}}
	if code := gate.Run(btcore.NewBTContext(t.Context(), bb)); code != 1 || child.runs != 1 {
		t.Fatalf("sandbox gate=%d child=%d", code, child.runs)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != before {
		t.Fatal("sandbox modified operational approvals")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatal("sandbox consulted operational lock")
	}
}
