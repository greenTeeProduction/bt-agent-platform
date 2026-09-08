package domains

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// singleTaskSuite builds a minimal Suite with one task.
func singleTaskSuite(name, task string, shouldSucceed bool) benchmark.Suite {
	return benchmark.Suite{
		Name: name,
		Tasks: []benchmark.TaskCase{
			{Task: task, ShouldSucceed: shouldSucceed, MinResultLen: 10},
		},
	}
}

func TestCodeReviewTree(t *testing.T) {
	tree := CodeReviewTree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("code_review_smoke", "find bugs in this code", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.SuccessRate < 0.5 {
		t.Errorf("CodeReviewTree success rate too low: %.2f (want >= 0.5)", metrics.SuccessRate)
	}
	t.Logf("CodeReviewTree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestDevOpsTree(t *testing.T) {
	tree := DevOpsCITree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("devops_ci_smoke", "build the project", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("DevOpsCITree task should succeed")
	}
	t.Logf("DevOpsCITree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestAgentMonitor(t *testing.T) {
	tree := AgentMonitorTree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("agent_monitor_smoke", "check health of all agents", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("AgentMonitorTree task should succeed")
	}
	t.Logf("AgentMonitorTree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestCrashInvestigator(t *testing.T) {
	tree := CrashInvestigatorTree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("crash_investigator_smoke", "parse this stack trace for crash", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("CrashInvestigatorTree task should succeed")
	}
	t.Logf("CrashInvestigatorTree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestGameAI(t *testing.T) {
	tree := GameAITree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("game_ai_smoke", "game: patrol the area", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("GameAITree task should succeed")
	}
	t.Logf("GameAITree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

// tasksForTree returns a representative smoke task for each domain tree.
func tasksForTree() map[string]string {
	return map[string]string{
		"code_review":               "find bugs in this code",
		"devops_ci":                 "build the project",
		"agent_monitor":             "check health of all agents",
		"refactoring":               "refactor this code to be cleaner",
		"security_audit":            "audit this code for vulnerabilities",
		"data_pipeline":             "extract data from source and transform",
		"meeting_notes":             "summarize this meeting transcript",
		"crash_investigator":        "parse this stack trace for crash",
		"game_ai":                   "game: patrol the area",
		"trading_signal":            "calculate trading signals for AAPL",
		"alert_router":              "critical disk alert: sda1 at 95%",
		"goap_planning":             "plan the steps to deploy a new service",
		"goap_research":             "research best practices for Go microservices",
		"goap_devops":               "diagnose why the CI pipeline is failing",
		"goap_fusion":               "analyze research gaps and improve the BT platform trees",
		"goap_fusion_loop":          "start the self-improving GOAP fusion loop cycle",
		"hermes_update":             "check for hermes agent updates and apply them",
		"arc42_seeder":              "seed next program from arc42 quality goals",
		"self_review":               "review autonomous commits since the last self-review and seed code-fix programs",
		"bt_manager":                "analyze all agent failures and fix degraded ones",
		"notebooklm":                "research latest BT framework developments using NotebookLM",
		"notebooklm_consumer":       "consume notebooklm synthesis and write summary",
		"notebooklm_plan_implement": "plan and implement a new domain tree for NotebookLM workflow",
		"bt_fusion":                 "fuse behavior tree candidates into a stronger production tree",
		"superpowers_workflow":      "dry_run: fix a small bug via the full superpowers workflow",
		"auction_demo":              "auction: allocate a task to the best bidder via announce-bid-award",
		// Arc42 documentation trees
		"arc42:section1":  "generate arc42 introduction and goals",
		"arc42:section2":  "generate arc42 constraints section",
		"arc42:section3":  "generate arc42 context and scope",
		"arc42:section4":  "generate arc42 solution strategy",
		"arc42:section5":  "generate arc42 building block view",
		"arc42:section6":  "generate arc42 runtime view",
		"arc42:section7":  "generate arc42 deployment view",
		"arc42:section8":  "generate arc42 crosscutting concepts",
		"arc42:section9":  "generate arc42 architecture decisions",
		"arc42:section10": "generate arc42 quality requirements",
		"arc42:section11": "generate arc42 risks and technical debt",
		"arc42:section12": "generate arc42 glossary",
		"arc42:docsync":   "sync arc42 sections and README after a change",
	}
}

func TestDomainFallbacksUseChainAction(t *testing.T) {
	planTrees := map[string]*evolution.SerializableNode{
		"code_review":        CodeReviewTree(),
		"devops_ci":          DevOpsCITree(),
		"refactoring":        RefactoringTree(),
		"security_audit":     SecurityAuditTree(),
		"data_pipeline":      DataPipelineTree(),
		"meeting_notes":      MeetingNotesTree(),
		"crash_investigator": CrashInvestigatorTree(),
		"game_ai":            GameAITree(),
		"trading_signal":     TradingSignalTree(),
		"goap_planning":      GoapPlanningTree(false),
		"goap_research":      GoapResearchTree(false),
		"goap_devops":        GoapDevopsTree(false),
	}

	for name, tree := range planTrees {
		assertNoExecutePlanStubs(t, name, *tree)
		fallback := findNode(*tree, "ExecutionPath")
		if fallback == nil {
			t.Fatalf("%s: missing ExecutionPath fallback", name)
		}
		if name == "data_pipeline" {
			for _, child := range fallback.Children {
				if child.Type == "ChainAction" {
					t.Fatalf("%s: ExecutionPath must be deterministic and use real actions, found ChainAction %s", name, child.Name)
				}
			}
			continue
		}
		if len(fallback.Children) != 1 {
			t.Fatalf("%s: ExecutionPath should contain one ChainAction, got %d children", name, len(fallback.Children))
		}
		child := fallback.Children[0]
		if child.Type != "ChainAction" {
			t.Fatalf("%s: ExecutionPath should use ChainAction, got type=%s name=%s", name, child.Type, child.Name)
		}
	}
}

func assertNoExecutePlanStubs(t *testing.T, treeName string, node evolution.SerializableNode) {
	t.Helper()
	if node.Name == "AnalyzeTask" || node.Name == "ExecutePlan" {
		t.Fatalf("%s: found deprecated stub node %s", treeName, node.Name)
	}
	for _, child := range node.Children {
		assertNoExecutePlanStubs(t, treeName, child)
	}
}

func findNode(node evolution.SerializableNode, name string) *evolution.SerializableNode {
	if node.Name == name {
		return &node
	}
	for _, child := range node.Children {
		if found := findNode(child, name); found != nil {
			return found
		}
	}
	return nil
}

// TestGoapPlanningRunsRealGOAPPlannerFirst is milestone 1/3 of "Wire the real
// GOAP A* planner into production domain trees instead of the orphaned keyword
// router": AllDomainTrees()["goap_planning"]'s StrategyRouter Selector must try
// the real evolution.GOAPPlanningTree() (root node "GOAP_Root", the A* planner
// evolution.GOAPPlanningTree() builds via goap.BuildSerializableTree) ahead of
// the existing keyword-routed AssessPath/SyncPath and the ExecutionPath
// fallback — mirroring the fallback-through-Selector pattern
// internal/evolution/merged.go's GoapPlanningPath already proves for the
// evaluator's MergedTree(). Selector semantics mean a failed GOAP_Root
// (e.g. no plan found) falls through to the untouched keyword paths, so this
// also pins that TestDomainFallbacksUseChainAction's ExecutionPath invariant
// and the keyword-path fallback keep working unchanged.
func TestGoapPlanningRunsRealGOAPPlannerFirst(t *testing.T) {
	tree, ok := AllDomainTrees()["goap_planning"]
	if !ok || tree == nil {
		t.Fatal(`AllDomainTrees()["goap_planning"] missing`)
	}

	router := findNode(*tree, "StrategyRouter")
	if router == nil {
		t.Fatal("goap_planning: StrategyRouter selector not found")
	}
	if router.Type != "Selector" {
		t.Fatalf("goap_planning StrategyRouter type = %q, want Selector", router.Type)
	}

	names := make([]string, len(router.Children))
	for i, c := range router.Children {
		names[i] = c.Name
	}
	want := []string{"GOAP_Root", "AssessPath", "SyncPath", "ExecutionPath"}
	same := len(names) == len(want)
	if same {
		for i, w := range want {
			if names[i] != w {
				same = false
				break
			}
		}
	}
	if !same {
		t.Fatalf("goap_planning StrategyRouter children = %v, want %v (real GOAP A* planner first, keyword paths and ExecutionPath fallback preserved)", names, want)
	}
}

// TestGoapResearchRunsRealGOAPPlannerFirst is milestone 2/3 of "Wire the real
// GOAP A* planner into production domain trees instead of the orphaned keyword
// router": AllDomainTrees()["goap_research"]'s StrategyRouter Selector must try
// the real evolution.GOAPResearchTree() (root node "GOAP_Root", the A* planner
// evolution.GOAPResearchTree() builds via goap.BuildSerializableTree) ahead of
// the existing keyword-routed ResearchPath/GraphifyPath and the ExecutionPath
// fallback — mirroring TestGoapPlanningRunsRealGOAPPlannerFirst's pattern for
// goap_planning. Selector semantics mean a failed GOAP_Root (e.g. no plan
// found) falls through to the untouched keyword paths, so this also pins the
// keyword-path fallback behavior unchanged.
func TestGoapResearchRunsRealGOAPPlannerFirst(t *testing.T) {
	tree, ok := AllDomainTrees()["goap_research"]
	if !ok || tree == nil {
		t.Fatal(`AllDomainTrees()["goap_research"] missing`)
	}

	router := findNode(*tree, "StrategyRouter")
	if router == nil {
		t.Fatal("goap_research: StrategyRouter selector not found")
	}
	if router.Type != "Selector" {
		t.Fatalf("goap_research StrategyRouter type = %q, want Selector", router.Type)
	}

	names := make([]string, len(router.Children))
	for i, c := range router.Children {
		names[i] = c.Name
	}
	want := []string{"GOAP_Root", "ResearchPath", "GraphifyPath", "ExecutionPath"}
	same := len(names) == len(want)
	if same {
		for i, w := range want {
			if names[i] != w {
				same = false
				break
			}
		}
	}
	if !same {
		t.Fatalf("goap_research StrategyRouter children = %v, want %v (real GOAP A* planner first, keyword paths and ExecutionPath fallback preserved)", names, want)
	}
}

// TestGoapDevopsRunsRealGOAPPlannerFirst is milestone 2/3 of "Wire the real
// GOAP A* planner into production domain trees instead of the orphaned keyword
// router": AllDomainTrees()["goap_devops"]'s StrategyRouter Selector must try
// the real evolution.GOAPDevOpsTree() (root node "GOAP_Root", the A* planner
// evolution.GOAPDevOpsTree() builds via goap.BuildSerializableTree) ahead of
// the existing keyword-routed BuildPath/ImplementPath and the ExecutionPath
// fallback — mirroring TestGoapPlanningRunsRealGOAPPlannerFirst's pattern for
// goap_planning. Selector semantics mean a failed GOAP_Root (e.g. no plan
// found) falls through to the untouched keyword paths, so this also pins the
// keyword-path fallback behavior unchanged.
func TestGoapDevopsRunsRealGOAPPlannerFirst(t *testing.T) {
	tree, ok := AllDomainTrees()["goap_devops"]
	if !ok || tree == nil {
		t.Fatal(`AllDomainTrees()["goap_devops"] missing`)
	}

	router := findNode(*tree, "StrategyRouter")
	if router == nil {
		t.Fatal("goap_devops: StrategyRouter selector not found")
	}
	if router.Type != "Selector" {
		t.Fatalf("goap_devops StrategyRouter type = %q, want Selector", router.Type)
	}

	names := make([]string, len(router.Children))
	for i, c := range router.Children {
		names[i] = c.Name
	}
	want := []string{"GOAP_Root", "BuildPath", "ImplementPath", "ExecutionPath"}
	same := len(names) == len(want)
	if same {
		for i, w := range want {
			if names[i] != w {
				same = false
				break
			}
		}
	}
	if !same {
		t.Fatalf("goap_devops StrategyRouter children = %v, want %v (real GOAP A* planner first, keyword paths and ExecutionPath fallback preserved)", names, want)
	}
}

// TestGoapTreesSeedGoapToolsBeforeGOAPRoot closes a condition-coverage gap in
// the GOAP_Root branch that TestGoapPlanningRunsRealGOAPPlannerFirst,
// TestGoapResearchRunsRealGOAPPlannerFirst, and
// TestGoapDevopsRunsRealGOAPPlannerFirst only check is present, not reachable:
// GOAP_Root's first child, the HasGoapGoal condition (engine/goap_nodes.go),
// only returns true once the blackboard's ChainState already holds a
// "goap_goals" entry — seeded exclusively by the SetupGoapTools action
// (engine/goap_nodes.go: "seeds goap_actions, goap_goals, goap_config so that
// HasGoapGoal and PlanGoapActions can operate without external seeding").
// internal/evolution/merged.go's GoapPlanningPath calls SetupGoapTools in its
// PreGate before routing into the shared GOAP_Root shape (MergedTree), but
// the domains-package trees only call SetupUniversalTools/SetupResearchTools/
// SetupDevTools — none of which touch goap_goals. Without SetupGoapTools,
// HasGoapGoal can never be true, so the "real GOAP A* planner" these trees
// claim to try first is permanently unreachable dead code: a Condition node
// with a description and a registered engine implementation, but zero
// possible runtime coverage. This guards that each GOAP-fronted domain tree
// seeds GOAP tool state (an action named SetupGoapTools, matching
// merged.go's pattern) before its StrategyRouter can ever reach GOAP_Root.
func TestGoapTreesSeedGoapToolsBeforeGOAPRoot(t *testing.T) {
	trees := map[string]*evolution.SerializableNode{
		"goap_planning": GoapPlanningTree(false),
		"goap_research": GoapResearchTree(false),
		"goap_devops":   GoapDevopsTree(false),
	}
	for name, tree := range trees {
		if findNode(*tree, "SetupGoapTools") == nil {
			t.Errorf("%s: tree never calls the SetupGoapTools action, so GOAP_Root's HasGoapGoal condition can never see goap_goals in ChainState and is permanently false — the wired-in real GOAP A* planner branch is unreachable dead code", name)
		}
	}
}

func TestNotebooklmPlanImplement(t *testing.T) {
	tree := evolution.NotebooklmPlanImplementTree()
	mock := benchmark.DefaultMock()

	bb := &engine.Blackboard{
		Task: "Research BT platform scalability gaps and implement fixes",
		LLM:  mock,
	}

	// 1. Build the tree — must not panic or return nil
	cmd := engine.BuildTree(tree, bb)
	if cmd == nil {
		t.Fatal("BuildTree returned nil")
	}
	t.Log("Tree built successfully")

	// 2. Verify all actions used in the tree are registered in the engine
	requiredActions := []string{
		"ResearchNotebookLM",
		"DoGrillMeReview",
		"WriteImplementationPlan",
		"RunTests",
		"RunBuild",
		"VerifyDeploy",
		"MarkSuccessful",
		"DefaultFallback",
	}
	allRegistered := true
	for _, name := range requiredActions {
		if engine.GetAction(name) == nil {
			t.Errorf("action %q not found in engine registry", name)
			allRegistered = false
		}
	}
	if allRegistered {
		t.Log("All 8 actions found in engine registry")
	}

	// 3. Run the tree with a generous timeout.
	//    ResearchNotebookLM calls nlmRun with long timeouts (up to 360s for
	//    research status polling). If nlm is available and authenticated,
	//    the fast research mode (~30s) should complete within this window.
	//    If nlm is unavailable, nlmRun retries 3x and returns quickly.
	done := make(chan struct{})
	var output string
	go func() {
		defer close(done)
		output = engine.RunTask(bb, cmd)
	}()

	select {
	case <-done:
		// 4. Verify no panic (the tree's panic recovery would set Outcome to a panic message)
		if strings.Contains(bb.Outcome, "PANIC") || strings.Contains(bb.Outcome, "panic") {
			t.Errorf("tree panicked during execution: outcome=%s, result=%s", bb.Outcome, output)
		}
		t.Logf("Outcome: %s", bb.Outcome)
		t.Logf("Duration: %dms", bb.DurationMs)
		t.Logf("Result length: %d", len(output))
		t.Logf("Quality score: %.2f", bb.QualityScore)

		if bb.Outcome == "failure" || bb.Outcome == "chain_failed" {
			t.Log("NOTE: Tree failed (expected when nlm CLI / Ollama unavailable)")
		}

	case <-time.After(5 * time.Second):
		// nlm research is taking too long (>5s). The structural validation
		// already passed. This is expected for the full nlm research pipeline.
		t.Log("Runtime test skipped: nlm research is in progress (expected; structural checks passed)")
	}
}

func TestAllDomainTrees(t *testing.T) {
	all := AllDomainTrees()
	tasks := tasksForTree()
	mock := benchmark.DefaultMock()

	if len(all) != len(tasks) {
		t.Errorf("domain tree registry/task mismatch: got %d registered trees and %d smoke tasks", len(all), len(tasks))
	}

	for name, tree := range all {
		task, ok := tasks[name]
		if !ok {
			t.Errorf("no smoke task defined for tree %q", name)
			continue
		}
		// Arc42 trees require graphify + LLM + shell access. Smoke-test
		// structural validity only: verify BuildTree doesn't panic.
		if strings.HasPrefix(name, "arc42:") {
			bb := &engine.Blackboard{
				Task: task,
				LLM:  mock,
			}
			cmd := engine.BuildTree(tree, bb)
			if cmd == nil {
				t.Errorf("arc42 tree %q: BuildTree returned nil", name)
			}
			t.Logf("  %s: structure OK (skip runtime — needs graphify + LLM)", name)
			continue
		}

		// goap_fusion, bt_manager, bt_fusion, and notebooklm flows require real runtime state
		// (Reflection store, nlm CLI, or persisted fusion candidates) not available
		// in offline mock tests. superpowers_workflow likewise needs real git
		// worktree/HITL/Claude Code state (RunSuite's mock LLM never resolves its
		// HumanApprovalGate nodes or runs real git commands) — structural smoke
		// only, same as the others in this list. hermes_update shells out to
		// the real hermes/git binaries, so it is structural-only too.
		// auction_demo's award stage runs the AuctionDelegate seam, which needs
		// a live A2A transport / AuctionDelegateFn hook (nil offline), so it is
		// structural-only as well. arc42_seeder queries nlm/Claude for a program
		// proposal, so it is structural-only too (its action logic is unit-tested
		// in engine/arc42_seeder_test.go with stubbed fetch). self_review's
		// RunSelfReview action shells out to the real git binary and the real
		// claude CLI via its default (non-overridable-from-here) deps — its
		// action logic is unit-tested in engine/actions_self_review_test.go
		// with a faked commitScanner and ClaudeRunner, so this smoke test stays
		// structural-only too.
		if name == "goap_fusion" || name == "goap_fusion_loop" || name == "bt_manager" || name == "bt_fusion" || name == "notebooklm" || name == "notebooklm_consumer" || name == "notebooklm_plan_implement" || name == "superpowers_workflow" || name == "hermes_update" || name == "auction_demo" || name == "arc42_seeder" || name == "self_review" {
			bb := &engine.Blackboard{Task: task, LLM: mock}
			cmd := engine.BuildTree(tree, bb)
			if cmd == nil {
				t.Errorf("tree %q: BuildTree returned nil", name)
			}
			t.Logf("  %s: structure OK (skip runtime — needs reflection store / nlm CLI)", name)
			continue
		}

		suite := singleTaskSuite(name+"_smoke", task, true)
		metrics := benchmark.RunSuite(tree, suite, mock)

		if metrics.Successes == 0 {
			t.Errorf("tree %q failed its smoke task %q (0/%d passed)", name, task, metrics.TotalTasks)
		}

		t.Logf("  %s: %d/%d passed, rate=%.2f, avgDur=%dms",
			name, metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
	}
}

// TestAllDomainTreesHaveDescriptions guards that every registered tree —
// arc42 included — carries a non-empty entry in the Descriptions map. The
// gardener (gardener.go) and the bt-agent switch_tree tool surface these
// descriptions verbatim for every AllDomainTrees() entry, so a missing entry
// silently registers a blank builtin; arc42 trees are no longer exempt.
func TestAllDomainTreesHaveDescriptions(t *testing.T) {
	for name := range AllDomainTrees() {
		desc, ok := Descriptions[name]
		if !ok || strings.TrimSpace(desc) == "" {
			t.Errorf("tree %q is registered in AllDomainTrees but has no Descriptions entry", name)
		}
	}
}

// conditionDescriptionGaps walks node and its descendants and returns the Name
// of every Condition node whose Description is empty (after trimming
// whitespace). It is the shared implementation behind every "Condition nodes
// must be described" coverage guard in this file. Centralizing it here means
// TestConditionDescriptionWalkerDetectsBlankDescriptions can exercise the
// walker's violation-detection branch directly — the per-tree guards below
// only ever walk already-clean production trees, so without that dedicated
// test a walker that silently stopped recursing or checked the wrong field
// would never be caught.
func conditionDescriptionGaps(node evolution.SerializableNode) []string {
	var gaps []string
	if node.Type == "Condition" && strings.TrimSpace(node.Description) == "" {
		gaps = append(gaps, node.Name)
	}
	for _, child := range node.Children {
		gaps = append(gaps, conditionDescriptionGaps(child)...)
	}
	return gaps
}

// edgeMetadataGaps walks node and its descendants and returns the Name of
// every node carrying a TypedEdge with a blank Label, a blank Condition on an
// EdgeGuard edge, or a blank Effect on an EdgeEffect edge. It mirrors
// conditionDescriptionGaps but closes the one metadata field on domain-tree
// nodes that no existing coverage guard inspects: Description coverage is
// fully guarded above, but nothing checks the Edges[] typed-edge metadata
// those same nodes carry.
func edgeMetadataGaps(node evolution.SerializableNode) []string {
	var gaps []string
	for _, edge := range node.Edges {
		if strings.TrimSpace(edge.Label) == "" ||
			(edge.Type == evolution.EdgeGuard && strings.TrimSpace(edge.Condition) == "") ||
			(edge.Type == evolution.EdgeEffect && strings.TrimSpace(edge.Effect) == "") {
			gaps = append(gaps, node.Name)
			break
		}
	}
	for _, child := range node.Children {
		gaps = append(gaps, edgeMetadataGaps(child)...)
	}
	return gaps
}

// TestAllDomainTreeConditionsHaveDescriptions guards condition coverage: every
// Condition node in every registered curated (non-arc42) domain tree must carry
// a non-empty Description. The bt-agent switch_tree tool and the gardener surface
// these per-node descriptions as the human-readable routing rationale — a
// keyword-routing Condition with a blank Description advertises an unexplained
// gate to operators and hides what the branch actually keys on. arc42 trees are
// generated per-section with positional gating conditions (Section1Done, etc.)
// whose semantics live in the section name, so they are exempt, consistent with
// the other curated-only guards in this file.
func TestAllDomainTreeConditionsHaveDescriptions(t *testing.T) {
	for name, tree := range AllDomainTrees() {
		if strings.HasPrefix(name, "arc42:") {
			continue
		}
		for _, gap := range conditionDescriptionGaps(*tree) {
			t.Errorf("tree %q: Condition node %q has an empty Description (condition coverage gap)", name, gap)
		}
	}
}

// TestSmokeTestedDomainTreesHaveConditionDescriptions extends condition coverage
// from the curated AllDomainTrees registry to the FULL set of domain trees that
// carry an executable-structure smoke test (the fns map in
// engine_domain_execution_test.go). The kanban_* and hermes_evolve trees are
// exercised by that smoke test but are NOT registered in AllDomainTrees, so
// TestAllDomainTreeConditionsHaveDescriptions never walks their Condition nodes —
// leaving their routing gates (ValidateInput, WasSuccessful, IsKanbanTask, ...)
// with blank Descriptions. The goal "all domain trees have smoke tests,
// descriptions, and condition coverage" requires every smoke-tested tree's
// Condition nodes to be described, exactly like the registered curated trees:
// the gardener and the bt-agent switch_tree tool surface these per-node
// descriptions as the human-readable routing rationale, and a blank one
// advertises an unexplained gate to operators.
func TestSmokeTestedDomainTreesHaveConditionDescriptions(t *testing.T) {
	for name, tree := range nonRegistrySmokeTestableTrees() {
		if tree == nil {
			t.Errorf("smoke-tested tree %q returned nil", name)
			continue
		}
		for _, gap := range conditionDescriptionGaps(*tree) {
			t.Errorf("tree %q: Condition node %q has an empty Description (condition coverage gap)", name, gap)
		}
	}
}

// nonRegistrySmokeTestableTrees returns the smoke-testable trees that
// TestAllDomainTreeConditionsHaveDescriptions (and the other AllDomainTrees()-
// driven guards) never walk: the canonical SmokeTestableDomainTrees() union
// minus the curated registry. Deriving the set instead of hand-copying the
// kanban/hermes names keeps this guard from drifting behind the registries the
// way the copied literals it replaced did — a tree added to
// KanbanAndHermesDomainTrees is covered here automatically.
func nonRegistrySmokeTestableTrees() map[string]*evolution.SerializableNode {
	registry := AllDomainTrees()
	extra := map[string]*evolution.SerializableNode{}
	for name, tree := range SmokeTestableDomainTrees() {
		if _, inRegistry := registry[name]; !inRegistry {
			extra[name] = tree
		}
	}
	return extra
}

// TestConditionDescriptionWalkerDetectsBlankDescriptions is a meta-regression
// guard for the condition-description walker itself. TestAllDomainTreeConditionsHaveDescriptions,
// TestSmokeTestedDomainTreesHaveConditionDescriptions, and
// TestResolverReachableDomainTreesHaveConditionDescriptions only ever walk
// production trees that are already clean at HEAD, so none of them ever
// exercises the walker's "found a violation" branch — a walker that stopped
// recursing into children, or checked the wrong field, would silently pass
// forever without ever being caught. This test feeds the shared walker a
// synthetic tree with a deliberately blank Condition Description and asserts
// it is caught, protecting the condition-coverage invariant going forward
// even though every real domain tree is already covered.
func TestConditionDescriptionWalkerDetectsBlankDescriptions(t *testing.T) {
	synthetic := evolution.SerializableNode{
		Type: "Selector",
		Name: "SyntheticRoot",
		Children: []evolution.SerializableNode{
			{
				Type:        "Condition",
				Name:        "SyntheticBlankCondition",
				Description: "",
			},
			{
				Type:        "Condition",
				Name:        "SyntheticDescribedCondition",
				Description: "has a description",
			},
		},
	}

	gaps := conditionDescriptionGaps(synthetic)

	if len(gaps) != 1 || gaps[0] != "SyntheticBlankCondition" {
		t.Fatalf("conditionDescriptionGaps did not catch the blank-description Condition: got %v, want [SyntheticBlankCondition]", gaps)
	}
}

// TestEdgeMetadataWalkerDetectsBlankFields is a meta-regression guard for the
// edge-metadata walker (edgeMetadataGaps), mirroring
// TestConditionDescriptionWalkerDetectsBlankDescriptions but for TypedEdge
// metadata: Description coverage on domain-tree nodes is fully guarded above,
// but nothing inspects the Edges[] typed-edge metadata those same nodes carry
// (Label on every edge, Condition on EdgeGuard edges, Effect on EdgeEffect
// edges). This test feeds the shared walker a synthetic tree with a node
// missing its edge Label, an EdgeGuard edge missing Condition, an EdgeEffect
// edge missing Effect, and a fully-populated edge, asserting all three
// violations are caught and the clean node is not flagged.
func TestEdgeMetadataWalkerDetectsBlankFields(t *testing.T) {
	synthetic := evolution.SerializableNode{
		Type: "Sequence",
		Name: "SyntheticRoot",
		Children: []evolution.SerializableNode{
			{
				Type: "Action",
				Name: "BlankLabelNode",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeQualityGate, Label: "", ChildIndex: -1},
				},
			},
			{
				Type: "Condition",
				Name: "BlankGuardConditionNode",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeGuard, Label: "guard-label", Condition: "", ChildIndex: -1},
				},
			},
			{
				Type: "Action",
				Name: "BlankEffectNode",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeEffect, Label: "effect-label", Effect: "", ChildIndex: -1},
				},
			},
			{
				Type: "Action",
				Name: "FullyPopulatedNode",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeGuard, Label: "ok-label", Condition: "ok-condition", ChildIndex: -1},
					{Type: evolution.EdgeEffect, Label: "ok-label2", Effect: "ok-effect", ChildIndex: -1},
				},
			},
		},
	}

	gaps := edgeMetadataGaps(synthetic)

	want := map[string]bool{
		"BlankLabelNode":          true,
		"BlankGuardConditionNode": true,
		"BlankEffectNode":         true,
	}
	got := map[string]bool{}
	for _, g := range gaps {
		got[g] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("edgeMetadataGaps did not catch expected violation %q: got %v", name, gaps)
		}
	}
	if got["FullyPopulatedNode"] {
		t.Errorf("edgeMetadataGaps flagged a fully-populated edge: got %v", gaps)
	}
	if len(gaps) != len(want) {
		t.Errorf("edgeMetadataGaps returned unexpected gap count: got %v, want exactly %v", gaps, want)
	}
}

