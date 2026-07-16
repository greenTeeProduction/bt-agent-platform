package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestArc42DocsyncNodesRegistered(t *testing.T) {
	names := RegisteredActionNames()
	reg := map[string]bool{}
	for _, n := range names {
		reg[n] = true
	}
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("SyncArc42Section%02d", i)
		if !reg[name] {
			t.Errorf("action %s not registered", name)
		}
	}
	if !reg["SyncReadme"] {
		t.Error("action SyncReadme not registered")
	}
}

func TestSyncArc42SectionNodeOutcomes(t *testing.T) {
	restore := arc42SectionSyncFn
	defer func() { arc42SectionSyncFn = restore }()

	arc42SectionSyncFn = func(_ context.Context, _ ClaudeRunner, _ CommandRunner, _ docChangeContext, sec Arc42Section) (bool, string) {
		return false, fmt.Sprintf("§%d: no impact", sec.Num)
	}
	bb := &Blackboard{ChainState: map[string]any{"changed_files": "internal/engine/tree.go", "change_summary": "test"}}
	status := actionRegistry["SyncArc42Section01"](&btcore.BTContext[Blackboard]{Blackboard: bb})
	if status != 1 {
		t.Fatalf("node must succeed on no-change, got %d", status)
	}
	if bb.Outcome != "arc42_sync_no_change" || bb.OutcomeRefinement != "no_change" {
		t.Errorf("want no_change outcome+refinement, got %q/%q", bb.Outcome, bb.OutcomeRefinement)
	}

	arc42SectionSyncFn = func(_ context.Context, _ ClaudeRunner, _ CommandRunner, _ docChangeContext, sec Arc42Section) (bool, string) {
		return true, fmt.Sprintf("§%d: updated %s", sec.Num, sec.File)
	}
	bb2 := &Blackboard{ChainState: map[string]any{"changed_files": "internal/engine/tree.go"}}
	if status := actionRegistry["SyncArc42Section01"](&btcore.BTContext[Blackboard]{Blackboard: bb2}); status != 1 {
		t.Fatalf("node must succeed on update, got %d", status)
	}
	if bb2.Outcome != "arc42_sync_updated" || !strings.Contains(bb2.Result, "updated") {
		t.Errorf("want updated outcome + note in Result, got %q / %q", bb2.Outcome, bb2.Result)
	}
}

func TestDocChangeContextFromBlackboardFallsBackToGit(t *testing.T) {
	restore := arc42GitDiffFn
	defer func() { arc42GitDiffFn = restore }()
	arc42GitDiffFn = func(_ context.Context, _ string) []string {
		return []string{"internal/engine/tree.go", "internal/agent/runner.go"}
	}
	chg := docChangeContextFromBlackboard(&Blackboard{Task: "sync docs"})
	if len(chg.ChangedFiles) != 2 || chg.Summary != "sync docs" {
		t.Errorf("git fallback not applied: %+v", chg)
	}
}
