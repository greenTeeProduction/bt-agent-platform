package domains

import (
	"strings"

	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
)

// DynamicResolveFn resolves tree IDs that are not compiled in — trees generated
// at runtime by the knowledge factory (bt_kg_auto_create, bt_factory_create) and
// persisted as tree-<id>.json. It is consulted after every builtin mapping and
// before the DefaultTree fallback, so compiled-in trees always win and a
// generated tree is only used when nothing else matches.
//
// Installed by agentexec at link time (see internal/agentexec/wiring.go) —
// domains itself cannot load the files because the store location is an
// execution-layer concern. Nil means no dynamic resolution (ADR-133 Phase 0).
var DynamicResolveFn func(id string) *evolution.SerializableNode

// DynamicResolveForUserFn is the user-scoped counterpart to DynamicResolveFn
// (ADR-133 personalization hardening, Q1 Correctness): resolves a
// runtime-generated tree ID against ONE requesting user's own workspace, so a
// deterministic slug ID (goal:automate_<slug>) can never resolve to a
// different user's tree just because it was compiled first. Installed by
// agentexec at link time alongside DynamicResolveFn. Nil means no per-user
// dynamic resolution is wired, matching DynamicResolveFn's nil-tolerant
// convention.
var DynamicResolveForUserFn func(user, id string) *evolution.SerializableNode

// SelectorStatsPath points at a shared durable Selector telemetry file
// (SelectorOptimizer.SaveSelectorStats format). When non-empty, every tree
// returned by ResolveTreeID has its Selector children reordered by learned
// success rate before the tree is handed to the engine. Selectors with fewer
// than the optimizer's MinSamples recorded outcomes keep their authored
// order, and fallback/default-path children stay last.
//
// NOBODY wires this by default: learned reordering is a semantic change
// (success-rate ordering inverts cost-first routers such as the
// nlm-before-Claude quota economy), so it is strictly opt-in —
// internal/agentexec wires the per-tree SelectorStatsPathFn only under
// BT_SELECTOR_REORDER=1. Empty means no learned reordering, so domains tests
// stay standalone.
var SelectorStatsPath string

// SelectorStatsPathFn resolves the durable Selector telemetry file for ONE
// tree id and takes precedence over the shared SelectorStatsPath. Per-tree
// files keep equal selector NAMES in unrelated trees from polluting each
// other's learned ordering. Returning "" for an id disables reordering for
// that tree. Wired (opt-in) by internal/agentexec.
var SelectorStatsPathFn func(treeID string) string

// SelectorOrderingStrategy picks the ranking algorithm
// applyLearnedSelectorOrdering uses once a stats file resolves —
// evolution.OrderByIG/OrderByGini/OrderByHybrid were otherwise unreachable in
// production despite being fully implemented (Selector-reordering
// consolidation milestone 4). Defaults to evolution.OrderBySuccessRate, the
// pass's original behavior; wired (opt-in) by internal/agentexec alongside
// SelectorStatsPathFn.
var SelectorOrderingStrategy = evolution.OrderBySuccessRate

// DTStatsPath points at a durable DTAnalyzer telemetry file
// (evolution.DTAnalyzer.Save format). When non-empty, every tree returned by
// ResolveTreeID has its Selector children additionally reordered by
// information gain via evolution.BTOptimizer.OptimizeSelectors — the
// non-destructive sibling of the SelectorOptimizer pass above, applied after
// it. Selectors with no recorded DT telemetry keep their (possibly already
// SelectorOptimizer-reordered) order. Empty means no DT reordering, matching
// SelectorStatsPath's nil-tolerant, opt-in convention.
var DTStatsPath string

// ResolveTreeID maps a tree identifier string to a serializable behavior tree,
// then applies any learned Selector ordering (SelectorStatsPath) so accumulated
// telemetry reorders Selector children before the tree reaches the engine.
// Used by bt-agent, A2A, and template validation tests.
func ResolveTreeID(id string) *evolution.SerializableNode {
	tree := resolveTreeID(id)
	if tree != nil {
		applyLearnedSelectorOrdering(id, tree)
	}
	return tree
}

