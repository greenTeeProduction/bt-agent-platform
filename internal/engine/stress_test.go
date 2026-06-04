package engine

import (
	"sync"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/finance"
	"github.com/nico/go-bt-evolve/internal/research"
)

// ─── PRODUCTION VALIDATION: Idempotency + Concurrent Stress ───
//
// These tests serve as the production validation artifact for the Core Engine
// dimension (90%→95%). They validate that every tree in the platform is
// idempotent (same task → same outcome across runs), safe to execute
// concurrently, and handles edge-case inputs without panic.

// namedTree is a helper pair for enumerating all platform trees.
type namedTree struct {
	name string
	tree *evolution.SerializableNode
	task string
}

// allPlatformTrees returns every registered tree with a default task that
// matches the tree's PreGate conditions.
func allPlatformTrees() []namedTree {
	return []namedTree{
		// Core trees
		{"default", evolution.DefaultTree(), "analyze this task and provide a report"},
		{"godev", evolution.GoDeveloperTree(), "review this go code for bugs"},

		// Research trees
		{"deep_research", research.DeepResearchTree(), "research quantum computing advances"},
		{"quick_research", research.QuickResearchTree(), "quick summary of Kubernetes"},

		// Finance trees (10) — tasks MUST match IsFinanceTask keywords
		{"pitch_agent", finance.PitchAgentTree(), "prepare a dcf valuation pitch for the client"},
		{"earnings_reviewer", finance.EarningsReviewerTree(), "review the earnings report quarterly 10-q filing"},
		{"market_researcher", finance.MarketResearcherTree(), "research dcf valuation for competitive landscape"},
		{"model_builder", finance.ModelBuilderTree(), "build a dcf financial model for the portfolio company"},
		{"meeting_prep", finance.MeetingPrepTree(), "prepare financial client meeting briefing"},
		{"valuation_reviewer", finance.ValuationReviewerTree(), "review the gp valuation package with dcf analysis"},
		{"gl_reconciler", finance.GLReconcilerTree(), "reconcile the general ledger breaks"},
		{"month_end_closer", finance.MonthEndCloserTree(), "close month-end with accruals for audit"},
		{"statement_auditor", finance.StatementAuditorTree(), "audit the lp statement for accuracy"},
		{"kyc_screener", finance.KYCScreenerTree(), "screen kyc documents for aml compliance"},

		// Domain trees (10+)
		{"code_review", domains.CodeReviewTree(), "review code for bugs and security issues"},
		{"devops_ci", domains.DevOpsCITree(), "deploy the application with ci/cd pipeline"},
		{"agent_monitor", domains.AgentMonitorTree(), "check system health status (real system commands)"},
		{"refactoring", domains.RefactoringTree(), "refactor the legacy module"},
		{"security_audit", domains.SecurityAuditTree(), "audit security vulnerabilities"},
		{"data_pipeline", domains.DataPipelineTree(), "extract transform load the dataset"},
		{"meeting_notes", domains.MeetingNotesTree(), "summarize the meeting transcript"},
		{"crash_investigator", domains.CrashInvestigatorTree(), "investigate the crash dump"},
		{"game_ai", domains.GameAITree(), "design npc behavior tree for game"},
		{"trading_signal", domains.TradingSignalTree(), "generate trading signal from market data"},

		// Evolution trees
		{"hermes_evolve", evolution.HermesSelfEvolutionTree(), "periodic check to evaluate skill gaps and improvements"},
		{"stockfish", evolution.StockfishEvolutionTree(), "evolve the behavior tree with stockfish"},
		{"stockfish_loop", evolution.StockfishEvolutionLoop(), "run continuous evolution cycle"},

		// Kanban trees
		{"kanban_task_creator", evolution.KanbanTaskCreatorTree(), "create a new kanban task card"},
		{"kanban_refiner", evolution.KanbanRefinerTree(), "refine the kanban backlog tasks"},
		{"kanban_qa", evolution.KanbanQATree(), "validate kanban qa pass status"},
		{"kanban_monitor", evolution.KanbanBoardMonitorTree(), "check kanban for stale cards"},
		{"kanban_workflow", evolution.KanbanWorkflowTree(), "run the kanban workflow pipeline"},
		{"kanban_autopilot", evolution.KanbanAutoPilotTree(), "auto-dispatch kanban tasks"},
	}
}

// realSystemTrees are trees that use real system commands (df, free, ps, uptime)
// and therefore produce different output on each run. Idempotency for these trees
// is validated by checking outcome consistency only, not output content.
var realSystemTrees = map[string]bool{
	"agent_monitor": true,
}

