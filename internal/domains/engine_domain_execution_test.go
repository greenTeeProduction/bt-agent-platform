package domains

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestAllDomainTreesHaveExecutableStructure smoke-executes every domain tree
// that is registered anywhere. The work list is derived from
// SmokeTestableDomainTrees() — the canonical union of the curated registry and
// the deliberately-off-registry kanban/hermes trees — rather than from a
// hand-maintained literal, so registering a tree is what subjects it to this
// smoke test and no separate opt-in list can fall behind.
// TestSmokeExecutionFnsMapCoversRegistry in domains_test.go guards that
// derivation.
func TestAllDomainTreesHaveExecutableStructure(t *testing.T) {
	trees := SmokeTestableDomainTrees()
	if len(trees) == 0 {
		t.Fatal("SmokeTestableDomainTrees() is empty; the smoke test has lost its subject")
	}
	for name, tree := range trees {
		t.Run(name, func(t *testing.T) {
			if tree == nil || len(tree.Children) == 0 {
				t.Fatalf("%s tree invalid", name)
			}
		})
	}
}

func TestDomainTreeMonitoringRoutesThroughEngine(t *testing.T) {
	bb := &engine.Blackboard{Task: "check system health status", LLM: &engine.MockLLM{}}
	tree := engine.BuildTree(AgentMonitorTree(), bb)
	outcome := engine.RunTask(bb, tree)
	if outcome == "" {
		t.Error("monitoring task should produce outcome")
	}
}

func TestDataPipelineTreeUsesObservedFileMetrics(t *testing.T) {
	tmp := t.TempDir()
	csvPath := filepath.Join(tmp, "input.csv")
	if err := os.WriteFile(csvPath, []byte("name,value\na,1\nb,2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	bb := &engine.Blackboard{Task: "extract data from " + csvPath, ChainState: map[string]any{}}
	bt := engine.BuildTree(DataPipelineTree(), bb)
	engine.RunTask(bb, bt)

	if !strings.Contains(bb.Result, "csv_records_observed: 3") {
		t.Fatalf("expected real csv record count, got: %s", bb.Result)
	}
	if strings.Contains(bb.Result, "10,420") || strings.Contains(bb.Result, "10,418") {
		t.Fatalf("fabricated canned row count leaked: %s", bb.Result)
	}
	if !strings.Contains(bb.Result, "verification: passed anti-fabrication gate") {
		t.Fatalf("expected anti-fabrication verification, got: %s", bb.Result)
	}
}

func TestDataPipelineTreeNoSourceReportsBlockedNotFabricated(t *testing.T) {
	bb := &engine.Blackboard{Task: "run ETL workflow with no source path", ChainState: map[string]any{}}
	bt := engine.BuildTree(DataPipelineTree(), bb)
	engine.RunTask(bb, bt)

	if !strings.Contains(bb.Result, "status: blocked") {
		t.Fatalf("expected blocked report, got: %s", bb.Result)
	}
	if strings.Contains(bb.Result, "10,420") || strings.Contains(bb.Result, "10,418") {
		t.Fatalf("fabricated canned row count leaked: %s", bb.Result)
	}
	if !strings.Contains(bb.Result, "available_tools:") {
		t.Fatalf("expected discovered tool list in report, got: %s", bb.Result)
	}
}

func TestNotebookLMTreePreGateDiscoversRealToolset(t *testing.T) {
	tree := NotebookLMTree()
	if len(tree.Children) == 0 || tree.Children[0].Name != "NotebookLM_PreGate" {
		t.Fatalf("expected NotebookLM_PreGate first child, got %#v", tree.Children)
	}
	preGate := tree.Children[0]
	seenSetup := false
	seenDiscover := false
	for _, child := range preGate.Children {
		if child.Name == "SetupNotebookLMTools" {
			seenSetup = true
		}
		if child.Name == "DiscoverAvailableTools" {
			seenDiscover = true
		}
	}
	if !seenSetup || !seenDiscover {
		t.Fatalf("NotebookLM pre-gate must setup and discover real tools before auth/use; setup=%v discover=%v", seenSetup, seenDiscover)
	}
}
