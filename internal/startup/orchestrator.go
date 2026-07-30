package startup

import (
	"context"
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"

	btcore "github.com/rvitorper/go-bt/core"
)

// CompanyOrchestrator runs the simulated company through sprints, quarters, and years.
// It coordinates the engineer, marketing, sales, CEO, and CTO role trees,
// updating CompanyState metrics based on agent decisions.
type CompanyOrchestrator struct {
	State          *CompanyState
	LLM            llm.LLM
	SprintHistory  []SprintResult
	QuarterHistory []QuarterResult
}

// NewOrchestrator creates a new orchestrator with the given company state and LLM.
func NewOrchestrator(state *CompanyState, llmClient llm.LLM) *CompanyOrchestrator {
	return &CompanyOrchestrator{
		State:          state,
		LLM:            llmClient,
		SprintHistory:  make([]SprintResult, 0, 52),
		QuarterHistory: make([]QuarterResult, 0, 4),
	}
}

// runTree executes a behavior tree against the orchestrator's LLM and collects results.
func (o *CompanyOrchestrator) runTree(tree *evolution.SerializableNode, task string) error {
	bb := &engine.Blackboard{
		Task:       task,
		LLM:        o.LLM,
		ChainState: make(map[string]any),
		ChainTools: make([]any, 0),
	}

	cmd := engine.BuildTree(tree, bb)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	btCtx := btcore.NewBTContext(ctx, bb)

	// Run the tree with multi-tick support for decorators (e.g., Retry)
	const maxTicks = 1000
	code := cmd.Run(btCtx)
	for tick := 1; code == 0 && tick < maxTicks; tick++ {
		code = cmd.Run(btCtx)
	}

	if code == -1 || bb.Outcome == "chain_failed" {
		return fmt.Errorf("tree execution failed: outcome=%s, result=%s", bb.Outcome, bb.Result)
	}
	return nil
}

// RunSprint executes one sprint: EngineerTree, MarketingTree, SalesTree in sequence.
// It updates the CompanyState metrics based on the outcomes and returns a SprintResult.
//
// The three o.runTree() calls each carry a 120s timeout and run against a
// real LLM in production, so they must not run while state's mutex is held —
// otherwise every concurrent reader (e.g. the dashboard's
// /api/company/default handler, or Summary()) blocks for the full sprint
// duration. Instead, the state needed for the tree tasks is snapshotted
// under a short lock, the trees run unlocked, and the lock is re-acquired
// only to apply the resulting mutations.
func (o *CompanyOrchestrator) RunSprint() *SprintResult {
	state := o.State

	state.Lock()
	sprintNum := state.CurrentSprint + 1
	state.CurrentSprint = sprintNum
	sprintGoal := state.SprintGoal
	productStage := state.ProductStage
	features := append([]string(nil), state.Features...)
	technicalDebt := state.TechnicalDebt
	engineers := state.Engineers
	users := state.Users
	mrr := state.MRR
	arr := state.ARR
	churnRate := state.ChurnRate
	cac := state.CAC
	ltv := state.LTV
	state.Unlock()

	result := &SprintResult{
		SprintNum: sprintNum,
		Goal:      sprintGoal,
	}

	// 1. Run EngineerTree
	engTask := fmt.Sprintf(
		"Sprint %d goal: %s. Product stage: %s. Features: %v. Tech debt: %.0f/100. Team: %d engineers.",
		sprintNum, sprintGoal, productStage, features, technicalDebt, engineers,
	)
	engErr := o.runTree(EngineerTree(), engTask)

	// 2. Run MarketingTree
	mktTask := fmt.Sprintf(
		"Sprint %d: %d users, $%.0f MRR, %.1f%% churn, $%.0f CAC. Plan content and campaigns.",
		sprintNum, users, mrr, churnRate*100, cac,
	)
	mktErr := o.runTree(MarketingTree(), mktTask)

	// 3. Run SalesTree
	salesTask := fmt.Sprintf(
		"Sprint %d: $%.0f MRR, $%.0f ARR, %.1f%% churn, $%.0f CAC, $%.0f LTV. Close deals and optimize pricing.",
		sprintNum, mrr, arr, churnRate*100, cac, ltv,
	)
	salesErr := o.runTree(SalesTree(), salesTask)

	state.Lock()
	defer state.Unlock()

	if engErr != nil {
		result.Deferred = append(result.Deferred, fmt.Sprintf("engineering: %v", engErr))
	} else {
		// Simulate feature completion and tech debt reduction
		completedFeature := fmt.Sprintf("feature_sprint_%d", sprintNum)
		state.Features = append(state.Features, completedFeature)
		result.Completed = append(result.Completed, completedFeature)

		// Reduce technical debt by 2-8 points (agent-driven)
		debtReduction := 5.0 // nominal reduction
		state.TechnicalDebt = clamp(state.TechnicalDebt-debtReduction, 0, 100)
		result.TechDebtDelta = -debtReduction

		result.BugsFixed = int(debtReduction)
		result.Velocity = float64(len(result.Completed)) * 3.0
	}

	if mktErr != nil {
		result.Deferred = append(result.Deferred, fmt.Sprintf("marketing: %v", mktErr))
	} else {
		// Simulate user acquisition from marketing
		newUsers := state.MarketingStaff * 50 // ~50 users per marketer per sprint
		if newUsers < 10 {
			newUsers = 10
		}
		state.Users += newUsers

		// Marketing spend reduces CAC efficiency slightly but grows top of funnel
		cacImprovement := 0.95 + float64(state.MarketingStaff)*0.01 // 0.95-0.99 range
		state.CAC *= cacImprovement
	}

	if salesErr != nil {
		result.Deferred = append(result.Deferred, fmt.Sprintf("sales: %v", salesErr))
	} else {
		// Simulate MRR growth from closed deals
		newMRR := float64(state.SalesPeople) * 2000.0 // ~$2k per salesperson per sprint
		state.MRR += newMRR
		state.ARR = state.MRR * 12

		// Burn rate increases slightly as team produces more
		state.BurnRate *= 1.01
		state.CashInBank -= state.BurnRate / 4.0 // weekly burn
		state.Runway = int(state.CashInBank / state.BurnRate)
		if state.Runway < 0 {
			state.Runway = 0
		}
	}

	// After all roles: update churn trend (slightly improve with better product)
	if len(result.Completed) > 0 {
		state.ChurnRate = clamp(state.ChurnRate*0.98, 0.005, 0.50)
	}

	result.Velocity = float64(len(result.Completed)) * 3.0

	o.SprintHistory = append(o.SprintHistory, *result)
	return result
}

