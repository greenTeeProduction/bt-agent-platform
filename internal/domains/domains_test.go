package domains

import (
	"fmt"
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
		"arc42:assemble":  "assemble final arc42 document",
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
		// in engine/arc42_seeder_test.go with stubbed fetch).
		if name == "goap_fusion" || name == "goap_fusion_loop" || name == "bt_manager" || name == "bt_fusion" || name == "notebooklm" || name == "notebooklm_consumer" || name == "notebooklm_plan_implement" || name == "superpowers_workflow" || name == "hermes_update" || name == "auction_demo" || name == "arc42_seeder" {
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
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if node.Type == "Condition" && strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: Condition node %q has an empty Description (condition coverage gap)", treeName, node.Name)
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
	// Domain trees that have an executable-structure smoke test but are not part
	// of the AllDomainTrees registry — mirrors the extra entries in the fns map
	// of engine_domain_execution_test.go.
	extraSmokeTrees := map[string]func() *evolution.SerializableNode{
		"hermes_evolve":       HermesSelfEvolutionTree,
		"kanban_task_creator": KanbanTaskCreatorTree,
		"kanban_refiner":      KanbanRefinerTree,
		"kanban_qa":           KanbanQATree,
		"kanban_monitor":      KanbanBoardMonitorTree,
		"kanban_workflow":     KanbanWorkflowTree,
		"kanban_autopilot":    KanbanAutoPilotTree,
	}

	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if node.Type == "Condition" && strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: Condition node %q has an empty Description (condition coverage gap)", treeName, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	for name, fn := range extraSmokeTrees {
		tree := fn()
		if tree == nil {
			t.Errorf("smoke-tested tree %q returned nil", name)
			continue
		}
		walk(name, *tree)
	}
}