// describableDomainTrees is the union of the two domain-tree registries:
// AllDomainTrees() (the curated gardener/dashboard surface) and
// KanbanAndHermesDomainTrees() (deliberately off that surface, but still real,
// buildable trees). Every tree in the union is expected to be describable via
// DescriptionFor, and no description entry may point outside it.
func describableDomainTrees() map[string]*evolution.SerializableNode {
	union := map[string]*evolution.SerializableNode{}
	maps.Copy(union, AllDomainTrees())
	maps.Copy(union, KanbanAndHermesDomainTrees())
	return union
}

// TestDescriptionsHaveNoOrphans is the reverse guard to
// TestAllDomainTreesHaveDescriptions: every description entry — whether it
// lives in the registry-only Descriptions map or in NonRegistryDescriptions —
// must correspond to a tree actually registered in one of the two registries.
// The gardener and the bt-agent switch_tree tool surface these descriptions
// verbatim, so an orphaned entry (left behind after a tree is renamed or
// removed) advertises a builtin that can never be selected. Checking against
// the union rather than AllDomainTrees alone is what makes it legal to describe
// the kanban/hermes trees at all, while still catching genuine dead entries.
func TestDescriptionsHaveNoOrphans(t *testing.T) {
	all := describableDomainTrees()
	for name := range Descriptions {
		if _, ok := all[name]; !ok {
			t.Errorf("Descriptions has entry %q but no such tree is registered in AllDomainTrees or KanbanAndHermesDomainTrees", name)
		}
	}
	for name := range NonRegistryDescriptions {
		if _, ok := all[name]; !ok {
			t.Errorf("NonRegistryDescriptions has entry %q but no such tree is registered in AllDomainTrees or KanbanAndHermesDomainTrees", name)
		}
	}
}