// ResolveTreeIDForUser is the user-scoped counterpart to ResolveTreeID
// (ADR-133 personalization hardening, Q1 Correctness): when user is
// non-empty, runtime-generated tree lookups are scoped to that user's own
// workspace via DynamicResolveForUserFn instead of the unscoped
// DynamicResolveFn, so a deterministic slug ID (goal:automate_<slug>) always
// resolves to the requesting user's own compiled tree. An empty user falls
// back to the unscoped ResolveTreeID.
func ResolveTreeIDForUser(user, id string) *evolution.SerializableNode {
	if user == "" {
		return ResolveTreeID(id)
	}
	tree := resolveTreeIDWithResolver(id, func(id string) *evolution.SerializableNode {
		return dynamicResolveForUser(user, id)
	})
	if tree != nil {
		applyLearnedSelectorOrdering(id, tree)
	}
	return tree
}

// applyLearnedSelectorOrdering seeds a SelectorOptimizer from the tree's
// durable telemetry (per-tree SelectorStatsPathFn first, shared
// SelectorStatsPath as fallback) and reorders the tree's Selector children in
// place by learned success rate. A missing/empty path or a load error leaves
// the authored order untouched, so cold or unwired deployments are unaffected.
// It then runs the non-destructive DTAnalyzer/BTOptimizer information-gain
// pass (applyDTOptimizerOrdering) on the same tree.
func applyLearnedSelectorOrdering(id string, tree *evolution.SerializableNode) {
	path := ""
	if SelectorStatsPathFn != nil {
		path = SelectorStatsPathFn(id)
	}
	if path == "" {
		path = SelectorStatsPath
	}
	if path != "" {
		so := evolution.NewSelectorOptimizer(SelectorOrderingStrategy)
		if err := so.LoadSelectorStats(path); err == nil {
			so.ApplyLearnedOrdering(tree)
		}
	}

	applyDTOptimizerOrdering(tree)
}

// applyDTOptimizerOrdering loads DTStatsPath into a fresh DTAnalyzer and, when
// telemetry exists, applies evolution.BTOptimizer.OptimizeSelectors to
// information-gain-reorder the tree's Selector children in place. A missing/
// empty DTStatsPath, a load error, or a Selector with no recorded stats
// leaves the tree's (possibly already SelectorOptimizer-reordered) order
// untouched — OptimizeSelectors itself is a no-op when the analyzer has no
// telemetry.
func applyDTOptimizerOrdering(tree *evolution.SerializableNode) {
	if DTStatsPath == "" {
		return
	}
	da := evolution.NewDTAnalyzer()
	if err := da.Load(DTStatsPath); err != nil {
		return
	}
	(&evolution.BTOptimizer{Analyzer: da}).OptimizeSelectors(tree)
}

// resolveTreeID performs the raw ID→tree mapping without the learned-ordering
// pass, so ResolveTreeID can funnel every resolved tree through a single
// reorder step regardless of which branch produced it. It consults the
// unscoped dynamicResolve for runtime-generated trees; ResolveTreeIDForUser
// shares the exact same branching logic via resolveTreeIDWithResolver, only
// swapping in a user-scoped resolver.
func resolveTreeID(id string) *evolution.SerializableNode {
	return resolveTreeIDWithResolver(id, dynamicResolve)
}

