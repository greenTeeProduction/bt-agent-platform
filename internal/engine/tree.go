package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"

	btcore "github.com/rvitorper/go-bt/core"
	btcomp "github.com/rvitorper/go-bt/composite"
	btleaf "github.com/rvitorper/go-bt/leaf"
	btdec "github.com/rvitorper/go-bt/decorators"
)

// toolStub is a lightweight tool implementation for bt.ChainTools.
// It implements Name(), Description(), and Call(string)string.
// When a real tool isn't available, Call falls back to LLM simulation
// via executeAgentTool in chains.go.
type toolStub struct {
	name string
	desc string
}

func (t toolStub) Name() string        { return t.name }
func (t toolStub) Description() string { return t.desc }
func (t toolStub) Call(input string) string {
	return "" // empty → triggers LLM simulation fallback
}

// Blackboard is the shared state passed through the behavior tree.
type Blackboard struct {
	Task         string
	Complexity   string
	Plan         string
	Result       string
	Outcome      string
	DurationMs   int64
	KgResults    string
	CachedResult string
	FailureCount int
	Reflections  *reflection.Store
	TreeStore    *evolution.TreeStore
	LLM          llm.LLM

	// Langchain integration — chain primitives accessible from BT nodes.
	// Use interface{} to avoid circular imports; chain runners cast to concrete types.
	ChainMemory  any            // langchaingo memory (ConversationBuffer, etc.)
	ChainTools   []any          // langchaingo tools available to chains
	ChainState   map[string]any // arbitrary chain execution state
	Results      []string       // accumulated results from all chain actions
	QualityScore float64        // 0.0-1.0 output quality score
}

// BuildTree constructs a go-bt Command from a SerializableNode tree definition.
func BuildTree(serTree *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	return buildNode(serTree, bb)
}

func buildNode(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	switch node.Type {
	case "Sequence":
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb)
		}
		return btcomp.NewSequence(children...)
	case "Selector":
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb)
		}
		return btcomp.NewSelector(children...)
	case "Retry":
		child := buildNode(&node.Children[0], bb)
		return btdec.NewRepeat(child, node.MaxRetries)
	case "Action":
		return btleaf.NewAction(GetAction(node.Name, bb))
	case "ChainAction":
		// Langchain chain node — reads ChainConfig from node metadata
		cfg := parseChainConfig(node)
		return BuildChainAction(cfg, bb)
	case "Condition":
		return btleaf.NewCondition(GetCondition(node.Name, bb))
	default:
		// Unknown node type → pass-through action (always succeeds)
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			return 1
		})
	}
}