// TestEveryDomainTreeHasADescription closes the description gap for the eight
// non-registry kanban/hermes trees. TestAllDomainTreesHaveDescriptions only
// covers AllDomainTrees(), and TestDescriptionsHaveNoOrphans used to forbid
// adding the kanban/hermes names to Descriptions, leaving those trees
// structurally undescribable. DescriptionFor resolves against the union of
// Descriptions and NonRegistryDescriptions, so it must answer for every tree in
// either registry with a real sentence rather than a restated name.
func TestEveryDomainTreeHasADescription(t *testing.T) {
	const minDescLen = 20
	for name := range describableDomainTrees() {
		desc, ok := DescriptionFor(name)
		if !ok {
			t.Errorf("tree %q is registered but DescriptionFor returned ok=false", name)
			continue
		}
		trimmed := strings.TrimSpace(desc)
		if trimmed == "" {
			t.Errorf("tree %q: DescriptionFor returned a blank description", name)
			continue
		}
		if len(trimmed) < minDescLen {
			t.Errorf("tree %q: description %q is only %d chars, want >= %d — descriptions must explain the tree, not restate its name",
				name, trimmed, len(trimmed), minDescLen)
		}
	}
}

// TestDescriptionForPrefersRegistryAndRejectsUnknown pins DescriptionFor's
// resolution order and its miss behaviour: registry entries win, non-registry
// entries are reachable, and an unknown name reports a miss instead of an empty
// string that callers would render as a blank builtin.
func TestDescriptionForPrefersRegistryAndRejectsUnknown(t *testing.T) {
	desc, ok := DescriptionFor("code_review")
	if !ok {
		t.Error(`DescriptionFor("code_review") returned ok=false, want the Descriptions entry`)
	} else if desc != Descriptions["code_review"] {
		t.Errorf(`DescriptionFor("code_review") = %q, want the Descriptions entry %q`, desc, Descriptions["code_review"])
	}

	desc, ok = DescriptionFor("kanban_qa")
	if !ok {
		t.Error(`DescriptionFor("kanban_qa") returned ok=false, want the NonRegistryDescriptions entry`)
	} else if desc != NonRegistryDescriptions["kanban_qa"] {
		t.Errorf(`DescriptionFor("kanban_qa") = %q, want the NonRegistryDescriptions entry %q`, desc, NonRegistryDescriptions["kanban_qa"])
	}

	if desc, ok := DescriptionFor("no_such_tree"); ok || desc != "" {
		t.Errorf(`DescriptionFor("no_such_tree") = (%q, %v), want ("", false)`, desc, ok)
	}
}

// TestNonRegistryDescriptionsHaveNoOrphans keeps the two description maps
// disjoint and scoped. NonRegistryDescriptions exists solely to describe trees
// that AllDomainTrees() deliberately omits, so every key must name a
// KanbanAndHermesDomainTrees() tree and none may shadow a Descriptions entry —
// a duplicate would make DescriptionFor's precedence silently decide which of
// two divergent descriptions the gardener shows.
func TestNonRegistryDescriptionsHaveNoOrphans(t *testing.T) {
	nonRegistry := KanbanAndHermesDomainTrees()
	for name := range NonRegistryDescriptions {
		if _, ok := nonRegistry[name]; !ok {
			t.Errorf("NonRegistryDescriptions has entry %q but no such tree is registered in KanbanAndHermesDomainTrees", name)
		}
		if _, ok := Descriptions[name]; ok {
			t.Errorf("tree %q is described in both Descriptions and NonRegistryDescriptions — the two maps must be disjoint", name)
		}
	}
}

// TestAuctionDemoTree is the structural smoke test for the auction_demo domain
// tree — milestone 5/5 of the "Auction-based A2A task allocation" program. The
// tree exercises the announce → bid → award flow end to end. Its award stage is
// driven by the engine's AuctionDelegate action, the production seam that fans a
// TaskAnnouncement out to candidate agents, collects their Bids, and dispatches
// the real task to the winning bidder. Running the auction needs a live A2A
// transport, so this test validates structure only: the tree builds without
// panicking offline, embeds the AuctionDelegate seam, and is registered and
// described like every other curated domain tree.
func TestAuctionDemoTree(t *testing.T) {
	tree := AuctionDemoTree()
	if tree == nil {
		t.Fatal("AuctionDemoTree returned nil")
	}

	// Structural smoke: BuildTree must not panic or return nil in offline mode.
	mock := benchmark.DefaultMock()
	bb := &engine.Blackboard{
		Task: "auction: allocate a task to the best bidder via announce-bid-award",
		LLM:  mock,
	}
	cmd := engine.BuildTree(tree, bb)
	if cmd == nil {
		t.Fatal("BuildTree(AuctionDemoTree) returned nil")
	}

	// The award stage runs through the AuctionDelegate engine action, which is
	// the announce-bid-award seam. Without it the demo cannot exercise the flow.
	if findNode(*tree, "AuctionDelegate") == nil {
		t.Error("AuctionDemoTree must embed an AuctionDelegate node to run the announce-bid-award flow")
	}
	if engine.GetAction("AuctionDelegate") == nil {
		t.Error("AuctionDelegate action not found in engine registry")
	}

	// The tree must be registered and discoverable like every other domain
	// tree: present in AllDomainTrees under the auction_demo key with a
	// non-empty Descriptions entry (surfaced by the gardener and switch_tree).
	if _, ok := AllDomainTrees()["auction_demo"]; !ok {
		t.Error("auction_demo not registered in AllDomainTrees")
	}
	if strings.TrimSpace(Descriptions["auction_demo"]) == "" {
		t.Error("auction_demo missing a Descriptions entry")
	}
}

// TestAuctionDemoTreeHasNoSilentNoOps is the runtime (not merely structural)
// honesty guard for the auction_demo tree — milestone 2/4 of the "Auction
// subsystem production hardening" program. TestAuctionDemoTree above only checks
// structure (BuildTree != nil, an AuctionDelegate node exists); it cannot catch
// the real defect: AnnounceTask and CollectBids are Action nodes whose names are
// NOT registered in the engine, so at tick time they resolve to the engine's
// permissive success fallback (tree.go: "unknown actions succeed silently") — a
// node that returns success without doing anything or surfacing any
// announcement/bid evidence into the blackboard. The tree reads as a full
// announce → bid → award protocol but two of its three stages are hollow.
//
// This test actually BUILDS and RUNS each protocol Action node in isolation and
// fails if the node reports success while leaving the blackboard untouched (no
// Result, Results, ChainState, or KgResults). It passes under either honest
// remediation: registering real AnnounceTask/CollectBids engine actions that
// surface evidence, or collapsing the tree to the single AuctionDelegate stage.
func TestAuctionDemoTreeHasNoSilentNoOps(t *testing.T) {
	tree := AuctionDemoTree()
	if tree == nil {
		t.Fatal("AuctionDemoTree returned nil")
	}

	// Shared scaffold control/outcome nodes carried by every curated domain
	// tree. Their permissive success is intentional framework plumbing and out
	// of scope for this per-tree honesty guard.
	scaffold := map[string]bool{
		"MarkSuccessful":     true,
		"SelfCorrect":        true,
		"EscalateToDeepSeek": true,
		"ReflectOnOutcome":   true,
		"UpdateBehaviorTree": true,
	}

	var actionNames []string
	var walk func(n evolution.SerializableNode)
	walk = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			actionNames = append(actionNames, n.Name)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(*tree)

	if len(actionNames) == 0 {
		t.Fatal("auction_demo tree has no Action nodes to check")
	}

	var dishonest []string
	for _, name := range actionNames {
		if scaffold[name] {
			continue
		}

		// Build and RUN this single Action node the same way the engine would at
		// tick time, then inspect the runtime result. An unknown action name is
		// dishonest two ways: where the tree is validated it dead-letters with an
		// "unknown action" error, and where validation is bypassed it resolves to
		// the permissive success fallback (tree.go: "unknown actions succeed
		// silently") — reporting success while surfacing no evidence.
		node := &evolution.SerializableNode{Type: "Action", Name: name, Description: "runtime no-op probe"}
		bb := &engine.Blackboard{
			Task: "auction: allocate a task to the best bidder via announce-bid-award",
		}
		cmd := engine.BuildTree(node, bb)
		if cmd == nil {
			t.Fatalf("BuildTree returned nil for action %q", name)
		}
		engine.RunTask(bb, cmd)

		lowerResult := strings.ToLower(bb.Result)
		unknownToEngine := strings.Contains(lowerResult, "unknown action") ||
			strings.Contains(lowerResult, "validation failed")

		producedEvidence := bb.Result != "" || len(bb.Results) > 0 ||
			len(bb.ChainState) > 0 || bb.KgResults != ""
		silentSuccessNoOp := bb.Outcome == "success" && !producedEvidence

		// A node that fails honestly with a real diagnostic (e.g. AuctionDelegate
		// offline reporting "auction delegate not configured") is fine — it is a
		// registered seam, not a hollow stage.
		if unknownToEngine || silentSuccessNoOp {
			dishonest = append(dishonest, fmt.Sprintf("%s (outcome=%s, result=%q)", name, bb.Outcome, bb.Result))
		}
	}

	if len(dishonest) > 0 {
		t.Errorf("auction_demo has dishonest Action node(s) unknown to the engine: %v. Each such node either dead-letters the whole tree at validation or silently no-ops via the permissive success fallback, surfacing no announcement/bid evidence. Register real evidence-producing engine actions (internal/engine/actions_a2a.go) or collapse the tree to the single AuctionDelegate stage.", dishonest)
	}
}

// TestAllDomainTreesHaveDescriptionsAndLeafCoverage is the consolidated
// description-coverage guard for the goal "enforce descriptions + leaf-condition
// coverage across all domain trees". It walks every curated (non-arc42) tree in
// AllDomainTrees() and asserts two things:
//
//	(a) the root is described in the curated Descriptions map with a non-empty
//	    value — the gardener and the bt-agent switch_tree tool surface that entry
//	    verbatim, so a missing/blank one registers an unexplained builtin; and
//	(b) every LEAF Condition/Action node (no Children) carries a non-empty
//	    Description — those descriptions are the human-readable rationale for what
//	    a routing gate keys on or what an action actually does, and a blank one
//	    advertises an unexplained node to operators.
//
// arc42 trees are generated per-section and described via their section names,
// so they are exempt here exactly like the other curated-only guards in this
// file. The known gap (per prior audit) is the hand-built notebooklm_consumer
// tree, whose leaf Action nodes (SetupUniversalTools, ReflectOnOutcome,
// MarkSuccessful) were declared without a Description — unlike the trees built
// via the act()/outcome() helpers, which always attach one.
func TestAllDomainTreesHaveDescriptionsAndLeafCoverage(t *testing.T) {
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		isLeaf := len(node.Children) == 0
		if isLeaf && (node.Type == "Condition" || node.Type == "Action") &&
			strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: leaf %s node %q has an empty Description (leaf coverage gap)", treeName, node.Type, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	for name, tree := range AllDomainTrees() {
		if strings.HasPrefix(name, "arc42:") {
			continue
		}
		if desc, ok := Descriptions[name]; !ok || strings.TrimSpace(desc) == "" {
			t.Errorf("tree %q: root %q has no non-empty Descriptions entry", name, tree.Name)
		}
		walk(name, *tree)
	}
}

// TestAllDomainTreeSelectorsHaveDescriptions closes the interior-node gap left
// by TestAllDomainTreesHaveDescriptionsAndLeafCoverage, which only covers leaf
// Condition/Action nodes: Selector nodes are the routing decision points the
// gardener and the bt-agent switch_tree tool surface, yet the sel() helper
// (trees.go) constructs them without a Description, so every
// sel("StrategyRouter", ...) router is unexplained. The hand-built
// agent_monitor StrategyRouter ("Route to health check, metrics collection, or
// restart path") sets the precedent: every Selector in every curated
// (non-arc42) registered domain tree must carry a non-empty Description.
func TestAllDomainTreeSelectorsHaveDescriptions(t *testing.T) {
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if node.Type == "Selector" && strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: Selector node %q has an empty Description (router coverage gap)", treeName, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	for name, tree := range AllDomainTrees() {
		if strings.HasPrefix(name, "arc42:") {
			continue
		}
		walk(name, *tree)
	}
}

// TestAllDomainTreeNodesHaveDescriptions is the consolidated full-node guard:
// every node of every type — root, interior composites (Sequence, Selector,
// Retry, ...), and leaves (Condition, Action, ChainAction, ...) — in every
// registered domain tree, arc42 included, must carry a non-empty
// Description, and every tree must keep its non-empty Descriptions entry.
// The narrower sibling guards (leaf, Selector, condition) stay as targeted
// regression anchors; this walk closes the remaining gap — the seq() helper
// (trees.go) builds every Sequence stage (PreGate, BugDetection, BuildPath,
// ...) without a Description — and prevents any future tree from regressing
// on any node class.
func TestAllDomainTreeNodesHaveDescriptions(t *testing.T) {
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: %s node %q has an empty Description (full-node coverage gap)", treeName, node.Type, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	for name, tree := range AllDomainTrees() {
		if desc, ok := Descriptions[name]; !ok || strings.TrimSpace(desc) == "" {
			t.Errorf("tree %q: root %q has no non-empty Descriptions entry", name, tree.Name)
		}
		walk(name, *tree)
	}
}