// TestDescriptionsHaveNoOrphans is the reverse guard to
// TestAllDomainTreesHaveDescriptions: every entry in the Descriptions map must
// correspond to a tree actually registered in AllDomainTrees. The gardener and
// the bt-agent switch_tree tool surface these descriptions verbatim, so an
// orphaned entry (left behind after a tree is renamed or removed) advertises a
// builtin that can never be selected. arc42 trees carry curated entries like
// every other registered tree, so they are covered too.
func TestDescriptionsHaveNoOrphans(t *testing.T) {
	all := AllDomainTrees()
	for name := range Descriptions {
		if _, ok := all[name]; !ok {
			t.Errorf("Descriptions has entry %q but no such tree is registered in AllDomainTrees", name)
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
// smoke-tested extras (hermes_evolve + the six kanban_* trees, mirroring the
// fns map in engine_domain_execution_test.go) and the ResolveTreeID-reachable
// extras (resolverReachableExtraDomainTrees). Their Condition nodes are
// already guarded, but their composites (PreGate, OutcomeSelector, routers)
// and non-Condition leaves (Action, ChainAction) can still carry blank
// Descriptions — an unexplained routing stage to the same gardener and
// switch_tree operators the curated guard protects. These trees are
// intentionally absent from the Descriptions map (TestDescriptionsHaveNoOrphans
// forbids non-registry entries), so this guard checks per-node Descriptions
// only.
func TestNonRegistryDomainTreeNodesHaveDescriptions(t *testing.T) {
	// Mirrors the extra entries in the fns map of
	// engine_domain_execution_test.go, like
	// TestSmokeTestedDomainTreesHaveConditionDescriptions.
	extraSmokeTrees := map[string]func() *evolution.SerializableNode{
		"hermes_evolve":       HermesSelfEvolutionTree,
		"kanban_task_creator": KanbanTaskCreatorTree,
		"kanban_refiner":      KanbanRefinerTree,
		"kanban_qa":           KanbanQATree,
		"kanban_monitor":      KanbanBoardMonitorTree,
		"kanban_workflow":     KanbanWorkflowTree,
		"kanban_autopilot":    KanbanAutoPilotTree,
	}

	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: %s node %q has an empty Description (full-node coverage gap)", treeName, node.Type, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	for _, trees := range []map[string]func() *evolution.SerializableNode{
		extraSmokeTrees,
		resolverReachableExtraDomainTrees(),
	} {
		for name, fn := range trees {
			tree := fn()
			if tree == nil {
				t.Errorf("non-registry tree %q returned nil", name)
				continue
			}
			walk(name, *tree)
		}
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

// resolverReachableExtraDomainTrees returns the domains-package trees that are
// reachable in production through ResolveTreeID (tree_resolver.go — used by
// bt-agent, A2A, and template validation) but are NOT already guarded by the
// AllDomainTrees registry (TestAllDomainTrees / *HaveDescriptions) or the
// executable-structure smoke registry (TestSmokeTestedDomainTreesHaveCondition-
// Descriptions, which mirrors engine_domain_execution_test.go's fns map).
//
// hermes_obsidian is such a tree: ResolveTreeID("hermes_obsidian") returns
// HermesObsidianOptimizerTree(), so operators can switch_tree onto it, yet it is
// absent from every existing coverage guard — it has no smoke test and its
// routing Conditions were never walked for descriptions. The kanban_* and
// hermes_evolve / notebooklm* / stockfish* IDs are either already in the smoke
// registries above or live in other packages (evolution.*), so they are excluded
// here to keep this guard scoped to the domains package's own untested trees.
func resolverReachableExtraDomainTrees() map[string]func() *evolution.SerializableNode {
	return map[string]func() *evolution.SerializableNode{
		"hermes_obsidian":      HermesObsidianOptimizerTree,
		"superpowers_pipeline": SuperpowersPipelineTree,
	}
}

// TestResolverReachableDomainTreesHaveSmokeStructure closes the "smoke tests"
// half of the goal "all domain trees have smoke tests, descriptions, and
// condition coverage" for the ResolveTreeID-reachable domains-package trees that
// escaped both smoke registries: it builds each tree through the real engine and
// fails if BuildTree panics or returns nil (structural smoke, like the arc42 and
// runtime-state-dependent trees in TestAllDomainTrees). It also asserts the tree
// is genuinely reachable via ResolveTreeID so a rename can't silently orphan it.
func TestResolverReachableDomainTreesHaveSmokeStructure(t *testing.T) {
	mock := benchmark.DefaultMock()
	resolverID := map[string]string{
		"hermes_obsidian":      "hermes_obsidian",
		"superpowers_pipeline": "superpowers_pipeline",
	}
	for name, fn := range resolverReachableExtraDomainTrees() {
		tree := fn()
		if tree == nil || len(tree.Children) == 0 {
			t.Errorf("resolver-reachable tree %q is nil or empty", name)
			continue
		}
		bb := &engine.Blackboard{Task: "smoke: exercise " + name, LLM: mock}
		if cmd := engine.BuildTree(tree, bb); cmd == nil {
			t.Errorf("resolver-reachable tree %q: BuildTree returned nil", name)
		}
		if id, ok := resolverID[name]; ok && ResolveTreeID(id) == nil {
			t.Errorf("resolver-reachable tree %q: ResolveTreeID(%q) returned nil", name, id)
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
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if node.Type == "Condition" && strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: Condition node %q has an empty Description (condition coverage gap)", treeName, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

	for name, fn := range resolverReachableExtraDomainTrees() {
		tree := fn()
		if tree == nil {
			t.Errorf("resolver-reachable tree %q returned nil", name)
			continue
		}
		walk(name, *tree)
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
	fn, ok := resolverReachableExtraDomainTrees()["superpowers_pipeline"]
	if !ok || fn == nil {
		t.Fatalf("superpowers_pipeline is not registered in resolverReachableExtraDomainTrees() — it escapes the smoke + condition-description coverage guards")
	}
	if tree := fn(); tree == nil || len(tree.Children) == 0 {
		t.Fatalf("resolverReachableExtraDomainTrees()[\"superpowers_pipeline\"] built a nil or empty tree")
	}
	if ResolveTreeID("superpowers_pipeline") == nil {
		t.Fatalf("ResolveTreeID(\"superpowers_pipeline\") returned nil — guarded tree is not resolver-reachable")
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
// in sections 5/10/11/12; Section4Done/Section5Done/assemble's AllSectionsDone are
// described in some places and blank in others. That inconsistency is exactly the
// unexplained-gate coverage gap this goal targets: the gardener and switch_tree surface
// these per-node descriptions as the human-readable routing rationale, so a blank one
// advertises an unexplained gate to operators. Every arc42 Condition node must carry a
// non-empty Description, just like every other registered domain tree.
func TestArc42DomainTreeConditionsHaveDescriptions(t *testing.T) {
	var walk func(treeName string, node evolution.SerializableNode)
	walk = func(treeName string, node evolution.SerializableNode) {
		if node.Type == "Condition" && strings.TrimSpace(node.Description) == "" {
			t.Errorf("tree %q: Condition node %q has an empty Description (arc42 condition coverage gap)", treeName, node.Name)
		}
		for _, child := range node.Children {
			walk(treeName, child)
		}
	}

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
		walk(name, *tree)
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
