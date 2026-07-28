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
	"github.com/nico/go-bt-evolve/internal/persona"
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
	// Auction-winner History attribution (mirrors internal/a2a/server.go's
	// Execute check): RunOnce cannot import internal/a2a directly (import
	// cycle), so it consults this cycle-safe seam instead.
	agent.AuctionWinnerNameFn = a2a.AuctionWinnerName
	// LLM plan-expansion (brainstorming): decompose substantial goals into
	// deeper multi-task plans. Wired here (not in an engine init) so engine
	// tests stay offline and deterministic.
	engine.WireGoalPlanBrainstorm()
	// Dynamic tree resolution (ADR-133 Phase 0): trees generated at runtime
	// (bt_kg_auto_create, bt_factory_create) are persisted as tree-<id>.json
	// in the reflections dir; this hook makes them resolvable by ID so the
	// agent runner, A2A, and bt_run_task execute them instead of silently
	// falling back to DefaultTree.
	domains.DynamicResolveFn = ResolveGeneratedTree
	// Per-user dynamic tree resolution (ADR-133 personalization hardening,
	// Q1 Correctness): scopes runtime-generated tree lookups to the
	// requesting user so a deterministic slug ID (goal:automate_<slug>) can
	// never resolve to a different user's tree.
	domains.DynamicResolveForUserFn = ResolveGeneratedTreeForUser
	// Learned Selector reordering (opt-in, BT_SELECTOR_REORDER=1).
	wireSelectorReorder()
}

// wireSelectorReorder wires learned Selector reordering at resolve time —
// STRICTLY opt-in via BT_SELECTOR_REORDER=1. Success-rate ordering inverts
// cost-first routers (e.g. the nlm-before-Claude quota economy in the goap
// research trees), so it must never become an ambient default. When enabled,
// every resolved tree reorders from its OWN per-tree telemetry file
// (agent.SelectorStatsFile), which the agent runner populates on every run.
// The same opt-in also wires the DTAnalyzer/BTOptimizer sibling pass from its
// own per-tree file (agent.DecisionTreeStatsFile), closing ADR-191's
// inert-activation gap for the resolve-time path (mirroring ADR-203's fix for
// the gardener's evolution-time dtStatsPathFor).
//
// BT_SELECTOR_ORDERING_STRATEGY additionally lets operators opt into
// evolution.OrderByIG/OrderByGini/OrderByHybrid instead of the default
// OrderBySuccessRate (Selector-reordering consolidation milestone 4); unset
// or unrecognized values keep today's behavior.
func wireSelectorReorder() {
	if os.Getenv("BT_SELECTOR_REORDER") == "1" {
		domains.SelectorStatsPathFn = agent.SelectorStatsFile
		domains.DTStatsPathFn = agent.DecisionTreeStatsFile
	}
	domains.SelectorOrderingStrategy = evolution.ParseSelectorOrderingStrategy(os.Getenv("BT_SELECTOR_ORDERING_STRATEGY"))
}

// generatedTreeDir is the directory scanned for runtime-generated trees.
// Overridable for tests; empty means "resolve the default at call time"
// so init order and per-test home dirs don't bake in a stale path.
var generatedTreeDir string

// usersTreeRoot is the root of per-user workspaces scanned as a fallback
// (users/<user>/trees, ADR-133 Phase 5). Overridable for tests; empty means
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
// (users/<user>/trees, ADR-133 Phase 5 — user-attributed compiles persist
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
//
// This is the legacy unscoped lookup path: it is what makes a deterministic
// slug ID (goal:automate_<slug>) resolve to the first user that happens to
// sort first, running one user's personalized tree under a different user's
// request. ResolveGeneratedTreeForUser is the fix — always prefer it when the
// requesting user is known.
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
			if !automationApproved(root, user, id) {
				continue
			}
			return tree
		}
	}
	return nil
}

// automationApproved reports whether a tree ID is safe to execute: true when
// no persona.AutomationRecord references it (not an automation-tracked
// tree — e.g. a manually compiled bt_factory_create tree) or when the
// matching record's Status is persona.AutomationApproved. Pending and
// rejected automation proposals must never run just because their compiled
// tree file happens to exist on disk (Q4 Personalization milestone 1).
func automationApproved(root, user, treeID string) bool {
	if root == "" || user == "" || treeID == "" {
		return true
	}
	store, err := persona.NewStore(root)
	if err != nil {
		return true
	}
	ledger, err := persona.NewAutomationStore(store.Workspace(user))
	if err != nil {
		return true
	}
	records, err := ledger.All()
	if err != nil {
		return true
	}
	for _, rec := range records {
		if rec.TreeID == treeID {
			return rec.Status == persona.AutomationApproved
		}
	}
	return true
}

// AutomationBlocked reports whether treeID has an automation record for user
// that exists and is not persona.AutomationApproved (e.g. pending, rejected,
// or paused via automationFlaggedStatus). Callers that resolve a user's tree
// through a path with a generic fallback for "nothing matched" — such as
// domains.ResolveTreeIDForUser, which falls back to evolution.DefaultTree()
// — must check this FIRST and refuse (return nil) rather than fall through:
// otherwise a gated automation's tree looks like "not found" and silently
// executes the default tree instead of failing closed (Q4 Personalization
// milestone 2 — the feedback-escalation resume loop must actually block
// execution while flagged, not just skip to a different runnable tree).
func AutomationBlocked(user, treeID string) bool {
	root := usersTreeRoot
	if root == "" {
		root = agent.UsersDir()
	}
	return !automationApproved(root, user, treeID)
}

// ResolveGeneratedTreeForUser is the user-scoped counterpart to
// ResolveGeneratedTree (ADR-133 personalization hardening, Q1 Correctness):
// the shared reflections dir is still consulted first (trees not yet
// user-attributed), but the per-user fallback loads ONLY the requesting
// user's own workspace — never the sorted scan across every user that
// resolveUserTree performs, which lets a deterministic slug ID
// (goal:automate_<slug>) collide across users and hand one user's tree to
// another. Returns nil when no such tree has been persisted for this user or
// the file is unreadable — resolution then falls through to DefaultTree.
func ResolveGeneratedTreeForUser(user, id string) *evolution.SerializableNode {
	root := usersTreeRoot
	if root == "" {
		root = agent.UsersDir()
	}
	dir := generatedTreeDir
	if dir == "" {
		d, err := ReflectionsPath()
		if err != nil {
			return nil
		}
		dir = d
	}
	if tree, err := evolution.LoadNamedTree(dir, id); err == nil && tree != nil {
		if !automationApproved(root, user, id) {
			return nil
		}
		return tree
	}
	tree, err := evolution.LoadNamedTree(filepath.Join(root, user, "trees"), id)
	if err == nil && tree != nil {
		if !automationApproved(root, user, id) {
			return nil
		}
		return tree
	}
	return nil
}