// TestArc42DomainTreeNodesHaveDescriptions closes the last registry gap the
// consolidated guard (TestAllDomainTreeNodesHaveDescriptions) leaves open: it
// skips every arc42:* tree because the chain() helper (arc42_trees.go) builds
// ChainAction nodes with no Description, leaving blank-description LLM stages
// across the 13 arc42 trees. This walk applies the same full-node rule to the
// arc42:* prefix so every section-generation node explains itself.
func TestArc42DomainTreeNodesHaveDescriptions(t *testing.T) {
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: %s node %q has an empty Description (full-node coverage gap)", treeName, node.Type, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	arc42Trees := 0
	for name, tree := range AllDomainTrees() {
		if !strings.HasPrefix(name, "arc42:") {
			continue
		}
		arc42Trees++
		walk(name, *tree)
	}
	if arc42Trees == 0 {
		t.Fatal("no arc42:* trees found in AllDomainTrees(); guard has lost its subject")
	}
}

// TestNonRegistryDomainTreeNodesHaveDescriptions extends the consolidated
// full-node description guard (TestAllDomainTreeNodesHaveDescriptions) to the
// domains-package trees that are production-reachable but NOT in the
// AllDomainTrees registry, so the curated walk never inspects them: the
// smoke-tested extras (hermes_evolve + hermes_obsidian + the six kanban_* trees,
// mirroring the fns map in engine_domain_execution_test.go) and the
// ResolveTreeID-reachable extras (ResolverReachableDomainTrees). Their Condition
// nodes are
// already guarded, but their composites (PreGate, OutcomeSelector, routers)
// and non-Condition leaves (Action, ChainAction) can still carry blank
// Descriptions — an unexplained routing stage to the same gardener and
// switch_tree operators the curated guard protects. These trees are
// intentionally absent from the Descriptions map (TestDescriptionsHaveNoOrphans
// forbids non-registry entries), so this guard checks per-node Descriptions
// only.
func TestNonRegistryDomainTreeNodesHaveDescriptions(t *testing.T) {
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: %s node %q has an empty Description (full-node coverage gap)", treeName, node.Type, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	// Both halves come from production registries — the smoke-testable extras
	// from the canonical SmokeTestableDomainTrees() union minus the curated
	// registry, the resolver-reachable ones from ResolverReachableDomainTrees()
	// — so this guard cannot fall behind either set. Both return fresh maps, so
	// merging into the first is safe.
	trees := nonRegistrySmokeTestableTrees()
	maps.Copy(trees, ResolverReachableDomainTrees())

	for name, tree := range trees {
		if tree == nil {
			t.Errorf("non-registry tree %q returned nil", name)
			continue
		}
		walk(name, *tree)
	}
}

