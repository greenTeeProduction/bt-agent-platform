package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestBTFusionApproveGateAutoApproves pins the fix for the bt-fusion HITL gate
// misclassification that deadlocked every unattended hourly cycle:
// ApproveFusionReportWrite must be classified local_reversible with
// auto_approve set, matching goap_fusion.go's ApproveGoapFusionApply gate.
func TestBTFusionApproveGateAutoApproves(t *testing.T) {
	tree := BTFusionTree()

	gate := findNode(*tree, "ApproveFusionReportWrite")
	if gate == nil {
		t.Fatal("ApproveFusionReportWrite node not found in BTFusionTree")
	}

	sideEffectClass, _ := gate.Metadata["side_effect_class"].(string)
	if sideEffectClass != "local_reversible" {
		t.Errorf("ApproveFusionReportWrite side_effect_class = %q, want %q", sideEffectClass, "local_reversible")
	}

	autoApprove, _ := gate.Metadata["auto_approve"].(bool)
	if !autoApprove {
		t.Errorf("ApproveFusionReportWrite auto_approve = %v, want true", gate.Metadata["auto_approve"])
	}
}

// TestBTFusionTreeUsesStrategyRouterNaming guards milestone 3 of the
// SuiteForTree benchmark-gating fix (milestone 2 fixed this for NotebookLM —
// see notebooklm_test.go). The engine's buildNodeInner only records
// bb.CurrentPath/VisitedPaths for a Sequence child whose parent Selector is
// named exactly "StrategyRouter" (internal/engine/tree.go). BTFusionTree's
// router is currently named "BTFusion_Route", which silently disables real
// path attribution: benchmark.BTFusionSuite() then falls back to a
// keyword-guessed, non-existent "FusionPath" instead of the tree's actual
// BTFusion_NoNewResearch/BTFusion_NewResearch nodes.
func TestBTFusionTreeUsesStrategyRouterNaming(t *testing.T) {
	tree := BTFusionTree()

	router := findChildByName(tree, "StrategyRouter")
	if router == nil {
		t.Fatal("StrategyRouter not found in BTFusionTree children — BT Fusion router must use the platform-wide StrategyRouter naming convention for path tracking to work")
	}
	if len(router.Children) < 2 {
		t.Fatalf("expected router paths, got %d", len(router.Children))
	}
	if got := router.Children[0].Name; got != "BTFusion_NoNewResearch" {
		t.Fatalf("first router path = %q, want BTFusion_NoNewResearch", got)
	}
	if got := router.Children[1].Name; got != "BTFusion_NewResearch" {
		t.Fatalf("second router path = %q, want BTFusion_NewResearch", got)
	}
}

// TestBTFusionTreeRecordsRealPathDuringExecution guards the actual production
// defect behind milestone 3: running BTFusionTree() end-to-end (the same
// engine.BuildTree/engine.RunTask path benchmark.RunSuite uses to score
// mutations) must set bb.CurrentPath to the tree's real Sequence node name.
// Benchmark scoring runs actions in Sandbox mode (internal/engine/tree.go
// actionForName), which stubs every registered Action — including
// SearchForBTPatterns/QueryNotebookLMResearch, the actions that would record
// new knowledge-store entries. bt_fusion_research_new_count therefore always
// reads 0 under benchmark scoring, so BTFusion_NoNewResearch is the only
// strategy branch a benchmark run can ever reach.
func TestBTFusionTreeRecordsRealPathDuringExecution(t *testing.T) {
	tree := BTFusionTree()
	bb := &engine.Blackboard{
		Task:    "gather new research knowledge and synthesize fusion candidates",
		LLM:     &engine.MockLLM{},
		Sandbox: true,
	}
	bt := engine.BuildTree(tree, bb)
	engine.RunTask(bb, bt)
	if bb.CurrentPath != "BTFusion_NoNewResearch" {
		t.Fatalf("bb.CurrentPath = %q, want %q (real BTFusionTree node name)", bb.CurrentPath, "BTFusion_NoNewResearch")
	}
}