// resolveTreeIDWithResolver is resolveTreeID parameterized over the
// runtime-generated-tree resolver, so the unscoped and per-user resolution
// paths (ResolveTreeID vs ResolveTreeIDForUser) share one branching
// implementation instead of drifting out of sync.
func resolveTreeIDWithResolver(id string, resolve func(id string) *evolution.SerializableNode) *evolution.SerializableNode {
	if id == "" {
		return nil
	}
	if id == "hermes_evolve" {
		return HermesSelfEvolutionTree()
	}
	if id == "stockfish_evolve" {
		return evolution.StockfishEvolutionTree()
	}
	if id == "stockfish_loop" {
		return evolution.StockfishEvolutionLoop()
	}
	if id == "vault_manager" {
		return evolution.VaultManagerTree()
	}
	if id == "kanban:task_creator" {
		return KanbanTaskCreatorTree()
	}
	if id == "kanban:refiner" {
		return KanbanRefinerTree()
	}
	if id == "kanban:qa" {
		return KanbanQATree()
	}
	if id == "kanban:monitor" {
		return KanbanBoardMonitorTree()
	}
	if id == "kanban:workflow" {
		return KanbanWorkflowTree()
	}
	if id == "kanban:autopilot" {
		return KanbanAutoPilotTree()
	}
	if id == "notebooklm" {
		return NotebookLMTree()
	}
	if id == "notebooklm-consumer" {
		return NotebookLMConsumerTree()
	}
	if id == "notebooklm-bridge" {
		return evolution.NotebookLMBridgeTree()
	}
	if id == "hermes_obsidian" {
		return HermesObsidianOptimizerTree()
	}
	if id == "superpowers_pipeline" {
		return SuperpowersPipelineTree()
	}
	if id == "godev" {
		return evolution.GoDeveloperTree()
	}
	if id == "fusion" || id == "fusion_deliberation" {
		return evolution.FusionDeliberationTree()
	}
	// Category-prefixed IDs: builtin catalog first; on a miss, consult the
	// dynamic resolver before preserving the branch's legacy miss behavior —
	// factory-generated tree IDs use exactly these "<category>:<name>" shapes
	// (ADR-133 Phase 0), so an early nil/default return here would make every
	// generated tree unreachable.
	if len(id) > 8 && id[:8] == "finance:" {
		if t := evolution.AllFinanceTrees()[id[8:]]; t != nil {
			return t
		}
		return resolve(id)
	}
	if len(id) > 9 && id[:9] == "research:" {
		if t := evolution.ResearchTrees()[id[9:]]; t != nil {
			return t
		}
		return resolve(id)
	}
	if len(id) > 7 && id[:7] == "domain:" {
		if t := AllDomainTrees()[id[7:]]; t != nil {
			return t
		}
		return resolve(id)
	}
	if len(id) > 8 && id[:8] == "startup:" {
		role := id[8:]
		trees := startup.StartupTrees()
		if t, ok := trees[role]; ok {
			return t
		}
		if t := startup.Roles()[role]; t != nil {
			return t
		}
		return resolve(id)
	}
	if len(id) > 10 && id[:10] == "thinktank:" {
		switch role := id[10:]; role {
		case "synthesis":
			return thinktank.SynthesisTree()
		case "peer_review":
			return thinktank.PeerReviewTree()
		case "report":
			return thinktank.ReportGenerationTree()
		default:
			if t := resolve(id); t != nil {
				return t
			}
			return thinktank.SynthesisTree()
		}
	}
	if len(id) > 9 && id[:9] == "composed:" {
		rest := id[9:]
		switch rest {
		case "task":
			if t, err := blocks.ComposeTaskTree(blocks.DefaultRegistry, "ComposedTask", nil); err == nil {
				return t
			}
		case "task:hitl":
			if t, err := blocks.ComposeTaskTreeWithHITL(blocks.DefaultRegistry, "ComposedTaskHITL", nil); err == nil {
				return t
			}
		case "task:agentic":
			if t, err := blocks.ComposeTaskTreeAgentic(blocks.DefaultRegistry, "ComposedTaskAgentic", nil); err == nil {
				return t
			}
		case "task:full":
			if t, err := blocks.ComposeTaskTreeFull(blocks.DefaultRegistry, "ComposedTaskFull", nil); err == nil {
				return t
			}
		default:
			ids := strings.Split(rest, ",")
			if t, err := blocks.Compose(blocks.DefaultRegistry, blocks.ComposeSpec{Name: "Composed_Main", Blocks: ids}, false); err == nil {
				return t
			}
		}
	}
	// Runtime-generated trees (knowledge factory, plan→BT compiler): resolved
	// last so a generated tree can never shadow a compiled-in ID.
	if t := resolve(id); t != nil {
		return t
	}
	return evolution.DefaultTree()
}

// dynamicResolve consults the injected DynamicResolveFn, tolerating the
// unwired (nil) state so domains tests stay standalone.
func dynamicResolve(id string) *evolution.SerializableNode {
	if DynamicResolveFn == nil {
		return nil
	}
	return DynamicResolveFn(id)
}

// dynamicResolveForUser consults the injected DynamicResolveForUserFn,
// tolerating the unwired (nil) state so domains tests stay standalone.
func dynamicResolveForUser(user, id string) *evolution.SerializableNode {
	if DynamicResolveForUserFn == nil {
		return nil
	}
	return DynamicResolveForUserFn(user, id)
}