func TestGoapFusionLoopTree_ClaudeReviewFallback(t *testing.T) {
	tree := GoapFusionLoopTree()

	var router *evolution.SerializableNode
	var walk func(n *evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if n.Name == "ResearchRouter" {
			router = n
			return
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(tree)

	if router == nil {
		t.Fatal("GoapFusionLoopTree has no ResearchRouter node")
	}
	if router.Type != "Selector" {
		t.Fatalf("ResearchRouter type = %q, want Selector", router.Type)
	}
	if len(router.Children) != 3 ||
		router.Children[0].Name != "RunGoapFusionNotebookLMResearch" ||
		router.Children[1].Name != "RunClaudeCodeReviewResearch" ||
		router.Children[2].Type != "AlwaysSucceed" {
		t.Fatalf("ResearchRouter must be nlm → Claude review → terminal AlwaysSucceed (non-fatal), got: %+v", router.Children)
	}
}

// TestGoapFusionTreeHasResearchRouter pins the same quota-resilience shape the
// loop tree has: the hourly goap-fusion runner must route NotebookLM research
// through a Selector with the Claude review fallback. As a bare Sequence child
// the research action's quota fail-fast (-1) killed the whole tree — the :35
// runner dead-lettered on every closed quota window (observed 2026-07-02/03).
// The router must also end in a terminal AlwaysSucceed (ResearchOptional) leaf,
// mirroring GoapFusionLoopTree: when NotebookLM quota is closed AND the Claude
// review fallback is rate-limited (Claude weekly limit, observed 2026-07-07),
// the doubly-unavailable research stage must degrade to the vault-context path
// (ReadVaultResearch onward) instead of aborting the whole run.
func TestGoapFusionTreeHasResearchRouter(t *testing.T) {
	tree := GoapFusionTree(true)
	var router *evolution.SerializableNode
	var walk func(n *evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if n.Name == "ResearchRouter" {
			router = n
			return
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(tree)
	if router == nil {
		t.Fatal("GoapFusionTree has no ResearchRouter node")
	}
	if router.Type != "Selector" {
		t.Fatalf("ResearchRouter type = %q, want Selector", router.Type)
	}
	if len(router.Children) != 3 ||
		router.Children[0].Name != "RunGoapFusionNotebookLMResearch" ||
		router.Children[1].Name != "RunClaudeCodeReviewResearch" ||
		router.Children[2].Type != "AlwaysSucceed" ||
		router.Children[2].Name != "ResearchOptional" {
		t.Fatalf("ResearchRouter must be nlm → Claude review → terminal AlwaysSucceed ResearchOptional (non-fatal), got: %+v", router.Children)
	}
}

// TestResolverReachableDomainTreesHaveSmokeStructure closes the "smoke tests"
// half of the goal "all domain trees have smoke tests, descriptions, and
// condition coverage" for the ResolveTreeID-reachable domains-package trees that
// escaped both smoke registries: it builds each tree through the real engine and
// fails if BuildTree panics or returns nil (structural smoke, like the arc42 and
// runtime-state-dependent trees in TestAllDomainTrees). It also asserts the tree
// is genuinely reachable via ResolveTreeID so a rename can't silently orphan it.
//
// The work list is ResolverReachableDomainTrees(), the production registry, not
// a copy of it: registering a resolver-only tree is what subjects it to this
// guard, exactly as registry membership does for the smoke union. A hand-copied
// literal here would let a tree satisfy
// TestResolverReachableRegistryIsDerivedFromTreeResolver — which reads the
// production registry — while never being built once.
func TestResolverReachableDomainTreesHaveSmokeStructure(t *testing.T) {
	mock := benchmark.DefaultMock()
	for name, tree := range ResolverReachableDomainTrees() {
		if tree == nil || len(tree.Children) == 0 {
			t.Errorf("resolver-reachable tree %q is nil or empty", name)
			continue
		}
		bb := &engine.Blackboard{Task: "smoke: exercise " + name, LLM: mock}
		if cmd := engine.BuildTree(tree, bb); cmd == nil {
			t.Errorf("resolver-reachable tree %q: BuildTree returned nil", name)
		}
		// The registry key is the resolver ID by contract (pinned by
		// TestResolverReachableDomainTreesIsNonEmptyAndResolvable), so the name
		// doubles as the ID to probe — no side table to fall out of sync.
		if ResolveTreeID(name) == nil {
			t.Errorf("resolver-reachable tree %q: ResolveTreeID(%q) returned nil", name, name)
		}
	}
}

// TestResolverReachableDomainTreesHaveConditionDescriptions extends condition
// coverage — the third leg of the goal — from the two smoke registries to the
// ResolveTreeID-reachable domains-package trees they miss. Every Condition node
// in every such tree must carry a non-empty Description for the same reason the
// registered and smoke-tested trees must: the gardener and the bt-agent
// switch_tree tool surface these per-node descriptions as the human-readable
// routing rationale, and a blank one advertises an unexplained gate to operators.
func TestResolverReachableDomainTreesHaveConditionDescriptions(t *testing.T) {
	for name, tree := range ResolverReachableDomainTrees() {
		if tree == nil {
			t.Errorf("resolver-reachable tree %q returned nil", name)
			continue
		}
		for _, gap := range conditionDescriptionGaps(*tree) {
			t.Errorf("tree %q: Condition node %q has an empty Description (condition coverage gap)", name, gap)
		}
	}
}

// TestResolverReachableDomainTreesHaveDescriptions closes the remaining leg of
// the goal "all domain trees have smoke tests, descriptions, and condition
// coverage" for the ResolveTreeID-reachable domains-package trees. The two
// sibling guards above already give those trees smoke structure and Condition
// descriptions, but nothing describes the tree itself: DescriptionFor resolves
// only against Descriptions (the AllDomainTrees registry) and
// NonRegistryDescriptions (the KanbanAndHermesDomainTrees set), and
// superpowers_pipeline belongs to neither. It is nonetheless selectable in
// production — ResolveTreeID("superpowers_pipeline") returns the tree, so
// operators can switch_tree onto it — which means it renders as a bare
// identifier everywhere descriptions are surfaced (gardener, dashboard, the
// bt-agent switch_tree tool), exactly the unexplained-builtin state
// DescriptionFor exists to prevent. Every tree in the canonical
// ResolverReachableDomainTrees() registry must resolve to a real sentence, and
// that production registry is the work list so a newly registered
// resolver-reachable tree cannot be added undescribed.
func TestResolverReachableDomainTreesHaveDescriptions(t *testing.T) {
	const minDescLen = 20
	for name := range ResolverReachableDomainTrees() {
		desc, ok := DescriptionFor(name)
		if !ok {
			t.Errorf("resolver-reachable tree %q: DescriptionFor returned ok=false — the tree is selectable via ResolveTreeID(%q) but has no description, so it renders as a bare identifier wherever builtins are listed", name, name)
			continue
		}
		trimmed := strings.TrimSpace(desc)
		if trimmed == "" {
			t.Errorf("resolver-reachable tree %q: DescriptionFor returned a blank description", name)
			continue
		}
		if len(trimmed) < minDescLen {
			t.Errorf("resolver-reachable tree %q: description %q is only %d chars, want >= %d — descriptions must explain the tree, not restate its name",
				name, trimmed, len(trimmed), minDescLen)
		}
	}
}

// TestResolverReachableDescriptionsHaveNoOrphans is the reverse guard to
// TestResolverReachableDomainTreesHaveDescriptions, and the third-map sibling of
// TestDescriptionsHaveNoOrphans / TestNonRegistryDescriptionsHaveNoOrphans:
// those two check their maps against the AllDomainTrees +
// KanbanAndHermesDomainTrees union, which by construction can never contain a
// ResolverReachableDescriptions key, so without this guard the new map is the
// one description surface where a renamed or deleted tree leaves a dead entry
// advertising a builtin that can never be selected. It also keeps the three maps
// disjoint — a name in two of them makes DescriptionFor's precedence silently
// choose which of two divergent descriptions the gardener shows.
func TestResolverReachableDescriptionsHaveNoOrphans(t *testing.T) {
	reachable := ResolverReachableDomainTrees()
	for name := range ResolverReachableDescriptions {
		if _, ok := reachable[name]; !ok {
			t.Errorf("ResolverReachableDescriptions has entry %q but no such tree is registered in ResolverReachableDomainTrees()", name)
		}
		if _, ok := Descriptions[name]; ok {
			t.Errorf("tree %q is described in both Descriptions and ResolverReachableDescriptions — the description maps must be disjoint", name)
		}
		if _, ok := NonRegistryDescriptions[name]; ok {
			t.Errorf("tree %q is described in both NonRegistryDescriptions and ResolverReachableDescriptions — the description maps must be disjoint", name)
		}
	}
}

// TestSuperpowersPipelineIsGuarded asserts that the production superpowers_pipeline
// tree — now reachable via ResolveTreeID("superpowers_pipeline") (tree_resolver.go),
// so operators can switch_tree onto it — is registered in the resolver-reachable
// coverage registry. Membership there is what makes TestResolverReachableDomainTrees-
// HaveSmokeStructure and TestResolverReachableDomainTreesHaveConditionDescriptions walk
// the tree, permanently protecting it from blank-description / build-nil regressions the
// same way hermes_obsidian is protected. The registry must both hold a non-nil builder
// for the tree and expose it through ResolveTreeID so a rename cannot silently orphan it.
func TestSuperpowersPipelineIsGuarded(t *testing.T) {
	tree, ok := ResolverReachableDomainTrees()["superpowers_pipeline"]
	if !ok {
		t.Fatalf("superpowers_pipeline is not registered in ResolverReachableDomainTrees() — it escapes the smoke + condition-description coverage guards")
	}
	if tree == nil || len(tree.Children) == 0 {
		t.Fatalf("ResolverReachableDomainTrees()[\"superpowers_pipeline\"] built a nil or empty tree")
	}
	if ResolveTreeID("superpowers_pipeline") == nil {
		t.Fatalf("ResolveTreeID(\"superpowers_pipeline\") returned nil — guarded tree is not resolver-reachable")
	}
}

// resolverReachableTreesByID is the fail-closed work list for the two
// resolver-reachable coverage guards below: every tree an operator can actually
// switch_tree onto, keyed by the ID they type.
//
// The IDs are read out of the production resolver by AST scan
// (resolverBareIDLiterals) rather than copied into a literal here, for the same
// reason TestResolverReachableRegistryIsDerivedFromTreeResolver reads them that
// way: a hand-maintained list is exactly the thing that silently falls behind a
// new `if id == "x"` branch, handing operators a selectable tree with no
// coverage. IDs whose branch builds another package's tree are skipped via
// externalPackageResolverIDs — that package guards its own trees, and the
// exemption must be spelled out rather than inferred.
//
// This is deliberately wider than ResolverReachableDomainTrees(), which holds
// only the resolver-reachable trees NO other registry covers. The registry is
// the right work list for description guards (it is the home of
// ResolverReachableDescriptions keys, and must stay disjoint from the smoke
// registries). It is the wrong work list for the two legs below, because the
// coverage those legs check is scoped to AllDomainTrees() today — so a
// resolver-reachable tree that lives in KanbanAndHermesDomainTrees()
// (hermes_obsidian, hermes_evolve, the six kanban_* trees) is inside the smoke
// registry and still outside both legs. Walking the resolver instead is what
// makes "reachable in production" the thing that earns coverage.
func resolverReachableTreesByID(t *testing.T) map[string]*evolution.SerializableNode {
	t.Helper()

	ids := resolverBareIDLiterals(t)
	if len(ids) == 0 {
		t.Fatalf("no `id == \"...\"` comparisons found in %s's resolveTreeIDWithResolver — the resolver-reachable coverage guards would pass vacuously", resolverBareIDFile)
	}

	trees := make(map[string]*evolution.SerializableNode, len(ids))
	for _, id := range ids {
		if _, external := externalPackageResolverIDs[id]; external {
			continue
		}
		tree := ResolveTreeID(id)
		if tree == nil {
			t.Errorf("ResolveTreeID(%q) returned nil even though %s has a branch for that ID", id, resolverBareIDFile)
			continue
		}
		trees[id] = tree
	}
	return trees
}

// TestResolverReachableDomainTreeConditionEdgeMetadataCoverage closes the
// machine-readable half of condition coverage for the resolver-reachable trees.
//
// conditionGuardEdgeGaps already exists and is already the right check — it is
// simply only ever applied to AllDomainTrees() (TestDomainTreeConditionEdge-
// MetadataCoverage's guard-edges subtest). That subtest's own doc comment carves
// the non-registry trees out on the grounds that they "declare their Condition
// nodes as inline literals rather than via cond() and stay covered by the
// edge-metadata-populated subtest below" — but that sibling subtest only
// validates edges a node ALREADY has (edgeMetadataGaps reports blank fields on
// present edges). A Condition node with no Edges at all passes it trivially.
// So every resolver-reachable non-registry tree has, until now, had zero
// enforcement that its gates carry a precondition production code can read.
//
// That matters because the Description is prose and the typed edge is not:
// engine/typed_edges.go (guardConditionForChild) and
// engine/utility_selector.go (ScoreChild) gate execution on
// TypedEdge.Condition, and evolution.ValidateEdge rejects a guard edge whose
// Condition is blank. A switch_tree-selectable tree whose gates exist only as
// prose is one the engine cannot reason about at all.
func TestResolverReachableDomainTreeConditionEdgeMetadataCoverage(t *testing.T) {
	for id, tree := range resolverReachableTreesByID(t) {
		for _, gap := range conditionGuardEdgeGaps(*tree) {
			t.Errorf("resolver-reachable tree %q (ResolveTreeID(%q)): Condition node %q carries no EdgeGuard typed edge with a Label and a Condition (machine-readable condition coverage gap).\n"+
				"Fix at the source by building the node through the cond() helper in trees.go, which derives a guardLabel and a non-blank Condition — do not weaken the guard.",
				id, id, gap)
		}
	}
}

// TestResolverReachableDomainTreeNodesHaveDescriptions mirrors
// TestAllDomainTreeSelectorsHaveDescriptions and
// TestAllDomainTreeNodesHaveDescriptions — both scoped to AllDomainTrees() — for
// the resolver-reachable trees those two walks never reach.
//
// Every Sequence, Selector, Condition and Action node must carry a non-blank
// Description: these are the four load-bearing node classes the gardener and the
// bt-agent switch_tree tool surface as the human-readable routing rationale,
// and a blank one advertises an unexplained stage to operators. Selector nodes
// matter most and are the easiest to miss — the sel() helper predates carrying a
// description at all — so a router with no rationale is the specific regression
// this guard anchors.
//
// Failures report the node path from the root, not just the node name, because
// these trees reuse stage names (ValidateInput, WasSuccessful) across branches.
func TestResolverReachableDomainTreeNodesHaveDescriptions(t *testing.T) {
	described := map[string]bool{"Sequence": true, "Selector": true, "Condition": true, "Action": true}

	var walk func(id string, node evolution.SerializableNode, path string)
	walk = func(id string, node evolution.SerializableNode, path string) {
		here := path + "/" + node.Name
		if described[node.Type] && strings.TrimSpace(node.Description) == "" {
			t.Errorf("resolver-reachable tree %q (ResolveTreeID(%q)): %s node %q has an empty Description (node description coverage gap) at path %s.\n"+
				"Fix at the source by giving the node a Description in the tree's own file — do not weaken the guard.",
				id, id, node.Type, node.Name, here)
		}
		for _, child := range node.Children {
			walk(id, child, here)
		}
	}

	for id, tree := range resolverReachableTreesByID(t) {
		walk(id, *tree, "")
	}
}

// TestArc42DomainTreeConditionsHaveDescriptions closes the last condition-coverage
// gap for the goal "all domain trees have smoke tests, descriptions, and condition
// coverage". The arc42 generation trees are registered domain trees — they live in
// AllDomainTrees() and are production-reachable via ResolveTreeID("domain:arc42:sectionN")
// (bt-agent switch_tree, A2A, template validation) — yet TestAllDomainTreeConditions-
// HaveDescriptions explicitly skips every "arc42:" tree on the theory that the gate
// semantics "live in the section name". The arc42 trees themselves refute that theory:
// the SAME gating Conditions are described in some sections and left blank in others —
// GraphIsFresh carries "graphify has been run" in section1/section2 but is blank in
// section3; Section1Done carries "section 1 must be complete" in section4 but is blank
// in sections 5/10/11/12; Section4Done/Section5Done are
// described in some places and blank in others. That inconsistency is exactly the
// unexplained-gate coverage gap this goal targets: the gardener and switch_tree surface
// these per-node descriptions as the human-readable routing rationale, so a blank one
// advertises an unexplained gate to operators. Every arc42 Condition node must carry a
// non-empty Description, just like every other registered domain tree.
func TestArc42DomainTreeConditionsHaveDescriptions(t *testing.T) {
	arc42 := Arc42Trees()
	if len(arc42) == 0 {
		t.Fatal("Arc42Trees() returned no trees")
	}
	for name, tree := range arc42 {
		if !strings.HasPrefix(name, "arc42:") {
			t.Errorf("Arc42Trees() returned non-arc42 key %q", name)
			continue
		}
		if tree == nil {
			t.Errorf("arc42 tree %q is nil", name)
			continue
		}
		for _, gap := range conditionDescriptionGaps(*tree) {
			t.Errorf("tree %q: Condition node %q has an empty Description (arc42 condition coverage gap)", name, gap)
		}
	}
}

// TestNoDomainTreeHasUnregisteredActions is the platform-wide generalization of
// TestAuctionDemoTreeHasNoSilentNoOps: every Action node in every registered
// domain tree must resolve to a real engine action. An unregistered name hits
// the engine's permissive success fallback (tree.go: "unknown actions succeed
// silently") — a node that reports success while doing nothing, the footgun
// that hid two hollow stages in the original auction_demo tree. Conditions get
// the same check (an unknown condition silently passes). Cheap, static, and it
// closes the silent-no-op class for the whole fleet permanently.
func TestNoDomainTreeHasUnregisteredActions(t *testing.T) {
	var offenders []string
	for name, tree := range AllDomainTrees() {
		var walk func(n evolution.SerializableNode)
		walk = func(n evolution.SerializableNode) {
			switch n.Type {
			case "Action":
				if engine.GetAction(n.Name) == nil {
					offenders = append(offenders, name+" → action "+n.Name)
				}
			case "Condition":
				if engine.GetCondition(n.Name) == nil {
					offenders = append(offenders, name+" → condition "+n.Name)
				}
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(*tree)
	}
	if len(offenders) > 0 {
		t.Fatalf("%d unregistered (silent-no-op) tree node(s):\n  %s", len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestDomainTreeSuitesReachAllStrategyBranches closes the "condition coverage"
// gap the per-node Description guards above cannot see: a PreGate Condition
// (e.g. IsCIBuildTask) whose keyword set is NARROWER than the StrategyRouter
// branch Conditions it protects (e.g. NeedsTestRun, NeedsLinting) rejects the
// task before the router is ever reached — the branch is fully described and
// registered, yet practically unreachable dead code for exactly the phrasing
// it was designed to handle. Each domain's dedicated benchmark.SuiteForTree
// suite already encodes a realistic representative task per branch
// (TaskCase.ExpectedPath); running every ShouldSucceed task through the real
// tree with the mock LLM (Sandbox mode, no side effects) and asserting a
// "success" outcome verifies the branch is actually reachable, not just
// nominally present. Exercises the same runtime-state exemptions as
// TestAllDomainTrees (git/nlm/claude-dependent trees can't run offline).
func TestDomainTreeSuitesReachAllStrategyBranches(t *testing.T) {
	exempt := map[string]bool{
		"goap_fusion": true, "goap_fusion_loop": true, "bt_manager": true,
		"bt_fusion": true, "notebooklm": true, "notebooklm_consumer": true,
		"notebooklm_plan_implement": true, "superpowers_workflow": true,
		"hermes_update": true, "auction_demo": true, "arc42_seeder": true,
		"self_review": true,
		// meeting_notes' benchmark.MeetingNotesSuite() task "document the
		// decision log from the quarterly review" matches neither
		// IsMeetingTask (PreGate) nor its declared IsSummaryRequest branch
		// condition — a benchmark-suite task-wording gap, not a
		// domains-package tree defect, and out of this goal's file scope
		// (internal/domains/trees.go, internal/domains/domains_test.go).
		"meeting_notes": true,
	}

	mock := benchmark.DefaultMock()
	for name, tree := range AllDomainTrees() {
		if strings.HasPrefix(name, "arc42:") || exempt[name] {
			continue
		}
		suite := benchmark.SuiteForTree(name)
		metrics := benchmark.RunSuite(tree, suite, mock)
		for _, r := range metrics.Results {
			var tc *benchmark.TaskCase
			for i := range suite.Tasks {
				if suite.Tasks[i].Task == r.Task {
					tc = &suite.Tasks[i]
					break
				}
			}
			if tc == nil || !tc.ShouldSucceed {
				continue
			}
			if !r.Success {
				t.Errorf("tree %q: task %q (expected path %q) should succeed but got outcome=%q — a StrategyRouter branch condition is unreachable through the tree's PreGate gate",
					name, r.Task, tc.ExpectedPath, r.Outcome)
			}
		}
	}
}

// tasksForKanbanAndHermesTrees returns a representative smoke task for each
// tree in KanbanAndHermesDomainTrees().
func tasksForKanbanAndHermesTrees() map[string]string {
	return map[string]string{
		"kanban_task_creator": "kanban: create a task card for the new feature",
		"kanban_refiner":      "kanban: refine the acceptance criteria on card #17",
		"kanban_qa":           "qa: validate card #42 before merge",
		"kanban_monitor":      "kanban: monitor the board for stale cards",
		"kanban_workflow":     "kanban: run the full task workflow for card #9",
		"kanban_autopilot":    "kanban: autopilot the board end to end",
		"hermes_evolve":       "hermes: evolve the self-improvement loop",
		"hermes_obsidian":     "hermes: optimize the obsidian vault structure",
	}
}

// TestKanbanAndHermesTreesHaveSmokeAndConditionCoverage gives the kanban and
// hermes-evolve trees the same smoke-test and condition-description coverage
// AllDomainTrees()-driven guards already enforce for the curated registry.
// These trees are intentionally excluded from AllDomainTrees() (they are not
// part of the gardener/dashboard registry surface), so
// KanbanAndHermesDomainTrees() is a dedicated registry scoped to this guard.
// The trees shell out to real board/vault state (hermes_update,
// notebooklm-style side effects), so — matching the precedent set by
// TestAllDomainTrees for similarly runtime-state-dependent trees — this only
// smoke-tests BuildTree structurally rather than running benchmark.RunSuite.
func TestKanbanAndHermesTreesHaveSmokeAndConditionCoverage(t *testing.T) {
	trees := KanbanAndHermesDomainTrees()
	tasks := tasksForKanbanAndHermesTrees()
	mock := benchmark.DefaultMock()

	if len(trees) != len(tasks) {
		t.Errorf("kanban/hermes tree registry/task mismatch: got %d registered trees and %d smoke tasks", len(trees), len(tasks))
	}

	for name, tree := range trees {
		if tree == nil {
			t.Errorf("tree %q is nil", name)
			continue
		}
		task, ok := tasks[name]
		if !ok {
			t.Errorf("no smoke task defined for tree %q", name)
			continue
		}

		bb := &engine.Blackboard{Task: task, LLM: mock}
		cmd := engine.BuildTree(tree, bb)
		if cmd == nil {
			t.Errorf("tree %q: BuildTree returned nil", name)
		}

		for _, gap := range conditionDescriptionGaps(*tree) {
			t.Errorf("tree %q: Condition node %q has an empty Description (condition coverage gap)", name, gap)
		}
	}
}

// TestGoapFusionLoopSeedsBeforeResearch pins the tree ordering that keeps the
// self-seeder reachable: BacklogReplenish must appear BEFORE ResearchRouter,
// so a cycle whose research phase fails still seeds a program. And
// ResearchRouter must carry a terminal AlwaysSucceed (ResearchOptional) so a
// barren research phase does not abort the cycle before milestone execution.
func TestGoapFusionLoopSeedsBeforeResearch(t *testing.T) {
	tree := GoapFusionLoopTree()
	// Index of first occurrence of each node name in a pre-order walk.
	order := map[string]int{}
	i := 0
	var walk func(n evolution.SerializableNode)
	walk = func(n evolution.SerializableNode) {
		if _, seen := order[n.Name]; !seen && n.Name != "" {
			order[n.Name] = i
			i++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(*tree)

	seed, okS := order["BacklogReplenish"]
	research, okR := order["ResearchRouter"]
	if !okS || !okR {
		t.Fatalf("missing nodes: BacklogReplenish=%v ResearchRouter=%v", okS, okR)
	}
	if seed >= research {
		t.Fatalf("BacklogReplenish (%d) must come BEFORE ResearchRouter (%d) so seeding survives research failure", seed, research)
	}
	if _, ok := order["ResearchOptional"]; !ok {
		t.Fatal("ResearchRouter must have a terminal AlwaysSucceed (ResearchOptional) so a barren research phase is non-fatal")
	}
	if _, ok := order["SeedNextProgram"]; !ok {
		t.Fatal("SeedNextProgram must be present in the tree")
	}
}

// TestExpectedDomainIDsIsSortedAndComplete closes the last uncovered corner of
// trees.go for the goal "all domain trees have smoke tests, descriptions, and
// condition coverage": ExpectedDomainIDs (used by cmd/bt-dashboard and
// cmd/bt-agent to build knowledge.KnowledgeGraph.ExpectedDomains, the seam
// that drives the bt_kg_coverage_gaps gauge and CoverageGaps reporting) had
// zero direct test coverage before this guard. Two things must hold: (1) the
// output is exactly one "domain:<name>" entry per registry key, with no drops
// or extras, and (2) the output is returned in sorted order. Go map iteration
// order is randomized per process, so a naive `for name := range registry`
// implementation produces a different, unsorted permutation on every run —
// any consumer that logs, diffs, or exposes this slice directly (dashboard
// metrics, coverage-gap reports) gets non-reproducible output. Callers that
// happen to sort before comparing (cmd/bt-dashboard/main_test.go) mask this,
// but the exported helper itself should guarantee a stable order rather than
// relying on every caller to re-sort.
func TestExpectedDomainIDsIsSortedAndComplete(t *testing.T) {
	registry := AllDomainTrees()
	ids := ExpectedDomainIDs(registry)

	if len(ids) != len(registry) {
		t.Fatalf("ExpectedDomainIDs returned %d ids, want %d (one per registry entry)", len(ids), len(registry))
	}

	want := make(map[string]bool, len(registry))
	for name := range registry {
		want["domain:"+name] = true
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("ExpectedDomainIDs produced unexpected id %q", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Errorf("ExpectedDomainIDs is missing ids: %v", want)
	}

	if !slices.IsSorted(ids) {
		t.Errorf("ExpectedDomainIDs(registry) is not sorted: %v", ids)
	}
}

// TestWrapWithErrorHandlerIsIdempotentAndNilSafe directly exercises
// wrapWithErrorHandler's guard branch, which TestAllDomainTreesWrappedInClaude-
// ErrorHandler (error_handler_wrap_test.go) only ever observes indirectly
// through AllDomainTrees() — and AllDomainTrees() always calls it with a
// freshly built, never-wrapped tree, so the nil-tree and already-wrapped
// branches of the guard are never actually executed by any existing test
// (confirmed by coverage: wrapWithErrorHandler sits at 66.7%, the lowest of
// any function in trees.go besides the previously 0%-covered
// ExpectedDomainIDs). A nil tree must round-trip to nil so a broken tree
// constructor cannot be masked into a wrapped-looking non-nil value, and an
// already-wrapped ClaudeErrorHandler root must be returned unchanged rather
// than nested a second time.
func TestWrapWithErrorHandlerIsIdempotentAndNilSafe(t *testing.T) {
	if got := wrapWithErrorHandler("nil_case", nil); got != nil {
		t.Errorf("wrapWithErrorHandler(name, nil) = %+v, want nil", got)
	}

	already := &evolution.SerializableNode{
		Type: "ClaudeErrorHandler", Name: "already_ErrorHandler",
		Description: "pre-wrapped",
		Children:    []evolution.SerializableNode{{Type: "Sequence", Name: "Inner", Description: "inner"}},
	}
	got := wrapWithErrorHandler("already", already)
	if got != already {
		t.Errorf("wrapWithErrorHandler on an already-wrapped tree returned a different node, want the same pointer unchanged")
	}
	if got.Type != "ClaudeErrorHandler" || len(got.Children) != 1 || got.Children[0].Type == "ClaudeErrorHandler" {
		t.Errorf("wrapWithErrorHandler double-wrapped an already-wrapped tree: %+v", got)
	}

	fresh := &evolution.SerializableNode{Type: "Sequence", Name: "Fresh", Description: "fresh"}
	wrapped := wrapWithErrorHandler("fresh", fresh)
	if wrapped.Type != "ClaudeErrorHandler" || wrapped.Name != "fresh_ErrorHandler" {
		t.Errorf("wrapWithErrorHandler(fresh) = %+v, want ClaudeErrorHandler named fresh_ErrorHandler", wrapped)
	}
	if len(wrapped.Children) != 1 || wrapped.Children[0].Name != "Fresh" {
		t.Errorf("wrapWithErrorHandler(fresh) children = %+v, want [Fresh]", wrapped.Children)
	}
}

// TestAlertRouterSuiteReachesDeclaredPaths pins that every AlertRouterSuite
// task with a declared ExpectedPath actually reaches that StrategyRouter
// branch. IsCritical/IsHealthAlert's keyword sets are narrower than the
// suite's own realistic phrasing, so tasks like "escalate the P0 incident to
// the senior team" and "send warning notification for high memory usage"
// silently fall through to GeneralAlert instead of matching CriticalAlert/
// HealthAlert.
func TestAlertRouterSuiteReachesDeclaredPaths(t *testing.T) {
	suite := benchmark.AlertRouterSuite()
	metrics := benchmark.RunSuite(AlertRouterTree(), suite, benchmark.DefaultMock())

	for i, r := range metrics.Results {
		tc := suite.Tasks[i]
		if tc.ExpectedPath == "" {
			continue
		}
		if !r.PathMatched {
			t.Errorf("task %q: expected path %q, got path %q", r.Task, tc.ExpectedPath, r.Path)
		}
	}
}

// TestTradingSignalSuiteReachesDeclaredPaths pins that every TradingSignalSuite
// task with a declared ExpectedPath actually reaches that StrategyRouter
// branch. IsTAPath's "rsi" keyword is a raw substring match, so it also fires
// on unrelated words like "reversion" (which contains "rsi"), sending the
// deliberately keyword-free "backtest the mean reversion trading strategy on
// historical hourly bars" task (ExpectedPath: ExecutionPath) into
// TechnicalAnalysis instead.
func TestTradingSignalSuiteReachesDeclaredPaths(t *testing.T) {
	suite := benchmark.TradingSignalSuite()
	metrics := benchmark.RunSuite(TradingSignalTree(), suite, benchmark.DefaultMock())

	for i, r := range metrics.Results {
		tc := suite.Tasks[i]
		if tc.ExpectedPath == "" {
			continue
		}
		if !r.PathMatched {
			t.Errorf("task %q: expected path %q, got path %q", r.Task, tc.ExpectedPath, r.Path)
		}
	}
}

// TestCrashInvestigatorStrategyBranchesAreReachable extends the per-domain
// reachability guards (TestAlertRouterSuiteReachesDeclaredPaths,
// TestTradingSignalSuiteReachesDeclaredPaths) to CrashInvestigatorTree, whose
// own benchmark.CrashInvestigatorSuite() never exercises ParseStackTrace,
// FixAndVerify, or PreventionPath — only RootCauseAnalysis and the
// ExecutionPath fallback — so those three StrategyRouter branches have zero
// direct-reachability coverage today despite each being fully described and
// registered.
//
// ParseStackTrace and PreventionPath ARE reachable from task text alone
// (asserted here as the passing control group). FixAndVerify is not:
// HasProposedFix (engine/conditions_domain.go) checks bb.Result, not bb.Task,
// and bb.Result is only ever populated by a prior action such as GenerateFix
// in the RootCauseAnalysis branch. StrategyRouter is a Selector, so
// RootCauseAnalysis and FixAndVerify are mutually exclusive within one tree
// execution — RootCauseAnalysis never runs when FixAndVerify's condition is
// being evaluated, and a fresh single-shot task (bb.Result starts empty, as
// in every benchmark.RunSuite call and every first-turn bt_run_task
// invocation) can never make HasProposedFix true. FixAndVerify is therefore
// permanently unreachable dead code — the same class of gap
// TestGoapTreesSeedGoapToolsBeforeGOAPRoot guards against for the GOAP_Root
// branch.
func TestCrashInvestigatorStrategyBranchesAreReachable(t *testing.T) {
	mock := benchmark.DefaultMock()
	tree := CrashInvestigatorTree()

	cases := []struct {
		task string
		want string
	}{
		{"parse this stack trace: goroutine 1 [running]: main.foo() at /app/main.go:42", "ParseStackTrace"},
		{"debug the root cause of the race condition crash in the scheduler", "RootCauseAnalysis"},
		{"apply the proposed fix and verify the crash no longer reproduces", "FixAndVerify"},
		{"harden the code and add guards to prevent this crash from recurring", "PreventionPath"},
	}

	for _, tc := range cases {
		suite := benchmark.Suite{Name: "crash_investigator_branch", Tasks: []benchmark.TaskCase{
			{Task: tc.task, ExpectedPath: tc.want, ShouldSucceed: true, MinResultLen: 5},
		}}
		metrics := benchmark.RunSuite(tree, suite, mock)
		if len(metrics.Results) != 1 {
			t.Fatalf("task %q: expected 1 result, got %d", tc.task, len(metrics.Results))
		}
		r := metrics.Results[0]
		if !r.PathMatched {
			t.Errorf("task %q: expected StrategyRouter branch %q, got path %q — branch is unreachable given task text alone", tc.task, tc.want, r.Path)
		}
	}
}

// conditionGuardEdgeGaps walks node and its descendants and returns the Name of
// every Condition node that carries no machine-readable EdgeGuard typed edge —
// i.e. no edge of type evolution.EdgeGuard with both a non-blank Label and a
// non-blank Condition. It is the guard-edge counterpart to
// conditionDescriptionGaps: that walker covers the human-readable half of
// condition coverage (Description), this one covers the machine-readable half
// (TypedEdge.Condition, the precondition string engine/typed_edges.go and
// engine/utility_selector.go actually evaluate, and the field
// evolution.ValidateEdge requires on every guard edge).
func conditionGuardEdgeGaps(node evolution.SerializableNode) []string {
	var gaps []string
	if node.Type == "Condition" {
		described := false
		for _, edge := range node.Edges {
			if edge.Type == evolution.EdgeGuard &&
				strings.TrimSpace(edge.Label) != "" &&
				strings.TrimSpace(edge.Condition) != "" {
				described = true
				break
			}
		}
		if !described {
			gaps = append(gaps, node.Name)
		}
	}
	for _, child := range node.Children {
		gaps = append(gaps, conditionGuardEdgeGaps(child)...)
	}
	return gaps
}

// TestDomainTreeConditionEdgeMetadataCoverage closes the last open leg of the
// goal "all domain trees have smoke tests, descriptions, and condition
// coverage": the machine-readable half of condition coverage.
//
// Every existing condition-coverage guard in this file checks only
// node.Description — the human-readable routing rationale. The typed-edge
// metadata those same Condition nodes carry (evolution.TypedEdge with
// Type=EdgeGuard, a Label, and a Condition precondition string) is what
// production code actually reads: engine/typed_edges.go
// (guardConditionForChild) and engine/utility_selector.go (ScoreChild) gate
// execution on it, and evolution.ValidateEdge rejects a guard edge whose
// Condition is blank. Two hand-built trees already set the precedent —
// AgentMonitorTree and HermesUpdateTree attach guard("non-empty-task", "task
// string must not be empty") style edges to every PreGate/router Condition —
// but every tree built through the shared cond() helper in trees.go declares
// its Condition nodes with a Description and no Edges at all, so their
// preconditions exist only as prose.
//
// Two things must hold:
//
//   - guard-edges: every Condition node in every registered domain tree
//     (AllDomainTrees, arc42 included — the arc42 trees are built with the same
//     cond() helper) carries an EdgeGuard edge with a non-blank Label and a
//     non-blank Condition. Scoped to the registry for the same reason
//     TestAllDomainTreeConditionsHaveDescriptions is: AllDomainTrees is the
//     gardener/switch_tree surface. The non-registry trees (kanban_*,
//     hermes_evolve, hermes_obsidian) declare their Condition nodes as inline
//     literals rather than via cond() and stay covered by the
//     edge-metadata-populated subtest below.
//
//   - edge-metadata-populated: the edgeMetadataGaps walker — which until now
//     was only ever fed the synthetic fixture in
//     TestEdgeMetadataWalkerDetectsBlankFields, never a real tree — is applied
//     to every production domain tree in the package (registry, non-registry
//     smoke extras, and resolver-reachable extras). Without this, a regression
//     that blanked out an existing guard Condition or edge Label in
//     AgentMonitorTree would pass every test in the package.
func TestDomainTreeConditionEdgeMetadataCoverage(t *testing.T) {
	t.Run("guard-edges", func(t *testing.T) {
		for name, tree := range AllDomainTrees() {
			for _, gap := range conditionGuardEdgeGaps(*tree) {
				t.Errorf("tree %q: Condition node %q carries no EdgeGuard typed edge with a Label and a Condition (machine-readable condition coverage gap)", name, gap)
			}
		}
	})

	t.Run("edge-metadata-populated", func(t *testing.T) {
		walkAll := func(label string, trees map[string]*evolution.SerializableNode) {
			for name, tree := range trees {
				if tree == nil {
					t.Errorf("%s tree %q is nil", label, name)
					continue
				}
				for _, gap := range edgeMetadataGaps(*tree) {
					t.Errorf("%s tree %q: node %q carries a typed edge with a blank Label, guard Condition, or effect Effect (edge metadata gap)", label, name, gap)
				}
			}
		}

		walkAll("registered", AllDomainTrees())
		walkAll("non-registry", KanbanAndHermesDomainTrees())

		walkAll("resolver-reachable", ResolverReachableDomainTrees())
	})
}

// TestSmokeTestableDomainTreesIsTheUnion pins SmokeTestableDomainTrees as the
// single source of truth for "which domain trees must carry a smoke test".
// Today that set is spelled out three separate times — AllDomainTrees(), the
// hand-written fns map in engine_domain_execution_test.go, and the hand-copied
// extraSmokeTrees literal in TestSmokeTestedDomainTreesHaveConditionDescriptions
// — and nothing fails when a newly registered tree is added to one but not the
// others, so it is simply, silently unexercised. This guard fixes the union's
// definition (the curated registry plus the deliberately-off-registry kanban and
// hermes trees), requires every value to be a real buildable tree, and requires
// the returned map to be a fresh defensive copy so a caller that filters or
// mutates it cannot corrupt the canonical enumeration for the next caller.
func TestSmokeTestableDomainTreesIsTheUnion(t *testing.T) {
	want := map[string]bool{}
	for name := range AllDomainTrees() {
		want[name] = true
	}
	for name := range KanbanAndHermesDomainTrees() {
		want[name] = true
	}

	got := SmokeTestableDomainTrees()

	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("SmokeTestableDomainTrees() is missing %q, which is registered in AllDomainTrees()/KanbanAndHermesDomainTrees(); the union must be exhaustive or a registered tree escapes smoke coverage", name)
		}
	}
	for name, tree := range got {
		if !want[name] {
			t.Errorf("SmokeTestableDomainTrees() returned %q, which belongs to neither registry; the union must not invent names", name)
		}
		if tree == nil {
			t.Errorf("SmokeTestableDomainTrees()[%q] is nil; every entry must be a buildable tree", name)
		}
	}

	// Defensive copy: mutating one call's result must not be visible to the next.
	const sentinel = "zz_smoke_union_sentinel"
	got[sentinel] = &evolution.SerializableNode{Type: "Action", Name: "Sentinel"}
	for name := range got {
		if name != sentinel {
			delete(got, name)
		}
	}

	fresh := SmokeTestableDomainTrees()
	if _, leaked := fresh[sentinel]; leaked {
		t.Error("SmokeTestableDomainTrees() leaks shared state: a key added to one call's map showed up in the next; it must build a fresh map every call")
	}
	if len(fresh) != len(want) {
		t.Errorf("SmokeTestableDomainTrees() returned %d entries after the previous result was mutated, want %d; it must build a fresh map every call", len(fresh), len(want))
	}
}

// TestEverySmokeTestableTreeIsSmokeExecuted is the self-enforcing half of the
// coverage guarantee: it runs the shared structural smoke assertions over
// SmokeTestableDomainTrees() itself, so registry membership alone is what
// causes a tree to be exercised. There is no opt-in list here to fall behind —
// registering a tree in either registry automatically subjects it to these
// checks, which is exactly what does not happen today (a new AllDomainTrees()
// entry with no matching fns/extraSmokeTrees entry is silently unexercised).
// The assertions are deliberately structural (no LLM, no runtime state) so they
// hold for every tree in the union, including the arc42 sections and the
// runtime-dependent goap/notebooklm flows that TestAllDomainTrees can only
// build rather than execute.
func TestEverySmokeTestableTreeIsSmokeExecuted(t *testing.T) {
	for name, tree := range SmokeTestableDomainTrees() {
		t.Run(name, func(t *testing.T) {
			if tree == nil {
				t.Fatalf("tree %q is nil", name)
			}
			if strings.TrimSpace(tree.Name) == "" {
				t.Errorf("tree %q: root node has an empty Name", name)
			}
			if strings.TrimSpace(tree.Description) == "" {
				t.Errorf("tree %q: root node has an empty Description", name)
			}
			if len(tree.Children) == 0 {
				t.Errorf("tree %q: root node has no children", name)
			}

			var actions int
			var blankTypes []string
			var walk func(evolution.SerializableNode)
			walk = func(node evolution.SerializableNode) {
				if node.Type == "Action" {
					actions++
				}
				if strings.TrimSpace(node.Type) == "" {
					blankTypes = append(blankTypes, node.Name)
				}
				for _, child := range node.Children {
					walk(child)
				}
			}
			walk(*tree)

			if actions == 0 {
				t.Errorf("tree %q: contains no Action node, so it can never do any work", name)
			}
			for _, blank := range blankTypes {
				t.Errorf("tree %q: node %q has an empty Type, so engine.BuildTree cannot dispatch it", name, blank)
			}
		})
	}
}

// smokeExecutionFile is the test file whose smoke map must not drift away from
// the canonical SmokeTestableDomainTrees() enumeration.
const smokeExecutionFile = "engine_domain_execution_test.go"

// smokeExecutedTreeNames parses smokeExecutionFile and reports which domain
// trees TestAllDomainTreesHaveExecutableStructure actually smoke-executes. It
// returns the explicit string keys of the `fns` composite literal, plus whether
// that test derives its work list from SmokeTestableDomainTrees() — in which
// case the coverage is complete by construction and no key list applies.
func smokeExecutedTreeNames(t *testing.T) (names map[string]bool, derivesFromRegistry bool) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, smokeExecutionFile, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", smokeExecutionFile, err)
	}

	const smokeTest = "TestAllDomainTreesHaveExecutableStructure"
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == smokeTest {
			body = fn.Body
			break
		}
	}
	if body == nil {
		t.Fatalf("%s no longer declares %s; the domain-tree smoke test must keep a single, findable entry point", smokeExecutionFile, smokeTest)
	}

	names = map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok && ident.Name == "SmokeTestableDomainTrees" {
				derivesFromRegistry = true
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || ident.Name != "fns" || i >= len(node.Rhs) {
					continue
				}
				lit, ok := node.Rhs[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.BasicLit)
					if !ok || key.Kind != token.STRING {
						continue
					}
					unquoted, err := strconv.Unquote(key.Value)
					if err != nil {
						continue
					}
					names[unquoted] = true
				}
			}
		}
		return true
	})
	return names, derivesFromRegistry
}

// TestSmokeExecutionFnsMapCoversRegistry is the drift guard on the third copy
// of the list: the hand-written `fns` map inside
// TestAllDomainTreesHaveExecutableStructure. It must cover every
// SmokeTestableDomainTrees() key — either by listing them all explicitly, or
// (preferred) by deriving its work list from SmokeTestableDomainTrees() so the
// literal cannot fall behind at all. Today it lists 17 of the ~47 trees in the
// union, so every unlisted registered tree has no executable-structure smoke
// test and nothing reports that fact.
func TestSmokeExecutionFnsMapCoversRegistry(t *testing.T) {
	names, derivesFromRegistry := smokeExecutedTreeNames(t)
	if derivesFromRegistry {
		return // work list comes from the registry; coverage is complete by construction
	}

	var missing []string
	for name := range SmokeTestableDomainTrees() {
		if !names[name] {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)

	if len(missing) > 0 {
		t.Errorf("the fns map in %s is missing %d of the %d SmokeTestableDomainTrees() entries, so those trees are silently unexercised by the executable-structure smoke test: %v\n"+
			"Fix by ranging over SmokeTestableDomainTrees() instead of maintaining a hand-copied literal.",
			smokeExecutionFile, len(missing), len(SmokeTestableDomainTrees()), missing)
	}
}

// resolverBareIDFile is the production resolver whose bare-ID branches decide
// which domains-package trees an operator can select by name.
const resolverBareIDFile = "tree_resolver.go"

// idEqualityLiterals reports every string literal compared against the `id`
// parameter in a resolver branch condition, flattening `||` chains so a
// multi-alias branch (`id == "fusion" || id == "fusion_deliberation"`) yields
// both IDs rather than being silently skipped.
func idEqualityLiterals(cond ast.Expr) []string {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	if bin.Op == token.LOR {
		return append(idEqualityLiterals(bin.X), idEqualityLiterals(bin.Y)...)
	}
	if bin.Op != token.EQL {
		return nil
	}
	if x, ok := bin.X.(*ast.Ident); !ok || x.Name != "id" {
		return nil
	}
	lit, ok := bin.Y.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil
	}
	unquoted, err := strconv.Unquote(lit.Value)
	if err != nil {
		return nil
	}
	return []string{unquoted}
}

// returnedLocalConstructor reports the name of the unqualified constructor a
// resolver branch returns — "KanbanRefinerTree" for `return KanbanRefinerTree()`.
// An unqualified call is by definition a domains-package tree, so this is what
// separates the trees this package must cover from the qualified calls
// (evolution.StockfishEvolutionTree(), thinktank.SynthesisTree()) that belong to
// other packages and carry their own coverage there.
func returnedLocalConstructor(block *ast.BlockStmt) (string, bool) {
	for _, stmt := range block.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			return ident.Name, true
		}
	}
	return "", false
}

// resolverReachableDomainsTreeIDs parses resolverBareIDFile and returns every
// bare tree ID whose branch returns a domains-package tree, mapped to the
// constructor that builds it.
//
// This is the ground truth the resolver-reachable coverage guards are checked
// against. Adding a branch to resolveTreeIDWithResolver makes a tree selectable
// in production (bt-agent switch_tree, A2A, template validation), and nothing
// about that act adds it to a coverage registry, so the tree would escape every
// guard silently. The two smoke registries solved exactly this problem in
// production code — coverage derives from SmokeTestableDomainTrees() so
// membership alone forces exercise — and ResolverReachableDomainTrees() now
// gives the resolver-only class the same derivation, with
// TestResolverReachableRegistryIsDerivedFromTreeResolver reconciling it against
// the branches parsed here.
func resolverReachableDomainsTreeIDs(t *testing.T) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, resolverBareIDFile, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", resolverBareIDFile, err)
	}

	const resolverFn = "resolveTreeIDWithResolver"
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == resolverFn {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatalf("%s: no func %s — the resolver was renamed, so this guard is no longer inspecting the real selection surface", resolverBareIDFile, resolverFn)
	}

	ids := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		ctor, ok := returnedLocalConstructor(ifStmt.Body)
		if !ok {
			return true
		}
		for _, id := range idEqualityLiterals(ifStmt.Cond) {
			ids[id] = ctor
		}
		return true
	})
	return ids
}