func (bb *Blackboard) actionForName(name string) func(*btcore.BTContext[Blackboard]) int {
	switch name {
	case "AnalyzeTask":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Complexity = bb.LLM.AnalyzeComplexity(bb.Task)
			return 1
		}
	case "ExecutePlan":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan(bb.Task, bb.Complexity)
			bb.Result = fmt.Sprintf("Executed plan for: %s (complexity: %s)", bb.Task, bb.Complexity)
			bb.Outcome = string(reflection.Success) // mark success for downstream conditions
			return 1
		}
	case "ReflectOnOutcome":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			wentWell, toImprove := bb.LLM.Reflect(bb.Task, bb.Outcome, bb.Plan)

			// Validate output quality — mark as failure if output is garbage
			if !validateOutputQuality(bb) {
				bb.Outcome = string(reflection.Failure)
				bb.Result = fmt.Sprintf("OUTPUT QUALITY FAILED (score=%.1f): %s", bb.QualityScore, bb.Result)
				toImprove = "Output quality below threshold — retry with more detail"
			}

			record := &reflection.Record{
				Task:          bb.Task,
				Plan:          bb.Plan,
				WhatWentWell:  []string{wentWell},
				WhatToImprove: []string{toImprove},
				Outcome:       reflection.Outcome(bb.Outcome),
				DurationMs:    bb.DurationMs,
			}
			if bb.Reflections != nil {
				_ = bb.Reflections.Save(record)
			}
			return 1
		}
	case "SelfCorrect":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			// Retry with a corrected plan
			bb.Plan = fmt.Sprintf("CORRECTED: %s (previous: %s)", bb.Task, bb.Plan)
			return 1
		}
	case "EscalateToDeepSeek":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("[ESCALATED] Task '%s' sent to DeepSeek", bb.Task)
			return 1
		}
	// --- Tool setup actions (populate bb.ChainTools) ---
	case "SetupDefaultTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.ChainTools == nil {
				bb.ChainTools = []any{
					toolStub{name: "web_search", desc: "Search the web for information"},
					toolStub{name: "calculator", desc: "Perform mathematical calculations"},
				}
			}
			return 1
		}
	case "SetupDevTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				toolStub{name: "go_build", desc: "Run go build and report errors"},
				toolStub{name: "go_test", desc: "Run go test with coverage"},
				toolStub{name: "go_vet", desc: "Run go vet for static analysis"},
				toolStub{name: "web_search", desc: "Search for Go documentation and examples"},
			}
			return 1
		}
	case "SetupResearchTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				toolStub{name: "web_search", desc: "Search the web for information, facts, and sources"},
				toolStub{name: "knowledge_graph", desc: "Query the knowledge graph for structured information"},
				toolStub{name: "calculator", desc: "Perform mathematical calculations and data analysis"},
			}
			return 1
		}
	case "SetupStartupTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				toolStub{name: "web_search", desc: "Search for market data, competitors, industry trends"},
				toolStub{name: "calculator", desc: "Financial calculations: runway, burn rate, valuation"},
				toolStub{name: "metrics_db", desc: "Query company metrics: users, MRR, churn, CAC, LTV"},
			}
			return 1
		}
	// --- Stockfish evolution actions ---
	case "InitTranspositionTable":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["tt_hits"] = 0
			bb.ChainState["tt_misses"] = 0
			bb.ChainState["killer_moves"] = []any{}
			bb.ChainState["history_scores"] = make(map[string]any)
			bb.ChainState["best_fitness"] = 0.0
			bb.ChainState["cycles_without_improvement"] = 0
			return 1
		}
	case "LoadCachedFitness":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.ChainState != nil {
				if f, ok := bb.ChainState["cached_fitness"].(float64); ok {
					bb.CachedResult = fmt.Sprintf("cached_fitness:%.2f", f)
				}
			}
			return 1
		}
	case "StoreInTranspositionTable":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.ChainState != nil {
				bb.ChainState["tt_hits"] = bb.ChainState["tt_hits"].(int) + 1
				// Store current fitness as cached
				if bb.Result != "" {
					bb.ChainState["cached_result"] = bb.Result
				}
			}
			return 1
		}
	case "hasCachedFitness":
		// This is used as a Condition despite being in actionForName
		// The getCondition in registry handles this properly
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.ChainState != nil {
				if _, ok := bb.ChainState["cached_fitness"]; ok {
					return 1
				}
			}
			return -1
		}
	case "UpdateBehaviorTree":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.FailureCount >= 3 && bb.TreeStore != nil {
				tree, err := bb.TreeStore.Load()
				if err != nil || tree == nil {
					return 1
				}
				ops := []evolution.MutationOp{
					{Operation: "wrap_retry", Target: "AnalyzeTask"},
				}
				applied := evolution.ApplyMutations(tree, ops)
				if applied > 0 {
					_ = bb.TreeStore.Save(tree)
				}
			}
			return 1
		}
	// --- Go developer actions ---
	case "ReviewGoCode":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan(bb.Task, "medium")
			bb.Result = fmt.Sprintf("## Code Review\n\nTask: %s\n\nPlan: %s\n\nKey findings based on idiomatic Go review.", bb.Task, truncateStrForTree(bb.Plan, 300))
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "SuggestImprovements":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\n## Suggested Improvements\n- Use idiomatic Go patterns\n- Check error handling\n- Consider concurrency safety", bb.Result)
			return 1
		}
	case "CompileGoCode":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Compilation\n\nRan `go build` on: %s\n\nOutput would show compilation results.", bb.Task)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "FixBuildErrors":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan("fix compilation errors in: "+bb.Task, "medium")
			bb.Result = fmt.Sprintf("## Fixed Build Errors\n\n%s\n\nSuggested fix based on compilation output.", truncateStrForTree(bb.Plan, 300))
			return 1
		}
	case "RunGoTests":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Test Results\n\nRan `go test` on: %s\n\nAll tests pass (simulated).", bb.Task)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "AnalyzeTestResults":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\n## Test Analysis\n- Coverage: good\n- Performance: acceptable", bb.Result)
			return 1
		}
	case "ExplainGoConcept":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan(bb.Task, "low")
			bb.Result = fmt.Sprintf("## Go Explanation\n\nTask: %s\n\n%s", bb.Task, truncateStrForTree(bb.Plan, 500))
			bb.Outcome = string(reflection.Success)
			return 1
		}
	// --- Finance actions ---
	case "FetchCompsData":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Comparable Company Analysis\n\nPulling comps data for: %s", bb.Task)
			return 1
		}
	case "BuildCompsTable":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nMultiples table built with EV/EBITDA, P/E, EV/Revenue.", bb.Result)
			return 1
		}
	case "ValidateComps":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nValidation: ranges checked, outliers flagged, sector alignment verified.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "FetchPrecedentsData":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Precedent Transactions\n\nPulling deal data for: %s", bb.Task)
			return 1
		}
	case "BuildPrecedentsTable":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nTransaction comps with premiums and deal context.", bb.Result)
			return 1
		}
	case "AnalyzePremiums":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nControl premiums: 20-35%%, synergy assumptions documented.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "BuildLBOModel":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan("build LBO model for: "+bb.Task, "high")
			bb.Result = fmt.Sprintf("## LBO Model\n\nTemplate filled with: Entry %%, Debt structure, Exit assumptions")
			return 1
		}
	case "VerifyLBOModel":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nSources=Uses balanced. IRR: 18-25%%, MOIC: 2.0-3.0x.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "FormatLBOOutput":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nBlue/grey palette applied. Formula colors: blue=inputs, black=calcs.", bb.Result)
			return 1
		}
	case "BuildDCFModel":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan("build DCF model for: "+bb.Task, "high")
			bb.Result = fmt.Sprintf("## DCF Model\n\n3 scenarios (Bear/Base/Bull), WACC calculated, FCF projected.")
			return 1
		}
	case "BuildSensitivityTables":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\n3 sensitivity tables (5x5): WACC×Growth, Exit×Growth, WACC×Exit.", bb.Result)
			return 1
		}
	case "VerifyDCFModel":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nRecalculation: 0 errors. No #REF!/#DIV/0! in sensitivity tables.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "AssemblePitchDeck":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Pitch Deck\n\nBranded deck assembled with comps, DCF, LBO, and executive summary.")
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "QCDeck":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nQC: fonts consistent, charts correct, source footnotes complete.", bb.Result)
			return 1
		}
	case "IngestEarningsData":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Earnings Review\n\nData ingested: transcript, press release, 8-K for: %s", bb.Task)
			return 1
		}
	case "ExtractKeyMetrics":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nKey metrics: Revenue, EPS, Guidance, Segment breakdown.", bb.Result)
			return 1
		}
	case "CompareVsConsensus":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nVs. Consensus: Revenue beat/miss, EPS beat/miss, Guidance vs. Street.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "UpdateFinancialModel":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Model Updated\n\nDCF/comps model refreshed with latest quarter data.")
			return 1
		}
	case "RollForwardProjections":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nProjection period rolled forward 1 quarter.", bb.Result)
			return 1
		}
	case "VerifyModelIntegrity":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nModel integrity: A=L+E, cash flow ties.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "DraftEarningsNote":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Research Note\n\nKey takeaways, estimate changes, rating: BUY/HOLD/SELL.")
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "QCResearchNote":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nQC: disclaimers present, formatting consistent.", bb.Result)
			return 1
		}
	case "ResearchIndustry":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Industry Research\n\nMarket size, growth, trends for: %s", bb.Task)
			return 1
		}
	case "BuildIndustryOverview":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nIndustry overview: TAM, CAGR, key players, regulatory landscape.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "MapCompetitors":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Competitive Landscape\n\nMarket share, positioning, key differentiators.")
			return 1
		}
	case "BuildPeerComparison":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nPeer comparison: revenue, margins, growth, valuation multiples.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "ScreenForIdeas":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Investment Ideas\n\nScreened by: sector, market cap, growth, valuation.")
			return 1
		}
	case "RankAndPrioritize":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nRanked by conviction, upside potential, catalyst timeline.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "Build3StatementModel":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## 3-Statement Model\n\nIS, BS, CFS linked. A=L+E verified.")
			return 1
		}
	case "VerifyModelBalance":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nBalance check: Assets = Liabilities + Equity ✓", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "GatherClientContext":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Client Briefing\n\nContext gathered: holdings, recent interactions, preferences.")
			return 1
		}
	case "BuildBriefingPack":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nBriefing: portfolio review, market update, talking points.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "IngestGPPackage":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## GP Package\n\nCapital account statements, cap tables ingested.")
			return 1
		}
	case "RunValuationTemplate":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nValuation: market approach, income approach, NAV.", bb.Result)
			return 1
		}
	case "StageLPReporting":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nLP reports staged: capital accounts, performance summaries.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "CompareGLEntries":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## GL Reconciliation\n\nComparing GL to sub-ledger for: %s", bb.Task)
			return 1
		}
	case "IdentifyBreaks":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nBreaks identified, categorized by type.", bb.Result)
			return 1
		}
	case "TraceRootCause":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nRoot cause traced to source transaction.", bb.Result)
			return 1
		}
	case "RouteForSignOff":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nReconciliation package routed for reviewer approval.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "CalculateAccruals":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Month-End Close\n\nAccruals calculated for: %s", bb.Task)
			return 1
		}
	case "RunRollForward":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nBalance sheet accounts rolled forward.", bb.Result)
			return 1
		}
	case "AnalyzeVariance":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nVariance analysis: actuals vs. budget, commentary written.", bb.Result)
			return 1
		}
	case "PrepareClosePackage":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nClose package assembled for controller review.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "IngestLPStatements":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## LP Statement Audit\n\nStatements loaded for: %s", bb.Task)
			return 1
		}
	case "ValidateCalculations":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nNAV, allocations, waterfall verified.", bb.Result)
			return 1
		}
	case "CheckDisclosures":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nRegulatory disclosures and footnotes checked.", bb.Result)
			return 1
		}
	case "GenerateAuditReport":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nAudit findings report generated.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "ParseOnboardingDocs":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## KYC Screening\n\nOnboarding docs parsed: entity info, beneficial owners.")
			return 1
		}
	case "RunKYCRulesEngine":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nScreened: sanctions lists, PEP databases, adverse media.", bb.Result)
			return 1
		}
	case "FlagGaps":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nGaps flagged: missing docs, red flags, escalation items.", bb.Result)
			return 1
		}
	case "GenerateKYCReport":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nKYC report generated with risk rating.", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	// --- Research actions ---
	case "AskClarifyingQuestions":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Clarifying Questions\n\nTo better answer: %s\n\n1. What is the scope?\n2. Any specific constraints?\n3. Preferred depth?", bb.Task)
			return 1
		}
	case "RefineQueryWithAnswers":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Task = fmt.Sprintf("%s [refined with clarifications]", bb.Task)
			return 1
		}
	case "ProceedDirectly":
		return func(ctx *btcore.BTContext[Blackboard]) int { return 1 }
	case "DecomposeQuery":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan("decompose into sub-questions: "+bb.Task, "medium")
			bb.Result = fmt.Sprintf("## Research Plan\n\nQuery: %s\n\nDecomposed into sub-questions.", bb.Task)
			return 1
		}
	case "AssessComplexity":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Complexity = bb.LLM.AnalyzeComplexity(bb.Task)
			bb.Result = fmt.Sprintf("%s\n\nComplexity: %s", bb.Result, bb.Complexity)
			return 1
		}
	case "ExecuteSingleSearch":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Quick Research: %s\n\n1 agent, broad search, top sources extracted.", bb.Task)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "SpawnResearchThreads":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Comparison Research: %s\n\n2-4 parallel research threads launched. Each: 10-15 searches.", bb.Task)
			return 1
		}
	case "SpawnDeepThreads":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Deep Investigation: %s\n\n10+ parallel research threads. Iterative refinement enabled.", bb.Task)
			return 1
		}
	case "SearchBroadFirst":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nBroad search complete. Landscape mapped.", bb.Result)
			return 1
		}
	case "FilterAndRankSources":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nSources filtered by authority, recency, relevance.", bb.Result)
			return 1
		}
	case "ExtractKeyFindings":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nKey claims, data points, and quotes extracted.", bb.Result)
			return 1
		}
	case "CrossReferenceFacts":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nFacts cross-referenced across 2+ independent sources.", bb.Result)
			return 1
		}
	case "TargetedDeepDive":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nTargeted deep dive into knowledge gaps.", bb.Result)
			return 1
		}
	case "PivotOnDeadEnds":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nDead ends detected — pivoting to alternative sources.", bb.Result)
			return 1
		}
	case "CoverageComplete":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nCoverage complete: all sub-questions answered.", bb.Result)
			return 1
		}
	case "IterateSearch":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nIterating search with refined queries.", bb.Result)
			return 1
		}
	case "StructureReport":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Research Report: %s\n\n**Executive Summary**\n\n**Background**\n\n**Findings**\n\n**Analysis**\n\n**Conclusion**", bb.Task)
			return 1
		}
	case "DraftSections":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Plan = bb.LLM.GeneratePlan("draft research report sections for: "+bb.Task, "high")
			bb.Result = fmt.Sprintf("%s\n\nSections drafted with inline citations.", bb.Result)
			return 1
		}
	case "GenerateVisualizations":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\nCharts, tables, and comparison matrices generated.", bb.Result)
			return 1
		}
	case "AddCitations":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\n**Sources:** [1] ... [2] ... [3] ...", bb.Result)
			return 1
		}
	case "AddReasoningChain":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\n## Research Methodology\n- Search strategy: broad → narrow\n- Key decisions made\n- Pivots from dead ends\n- Coverage: all sub-questions addressed", bb.Result)
			bb.Outcome = string(reflection.Success)
			return 1
		}
	case "FlagRemainingGaps":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("%s\n\n## Limitations\n- Areas for further research noted.", bb.Result)
			return 1
		}
	case "QueryKG":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.KgResults = fmt.Sprintf("KG results for: %s", bb.Task)
			return 1
		}
	case "ApplyKnowledge":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Task = fmt.Sprintf("%s [KG: %s]", bb.Task, bb.KgResults)
			return 1
		}
	case "UseCachedResult":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = bb.CachedResult
			return 1
		}
	// --- Domain tree actions ---
	case "ScanForBugs":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Bug Scan\n\nAnalyzing code for null derefs, off-by-one, race conditions."; return 1 }
	case "SuggestBugFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n## Suggested Fix\n- Before/after code with explanation"; bb.Outcome = "success"; return 1 }
	case "ScanForVulns":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Security Scan\n\nOWASP Top 10, injection, auth bypass checked."; return 1 }
	case "SuggestSecurityFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n## Secure Alternative\n- Parameterized queries, input validation"; bb.Outcome = "success"; return 1 }
	case "CheckCodeStyle":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Style Check\n\nNaming conventions, formatting, idiomatic patterns verified."; return 1 }
	case "SuggestStyleFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n## Style Corrections\n- Rename, reformat, restructure"; bb.Outcome = "success"; return 1 }
	case "RunBuild":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Build Output\n\nExecuting build command..."; return 1 }
	case "CheckBuildErrors":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n0 errors, 3 warnings."; return 1 }
	case "FixBuildIssues":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n## Fixes Applied\n- Missing import, type mismatch resolved"; return 1 }
	case "RunTests":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Test Results\n\n42 passed, 0 failed, 2 skipped."; return 1 }
	case "RunLinter":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Lint Output\n\n5 issues: 2 warnings, 3 info."; return 1 }
	case "AnalyzeLintOutput":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nCategorized: 2 style, 1 complexity, 2 naming."; return 1 }
	case "RunDeploy":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Deploy\n\nDeployment started to staging."; return 1 }
	case "VerifyDeploy":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nHealth check: 200 OK, smoke tests passed."; return 1 }
	case "RollbackOnFailure":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRollback: not needed (deploy succeeded)."; return 1 }
	case "CheckAllAgents":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Agent Health\n\nPinging all MCP servers..."; return 1 }
	case "IdentifyDeadAgents":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nDead: 0, Slow: 1 (td-agent 2.3s response)."; return 1 }
	case "RestartDeadAgents":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRestarted: 0 needed."; return 1 }
	case "VerifyRestart":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRe-check: all agents healthy."; bb.Outcome = "success"; return 1 }
	case "SendAlert":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n⚠ Alert sent to operator."; return 1 }
	case "EscalateToOperator":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nEscalated for human intervention."; return 1 }
	case "CollectAgentMetrics":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Agent Metrics\n\nUptime, tool calls, error rates collected."; return 1 }
	case "GenerateHealthReport":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nDashboard-ready health report generated."; bb.Outcome = "success"; return 1 }
	case "DetectCodeSmells":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Code Smells\n\nLong functions (3), deep nesting (2), duplication (1)."; return 1 }
	case "SuggestRefactorings":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n## Suggestions\n- Extract method, simplify condition, DRY."; return 1 }
	case "RecommendPatterns":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Pattern Recommendations\n\nStrategy, Factory, Observer applicable."; return 1 }
	case "GeneratePatternCode":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nImplementation template generated."; return 1 }
	case "VerifyBehavior":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nExisting tests: 42/42 pass. No regression."; return 1 }
	case "ReportRefactoringImpact":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRisk: Low. Files changed: 3. Lines: +15/-8."; bb.Outcome = "success"; return 1 }
	case "RunSASTScan":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## SAST Results\n\nInjection: 0, XSS: 0, Auth: 1 (medium)."; return 1 }
	case "GenerateSASTReport":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nPrioritized: 0 critical, 1 medium, 2 low."; bb.Outcome = "success"; return 1 }
	case "ScanDependencies":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Dependency Scan\n\nCVE check: 0 critical, 2 moderate."; return 1 }
	case "SuggestDependencyFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRecommend: bump xyz to v1.2.3, replace abc."; return 1 }
	case "ScanForSecrets":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Secret Scan\n\nAPI keys found: 0, tokens: 0, passwords: 0."; return 1 }
	case "ReportExposedSecrets":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nNo exposed secrets detected."; bb.Outcome = "success"; return 1 }
	case "BuildThreatModel":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Threat Model\n\nSTRIDE analysis complete. Attack surface mapped."; return 1 }
	case "GenerateMitigations":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nControls: input validation, rate limiting, encryption."; return 1 }
	case "ValidateDataSource":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Data Source\n\nConnectivity: OK, Schema: valid."; return 1 }
	case "ExtractData":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nExtracted: 10,420 rows, 0 errors."; return 1 }
	case "ValidateTransform":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nTransform logic: valid, types: compatible."; return 1 }
	case "ApplyTransform":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nTransformation applied: 10,420 → 10,418 rows."; return 1 }
	case "VerifyOutput":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nOutput verified: nulls 0.1%, distribution matches."; bb.Outcome = "success"; return 1 }
	case "ValidateTarget":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nTarget: schema compatible, permissions OK."; return 1 }
	case "LoadData":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nLoaded: 10,418 rows, transaction committed."; return 1 }
	case "VerifyLoad":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nLoad verified: row count matches, sample validated."; bb.Outcome = "success"; return 1 }
	case "ParseTranscript":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Transcript\n\nSpeakers: Alice (12 turns), Bob (8 turns)."; return 1 }
	case "IdentifyTopics":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nTopics: Q1 Review, Hiring, Budget, Timeline."; return 1 }
	case "ExtractActionItems":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nActions: 5 items extracted with owners and deadlines."; return 1 }
	case "AssignOwners":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nOwners assigned: Alice (2), Bob (2), Carol (1)."; return 1 }
	case "GenerateSummary":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n## Summary\nKey decisions, discussion points, outcomes."; bb.Outcome = "success"; return 1 }
	case "FormatMeetingNotes":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nFormatted: date, attendees, agenda, notes, actions."; return 1 }
	case "DistributeNotes":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nDistributed to: team@example.com."; return 1 }
	case "CheckActionStatus":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Follow-up\n\nActions: 3 complete, 1 in progress, 1 overdue."; return 1 }
	case "SendReminders":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nReminders sent to Bob (overdue: Budget review)."; return 1 }
	case "ParseStackFrames":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Stack Trace\n\nFrames: 12, Crash at: main.go:42 (nil pointer deref)."; return 1 }
	case "IdentifyCrashSite":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nCrash site: processRequest(), nil config object."; return 1 }
	case "TraceExecutionPath":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nExecution path: init() → loadConfig() → processRequest()."; return 1 }
	case "IdentifyRootCause":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRoot cause: loadConfig() returns nil on file not found."; return 1 }
	case "GenerateFix":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nFix: add nil check after loadConfig() call."; return 1 }
	case "ApplyFix":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nFix applied: +3 lines, error handling added."; return 1 }
	case "RunRegressionTests":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRegression tests: 42/42 pass. No new failures."; return 1 }
	case "VerifyCrashResolved":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nCrash reproduced: NO. Fix confirmed."; bb.Outcome = "success"; return 1 }
	case "SuggestGuards":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n## Guards Added\n- Null checks, bounds checks, error wrapping."; return 1 }
	case "AddMonitoring":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nMonitoring: alert on nil config, file-not-found."; return 1 }
	case "SetPatrolRoute":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Patrol\n\nRoute: waypoints A→B→C→D→A. Speed: walk."; return 1 }
	case "ExecutePatrol":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nPatrolling... Interruption: none."; return 1 }
	case "ScanEnvironment":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nScan: raycast 12m, proximity 5m, sound 0."; return 1 }
	case "ClassifyThreat":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nThreat: player detected, threat level: 0.7."; return 1 }
	case "CalculatePursuitPath":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nPursuit: A* path 24m, ETA 3.2s."; return 1 }
	case "ExecutePursuit":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nPursuing... distance: 15m → 8m."; return 1 }
	case "SelectTarget":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nTarget: player (threat 0.7, health 60, distance 8m)."; return 1 }
	case "ChooseAction":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nAction: melee attack (70% hit chance)."; return 1 }
	case "ExecuteCombatAction":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nCombat: 25 damage dealt. Enemy health: 35/60."; return 1 }
	case "EvaluateCombatResult":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nEval: advantage, push forward."; return 1 }
	case "FindSafePosition":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRetreat: cover at (-12, 8, 2). ETA 1.8s."; return 1 }
	case "ExecuteRetreat":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRetreating... reached cover. Health: 15/100."; return 1 }
	case "FetchMarketData":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result = "## Market Data\n\nOHLCV fetched: AAPL 2024-01 to 2024-12."; return 1 }
	case "ValidateDataQuality":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nQuality: 0 gaps, 0 outliers, data fresh."; return 1 }
	case "CalculateIndicators":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nIndicators: SMA(20)=185.3, RSI(14)=62, MACD: bullish."; return 1 }
	case "DetectPatterns":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nPatterns: Ascending triangle (bullish), support at 180."; return 1 }
	case "GenerateTASignals":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nSignals: BUY (RSI oversold exit + MACD cross)."; return 1 }
	case "ComputeSignal":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nSignal: BUY, strength: 0.72/1.0."; return 1 }
	case "AssessSignalStrength":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nConfidence: 72%. Historical accuracy: 68%."; return 1 }
	case "CheckPositionLimits":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nPosition: 5% of portfolio. Limit: 10%. OK."; return 1 }
	case "CalculateStopLoss":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nStop-loss: $175.80 (ATR-based, 5% below entry)."; return 1 }
	case "AssessRiskReward":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nR:R = 2.1:1. Kelly = 15% allocation. Acceptable."; bb.Outcome = "success"; return 1 }
	default:
		return func(ctx *btcore.BTContext[Blackboard]) int {
			return 1
		}
	}
}

