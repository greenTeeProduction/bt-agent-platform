package blocks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/audit"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestRegistry_FreezeAndFilterMutations(t *testing.T) {
	reg := NewRegistry("")
	if reg.IsEvolutionMutable("core:pre_gate") {
		t.Fatal("core:pre_gate should not be evolution-mutable")
	}
	b := reg.Get("core:pre_gate")
	if b == nil {
		t.Fatal("missing pre_gate")
	}
	ops := []evolution.MutationOp{{
		Operation: "insert_block_before",
		Target:    "PreGate",
		Node:      &evolution.SerializableNode{Metadata: map[string]any{MutationBlockIDKey: "core:pre_gate"}},
	}}
	filtered := reg.FilterEvolutionMutations(ops)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 ops after filter, got %d", len(filtered))
	}
}

func TestRegistry_PromoteVersion(t *testing.T) {
	reg := NewRegistry(t.TempDir())
	_, err := reg.PromoteVersion("core:plan", "custom:plan_v2")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Get("custom:plan_v2") == nil {
		t.Fatal("promoted block missing")
	}
}

func TestFitnessProbeAction(t *testing.T) {
	dir := t.TempDir()
	audit.Init(dir)
	bb := &engine.Blackboard{
		Task:         "test",
		Outcome:      "success",
		QualityScore: 0.9,
		ChainState:   make(map[string]any),
	}
	tree := FitnessProbeBlock()
	cmd, err := engine.BuildAndValidate(&tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	code := cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if code != 1 {
		t.Fatalf("expected success, got %d", code)
	}
	if _, ok := bb.ChainState["block_fitness"]; !ok {
		t.Fatal("expected block_fitness in ChainState")
	}
}

func TestAuditLogAction(t *testing.T) {
	dir := t.TempDir()
	audit.Init(dir)
	bb := &engine.Blackboard{Task: "audit test", Outcome: "success", ChainState: make(map[string]any)}
	tree := AuditLogBlock()
	cmd, err := engine.BuildAndValidate(&tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	_ = cmd.Run(btcore.NewBTContext(t.Context(), bb))
	path := filepath.Join(dir, "audit", "task.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audit file not created: %v", err)
	}
}

func TestPushToDLQAction(t *testing.T) {
	dir := t.TempDir()
	dlq := reliability.NewDeadLetterQueue(filepath.Join(dir, "dlq.json"))
	engine.TaskDLQ = dlq
	defer func() { engine.TaskDLQ = nil }()

	bb := &engine.Blackboard{
		Task:         "failed task",
		Result:       "3+ persistent failures detected",
		FailureCount: 3,
		ChainState:   make(map[string]any),
	}
	tree := DLQEscalateBlock()
	cmd, err := engine.BuildAndValidate(&tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	code := cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if code != 1 {
		t.Fatalf("expected success, got %d", code)
	}
	if dlq.Len() != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", dlq.Len())
	}
}

func TestRecordTaskBlockFitness(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Metadata: map[string]any{
			"block_id": "core:pre_gate",
		},
		Children: []evolution.SerializableNode{
			SubTreeRefNode("core:tool_execution"),
		},
	}
	RecordTaskBlockFitness(tree, "tester", "success", 0.85, true)
	snap := dashboard.BlockFitnessSnapshot()
	if len(snap) == 0 {
		t.Fatal("expected fitness metrics recorded")
	}
}

func TestMatchBlockPattern(t *testing.T) {
	tree := &evolution.SerializableNode{
		Children: []evolution.SerializableNode{SubTreeRefNode("core:pre_gate")},
	}
	if !evolution.MatchBlockPattern(tree, "core:pre_gate") {
		t.Fatal("expected match for core:pre_gate ref")
	}
}