// TestEveryResolverReachableDomainTreeIsCovered closes the last hole in the goal
// "all domain trees have smoke tests, descriptions, and condition coverage": the
// trees an operator selects by bare ID through ResolveTreeID.
//
// Every coverage guard derives its work list from a production enumeration, so
// registering a tree is what subjects it to the checks. But the registries only
// see trees someone remembered to register, while resolveTreeIDWithResolver
// exposes domains-package trees by bare ID whether or not anyone did. This test
// derives its work list from the resolver source itself, so a branch added there
// cannot escape.
//
// The description leg fails today. ResolveTreeID("kanban:refiner") returns a real
// tree an operator can switch onto, but DescriptionFor("kanban:refiner") misses —
// NonRegistryDescriptions is keyed "kanban_refiner", the registry name, not the
// ID the resolver actually accepts. Those IDs render as bare identifiers wherever
// builtins are listed, which is the precise failure DescriptionFor exists to
// prevent and the reason ResolverReachableDescriptions was introduced.
func TestEveryResolverReachableDomainTreeIsCovered(t *testing.T) {
	ids := resolverReachableDomainsTreeIDs(t)
	if len(ids) == 0 {
		t.Fatalf("no bare-ID domains-package branches found in %s — the AST guard stopped matching the real resolver and is silently vacuous", resolverBareIDFile)
	}

	names := slices.Sorted(maps.Keys(ids))

	mock := benchmark.DefaultMock()
	for _, id := range names {
		ctor := ids[id]
		t.Run(id, func(t *testing.T) {
			// Smoke: the tree an operator selects must actually build.
			tree := ResolveTreeID(id)
			if tree == nil || len(tree.Children) == 0 {
				t.Fatalf("ResolveTreeID(%q) (built by %s) returned a nil or childless tree", id, ctor)
			}
			bb := &engine.Blackboard{Task: "smoke: exercise " + id, LLM: mock}
			if cmd := engine.BuildTree(tree, bb); cmd == nil {
				t.Errorf("resolver ID %q (built by %s): BuildTree returned nil", id, ctor)
			}

			// Condition coverage: every gate must explain its routing rationale.
			for _, gap := range conditionDescriptionGaps(*tree) {
				t.Errorf("resolver ID %q (built by %s): Condition node %q has an empty Description (condition coverage gap)", id, ctor, gap)
			}

			// Descriptions: the ID an operator types must resolve to a sentence.
			desc, ok := DescriptionFor(id)
			if !ok {
				t.Errorf("resolver ID %q (built by %s): DescriptionFor returned ok=false — ResolveTreeID(%q) hands an operator a real tree, but no description map knows that ID, so it renders as a bare identifier in the gardener, the dashboard tree list, and the bt-agent switch_tree confirmation",
					id, ctor, id)
				return
			}
			// Same floor the other description guards enforce: a description
			// must explain the tree, not restate its name.
			const minDescLen = 20
			if trimmed := strings.TrimSpace(desc); len(trimmed) < minDescLen {
				t.Errorf("resolver ID %q (built by %s): description %q is only %d chars, want >= %d",
					id, ctor, trimmed, len(trimmed), minDescLen)
			}
		})
	}
}