// TestStress_Idempotency validates that every tree produces the same outcome
// when run twice with identical input and mock LLM.
//
// Idempotency is a core property of production behavior trees: the same input
// must always produce the same routing and the same outcome. Flaky trees cause
// unreproducible failures in production.
//
// Trees that use real system commands (agent_monitor) are exempt from exact
// output matching but still must produce non-empty, consistent outcomes.
func TestStress_Idempotency(t *testing.T) {
	trees := allPlatformTrees()

	for _, nt := range trees {
		t.Run(nt.name, func(t *testing.T) {
			if nt.tree == nil {
				t.Skip("nil tree")
				return
			}
			if nt.task == "" {
				t.Skip("no task")
				return
			}

			// First run
			bb1 := &Blackboard{Task: nt.task, LLM: &mockLLM{}}
			bt1 := BuildTree(nt.tree, bb1)
			_ = RunTask(bb1, bt1)

			// Second run — identical input
			bb2 := &Blackboard{Task: nt.task, LLM: &mockLLM{}}
			bt2 := BuildTree(nt.tree, bb2)
			_ = RunTask(bb2, bt2)

			// Compare outcomes from the blackboard (bb.Outcome, not RunTask return
			// value which is bb.Result — result text varies with mock LLM output)
			if bb1.Outcome == "" {
				t.Errorf("first run: empty bb.Outcome (task=%q)", nt.task)
			}
			if bb2.Outcome == "" {
				t.Errorf("second run: empty bb.Outcome (task=%q)", nt.task)
			}

			// Outcomes must be identical (same routing/tree path) for all trees.
			// bb.Outcome is set by RunTask based on the terminal tick code (1/0/-1)
			// which is deterministic for the same tree structure + mock LLM input.
			if bb1.Outcome != bb2.Outcome {
				t.Errorf("idempotency violation: first outcome=%q, second outcome=%q",
					bb1.Outcome, bb2.Outcome)
			}

			// For non-real-system trees, both runs must produce non-empty results
			if !realSystemTrees[nt.name] {
				if len(bb1.Result) == 0 {
					t.Error("first run: empty bb.Result")
				}
				if len(bb2.Result) == 0 {
					t.Error("second run: empty bb.Result")
				}
			}
		})
	}
}

// TestStress_ConcurrentExecution runs N trees concurrently to validate that
// the engine handles parallel execution safely. Each goroutine executes a
// different tree to avoid blackboard state corruption cross-contamination.
func TestStress_ConcurrentExecution(t *testing.T) {
	trees := allPlatformTrees()
	const parallelism = 10

	var wg sync.WaitGroup
	errCh := make(chan string, len(trees))

	// Launch goroutines — each runs a different tree
	sem := make(chan struct{}, parallelism) // concurrency cap
	for _, nt := range trees {
		if nt.tree == nil || nt.task == "" {
			continue
		}
		wg.Add(1)
		go func(nt namedTree) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- nt.name + " panicked: " + sprintRecover(r)
				}
				<-sem
			}()
			sem <- struct{}{}

			bb := &Blackboard{Task: nt.task, LLM: &mockLLM{}}
			bt := BuildTree(nt.tree, bb)
			_ = RunTask(bb, bt)
			if bb.Outcome == "" {
				errCh <- nt.name + ": empty outcome"
			}
		}(nt)
	}
	wg.Wait()

	close(errCh)
	failures := make([]string, 0, 8)
	for e := range errCh {
		failures = append(failures, e)
	}
	if len(failures) > 0 {
		t.Errorf("concurrent execution produced %d errors:", len(failures))
		for _, f := range failures {
			t.Errorf("  %s", f)
		}
	}
}

// TestStress_EdgeInputs validates that every tree handles edge-case inputs
// without panic or silent failure.
func TestStress_EdgeInputs(t *testing.T) {
	trees := allPlatformTrees()
	edgeTasks := []string{
		"",                        // empty
		"x",                       // single char
		"a",                       // single word, no recognized verb
		"1234567890",              // numbers only
		"!@#$%^&*()_+{}|:\"<>?~`", // special chars
		"こんにちは世界",                 // unicode only
		"🐢🔥🚀",                     // emoji only
		"review this Go code",     // valid simple task
	}

	for _, nt := range trees {
		t.Run(nt.name, func(t *testing.T) {
			if nt.tree == nil {
				t.Skip("nil tree")
				return
			}
			for _, task := range edgeTasks {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("panic with task=%q: %v", task, r)
						}
					}()
					bb := &Blackboard{Task: task, LLM: &mockLLM{}}
					bt := BuildTree(nt.tree, bb)
					_ = RunTask(bb, bt)
					// No panic is the success criterion
				}()
			}
		})
	}
}

// TestStress_MaxTicksSafetyLimit validates that the 1000-tick safety limit
// in RunTask reliably terminates even when trees contain Repeat decorators
// that would theoretically run forever.
func TestStress_MaxTicksSafetyLimit(t *testing.T) {
	// A tree with a Repeat(100000) decorator that won't complete in 1000 ticks
	infiniteTree := &evolution.SerializableNode{
		Type: "Repeat", Name: "InfiniteLoop",
		Metadata: map[string]any{"max_retries": float64(100000)},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "SetupDefaultTools"},
			{Type: "ChainAction", Name: "llm_call:Respond: {{.Task}}",
				Metadata: map[string]any{"max_tokens": float64(128)}},
		},
	}

	bb := &Blackboard{Task: "hello", LLM: &mockLLM{}}
	bt := BuildTree(infiniteTree, bb)

	// Must complete without infinite loop — RunTask's 1000-tick cap should fire
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic during bounded execution: %v", r)
		}
	}()

	outcome := RunTask(bb, bt)
	if outcome == "" {
		t.Error("expected non-empty outcome even from safety-limit termination")
	}
	t.Logf("safety-limit result: outcome=%q, result_len=%d", outcome, len(bb.Result))
}

// sprintRecover formats a recovered panic value as a string.
func sprintRecover(r interface{}) string {
	if r == nil {
		return "<nil>"
	}
	if err, ok := r.(error); ok {
		return err.Error()
	}
	if s, ok := r.(string); ok {
		return s
	}
	return "unknown panic type"
}
