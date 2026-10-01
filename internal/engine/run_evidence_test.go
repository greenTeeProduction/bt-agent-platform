package engine

import (
	"os"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestRunEvidenceUsesFinalOutcomeAndExecutedDefinition(t *testing.T) {
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	RegisterAction("EvidenceWork", func(ctx *btcore.BTContext[Blackboard]) int {
		time.Sleep(5 * time.Millisecond)
		ctx.Blackboard.Result = `{"verified":true,"message":"a completed task with sufficient evidence"}`
		return 1
	})
	RegisterAction("EvidenceFinalFailure", func(_ *btcore.BTContext[Blackboard]) int { return -1 })
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "source", Children: []evolution.SerializableNode{
		{Type: "Action", Name: "EvidenceWork"}, {Type: "Action", Name: "ReflectOnOutcome"}, {Type: "Action", Name: "EvidenceFinalFailure"},
	}}
	bb := &Blackboard{Task: "perform task", User: "alice", TreeID: "personal:task", Reflections: store}
	command := BuildTree(tree, bb)
	// A subsequently built tree cannot rewrite the identity of an older command.
	_ = BuildTree(&evolution.SerializableNode{Type: "Action", Name: "AlwaysSucceed"}, bb)
	RunTask(bb, command)
	records, err := store.LoadAll()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	r := records[0]
	version, _ := evolution.TreeVersion(tree)
	if r.Outcome != evolution.Failure || r.TreeName != "personal:task" || r.TreeVersion != version || r.User != "alice" || r.DurationMs < 5 || r.Result != bb.Result || r.EvidenceKind != evolution.EvidenceExecution || len(r.ExecutionVersions) != 1 {
		t.Fatalf("wrong terminal evidence: %+v", r)
	}
	if err := FinalizeRunEvidence(bb); err != nil {
		t.Fatal(err)
	}
	again, _ := store.LoadAll()
	if len(again) != 1 {
		t.Fatal("duplicate finalization saved another record")
	}
}

func TestRunEvidenceSurvivesPanicAndSeparatesRepeatedRuns(t *testing.T) {
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	RegisterAction("EvidencePanic", func(_ *btcore.BTContext[Blackboard]) int { panic("terminal evidence probe") })
	tree := &evolution.SerializableNode{Type: "Action", Name: "EvidencePanic"}
	bb := &Blackboard{Task: "panic probe", Reflections: store, RunID: "shared-session"}
	command := BuildTree(tree, bb)
	for _, owner := range []string{"alice", "bob"} {
		bb.User = owner
		RunTask(bb, command)
	}
	records, err := store.LoadAll()
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if records[0].TaskID == records[1].TaskID || records[0].User == records[1].User {
		t.Fatal("run identity or owner collided")
	}
	for _, r := range records {
		if r.Outcome != evolution.Failure || r.Result == "" {
			t.Fatalf("panic evidence lost: %+v", r)
		}
	}
}

func TestRunEvidenceFailureDoesNotReplayCompletedWork(t *testing.T) {
	dir := t.TempDir()
	store, err := evolution.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dir, []byte("block storage"), 0600); err != nil {
		t.Fatal(err)
	}
	count := 0
	RegisterAction("EvidenceOnce", func(ctx *btcore.BTContext[Blackboard]) int {
		count++
		ctx.Blackboard.Result = `{"completed":true,"message":"independently observed completed work"}`
		return 1
	})
	bb := &Blackboard{Task: "once", Reflections: store}
	RunTask(bb, BuildTree(&evolution.SerializableNode{Type: "Action", Name: "EvidenceOnce"}, bb))
	if count != 1 || bb.Outcome != "success" || bb.EvidenceError == nil {
		t.Fatalf("count=%d outcome=%s evidence=%v", count, bb.Outcome, bb.EvidenceError)
	}
	if FinalizeRunEvidence(bb) == nil || count != 1 {
		t.Fatal("lost persistence error or replayed work")
	}
}