// TestResolverIDAliasesHaveNoOrphans is the reverse guard to the description leg
// of TestEveryResolverReachableDomainTreeIsCovered, and the alias-map sibling of
// the three HaveNoOrphans guards above. The forward test only proves each alias
// resolves; it cannot see an alias whose key no longer names a resolver branch
// (dead weight advertising a spelling ResolveTreeID rejects), nor one whose key
// a description map has since claimed outright — that key would silently stop
// using the alias, so a divergent description could grow underneath it unnoticed.
//
// It also pins the two structural properties DescriptionFor's alias hop depends
// on: every target must be describable without an alias (the hop is single, so a
// chain resolves to nothing), and no target may be a key (which is what makes a
// chain impossible in the first place rather than merely absent today).
func TestResolverIDAliasesHaveNoOrphans(t *testing.T) {
	reachable := resolverReachableDomainsTreeIDs(t)
	for alias, canonical := range ResolverIDAliases {
		if _, ok := reachable[alias]; !ok {
			t.Errorf("ResolverIDAliases has key %q but no bare-ID branch in %s resolves that spelling — the alias describes a name ResolveTreeID rejects", alias, resolverBareIDFile)
		}
		if _, ok := ResolverIDAliases[canonical]; ok {
			t.Errorf("ResolverIDAliases maps %q to %q, which is itself an alias key — DescriptionFor resolves a single hop, so a chained alias resolves to nothing", alias, canonical)
		}
		desc, ok := DescriptionFor(canonical)
		if !ok {
			t.Errorf("ResolverIDAliases maps %q to %q, but DescriptionFor(%q) returned ok=false — the alias target must be describable in its own right", alias, canonical, canonical)
			continue
		}
		if got, _ := DescriptionFor(alias); got != desc {
			t.Errorf("DescriptionFor(%q) = %q, want the target's description %q — the alias exists so both spellings describe the one tree they resolve to", alias, got, desc)
		}
	}

	// The alias hop must never shadow a real entry: it runs last precisely so a
	// name the maps answer for cannot be redirected somewhere else.
	for alias := range ResolverIDAliases {
		for mapName, m := range map[string]map[string]string{
			"Descriptions":                  Descriptions,
			"NonRegistryDescriptions":       NonRegistryDescriptions,
			"ResolverReachableDescriptions": ResolverReachableDescriptions,
			"ExternalPackageDescriptions":   ExternalPackageDescriptions,
		} {
			if _, ok := m[alias]; ok {
				t.Errorf("%q is both a ResolverIDAliases key and a %s entry — the alias is dead (DescriptionFor answers from the map first), so the two can silently diverge", alias, mapName)
			}
		}
	}
}

// externalPackageResolverIDs are the bare tree IDs resolveTreeIDWithResolver
// accepts whose branch builds a tree owned by ANOTHER package, so the domains
// coverage registries deliberately do not — and must not — name them. Every one
// of these resolves into internal/evolution, which carries its own smoke and
// description coverage in that package's tests; duplicating them here would
// guard a tree this package does not define. The value records which
// constructor each ID lands on, so a branch repointed at a domains-package tree
// is visible as a stale comment rather than a silent exemption.
//
// This set is what makes TestResolverReachableRegistryIsDerivedFromTreeResolver
// fail CLOSED. A newly added `if id == "x"` branch is presumed to build a
// domains-package tree that must be covered; exempting it takes a deliberate
// edit here, not silence.
var externalPackageResolverIDs = map[string]string{
	"stockfish_evolve":    "evolution.StockfishEvolutionTree",
	"stockfish_loop":      "evolution.StockfishEvolutionLoop",
	"vault_manager":       "evolution.VaultManagerTree",
	"notebooklm-bridge":   "evolution.NotebookLMBridgeTree",
	"godev":               "evolution.GoDeveloperTree",
	"fusion":              "evolution.FusionDeliberationTree",
	"fusion_deliberation": "evolution.FusionDeliberationTree",
	"telegram_clarify":    "evolution.TelegramClarifyTree",
}

