package engine

import (
	"fmt"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// runFinanceAction invokes a registered action by name against bb and returns
// the BT status code the action reported.
func runFinanceAction(t *testing.T, name string, bb *Blackboard) int {
	t.Helper()
	fn := GetAction(name)
	if fn == nil {
		t.Fatalf("action %q not registered", name)
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	return fn(ctx)
}

// ─── Fixed-output finance actions ───────────────────────────────────────────
//
// registerFinanceActions registers ~52 finance-workflow actions. All but
// BuildLBOModel and BuildDCFModel (which also call bb.LLM.GeneratePlan) follow
// one of two shapes:
//   - bb.Result = "<fixed text>"                          (overwrites Result)
//   - bb.Result = fmt.Sprintf("%s\n\n<text>", bb.Result)   (appends to Result)
// A handful of overwrite actions additionally interpolate bb.Task via %s.

type financeActionCase struct {
	name        string
	appendMode  bool
	useTask     bool // text is a %s format string consuming bb.Task
	text        string
	wantOutcome string
}

func financeActionCases() []financeActionCase {
	return []financeActionCase{
		{name: "FetchCompsData", useTask: true, text: "## Comparable Company Analysis\n\nPulling comps data for: %s", wantOutcome: ""},
		{name: "BuildCompsTable", appendMode: true, text: "\n\nMultiples table built with EV/EBITDA, P/E, EV/Revenue.", wantOutcome: ""},
		{name: "ValidateComps", appendMode: true, text: "\n\nValidation: ranges checked, outliers flagged, sector alignment verified.", wantOutcome: "success"},
		{name: "FetchPrecedentsData", useTask: true, text: "## Precedent Transactions\n\nPulling deal data for: %s", wantOutcome: ""},
		{name: "BuildPrecedentsTable", appendMode: true, text: "\n\nTransaction comps with premiums and deal context.", wantOutcome: ""},
		{name: "AnalyzePremiums", appendMode: true, text: "\n\nControl premiums: 20-35%, synergy assumptions documented.", wantOutcome: "success"},
		{name: "VerifyLBOModel", appendMode: true, text: "\n\nSources=Uses balanced. IRR: 18-25%, MOIC: 2.0-3.0x.", wantOutcome: "success"},
		{name: "FormatLBOOutput", appendMode: true, text: "\n\nBlue/grey palette applied. Formula colors: blue=inputs, black=calcs.", wantOutcome: ""},
		{name: "BuildSensitivityTables", appendMode: true, text: "\n\n3 sensitivity tables (5x5): WACC×Growth, Exit×Growth, WACC×Exit.", wantOutcome: ""},
		{name: "VerifyDCFModel", appendMode: true, text: "\n\nRecalculation: 0 errors. No #REF!/#DIV/0! in sensitivity tables.", wantOutcome: "success"},
		{name: "AssemblePitchDeck", text: "## Pitch Deck\n\nBranded deck assembled with comps, DCF, LBO, and executive summary.", wantOutcome: "success"},
		{name: "QCDeck", appendMode: true, text: "\n\nQC: fonts consistent, charts correct, source footnotes complete.", wantOutcome: ""},
		{name: "IngestEarningsData", useTask: true, text: "## Earnings Review\n\nData ingested: transcript, press release, 8-K for: %s", wantOutcome: ""},
		{name: "ExtractKeyMetrics", appendMode: true, text: "\n\nKey metrics: Revenue, EPS, Guidance, Segment breakdown.", wantOutcome: ""},
		{name: "CompareVsConsensus", appendMode: true, text: "\n\nVs. Consensus: Revenue beat/miss, EPS beat/miss, Guidance vs. Street.", wantOutcome: "success"},
		{name: "UpdateFinancialModel", text: "## Model Updated\n\nDCF/comps model refreshed with latest quarter data.", wantOutcome: ""},
		{name: "RollForwardProjections", appendMode: true, text: "\n\nProjection period rolled forward 1 quarter.", wantOutcome: ""},
		{name: "VerifyModelIntegrity", appendMode: true, text: "\n\nModel integrity: A=L+E, cash flow ties.", wantOutcome: "success"},
		{name: "DraftEarningsNote", text: "## Research Note\n\nKey takeaways, estimate changes, rating: BUY/HOLD/SELL.", wantOutcome: "success"},
		{name: "QCResearchNote", appendMode: true, text: "\n\nQC: disclaimers present, formatting consistent.", wantOutcome: ""},
		{name: "ResearchIndustry", useTask: true, text: "## Industry Research\n\nMarket size, growth, trends for: %s", wantOutcome: ""},
		{name: "BuildIndustryOverview", appendMode: true, text: "\n\nIndustry overview: TAM, CAGR, key players, regulatory landscape.", wantOutcome: "success"},
		{name: "MapCompetitors", text: "## Competitive Landscape\n\nMarket share, positioning, key differentiators.", wantOutcome: ""},
		{name: "BuildPeerComparison", appendMode: true, text: "\n\nPeer comparison: revenue, margins, growth, valuation multiples.", wantOutcome: "success"},
		{name: "ScreenForIdeas", text: "## Investment Ideas\n\nScreened by: sector, market cap, growth, valuation.", wantOutcome: ""},
		{name: "RankAndPrioritize", appendMode: true, text: "\n\nRanked by conviction, upside potential, catalyst timeline.", wantOutcome: "success"},
		{name: "Build3StatementModel", text: "## 3-Statement Model\n\nIS, BS, CFS linked. A=L+E verified.", wantOutcome: ""},
		{name: "VerifyModelBalance", appendMode: true, text: "\n\nBalance check: Assets = Liabilities + Equity ✓", wantOutcome: "success"},
		{name: "GatherClientContext", text: "## Client Briefing\n\nContext gathered: holdings, recent interactions, preferences.", wantOutcome: ""},
		{name: "BuildBriefingPack", appendMode: true, text: "\n\nBriefing: portfolio review, market update, talking points.", wantOutcome: "success"},
		{name: "QCBriefingPack", text: "## Quality Check\n\n**Briefing**: Verified data accuracy, formatting, completeness.\n**Status**: Approved.", wantOutcome: "success"},
		{name: "IngestGPPackage", text: "## GP Package\n\nCapital account statements, cap tables ingested.", wantOutcome: ""},
		{name: "RunValuationTemplate", appendMode: true, text: "\n\nValuation: market approach, income approach, NAV.", wantOutcome: ""},
		{name: "StageLPReporting", appendMode: true, text: "\n\nLP reports staged: capital accounts, performance summaries.", wantOutcome: "success"},
		{name: "CompareGLEntries", useTask: true, text: "## GL Reconciliation\n\nComparing GL to sub-ledger for: %s", wantOutcome: ""},
		{name: "IdentifyBreaks", appendMode: true, text: "\n\nBreaks identified, categorized by type.", wantOutcome: ""},
		{name: "TraceRootCause", appendMode: true, text: "\n\nRoot cause traced to source transaction.", wantOutcome: ""},
		{name: "RouteForSignOff", appendMode: true, text: "\n\nReconciliation package routed for reviewer approval.", wantOutcome: "success"},
		{name: "CalculateAccruals", useTask: true, text: "## Month-End Close\n\nAccruals calculated for: %s", wantOutcome: ""},
		{name: "RunRollForward", appendMode: true, text: "\n\nBalance sheet accounts rolled forward.", wantOutcome: ""},
		{name: "AnalyzeVariance", appendMode: true, text: "\n\nVariance analysis: actuals vs. budget, commentary written.", wantOutcome: ""},
		{name: "PrepareClosePackage", appendMode: true, text: "\n\nClose package assembled for controller review.", wantOutcome: "success"},
		{name: "IngestLPStatements", useTask: true, text: "## LP Statement Audit\n\nStatements loaded for: %s", wantOutcome: ""},
		{name: "ValidateCalculations", appendMode: true, text: "\n\nNAV, allocations, waterfall verified.", wantOutcome: ""},
		{name: "CheckDisclosures", appendMode: true, text: "\n\nRegulatory disclosures and footnotes checked.", wantOutcome: ""},
		{name: "GenerateAuditReport", appendMode: true, text: "\n\nAudit findings report generated.", wantOutcome: "success"},
		{name: "ParseOnboardingDocs", text: "## KYC Screening\n\nOnboarding docs parsed: entity info, beneficial owners.", wantOutcome: ""},
		{name: "RunKYCRulesEngine", appendMode: true, text: "\n\nScreened: sanctions lists, PEP databases, adverse media.", wantOutcome: ""},
		{name: "FlagGaps", appendMode: true, text: "\n\nGaps flagged: missing docs, red flags, escalation items.", wantOutcome: ""},
		{name: "GenerateKYCReport", appendMode: true, text: "\n\nKYC report generated with risk rating.", wantOutcome: "success"},
	}
}

func TestFinanceActions_FixedOutputs(t *testing.T) {
	const task = "restructure the portfolio company"

	for _, c := range financeActionCases() {
		t.Run(c.name, func(t *testing.T) {
			bb := &Blackboard{Task: task, Result: "SEED"}
			status := runFinanceAction(t, c.name, bb)

			if status != 1 {
				t.Errorf("status = %d, want 1", status)
			}

			var wantResult string
			switch {
			case c.useTask:
				wantResult = fmt.Sprintf(c.text, task)
			case c.appendMode:
				wantResult = "SEED" + c.text
			default:
				wantResult = c.text
			}
			if bb.Result != wantResult {
				t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
			}

			if bb.Outcome != c.wantOutcome {
				t.Errorf("Outcome = %q, want %q", bb.Outcome, c.wantOutcome)
			}
		})
	}
}

// ─── LBO / DCF model actions (bb.LLM.GeneratePlan) ─────────────────────────

// financeSpyLLM wraps MockLLM to capture the arguments GeneratePlan was
// called with, so tests can pin the exact prompt/complexity each action
// sends to the LLM.
type financeSpyLLM struct {
	*MockLLM
	gotTask       string
	gotComplexity string
}

func (s *financeSpyLLM) GeneratePlan(task, complexity string) string {
	s.gotTask = task
	s.gotComplexity = complexity
	return s.MockLLM.GeneratePlan(task, complexity)
}

func TestBuildLBOModel(t *testing.T) {
	spy := &financeSpyLLM{MockLLM: &MockLLM{PlanResp: "1. Size the entry multiple\n2. Layer debt tranches"}}
	bb := &Blackboard{Task: "acquire target co", LLM: spy, Result: "SEED"}
	status := runFinanceAction(t, "BuildLBOModel", bb)

	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if spy.gotTask != "build LBO model for: acquire target co" {
		t.Errorf("GeneratePlan task = %q, want %q", spy.gotTask, "build LBO model for: acquire target co")
	}
	if spy.gotComplexity != "high" {
		t.Errorf("GeneratePlan complexity = %q, want %q", spy.gotComplexity, "high")
	}
	if bb.Plan != spy.PlanResp {
		t.Errorf("Plan = %q, want %q", bb.Plan, spy.PlanResp)
	}
	wantResult := "## LBO Model\n\nTemplate filled with: Entry %, Debt structure, Exit assumptions"
	if bb.Result != wantResult {
		t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
	}
	if bb.Outcome != "" {
		t.Errorf("Outcome = %q, want unset", bb.Outcome)
	}
}

func TestBuildDCFModel(t *testing.T) {
	spy := &financeSpyLLM{MockLLM: &MockLLM{PlanResp: "1. Project FCF\n2. Calculate WACC"}}
	bb := &Blackboard{Task: "value target co", LLM: spy, Result: "SEED"}
	status := runFinanceAction(t, "BuildDCFModel", bb)

	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if spy.gotTask != "build DCF model for: value target co" {
		t.Errorf("GeneratePlan task = %q, want %q", spy.gotTask, "build DCF model for: value target co")
	}
	if spy.gotComplexity != "high" {
		t.Errorf("GeneratePlan complexity = %q, want %q", spy.gotComplexity, "high")
	}
	if bb.Plan != spy.PlanResp {
		t.Errorf("Plan = %q, want %q", bb.Plan, spy.PlanResp)
	}
	wantResult := "## DCF Model\n\n3 scenarios (Bear/Base/Bull), WACC calculated, FCF projected."
	if bb.Result != wantResult {
		t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
	}
	if bb.Outcome != "" {
		t.Errorf("Outcome = %q, want unset", bb.Outcome)
	}
}
