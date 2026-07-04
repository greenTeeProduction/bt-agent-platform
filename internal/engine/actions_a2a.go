package engine

import (
	"fmt"

	btcore "github.com/rvitorper/go-bt/core"
)

// AuctionDelegateFn wires auction-based A2A task allocation into the engine.
//
// internal/a2a imports internal/engine, so the engine cannot import a2a; the
// auction is reached through this injected hook (mirroring DelegateToA2AFn /
// DelegateToTreeFn). The hook receives the task and the current chain state and
// returns (result, awarded, err): awarded reports whether an eligible bidder
// won the auction. When awarded is false the AuctionDelegate action falls back
// to running the task through a delegate tree via DelegateToTreeFn.
var AuctionDelegateFn func(task string, chainState map[string]any) (result string, awarded bool, err error)

func init() {
	registerAuctionDelegateNode()
}

// registerAuctionDelegateNode registers the AuctionDelegate behavior tree action.
func registerAuctionDelegateNode() {
	actionRegistry["AuctionDelegate"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard

		task := b.Task
		if task == "" {
			b.Result = "no task provided for auction delegation"
			b.Outcome = "failure"
			return -1
		}

		if AuctionDelegateFn == nil {
			b.Result = "auction delegate not configured (set engine.AuctionDelegateFn)"
			b.Outcome = "failure"
			return -1
		}

		result, awarded, err := AuctionDelegateFn(task, b.ChainState)
		if err != nil {
			b.Result = fmt.Sprintf("auction delegation failed: %v", err)
			b.Outcome = "failure"
			return -1
		}

		if awarded {
			b.Result = result
			b.Outcome = "success"
			return 1
		}

		// No eligible bidders — fall back to a delegate tree if one is configured.
		treeID, _ := b.ChainState["delegate_tree_id"].(string)
		if treeID == "" {
			b.Result = "auction produced no bidders and no delegate_tree_id fallback is configured"
			b.Outcome = "failure"
			return -1
		}

		if DelegateToTreeFn == nil {
			b.Result = "delegate tree not configured (set engine.DelegateToTreeFn)"
			b.Outcome = "failure"
			return -1
		}

		treeResult, err := DelegateToTreeFn(treeID, b)
		if err != nil {
			b.Result = fmt.Sprintf("auction fallback tree %q failed: %v", treeID, err)
			b.Outcome = "failure"
			return -1
		}

		b.Result = treeResult
		b.Outcome = "success"
		return 1
	}
}