// TestBTManagerTreeUsesStrategyRouterNaming guards the BTManager half of
// milestone 3. BTManagerTree's router is currently named
// "BTManager_StrategyRouter" — close to, but not literally, the platform-wide
// "StrategyRouter" convention buildNodeInner requires for path recording, so
// benchmark.BTManagerSuite() falls back to a keyword-guessed, non-existent
// "ManagerPath" instead of the tree's actual DegradedPerformancePath /
// NewAgentBootstrapPath / HealthyReportPath nodes.
func TestBTManagerTreeUsesStrategyRouterNaming(t *testing.T) {
	tree := BTManagerTree()

	router := findChildByName(tree, "StrategyRouter")
	if router == nil {
		t.Fatal("StrategyRouter not found in BTManagerTree children — BT Manager router must use the platform-wide StrategyRouter naming convention for path tracking to work")
	}
	wantChildren := []string{"DegradedPerformancePath", "NewAgentBootstrapPath", "HealthyReportPath"}
	if len(router.Children) != len(wantChildren) {
		t.Fatalf("expected %d router paths, got %d", len(wantChildren), len(router.Children))
	}
	for i, want := range wantChildren {
		if got := router.Children[i].Name; got != want {
			t.Fatalf("router path[%d] = %q, want %q", i, got, want)
		}
	}
}

// TestBTManagerTreeRecordsRealPathDuringExecution guards the actual
// production defect behind the BTManager half of milestone 3: running
// BTManagerTree() end-to-end must set bb.CurrentPath to the real Sequence
// node name for whichever strategy fired. Unlike BTFusion, BTManager's
// routing conditions (IsDegradedAgent/IsNewAgent/IsHealthy) read directly
// from the reflection store rather than from state a Sandboxed action would
// have written, so all three branches are independently reachable by seeding
// bb.Reflections before the run.
func TestBTManagerTreeRecordsRealPathDuringExecution(t *testing.T) {
	tree := BTManagerTree()

	tests := []struct {
		name     string
		seed     func(t *testing.T) *evolution.Store
		wantPath string
	}{
		{
			name: "degraded agent",
			seed: func(t *testing.T) *evolution.Store {
				store, err := evolution.NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 3; i++ {
					if err := store.Save(&evolution.Record{
						TaskID:    "f" + string(rune('0'+i)),
						TreeName:  "some_tree",
						Outcome:   evolution.Failure,
						Timestamp: int64(i + 1),
					}); err != nil {
						t.Fatal(err)
					}
				}
				return store
			},
			wantPath: "DegradedPerformancePath",
		},
		{
			name: "new agent (empty reflection store)",
			seed: func(t *testing.T) *evolution.Store {
				store, err := evolution.NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				return store
			},
			wantPath: "NewAgentBootstrapPath",
		},
		{
			name: "healthy fleet",
			seed: func(t *testing.T) *evolution.Store {
				store, err := evolution.NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 5; i++ {
					if err := store.Save(&evolution.Record{
						TaskID:    "s" + string(rune('0'+i)),
						TreeName:  "some_tree",
						Outcome:   evolution.Success,
						Timestamp: int64(i + 1),
					}); err != nil {
						t.Fatal(err)
					}
				}
				return store
			},
			wantPath: "HealthyReportPath",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &engine.Blackboard{
				Task:        "check the status of the managed agent fleet",
				LLM:         &engine.MockLLM{},
				Sandbox:     true,
				Reflections: tt.seed(t),
			}
			bt := engine.BuildTree(tree, bb)
			engine.RunTask(bb, bt)
			if bb.CurrentPath != tt.wantPath {
				t.Fatalf("bb.CurrentPath = %q, want %q (real BTManagerTree node name)", bb.CurrentPath, tt.wantPath)
			}
		})
	}
}
