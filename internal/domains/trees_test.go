package domains

import (
	"maps"
	"slices"
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestSuiteForTreeMilestone4RealNodeNames guards milestone 4/5 of the
// SuiteForTree benchmark-gating fix (milestone 2 fixed NotebookLM — see
// notebooklm_test.go; milestone 3 fixed BTFusion/BTManager — see
// bt_fusion_test.go). SelfReviewSuite/HermesUpdateSuite/AuctionDemoSuite/
// SuperpowersWorkflowSuite currently declare TaskCase.ExpectedPath values
// ("SelfReviewPath", "UpdatePath", "AuctionPath", "WorkflowPath") that do not
// correspond to any node anywhere in their trees:
//   - SelfReviewTree, HermesUpdateTree, and AuctionDemoTree are plain linear
//     Sequences with no StrategyRouter branching at all (their real node
//     names are SelfReview_Main/TaskIsNotEmpty,
//     HermesUpdate_Main/IsUpdateTask/HermesUpdateAgent, and
//     AuctionDemo_Main/IsAuctionTask/AuctionDelegate respectively).
//   - SuperpowersWorkflowTree does branch, but its real Sequence node names
//     are ParallelPath and VerifyPath, not "WorkflowPath".
//
// A benchmark suite whose ExpectedPath references a name that exists nowhere
// in the tree can never be satisfied by real tree execution — it is the same
// blind spot NotebookLM/BTFusion/BTManager had before milestones 2 and 3.
func TestSuiteForTreeMilestone4RealNodeNames(t *testing.T) {
	tests := []struct {
		domain string
		suite  benchmark.Suite
		tree   *evolution.SerializableNode
	}{
		{"self_review", benchmark.SelfReviewSuite(), SelfReviewTree()},
		{"hermes_update", benchmark.HermesUpdateSuite(), HermesUpdateTree()},
		{"auction_demo", benchmark.AuctionDemoSuite(), AuctionDemoTree()},
		{"superpowers_workflow", benchmark.SuperpowersWorkflowSuite(), SuperpowersWorkflowTree()},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			for _, tc := range tt.suite.Tasks {
				if tc.ExpectedPath == "" {
					continue
				}
				if !hasNode(tt.tree, tc.ExpectedPath) {
					t.Errorf("task %q declares ExpectedPath %q, which is not a real node name anywhere in %s's tree",
						tc.Task, tc.ExpectedPath, tt.domain)
				}
			}
		})
	}
}

// TestSuiteForTreeExpectedPathsExistInEveryDomainTree generalizes the
// milestone-4 guard above into a registry-wide invariant. Milestones 2-4 each
// fixed one tree at a time (NotebookLM, BTFusion/BTManager, then
// SelfReview/HermesUpdate/AuctionDemo/SuperpowersWorkflow) by hand-listing the
// suspect trees; this sweep derives its work list from
// SmokeTestableDomainTrees() instead, so no tree can stay outside the guard.
//
// The invariant: for every domain tree, the suite benchmark.SuiteForTree
// returns for it may only declare TaskCase.ExpectedPath values that name a
// real node somewhere in that tree. An ExpectedPath that exists nowhere in the
// tree can never be satisfied by real execution, so the suite's condition
// coverage is unsatisfiable and the benchmark silently measures nothing.
func TestSuiteForTreeExpectedPathsExistInEveryDomainTree(t *testing.T) {
	trees := SmokeTestableDomainTrees()

	for _, name := range sortedTreeNames(trees) {
		tree := trees[name]
		t.Run(name, func(t *testing.T) {
			suite := benchmark.SuiteForTree(name)
			for _, tc := range suite.Tasks {
				if tc.ExpectedPath == "" {
					continue
				}
				if !hasNode(tree, tc.ExpectedPath) {
					t.Errorf("domain %q: suite %q task %q declares ExpectedPath %q, which is not a real node name anywhere in the tree",
						name, suite.Name, tc.Task, tc.ExpectedPath)
				}
			}
		})
	}
}

// defaultFallbackAllowlist enumerates the domain trees that are permitted to
// fall through SuiteForTree's default branch and inherit the generic suite
// instead of getting a bespoke one.
//
// It is deliberately empty: a tree that falls through picks up ExpectedPath
// values written against an unrelated tree's node names, which is exactly the
// unsatisfiable-condition-coverage bug this guard exists to prevent. If a tree
// genuinely has no routing conditions worth its own suite, add it here with a
// comment saying why — an unexplained silent inheritance must fail the build.
var defaultFallbackAllowlist = map[string]string{}

// TestSuiteForTreeReportsDefaultFallback pins that every registry tree resolves
// to a suite deliberately written for it. SuiteForTree alone cannot express
// this — it returns the generic suite for both "matched the generic tree" and
// "matched nothing", so a newly added tree silently inheriting an unrelated
// suite is indistinguishable from a real match. SuiteForTreeNamed reports the
// difference, and this test holds the fallback set to the documented allowlist.
func TestSuiteForTreeReportsDefaultFallback(t *testing.T) {
	trees := SmokeTestableDomainTrees()

	for _, name := range sortedTreeNames(trees) {
		suite, matched := benchmark.SuiteForTreeNamed(name)
		reason, allowed := defaultFallbackAllowlist[name]
		switch {
		case matched && allowed:
			t.Errorf("domain %q is on defaultFallbackAllowlist (%s) but now resolves to a real suite %q — remove it from the allowlist",
				name, reason, suite.Name)
		case !matched && !allowed:
			t.Errorf("domain %q silently falls back to the generic default suite %q; give it a suite whose ExpectedPath values name real nodes in its tree, or add it to defaultFallbackAllowlist with a reason",
				name, suite.Name)
		}
	}
}

// TestHasNodeDetectsMissingAndNestedNames exercises the hasNode walker the two
// sweeps above rely on, so a false "path exists" answer from the helper cannot
// quietly neuter the guards.
func TestHasNodeDetectsMissingAndNestedNames(t *testing.T) {
	deep := &evolution.SerializableNode{
		Type: "sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "condition", Name: "TopCondition"},
			{
				Type: "selector", Name: "Middle",
				Children: []evolution.SerializableNode{
					{Type: "sequence", Name: "Inner", Children: []evolution.SerializableNode{
						{Type: "action", Name: "DeeplyNestedLeaf"},
					}},
				},
			},
		},
	}

	tests := []struct {
		name string
		node string
		want bool
	}{
		{"root itself", "Root", true},
		{"direct child", "TopCondition", true},
		{"grandchild", "Inner", true},
		{"deeply nested leaf", "DeeplyNestedLeaf", true},
		{"absent name", "NoSuchPath", false},
		{"empty name", "", false},
		{"case sensitive", "deeplynestedleaf", false},
		{"substring is not a match", "Deeply", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasNode(deep, tt.node); got != tt.want {
				t.Errorf("hasNode(deep, %q) = %v, want %v", tt.node, got, tt.want)
			}
		})
	}
}

// sortedTreeNames gives the registry sweeps a deterministic subtest order so
// failures are reported in a stable, diffable sequence across runs.
func sortedTreeNames(trees map[string]*evolution.SerializableNode) []string {
	names := slices.Sorted(maps.Keys(trees))
	return names
}
