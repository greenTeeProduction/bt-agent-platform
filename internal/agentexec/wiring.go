package agentexec

import (
	"github.com/nico/go-bt-evolve/internal/a2a"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// init installs the engine's production wiring for the scheduled
// goap_fusion_loop tree and the auctioneer. agentexec is the one layer every
// tree-running binary links (bt-agent via cmd/bt-agent/tools.go, bt-agent-cli,
// bt-dashboard) that may import domains, engine, and a2a together — domains
// itself cannot import engine (its in-package tests import engine), and
// internal/agent cannot import domains (domains→blocks→dashboard→agent).
//
// The auction hook is installed here as a link-time side effect (not from
// main) so it is non-nil the moment the binary's packages are linked: the
// AuctionDelegate action never reports "not configured" at runtime, and the
// wiring cannot be silently dropped without failing a binary-level regression
// test. The daemon separately supplies the live candidate source at startup
// (a2a.AuctionCardsFn); until then AuctionDelegate simply finds no candidates.
func init() {
	domains.GoapFusionLoopWireFn = engine.WireGoapFusionLoopTree
	engine.AuctionDelegateFn = a2a.AuctionDelegate
}
