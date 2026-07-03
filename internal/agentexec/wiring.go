package agentexec

import (
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// init installs the engine's production wiring for the scheduled
// goap_fusion_loop tree. agentexec is the one layer every tree-running
// binary links (bt-agent via cmd/bt-agent/tools.go, bt-agent-cli,
// bt-dashboard) that may import both domains and engine — domains itself
// cannot import engine (its in-package tests import engine), and
// internal/agent cannot import domains (domains→blocks→dashboard→agent).
func init() {
	domains.GoapFusionLoopWireFn = engine.WireGoapFusionLoopTree
}