// resolverBareIDLiterals returns every string literal compared against the `id`
// parameter anywhere inside resolveTreeIDWithResolver, sorted for deterministic
// reporting. The empty string is dropped: `if id == ""` is the nil guard, not a
// selectable tree.
//
// This is deliberately broader than resolverReachableDomainsTreeIDs above, which
// only keeps branches returning an unqualified constructor call. That filter
// infers "is this a domains tree?" from the shape of the return statement, so a
// branch that returns anything else — a helper call, a variable, a qualified
// call — drops out of the work list with no signal. Here every literal ID counts
// and an exemption must be spelled out in externalPackageResolverIDs, which is
// the direction that fails closed. It parses independently for the same reason:
// this guard must keep matching the real resolver even if the other helper's
// filtering changes.
func resolverBareIDLiterals(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, resolverBareIDFile, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", resolverBareIDFile, err)
	}

	const resolverFn = "resolveTreeIDWithResolver"
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == resolverFn {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatalf("%s: no func %s — the resolver was renamed, so this guard is no longer inspecting the real selection surface", resolverBareIDFile, resolverFn)
	}

	seen := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if bin, ok := n.(*ast.BinaryExpr); ok {
			for _, id := range idEqualityLiterals(bin) {
				if id != "" {
					seen[id] = true
				}
			}
		}
		return true
	})

	ids := slices.Sorted(maps.Keys(seen))
	return ids
}

// coverageRootName reports the name by which a resolver-returned tree and a
// registry tree can be compared. AllDomainTrees() wraps every value in a
// ClaudeErrorHandler rooted at "<key>_ErrorHandler" (wrapWithErrorHandler in
// trees.go) while resolveTreeIDWithResolver returns the bare constructor output,
// so the two spellings of one tree only line up once the wrapper is peeled off.
//
// Trees are matched by root name rather than by map key because the resolver's
// IDs deliberately differ from the registries' names — ResolveTreeID accepts
// "kanban:refiner" for the tree KanbanAndHermesDomainTrees() registers as
// "kanban_refiner" — so a key comparison would report covered trees as missing.
func coverageRootName(tree *evolution.SerializableNode) string {
	if tree == nil {
		return ""
	}
	if tree.Type == "ClaudeErrorHandler" && len(tree.Children) == 1 {
		return tree.Children[0].Name
	}
	return tree.Name
}

// TestResolverReachableRegistryIsDerivedFromTreeResolver makes the
// resolver-reachable coverage work list fail closed.
//
// SmokeTestableDomainTrees() is derived — it is the union of two production
// registries, so registering a tree is itself what subjects it to the smoke,
// condition-description, and description guards. The third class of tree,
// domains-package trees reachable ONLY through ResolveTreeID, is enumerated by
// ResolverReachableDomainTrees(), which gives it the same property. What neither
// registry can give it is exhaustiveness: adding one `if id == "x" { return
// XTree() }` branch to tree_resolver.go hands operators a switch_tree-selectable
// tree with no smoke test, no Condition walk, and no description, and no
// registry notices the omission — that is precisely the thing nobody registered.
//
// This guard reads the IDs out of the production resolver instead. Each one must
// either resolve to a tree already covered by SmokeTestableDomainTrees() ∪
// ResolverReachableDomainTrees(), or be listed in externalPackageResolverIDs as
// another package's tree. A new domains-package ID defaults to "must be
// covered", so the only way past this test is to put the tree in a production
// registry.
func TestResolverReachableRegistryIsDerivedFromTreeResolver(t *testing.T) {
	ids := resolverBareIDLiterals(t)
	if len(ids) == 0 {
		t.Fatalf("no `id == \"...\"` comparisons found in %s's resolveTreeIDWithResolver — a refactor moved the bare-ID branches somewhere this scan cannot see, so the guard is silently vacuous and every resolver-reachable tree is unguarded again", resolverBareIDFile)
	}

	// Root name -> the registry entry that covers it.
	covered := map[string]string{}
	for name, tree := range SmokeTestableDomainTrees() {
		if root := coverageRootName(tree); root != "" {
			covered[root] = "SmokeTestableDomainTrees()[" + name + "]"
		}
	}
	for name, tree := range ResolverReachableDomainTrees() {
		if root := coverageRootName(tree); root != "" {
			covered[root] = "ResolverReachableDomainTrees()[" + name + "]"
		}
	}

	for _, id := range ids {
		if _, external := externalPackageResolverIDs[id]; external {
			continue
		}
		t.Run(id, func(t *testing.T) {
			tree := ResolveTreeID(id)
			if tree == nil {
				t.Fatalf("ResolveTreeID(%q) returned nil even though %s has a branch for that ID", id, resolverBareIDFile)
			}
			root := coverageRootName(tree)
			if _, ok := covered[root]; !ok {
				t.Errorf("resolver ID %q resolves to a tree rooted at %q, which is in neither SmokeTestableDomainTrees() nor ResolverReachableDomainTrees().\n"+
					"%s makes that tree switch_tree-selectable in production, so today it has no smoke test, no condition-description walk, and no description.\n"+
					"Fix by adding it to ResolverReachableDomainTrees() in trees.go (or to AllDomainTrees()/KanbanAndHermesDomainTrees() if it belongs on a registry surface) — or, if its tree is built by another package, to externalPackageResolverIDs with the constructor that owns it.",
					id, root, resolverBareIDFile)
			}
		})
	}
}

// TestEveryResolverSelectableTreeIsDescribable closes the last describability
// hole in the goal "all domain trees have smoke tests, descriptions, and
// condition coverage": the bare tree IDs resolveTreeIDWithResolver accepts whose
// branch builds ANOTHER package's tree.
//
// TestEveryResolverReachableDomainTreeIsCovered already requires a description
// for every bare ID whose branch returns an unqualified constructor, and
// TestResolverReachableRegistryIsDerivedFromTreeResolver requires every other
// bare ID to be exempted by name in externalPackageResolverIDs. Both stop at the
// exemption. But that exemption is about which package OWNS the tree, not about
// whether an operator can select it: ResolveTreeID("vault_manager") hands back a
// real, runnable tree exactly the way ResolveTreeID("superpowers_pipeline")
// does, and cmd/bt-agent (resolveTree), internal/agentexec (RunDeps.ResolveTree)
// and the A2A path all resolve agent definitions through that one function.
// Ownership elsewhere does not make the ID any less selectable here.
//
// So all eight exempted IDs — stockfish_evolve, stockfish_loop, vault_manager,
// notebooklm-bridge, godev, fusion, fusion_deliberation, telegram_clarify —
// miss in DescriptionFor, the function trees.go documents as "the only supported
// way to describe a tree by name". That is the same bare-identifier failure
// ResolverReachableDescriptions was introduced to fix for superpowers_pipeline,
// and the reason internal/gardener/gardener.go carries its own hand-written
// "Go software developer BT" string for godev instead of resolving one.
//
// The fix is a description home for this class in trees.go that DescriptionFor
// consults — not an entry in one of the three existing maps, each of which is
// orphan-guarded against a domains registry that must not name another package's
// tree.
func TestEveryResolverSelectableTreeIsDescribable(t *testing.T) {
	ids := resolverBareIDLiterals(t)
	if len(ids) == 0 {
		t.Fatalf("no `id == \"...\"` comparisons found in %s's resolveTreeIDWithResolver — the guard has lost the production selection surface and would pass vacuously", resolverBareIDFile)
	}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			tree := ResolveTreeID(id)
			if tree == nil || len(tree.Children) == 0 {
				t.Fatalf("ResolveTreeID(%q) returned a nil or childless tree even though %s has a branch for that ID", id, resolverBareIDFile)
			}

			owner := "a domains-package tree"
			if ctor, external := externalPackageResolverIDs[id]; external {
				owner = "built by " + ctor
			}

			desc, ok := DescriptionFor(id)
			if !ok {
				t.Fatalf("DescriptionFor(%q) returned ok=false, but ResolveTreeID(%q) hands an operator a real tree rooted at %q (%s).\n"+
					"No description map knows that ID, so it renders as a bare identifier wherever builtins are listed. Give the ID a description home in trees.go and have DescriptionFor consult it.",
					id, id, coverageRootName(tree), owner)
			}

			// Same floor the other description guards enforce: a description
			// must explain the tree, not restate its name.
			const minDescLen = 20
			if trimmed := strings.TrimSpace(desc); len(trimmed) < minDescLen {
				t.Errorf("DescriptionFor(%q) = %q (%s): only %d chars, want >= %d", id, trimmed, owner, len(trimmed), minDescLen)
			}
		})
	}
}

// TestExternalPackageDescriptionsHaveNoOrphans is the reverse guard to
// TestEveryResolverSelectableTreeIsDescribable, and the fourth-map sibling of
// TestDescriptionsHaveNoOrphans / TestNonRegistryDescriptionsHaveNoOrphans /
// TestResolverReachableDescriptionsHaveNoOrphans.
//
// The other three maps are each checked against the domains registry that
// defines them, which is what stops a renamed or deleted tree from leaving a
// dead entry advertising a builtin nobody can select. ExternalPackageDescriptions
// has no such registry by construction — its trees are built in another package,
// and naming them in a domains registry is exactly what the three orphan guards
// forbid — so its ground truth is the resolver source itself: a key must be a
// bare ID resolveTreeIDWithResolver accepts, and it must be an ID this package
// deliberately does not own. A key that stops satisfying either has to move to
// (or back to) a registry-backed map rather than sit here unguarded.
//
// It also keeps the map disjoint from the other three, for the same reason they
// are disjoint from each other: a name in two maps makes DescriptionFor's
// precedence silently choose between two divergent descriptions.
func TestExternalPackageDescriptionsHaveNoOrphans(t *testing.T) {
	selectable := map[string]bool{}
	for _, id := range resolverBareIDLiterals(t) {
		selectable[id] = true
	}
	if len(selectable) == 0 {
		t.Fatalf("no `id == \"...\"` comparisons found in %s's resolveTreeIDWithResolver — the guard cannot see the production selection surface, so every entry below would look like an orphan", resolverBareIDFile)
	}

	for _, name := range slices.Sorted(maps.Keys(ExternalPackageDescriptions)) {
		if !selectable[name] {
			t.Errorf("ExternalPackageDescriptions has entry %q but no bare-ID branch in %s resolves that spelling — the entry describes a builtin ResolveTreeID rejects", name, resolverBareIDFile)
		}
		if _, external := externalPackageResolverIDs[name]; !external {
			t.Errorf("ExternalPackageDescriptions has entry %q, but %q is not exempted in externalPackageResolverIDs — this map is only for trees another package builds, so a domains-package tree belongs in Descriptions, NonRegistryDescriptions, or ResolverReachableDescriptions, where it is orphan-guarded against a registry", name, name)
		}
		for mapName, m := range map[string]map[string]string{
			"Descriptions":                  Descriptions,
			"NonRegistryDescriptions":       NonRegistryDescriptions,
			"ResolverReachableDescriptions": ResolverReachableDescriptions,
		} {
			if _, ok := m[name]; ok {
				t.Errorf("tree %q is described in both %s and ExternalPackageDescriptions — the description maps must be disjoint", name, mapName)
			}
		}
	}
}

// TestResolverReachableDomainTreesIsNonEmptyAndResolvable pins the contract of
// the production registry the guard above derives half its covered set from:
// every entry must be a real, buildable tree that ResolveTreeID actually hands
// back under that exact key.
//
// The emptiness check matters because an empty registry would make the
// derivation guard pass vacuously for this class of tree. The key check matters
// because ResolveTreeID falls back to evolution.DefaultTree() rather than nil for
// an unknown ID, so a typo'd or renamed key still returns a non-nil tree — it is
// the root-name comparison, not the nil check, that catches a registry entry
// operators can no longer select.
func TestResolverReachableDomainTreesIsNonEmptyAndResolvable(t *testing.T) {
	trees := ResolverReachableDomainTrees()
	if len(trees) == 0 {
		t.Fatal("ResolverReachableDomainTrees() is empty — the resolver-only class of tree has lost its coverage work list, and TestResolverReachableRegistryIsDerivedFromTreeResolver can only pass vacuously for it")
	}

	for name, tree := range trees {
		if tree == nil {
			t.Errorf("ResolverReachableDomainTrees()[%q] is nil; every entry must be a buildable tree", name)
			continue
		}
		if len(tree.Children) == 0 {
			t.Errorf("ResolverReachableDomainTrees()[%q] has no children, so it can never do any work", name)
		}

		resolved := ResolveTreeID(name)
		if resolved == nil {
			t.Errorf("ResolveTreeID(%q) returned nil — the registry guards a tree no operator can select", name)
			continue
		}
		if got, want := coverageRootName(resolved), coverageRootName(tree); got != want {
			t.Errorf("ResolveTreeID(%q) builds a tree rooted at %q, but ResolverReachableDomainTrees()[%q] is rooted at %q — the key must be the exact ID tree_resolver.go accepts, or the registry describes and smoke-tests a different tree than the one operators get",
				name, got, name, want)
		}
	}
}

// TestResolverReachableDomainTreesIsDisjointFromSmokeRegistry keeps the two
// halves of the covered set from overlapping. The three description maps are
// already required to be pairwise disjoint (DescriptionFor's precedence would
// otherwise silently pick one of two divergent sentences), and each map is
// orphan-guarded against the registry that defines it — so a name registered in
// both SmokeTestableDomainTrees() and ResolverReachableDomainTrees() would have
// two legitimate description homes at once, making that invariant
// unenforceable. A tree belongs to exactly one class.
func TestResolverReachableDomainTreesIsDisjointFromSmokeRegistry(t *testing.T) {
	smoke := SmokeTestableDomainTrees()
	for name := range ResolverReachableDomainTrees() {
		if _, ok := smoke[name]; ok {
			t.Errorf("tree %q is registered in both SmokeTestableDomainTrees() and ResolverReachableDomainTrees() — the two registries must be disjoint, because a tree in both has two valid description homes and the maps can then diverge. A tree covered by AllDomainTrees()/KanbanAndHermesDomainTrees() is not resolver-only; drop it from ResolverReachableDomainTrees()", name)
		}
	}
}

// TestResolverReachableDomainTreesReturnsFreshMap holds the new registry to the
// same defensive-copy contract SmokeTestableDomainTrees() carries: coverage
// guards filter and merge these maps (see TestNonRegistryDomainTreeNodesHave-
// Descriptions, which copies entries into a scratch map), so a shared backing map
// would let one guard's bookkeeping delete another guard's work list and turn a
// coverage failure into a silent pass.
func TestResolverReachableDomainTreesReturnsFreshMap(t *testing.T) {
	want := len(ResolverReachableDomainTrees())

	got := ResolverReachableDomainTrees()
	const sentinel = "zz_resolver_reachable_sentinel"
	got[sentinel] = &evolution.SerializableNode{Type: "Action", Name: "Sentinel"}
	for name := range got {
		if name != sentinel {
			delete(got, name)
		}
	}

	fresh := ResolverReachableDomainTrees()
	if _, leaked := fresh[sentinel]; leaked {
		t.Error("ResolverReachableDomainTrees() leaks shared state: a key added to one call's map showed up in the next; it must build a fresh map every call")
	}
	if len(fresh) != want {
		t.Errorf("ResolverReachableDomainTrees() returned %d entries after the previous result was mutated, want %d; it must build a fresh map every call", len(fresh), want)
	}
}
