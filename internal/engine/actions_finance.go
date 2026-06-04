// Package engine — Finance actions extracted from tree.go actionForName switch.
// Registers ~52 actions: comps, precedents, LBO, DCF, pitch deck, earnings,
// industry research, model building, GL reconciliation, KYC screening, etc.
package engine

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerFinanceActions()
}

func registerFinanceActions() {
	RegisterAction("FetchCompsData", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Comparable Company Analysis\n\nPulling comps data for: %s", bb.Task)
		return 1
	})

	RegisterAction("BuildCompsTable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nMultiples table built with EV/EBITDA, P/E, EV/Revenue.", bb.Result)
		return 1
	})

	RegisterAction("ValidateComps", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nValidation: ranges checked, outliers flagged, sector alignment verified.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("FetchPrecedentsData", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Precedent Transactions\n\nPulling deal data for: %s", bb.Task)
		return 1
	})

	RegisterAction("BuildPrecedentsTable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nTransaction comps with premiums and deal context.", bb.Result)
		return 1
	})

	RegisterAction("AnalyzePremiums", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nControl premiums: 20-35%%, synergy assumptions documented.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("BuildLBOModel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Plan = bb.LLM.GeneratePlan("build LBO model for: "+bb.Task, "high")
		bb.Result = fmt.Sprintf("## LBO Model\n\nTemplate filled with: Entry %%, Debt structure, Exit assumptions")
		return 1
	})

	RegisterAction("VerifyLBOModel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nSources=Uses balanced. IRR: 18-25%%, MOIC: 2.0-3.0x.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("FormatLBOOutput", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nBlue/grey palette applied. Formula colors: blue=inputs, black=calcs.", bb.Result)
		return 1
	})

	RegisterAction("BuildDCFModel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Plan = bb.LLM.GeneratePlan("build DCF model for: "+bb.Task, "high")
		bb.Result = fmt.Sprintf("## DCF Model\n\n3 scenarios (Bear/Base/Bull), WACC calculated, FCF projected.")
		return 1
	})

	RegisterAction("BuildSensitivityTables", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\n3 sensitivity tables (5x5): WACC×Growth, Exit×Growth, WACC×Exit.", bb.Result)
		return 1
	})

	RegisterAction("VerifyDCFModel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nRecalculation: 0 errors. No #REF!/#DIV/0! in sensitivity tables.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("AssemblePitchDeck", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Pitch Deck\n\nBranded deck assembled with comps, DCF, LBO, and executive summary.")
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("QCDeck", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nQC: fonts consistent, charts correct, source footnotes complete.", bb.Result)
		return 1
	})

	RegisterAction("IngestEarningsData", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Earnings Review\n\nData ingested: transcript, press release, 8-K for: %s", bb.Task)
		return 1
	})

	RegisterAction("ExtractKeyMetrics", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nKey metrics: Revenue, EPS, Guidance, Segment breakdown.", bb.Result)
		return 1
	})

	RegisterAction("CompareVsConsensus", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nVs. Consensus: Revenue beat/miss, EPS beat/miss, Guidance vs. Street.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("UpdateFinancialModel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Model Updated\n\nDCF/comps model refreshed with latest quarter data.")
		return 1
	})

	RegisterAction("RollForwardProjections", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nProjection period rolled forward 1 quarter.", bb.Result)
		return 1
	})

	RegisterAction("VerifyModelIntegrity", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nModel integrity: A=L+E, cash flow ties.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("DraftEarningsNote", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Research Note\n\nKey takeaways, estimate changes, rating: BUY/HOLD/SELL.")
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("QCResearchNote", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nQC: disclaimers present, formatting consistent.", bb.Result)
		return 1
	})

	RegisterAction("ResearchIndustry", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Industry Research\n\nMarket size, growth, trends for: %s", bb.Task)
		return 1
	})

	RegisterAction("BuildIndustryOverview", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nIndustry overview: TAM, CAGR, key players, regulatory landscape.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("MapCompetitors", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Competitive Landscape\n\nMarket share, positioning, key differentiators.")
		return 1
	})

	RegisterAction("BuildPeerComparison", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nPeer comparison: revenue, margins, growth, valuation multiples.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("ScreenForIdeas", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Investment Ideas\n\nScreened by: sector, market cap, growth, valuation.")
		return 1
	})

	RegisterAction("RankAndPrioritize", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nRanked by conviction, upside potential, catalyst timeline.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("Build3StatementModel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## 3-Statement Model\n\nIS, BS, CFS linked. A=L+E verified.")
		return 1
	})

	RegisterAction("VerifyModelBalance", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nBalance check: Assets = Liabilities + Equity ✓", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("GatherClientContext", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Client Briefing\n\nContext gathered: holdings, recent interactions, preferences.")
		return 1
	})

	RegisterAction("BuildBriefingPack", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nBriefing: portfolio review, market update, talking points.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("QCBriefingPack", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Quality Check\n\n**Briefing**: Verified data accuracy, formatting, completeness.\n**Status**: Approved.")
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("IngestGPPackage", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## GP Package\n\nCapital account statements, cap tables ingested.")
		return 1
	})

	RegisterAction("RunValuationTemplate", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nValuation: market approach, income approach, NAV.", bb.Result)
		return 1
	})

	RegisterAction("StageLPReporting", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nLP reports staged: capital accounts, performance summaries.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("CompareGLEntries", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## GL Reconciliation\n\nComparing GL to sub-ledger for: %s", bb.Task)
		return 1
	})

	RegisterAction("IdentifyBreaks", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nBreaks identified, categorized by type.", bb.Result)
		return 1
	})

	RegisterAction("TraceRootCause", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nRoot cause traced to source transaction.", bb.Result)
		return 1
	})

	RegisterAction("RouteForSignOff", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nReconciliation package routed for reviewer approval.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("CalculateAccruals", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Month-End Close\n\nAccruals calculated for: %s", bb.Task)
		return 1
	})

	RegisterAction("RunRollForward", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nBalance sheet accounts rolled forward.", bb.Result)
		return 1
	})

	RegisterAction("AnalyzeVariance", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nVariance analysis: actuals vs. budget, commentary written.", bb.Result)
		return 1
	})

	RegisterAction("PrepareClosePackage", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nClose package assembled for controller review.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("IngestLPStatements", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## LP Statement Audit\n\nStatements loaded for: %s", bb.Task)
		return 1
	})

	RegisterAction("ValidateCalculations", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nNAV, allocations, waterfall verified.", bb.Result)
		return 1
	})

	RegisterAction("CheckDisclosures", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nRegulatory disclosures and footnotes checked.", bb.Result)
		return 1
	})

	RegisterAction("GenerateAuditReport", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nAudit findings report generated.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("ParseOnboardingDocs", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## KYC Screening\n\nOnboarding docs parsed: entity info, beneficial owners.")
		return 1
	})

	RegisterAction("RunKYCRulesEngine", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nScreened: sanctions lists, PEP databases, adverse media.", bb.Result)
		return 1
	})

	RegisterAction("FlagGaps", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nGaps flagged: missing docs, red flags, escalation items.", bb.Result)
		return 1
	})

	RegisterAction("GenerateKYCReport", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nKYC report generated with risk rating.", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})
}
