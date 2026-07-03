package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// The bt_fusion research actions must consult the persistent knowledge store
// (~/.go-bt-evolve/research/knowledge.json in production) so that scheduled
// cycles never re-report findings recorded by an earlier cycle. Regression
// context: until 2026-07-03 SearchForBTPatterns emitted the same hardcoded
// findings every run and QueryNotebookLMResearch re-read bt_fusion's own
// previous report, so every scheduled run broadcast an identical report.

func withFusionKnowledge(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knowledge.json")
	old := btFusionKnowledgePath
	btFusionKnowledgePath = path
	t.Cleanup(func() { btFusionKnowledgePath = old })
	return path
}

func withFusionVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := btFusionVaultDirs
	btFusionVaultDirs = []string{dir}
	t.Cleanup(func() { btFusionVaultDirs = old })
	return dir
}

func runFusionAction(t *testing.T, name string, bb *Blackboard) int {
	t.Helper()
	fn := GetAction(name)
	if fn == nil {
		t.Fatalf("action %q not registered", name)
	}
	return fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
}

func TestSearchForBTPatternsOnlyReportsUnknownFindings(t *testing.T) {
	withFusionKnowledge(t)

	first := &Blackboard{Task: "research bt patterns"}
	if got := runFusionAction(t, "SearchForBTPatterns", first); got != 1 {
		t.Fatalf("first run status = %d, want 1; result: %s", got, first.Result)
	}
	if fusionNewCount(first) == 0 {
		t.Fatal("first run against an empty store must surface new findings")
	}
	if !strings.Contains(first.Result, "Typed-edge validation") {
		t.Fatalf("first run must report the new findings, got: %s", first.Result)
	}

	second := &Blackboard{Task: "research bt patterns"}
	if got := runFusionAction(t, "SearchForBTPatterns", second); got != 1 {
		t.Fatalf("second run status = %d, want 1", got)
	}
	if n := fusionNewCount(second); n != 0 {
		t.Fatalf("second run new count = %d, want 0 (all findings already recorded)", n)
	}
	if strings.Contains(second.Result, "Typed-edge validation: preserve") {
		t.Fatalf("second run must not re-report known findings, got: %s", second.Result)
	}
}

func TestQueryNotebookLMResearchSurfacesOnlyNewVaultNotes(t *testing.T) {
	withFusionKnowledge(t)
	vault := withFusionVault(t)

	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(vault, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("note-a.md", "Paper A: LLM-supervised genetic programming for multi-robot BT evolution.")
	write("bt-fusion-latest.md", "bt_fusion's OWN previous report must never feed back into research.")
	write("quota-err.md", "NotebookLM query failed: RESOURCE_EXHAUSTED quota for today.")

	first := &Blackboard{Task: "research"}
	if got := runFusionAction(t, "QueryNotebookLMResearch", first); got != 1 {
		t.Fatalf("first run status = %d, want 1; result: %s", got, first.Result)
	}
	if n := fusionNewCount(first); n != 1 {
		t.Fatalf("first run new count = %d, want 1 (only note-a.md)", n)
	}
	if !strings.Contains(first.Result, "note-a.md") {
		t.Fatalf("first run must surface note-a.md, got: %s", first.Result)
	}
	if strings.Contains(first.Result, "OWN previous report") {
		t.Fatalf("own report content must be excluded, got: %s", first.Result)
	}
	if strings.Contains(first.Result, "RESOURCE_EXHAUSTED") {
		t.Fatalf("quota-error garbage must be skipped, got: %s", first.Result)
	}

	second := &Blackboard{Task: "research"}
	runFusionAction(t, "QueryNotebookLMResearch", second)
	if n := fusionNewCount(second); n != 0 {
		t.Fatalf("second run new count = %d, want 0", n)
	}

	write("note-b.md", "Paper B: typed-edge semantics for generated behavior trees.")
	third := &Blackboard{Task: "research"}
	runFusionAction(t, "QueryNotebookLMResearch", third)
	if n := fusionNewCount(third); n != 1 {
		t.Fatalf("third run new count = %d, want 1 (only the new note-b.md)", n)
	}
	if !strings.Contains(third.Result, "note-b.md") || strings.Contains(third.Result, "Paper A") {
		t.Fatalf("third run must surface only note-b.md, got: %s", third.Result)
	}
}

func TestFusionResearchRoutingConditions(t *testing.T) {
	hasNew := GetCondition("HasNewResearch")
	noNew := GetCondition("NoNewResearch")
	if hasNew == nil || noNew == nil {
		t.Fatal("routing conditions not registered")
	}

	fresh := &Blackboard{}
	addFusionNewCount(fresh, 2)
	if !hasNew(fresh) || noNew(fresh) {
		t.Fatal("blackboard with new research must route to the fusion branch")
	}

	stale := &Blackboard{}
	addFusionNewCount(stale, 0)
	if hasNew(stale) || !noNew(stale) {
		t.Fatal("blackboard without new research must route to the no-op branch")
	}
}

func TestReportNoNewResearchSucceedsWithoutReportWrite(t *testing.T) {
	withFusionKnowledge(t)
	bb := &Blackboard{Task: "research"}
	if got := runFusionAction(t, "ReportNoNewResearch", bb); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "No New Research") {
		t.Fatalf("result must state that nothing new was found, got: %s", bb.Result)
	}
}
