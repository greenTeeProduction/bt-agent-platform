package agentexec

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/nico/go-bt-evolve/internal/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
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
	// LLM plan-expansion (brainstorming): decompose substantial goals into
	// deeper multi-task plans. Wired here (not in an engine init) so engine
	// tests stay offline and deterministic.
	engine.WireGoalPlanBrainstorm()
	// Dynamic tree resolution (ADR-010 Phase 0): trees generated at runtime
	// (bt_kg_auto_create, bt_factory_create) are persisted as tree-<id>.json
	// in the reflections dir; this hook makes them resolvable by ID so the
	// agent runner, A2A, and bt_run_task execute them instead of silently
	// falling back to DefaultTree.
	domains.DynamicResolveFn = ResolveGeneratedTree
	// Learned Selector reordering (opt-in, BT_SELECTOR_REORDER=1).
	wireSelectorReorder()
}

// wireSelectorReorder wires learned Selector reordering at resolve time —
// STRICTLY opt-in via BT_SELECTOR_REORDER=1. Success-rate ordering inverts
// cost-first routers (e.g. the nlm-before-Claude quota economy in the goap
// research trees), so it must never become an ambient default. When enabled,
// every resolved tree reorders from its OWN per-tree telemetry file
// (agent.SelectorStatsFile), which the agent runner populates on every run.
func wireSelectorReorder() {
	if os.Getenv("BT_SELECTOR_REORDER") == "1" {
		domains.SelectorStatsPathFn = agent.SelectorStatsFile
	}
}

// generatedTreeDir is the directory scanned for runtime-generated trees.
// Overridable for tests; empty means "resolve the default at call time"
// so init order and per-test home dirs don't bake in a stale path.
var generatedTreeDir string

// usersTreeRoot is the root of per-user workspaces scanned as a fallback
// (users/<user>/trees, ADR-010 Phase 5). Overridable for tests; empty means
// "resolve agent.UsersDir() at call time".
var usersTreeRoot string

// ReflectionsPath returns the shared persistence root used by the reflection
// store, tree store, and blocks registry (~/.go-bt-reflections).
func ReflectionsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".go-bt-reflections"), nil
}

// ResolveGeneratedTree loads a runtime-generated tree by ID: first from the
// shared reflections dir, then from per-user personalization workspaces
// (users/<user>/trees, ADR-010 Phase 5 — user-attributed compiles persist
// there so the gardener evolves them per user). Returns nil when no such
// tree has been persisted or the file is unreadable — resolution then falls
// through to DefaultTree.
func ResolveGeneratedTree(id string) *evolution.SerializableNode {
	dir := generatedTreeDir
	if dir == "" {
		d, err := ReflectionsPath()
		if err != nil {
			return nil
		}
		dir = d
	}
	if tree, err := evolution.LoadNamedTree(dir, id); err == nil && tree != nil {
		return tree
	}
	return resolveUserTree(id)
}

// resolveUserTree scans user workspaces for a personal tree with the given
// ID. Users are visited in sorted order so a (rare) cross-user ID collision
// resolves deterministically.
func resolveUserTree(id string) *evolution.SerializableNode {
	root := usersTreeRoot
	if root == "" {
		root = agent.UsersDir()
	}
	users, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		if u.IsDir() {
			names = append(names, u.Name())
		}
	}
	sort.Strings(names)
	for _, user := range names {
		tree, err := evolution.LoadNamedTree(filepath.Join(root, user, "trees"), id)
		if err == nil && tree != nil {
			return tree
		}
	}
	return nil
}
