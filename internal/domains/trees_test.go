package domains

import (
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