// RunQuarter executes one quarter: runs 12 sprints via RunSprint.
// Collects quarterly metrics and returns a QuarterResult.
func (o *CompanyOrchestrator) RunQuarter() *QuarterResult {
	state := o.State
	quarterNum := len(o.QuarterHistory) + 1

	state.Lock()
	startMRR := state.MRR
	startUsers := state.Users
	startCash := state.CashInBank
	state.Unlock()

	// Run 12 sprints (2-week sprints = 24 weeks ≈ 1 quarter). RunSprint locks
	// state itself per call, so the lock must be released here first —
	// state.Lock() is not reentrant.
	for i := 0; i < 12; i++ {
		o.RunSprint()
	}

	state.Lock()
	endMRR := state.MRR
	endUsers := state.Users
	churnRate := state.ChurnRate
	cashInBank := state.CashInBank
	burnRate := state.BurnRate
	quarterGoals := append([]string(nil), state.QuarterGoals...)
	state.Unlock()

	result := &QuarterResult{
		Quarter:     quarterNum,
		Revenue:     endMRR * 3, // quarterly revenue
		Growth:      ((endMRR - startMRR) / startMRR) * 100,
		UsersAdded:  endUsers - startUsers,
		Churn:       churnRate,
		CashBurned:  startCash - cashInBank,
		Highlights:  []string{},
		Lowlights:   []string{},
		OKRProgress: map[string]float64{},
	}

	// Evaluate quarter goals
	for _, goal := range quarterGoals {
		result.OKRProgress[goal] = 0.8 // nominal progress, refined by agent decision
	}

	if result.Growth > 10 {
		result.Highlights = append(result.Highlights,
			fmt.Sprintf("Strong growth: %.1f%% MRR increase", result.Growth),
			fmt.Sprintf("Added %d users to reach %d total", result.UsersAdded, endUsers),
		)
	}
	if result.CashBurned > burnRate*4 {
		result.Lowlights = append(result.Lowlights,
			fmt.Sprintf("Cash burn exceeded: $%.0f burned this quarter", result.CashBurned))
	}
	if churnRate > 0.05 {
		result.Lowlights = append(result.Lowlights,
			fmt.Sprintf("Churn elevated at %.1f%% — retention work needed", churnRate*100))
	}

	o.QuarterHistory = append(o.QuarterHistory, *result)
	return result
}

// RunYear executes four quarters and returns the results.
func (o *CompanyOrchestrator) RunYear() []QuarterResult {
	results := make([]QuarterResult, 0, 4)
	for i := 0; i < 4; i++ {
		qr := o.RunQuarter()
		results = append(results, *qr)
	}
	return results
}

// Summary returns a human-readable summary of the company's current state.
func (o *CompanyOrchestrator) Summary() string {
	state := o.State
	state.Lock()
	defer state.Unlock()
	return fmt.Sprintf(
		"%s (%s) — %s\n"+
			"  Stage: %s | Sprint: %d | Team: %d (eng=%d, sales=%d, mkt=%d)\n"+
			"  Users: %d | MRR: $%.0f | ARR: $%.0f | Churn: %.1f%%\n"+
			"  CAC: $%.0f | LTV: $%.0f | LTV/CAC: %.1f\n"+
			"  Cash: $%.0f | Burn: $%.0f/mo | Runway: %d months\n"+
			"  Tech Debt: %.0f/100 | Features: %d\n"+
			"  Sprints completed: %d | Quarters completed: %d",
		state.Name, state.FundingRound, state.Mission,
		state.ProductStage, state.CurrentSprint, state.TeamSize,
		state.Engineers, state.SalesPeople, state.MarketingStaff,
		state.Users, state.MRR, state.ARR, state.ChurnRate*100,
		state.CAC, state.LTV, safeDiv(state.LTV, state.CAC),
		state.CashInBank, state.BurnRate, state.Runway,
		state.TechnicalDebt, len(state.Features),
		len(o.SprintHistory), len(o.QuarterHistory),
	)
}

// --- helpers ---

func clamp(val, minVal, maxVal float64) float64 {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