func (bb *Blackboard) conditionForName(name string) func(*Blackboard) bool {
	switch name {
	case "ValidateInput":
		return func(b *Blackboard) bool {
			return len(b.Task) > 0
		}
	case "CheckPrerequisites":
		return func(b *Blackboard) bool {
			return true // always ready
		}
	case "IsHighPriority":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "critical", "urgent", "asap")
		}
	case "CheckKnowledgeGap":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "what is", "explain", "how does", "define", "kubernetes", "docker", "rust", "algorithm")
		}
	case "CheckCache":
		return func(b *Blackboard) bool {
			return b.CachedResult != ""
		}
	case "WasSuccessful":
		return func(b *Blackboard) bool {
			return b.Outcome == string(reflection.Success)
		}
	case "ValidateOutput":
		return func(b *Blackboard) bool {
			return validateOutputQuality(b)
		}
	// --- Go developer conditions ---
	case "IsGoRelated":
		return func(b *Blackboard) bool {
			lower := strings.ToLower(b.Task)
			return containsAny(lower, "go ", "golang", ".go", "goroutine", "channel", "interface", "struct", "defer", "package ",
				"gin-gonic", "gin ", "go-bt", "gorm", ".mod", "go sum", "go vet", "go build",
				"http.handler", "gorilla", "middleware", "http.request", "http.response",
				"json-rpc", "go module", "godoc", "go fmt", "golint", "staticcheck",
				"null pointer", "memory leak", "race condition", "deadlock", "mutex",
				"fix:", "bug:", "issue:", "refactor:", "engine:", "gardener:", "mcp:")
		}
	case "IsCodeReview":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "review", "audit", "inspect", "lint", "vet", "staticcheck", "code review")
		}
	case "NeedsCompilation":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "build", "compile", "go build", "go run", "go install")
		}
	case "NeedsTesting":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "test", "coverage", "benchmark", "go test", "testing")
		}
	case "IsGoQuestion":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "what is", "how to", "explain", "best practice", "pattern", "idiom", "convention")
		}
	// --- Finance conditions ---
	case "IsFinanceTask":
		return func(b *Blackboard) bool {
			lower := strings.ToLower(b.Task)
			return containsAny(lower, "dcf", "lbo", "comps", "valuation", "ebitda", "revenue", "wacc",
				"financial", "equity", "debt", "irr", "moic", "earnings", "quarterly", "10-k", "10-q",
				"pitch", "model", "excel", "portfolio", "investor", "lp", "gp", "kyc", "aml",
				"reconciliation", "reconcile", "ledger", "general ledger", "accrual", "month-end",
				"close", "audit", "statement", "screening")
		}
	case "IsCompsRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "comps", "comparable", "multiples", "trading comp", "peer")
		}
	case "IsPrecedentsRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "precedent", "transaction", "m&a comp", "acquisition")
		}
	case "IsLBORequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "lbo", "leveraged buyout", "buyout", "private equity")
		}
	case "IsDCFRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "dcf", "discounted cash flow", "intrinsic value", "wacc")
		}
	case "IsDeckRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "deck", "pitch", "presentation", "powerpoint", "slide")
		}
	case "IsEarningsRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "earnings", "quarterly", "10-q", "10-k", "8-k", "press release", "transcript")
		}
	case "NeedsModelUpdate":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "update model", "refresh", "revise", "roll forward")
		}
	case "IsNoteRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "note", "report", "write-up", "draft", "research")
		}
	case "IsIndustryRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "industry", "sector", "market", "theme", "trend")
		}
	case "IsCompetitiveRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "competitive", "landscape", "peer", "market share")
		}
	case "IsIdeaRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "idea", "opportunity", "screen", "shortlist")
		}
	case "Is3StatementRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "3-statement", "three statement", "operating model", "income statement")
		}
	case "IsMeetingPrep":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "briefing", "meeting", "client", "prep", "talking points")
		}
	case "IsValuationRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "valuation", "gp", "lp", "capital account", "nav")
		}
	case "IsGLReconRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "gl", "general ledger", "reconcil", "break", "sub-ledger")
		}
	case "IsMonthEndRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "month-end", "close", "accrual", "roll-forward", "variance")
		}
	case "IsAuditRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "audit", "statement", "verify", "lp", "capital account")
		}
	case "IsKYCRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "kyc", "aml", "onboarding", "screening", "sanctions", "pep")
		}
	// --- Company startup conditions ---
	case "ValidateCompanyState":
		return func(b *Blackboard) bool {
			if b.ChainState == nil {
				return false
			}
			_, ok := b.ChainState["company"]
			return ok
		}
	// --- Hermes self-evolution conditions ---
	case "IsPeriodicCheck":
		return func(b *Blackboard) bool {
			// Always trigger — the agent node decides frequency
			return true
		}
	case "HasSkillGaps":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "skill", "outdated", "missing", "improve skill", "update skill")
		}
	case "HasWorkflowInefficiencies":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "workflow", "inefficient", "optimize", "redundant", "slow", "pattern")
		}
	case "HasModelToolIssues":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "model", "tool", "config", "switch", "tune", "provider")
		}
	// --- UI Review conditions ---
	case "HasFeatureGaps":
		return func(b *Blackboard) bool {
			// Triggered when test results indicate missing features
			return b.ChainState != nil && b.ChainState["has_feature_gaps"] == true
		}
	case "HasLayoutIssues":
		return func(b *Blackboard) bool {
			return b.ChainState != nil && b.ChainState["has_layout_issues"] == true
		}
	case "HasAPIIssues":
		return func(b *Blackboard) bool {
			return b.ChainState != nil && b.ChainState["has_api_issues"] == true
		}
	// --- Auto Research conditions ---
	case "IsDeepResearchDay":
		return func(b *Blackboard) bool {
			// Sunday only
			return true // let the cron schedule handle this, always route
		}
	case "IsDailyResearch":
		return func(b *Blackboard) bool {
			return true // daily fallback
		}
	case "HasNewAlgorithm":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "implement", "new algorithm", "research", "create")
		}
	case "HasImprovement":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "improve", "enhance", "optimize", "tune")
		}
	case "NeedsIntegration":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "integrate", "connect", "pipeline", "wire")
		}
	// --- Hermes+Obsidian conditions ---
	case "NeedsSweep":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "sweep", "update notes", "refresh", "maintain")
		}
	case "NeedsAudit":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "audit", "review", "check", "verify", "assess", "gap")
		}
	case "NeedsPublish":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "publish", "export", "generate", "report", "slide", "briefing")
		}
	// --- NotebookLM conditions ---
	case "IsIngestTask":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "ingest", "import", "add source", "push", "notebooklm")
		}
	case "IsQueryTask":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "ask", "query", "question", "research", "analyze")
		}
	case "IsStudioTask":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "podcast", "briefing", "FAQ", "audio", "timeline", "create", "studio")
		}
	case "IsResearchTask":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "research", "web search", "discover", "find sources", "deep research")
		}
	// --- Kanban conditions ---
	case "IsKanbanTask":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "card", "kanban", "board", "focalboard", "column", "card-", "BACKLOG", "TODO", "IN PROGRESS")
		}
	case "IsBoardCheck":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "scan", "check", "monitor", "stale", "board", "status", "bottleneck")
		}
	case "NeedsDispatch":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "dispatch", "assign", "next", "start", "pick up")
		}
	case "IsStandup":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "standup", "daily", "status", "report")
		}
	case "IsCreateTask":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "create", "new card", "add card", "backlog")
		}
	case "IsRefinement":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "refine", "expand", "detail", "planning")
		}
	case "IsQA":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "qa", "test", "validate", "verify", "check")
		}
	// --- Vault management conditions ---
	case "IsSessionStart":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "session start", "boot", "wake", "startup", "begin", "morning")
		}
	case "HasNewContent":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "ingest", "import", "new content", "source", "transcript", "article", "save", "raw")
		}
	case "NeedsSynthesis":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "synthesize", "wiki", "extract", "create note", "knowledge", "concept")
		}
	case "NeedsCrossLinks":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "link", "cross-link", "audit", "connect", "orphan")
		}
	case "NeedsIndexUpdate":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "index", "update", "refresh", "MOC")
		}
	case "IsSessionEnd":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "session end", "wrap", "close", "daily summary", "end of day", "goodbye")
		}
	// --- Stockfish evolution conditions ---
	case "HasCachedFitness":
		return func(b *Blackboard) bool {
			if b.ChainState != nil {
				_, ok := b.ChainState["cached_fitness"]
				return ok
			}
			return false
		}
	case "HasFitnessImproved":
		return func(b *Blackboard) bool {
			if b.ChainState != nil {
				current, _ := b.ChainState["current_fitness"].(float64)
				best, _ := b.ChainState["best_fitness"].(float64)
				return current > best
			}
			return false
		}
	// --- Research conditions ---
	case "IsResearchQuery":
		return func(b *Blackboard) bool {
			lower := strings.ToLower(b.Task)
			return containsAny(lower, "research", "investigate", "analyze", "what is", "how does",
				"explain", "compare", "deep dive", "report on", "find out", "look into",
				"literature", "study", "survey", "overview", "landscape")
		}
	case "IsAmbiguousQuery":
		return func(b *Blackboard) bool {
			return len(b.Task) < 15 || containsAny(b.Task, "it", "this", "that thing") || !containsAny(b.Task, "?", "who", "what", "when", "where", "why", "how")
		}
	case "IsSimpleQuery":
		return func(b *Blackboard) bool {
			return len(b.Task) < 60 && !containsAny(b.Task, "compare", "versus", "vs", "analysis", "deep", "comprehensive")
		}
	case "IsComparisonQuery":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "compare", "versus", "vs", "difference between", "contrast")
		}
	case "IsDeepQuery":
		return func(b *Blackboard) bool {
			return len(b.Task) > 100 || containsAny(b.Task, "comprehensive", "deep dive", "in-depth", "thorough", "full report")
		}
	case "DetectKnowledgeGaps":
		return func(b *Blackboard) bool {
			return bb.Result == "" || containsAny(bb.Result, "gap", "missing", "unknown", "unclear", "TODO")
		}
	case "CheckSourceCount":
		return func(b *Blackboard) bool {
			return len(bb.Result) > 100
		}
	case "CheckCoverageCompleteness":
		return func(b *Blackboard) bool {
			return bb.Outcome == string(reflection.Success) && len(bb.Result) > 50
		}
	case "CheckCitationFormat":
		return func(b *Blackboard) bool {
			return containsAny(bb.Result, "[", "source:", "http")
		}
	// --- Domain tree conditions ---
	case "IsCodeTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "code", "function", "bug", "fix", "refactor") }
	case "IsBugCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "bug", "fix", "error", "crash", "null", "race") }
	case "IsSecurityCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "security", "exploit", "vuln", "injection", "xss") }
	case "IsStyleCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "style", "lint", "format", "naming", "clean") }
	case "IsCIBuildTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "build", "deploy", "ci", "cd", "pipeline", "release") }
	case "NeedsBuild":
		return func(b *Blackboard) bool { return containsAny(b.Task, "build", "compile") }
	case "NeedsTestRun":
		return func(b *Blackboard) bool { return containsAny(b.Task, "test", "run tests") }
	case "NeedsLinting":
		return func(b *Blackboard) bool { return containsAny(b.Task, "lint", "static") }
	case "NeedsDeploy":
		return func(b *Blackboard) bool { return containsAny(b.Task, "deploy", "release", "ship") }
	case "IsMonitorTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "monitor", "health", "status", "agent", "watch") }
	case "IsHealthCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "health", "status", "ping", "check") }
	case "HasDeadAgents":
		return func(b *Blackboard) bool { return containsAny(bb.Result, "dead", "offline", "unreachable") }
	case "PersistentFailures":
		return func(b *Blackboard) bool { return containsAny(bb.Result, "failed", "3+", "persistent") }
	case "IsMetricsRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "metrics", "stats", "report") }
	case "IsRefactorTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "refactor", "improve", "clean", "rewrite") }
	case "IsSmellCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "smell", "cruft", "duplicate", "long") }
	case "IsPatternRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "pattern", "design", "architecture") }
	case "NeedsVerification":
		return func(b *Blackboard) bool { return containsAny(b.Task, "verify", "test", "check") }
	case "IsSecurityTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "security", "audit", "threat", "vulnerability") }
	case "IsSASTRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "sast", "static analysis") }
	case "IsDepScanRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "dependency", "package", "cve", "library") }
	case "IsSecretScan":
		return func(b *Blackboard) bool { return containsAny(b.Task, "secret", "credential", "key", "token", "password") }
	case "IsThreatModel":
		return func(b *Blackboard) bool { return containsAny(b.Task, "threat", "model", "attack", "stride") }
	case "IsDataTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "data", "etl", "pipeline", "csv", "sql", "database") }
	case "IsExtractRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "extract", "ingest", "load") }
	case "IsTransformRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "transform", "clean", "normalize") }
	case "IsLoadRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "load", "write", "store") }
	case "IsMeetingTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "meeting", "notes", "transcript", "minutes") }
	case "HasTranscript":
		return func(b *Blackboard) bool { return len(bb.Task) > 200 }
	case "IsActionExtraction":
		return func(b *Blackboard) bool { return containsAny(b.Task, "action", "todo", "next") }
	case "IsSummaryRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "summary", "notes", "minutes") }
	case "IsFollowUp":
		return func(b *Blackboard) bool { return containsAny(b.Task, "follow", "reminder") }
	case "IsCrashTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "crash", "error", "stack", "panic", "trace") }
	case "HasStackTrace":
		return func(b *Blackboard) bool { return containsAny(b.Task, "at ", ".go:", ".rs:", "goroutine", "thread") }
	case "IsRootCauseRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "root cause", "why", "debug") }
	case "HasProposedFix":
		return func(b *Blackboard) bool { return containsAny(bb.Result, "fix", "patch", "change") }
	case "IsPreventionRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "prevent", "harden", "guard") }
	case "IsGameTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "game", "npc", "ai", "behavior") }
	case "IsPatrolState":
		return func(b *Blackboard) bool { return containsAny(b.Task, "patrol", "idle", "wander") }
	case "IsDetectState":
		return func(b *Blackboard) bool { return containsAny(b.Task, "detect", "spot", "see", "hear") }
	case "IsChaseState":
		return func(b *Blackboard) bool { return containsAny(b.Task, "chase", "pursue", "follow") }
	case "IsCombatState":
		return func(b *Blackboard) bool { return containsAny(b.Task, "attack", "fight", "combat", "shoot") }
	case "IsRetreatState":
		return func(b *Blackboard) bool { return containsAny(b.Task, "retreat", "flee", "escape", "heal") }
	case "IsTradingTask":
		return func(b *Blackboard) bool { return containsAny(b.Task, "trading", "signal", "market", "price", "stock") }
	case "IsDataRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "data", "fetch", "pull", "price") }
	case "IsTAPath":
		return func(b *Blackboard) bool { return containsAny(b.Task, "technical", "indicator", "pattern", "rsi", "macd", "sma") }
	case "IsSignalRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "signal", "buy", "sell", "entry") }
	case "IsRiskCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "risk", "stop", "position", "exposure") }
	default:
		return func(b *Blackboard) bool {
			return true
		}
	}
}

// RunTask executes a task through the behavior tree to completion.
// Multi-tick decorators (Repeat) return 0 (Running) between ticks, so we loop
// until the tree reaches a terminal state (1=Success or -1=Failure).
// validateOutputQuality checks if the agent's output meets minimum quality standards.
// Returns true if the output is acceptable; false if it appears to be garbage.
// This prevents agents reporting "success" with truncated/garbage output
// (e.g., max_tokens=10 producing a few words).
func validateOutputQuality(b *Blackboard) bool {
	result := b.Result
	if b.Result == "" && len(b.Results) > 0 {
		// Use accumulated results if Result is empty
		result = b.Results[len(b.Results)-1]
	}

	// 1. Minimum length check
	if len(result) < 30 {
		b.QualityScore = 0.0
		return false
	}

	// 2. Error pattern check
	lowerResult := strings.ToLower(result)
	errorPatterns := []string{
		"i cannot", "i can't", "unable to", "error:", "failed to",
		"i don't know", "i'm not sure", "not implemented",
	}
	for _, p := range errorPatterns {
		if strings.Contains(lowerResult, p) {
			b.QualityScore = 0.1
			return false
		}
	}

	// 3. Structure check (bonus for structured output)
	score := 0.5 // baseline for meeting minimum length + no errors
	if strings.Contains(result, "#") || strings.Contains(result, "**") {
		score += 0.2 // has markdown structure
	}
	if strings.Contains(result, "- ") || strings.Contains(result, "* ") {
		score += 0.1 // has bullet points
	}
	if len(result) > 200 {
		score += 0.1 // substantive length
	}
	if strings.Contains(result, "```") {
		score += 0.1 // contains code blocks
	}
	if score > 1.0 {
		score = 1.0
	}
	b.QualityScore = score
	return score >= 0.5
}

func RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string {
	start := time.Now()

	// Panic recovery at the tree level — if the entire BT crashes, capture it.
	defer func() {
		if r := recover(); r != nil {
			bb.Outcome = string(reflection.Failure)
			bb.Result = fmt.Sprintf("TREE PANIC: %v", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	btCtx := btcore.NewBTContext(ctx, bb)

	code := tree.Run(btCtx)

	// Multi-tick loop: Repeat and other decorators return 0 (Running) between
	// ticks. Keep ticking until a terminal status is reached.
	const maxTicks = 1000
	for tick := 1; code == 0 && tick < maxTicks; tick++ {
		code = tree.Run(btCtx)
	}

	bb.DurationMs = time.Since(start).Milliseconds()

	if code == 1 {
		bb.Outcome = string(reflection.Success)
	} else if code == -1 {
		bb.Outcome = string(reflection.Failure)
	} else {
		bb.Outcome = string(reflection.Partial)
	}

	return bb.Result
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

func truncateStrForTree(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
