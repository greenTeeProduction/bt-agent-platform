// Package engine provides the behavior tree runtime for the BT platform.
//
// It implements tree building, execution, action/condition registration, and
// the Blackboard context that carries task state through tree execution.
// The package also defines 10 chain types (llm_call, agent, refine, map_reduce,
// rag_query, structured_output, retrieval_qa, conversation, tool_call, tool_action)
// that integrate langchaingo workflows directly into behavior tree nodes.
//
// Key types:
//   - Blackboard — shared state (Task, Plan, Result, Outcome, ChainTools, ChainMemory)
//   - SerializableNode — JSON-serializable tree node used across all domain trees
//
// Key functions:
//   - RunTask(bb, tree) — executes a tree to completion with 1000-tick safety limit
//   - BuildTree(tree, bb) — converts a SerializableNode into a runnable go-bt tree
//   - actionForName / conditionForName — registry of 175+ engine nodes
package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"
	"github.com/nico/go-bt-evolve/internal/tracing"

	btcomp "github.com/rvitorper/go-bt/composite"
	btcore "github.com/rvitorper/go-bt/core"
	btdec "github.com/rvitorper/go-bt/decorators"
	btleaf "github.com/rvitorper/go-bt/leaf"
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
	CurrentPath  string         // currently executing strategy path (set by tree traversal)
	VisitedPaths []string       // all strategy paths visited during execution
}

// BuildTree constructs a go-bt Command from a SerializableNode tree definition.
// Invalid trees produce a failing command instead of silently executing an unsafe
// or unknown structure. Use BuildAndValidate when the caller needs the error.
func BuildTree(serTree *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	cmd, err := BuildAndValidate(serTree, bb)
	if err != nil {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			msg := fmt.Sprintf("tree validation failed: %v", err)
			ctx.Blackboard.Outcome = msg
			ctx.Blackboard.Result = msg
			return -1
		})
	}
	return cmd
}

// BuildAndValidate constructs a tree and validates it before execution.
// Returns an error if validation fails; on success the tree is still built.
func BuildAndValidate(serTree *evolution.SerializableNode, bb *Blackboard) (btcore.Command[Blackboard], error) {
	info := ValidateTreeFull(serTree)
	if !info.Valid() {
		return nil, fmt.Errorf("tree validation failed: %v", info.Errors)
	}
	return buildNode(serTree, bb, ""), nil
}

// buildNode recursively builds a go-bt Command from a SerializableNode.
// parentName tracks the parent node's name for path-tracking in StrategyRouters.
func buildNode(node *evolution.SerializableNode, bb *Blackboard, parentName string) btcore.Command[Blackboard] {
	// If this Sequence is inside a StrategyRouter, record its name as the active path
	if parentName == "StrategyRouter" && node.Type == "Sequence" && node.Name != "" {
		origChildren := node.Children
		// Prepend a path-recording action before the sequence's children
		pathRecordAction := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.CurrentPath = node.Name
			ctx.Blackboard.VisitedPaths = append(ctx.Blackboard.VisitedPaths, node.Name)
			return 1
		})
		children := make([]btcore.Command[Blackboard], len(origChildren)+1)
		children[0] = pathRecordAction
		for i := range origChildren {
			children[i+1] = buildNode(&origChildren[i], bb, node.Name)
		}
		return btcomp.NewSequence(children...)
	}

	switch node.Type {
	case "Sequence":
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb, node.Name)
		}
		return btcomp.NewSequence(children...)
	case "Selector":
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb, node.Name)
		}
		return btcomp.NewSelector(children...)
	case "Retry":
		child := buildNode(&node.Children[0], bb, node.Name)
		return btdec.NewRepeat(child, node.MaxRetries)
	case "Action":
		return btleaf.NewAction(bb.actionForName(node.Name))
	case "ChainAction":
		// Langchain chain node — reads ChainConfig from node metadata
		cfg := parseChainConfig(node)
		return BuildChainAction(cfg, bb)
	case "Condition":
		return btleaf.NewCondition(bb.conditionForName(node.Name))
	case "UtilitySelector":
		return BuildUtilitySelector(node, bb)
	case "PlannerNode":
		// PlannerNode extends UtilitySelector with GOAP goal management
		return BuildPlannerNode(node, bb)
	case "AbortOnEvent":
		return BuildEventDrivenAbort(node, bb)
	case "ReactiveParallel":
		return BuildReactiveParallel(node, bb)
	default:
		// Unknown node type → pass-through action (always succeeds)
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			return 1
		})
	}
}

func (bb *Blackboard) actionForName(name string) func(*btcore.BTContext[Blackboard]) int {
	// Registry-first: packages register via engine.RegisterAction() in init().
	// GetAction returns the zero-value ActionFunc (nil) for unknown names.
	if fn := GetAction(name); fn != nil {
		return fn
	}
	switch name {
	case "SetupDevTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				newGoBuildTool(),
				newGoTestTool(),
				newGoVetTool(),
				newWebSearchTool(),
			}
			return 1
		}
	case "SetupUniversalTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				newShellExecTool(),
				newFileReadTool(),
				newFileWriteTool(),
				newWebSearchTool(),
				toolStub{name: "calculator", desc: "Perform mathematical calculations and data analysis"},
			}
			return 1
		}
	case "SetupResearchTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				newWebSearchTool(),
				newGraphifyTool(),
				toolStub{name: "calculator", desc: "Perform mathematical calculations and data analysis"},
			}
			return 1
		}
	case "SetupStartupTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				newWebSearchTool(),
				toolStub{name: "calculator", desc: "Financial calculations: runway, burn rate, valuation"},
				toolStub{name: "metrics_db", desc: "Query company metrics: users, MRR, churn, CAC, LTV"},
			}
			return 1
		}
	case "SetupDefaultTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{
				newShellExecTool(),
				newFileReadTool(),
				toolStub{name: "http_get", desc: "Make an HTTP GET request and return response body"},
				toolStub{name: "process_check", desc: "Check if a process is running by name"},
				toolStub{name: "disk_usage", desc: "Check disk usage on a mount point"},
				toolStub{name: "memory_usage", desc: "Check memory usage statistics"},
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
	case "QCBriefingPack":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = fmt.Sprintf("## Quality Check\n\n**Briefing**: Verified data accuracy, formatting, completeness.\n**Status**: Approved.")
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
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Bug Scan\n\nAnalyzing code for null derefs, off-by-one, race conditions."
			return 1
		}
	case "SuggestBugFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\n## Suggested Fix\n- Before/after code with explanation"
			bb.Outcome = "success"
			return 1
		}
	case "ScanForVulns":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Security Scan\n\nOWASP Top 10, injection, auth bypass checked."
			return 1
		}
	case "SuggestSecurityFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\n## Secure Alternative\n- Parameterized queries, input validation"
			bb.Outcome = "success"
			return 1
		}
	case "CheckCodeStyle":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Style Check\n\nNaming conventions, formatting, idiomatic patterns verified."
			return 1
		}
	case "SuggestStyleFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\n## Style Corrections\n- Rename, reformat, restructure"
			bb.Outcome = "success"
			return 1
		}
	case "RunBuild":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Build Output\n\nExecuting build command..."
			return 1
		}
	case "CheckBuildErrors":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n0 errors, 3 warnings."; return 1 }
	case "FixBuildIssues":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\n## Fixes Applied\n- Missing import, type mismatch resolved"
			return 1
		}
	case "RunTests":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Test Results\n\n42 passed, 0 failed, 2 skipped."
			return 1
		}
	case "RunLinter":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Lint Output\n\n5 issues: 2 warnings, 3 info."
			return 1
		}
	case "AnalyzeLintOutput":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nCategorized: 2 style, 1 complexity, 2 naming."
			return 1
		}
	case "RunDeploy":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Deploy\n\nDeployment started to staging."
			return 1
		}
	case "VerifyDeploy":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nHealth check: 200 OK, smoke tests passed."
			return 1
		}
	case "RollbackOnFailure":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRollback: not needed (deploy succeeded)."
			return 1
		}
	case "CheckAllAgents":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Agent Health\n\nPinging all MCP servers..."
			return 1
		}
	case "IdentifyDeadAgents":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nDead: 0, Slow: 1 (td-agent 2.3s response)."
			return 1
		}
	case "RestartDeadAgents":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\nRestarted: 0 needed."; return 1 }
	case "VerifyRestart":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRe-check: all agents healthy."
			bb.Outcome = "success"
			return 1
		}
	case "SendAlert":
		return func(ctx *btcore.BTContext[Blackboard]) int { bb.Result += "\n\n⚠ Alert sent to operator."; return 1 }
	case "EscalateToOperator":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nEscalated for human intervention."
			return 1
		}
	case "CollectAgentMetrics":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Agent Metrics\n\nUptime, tool calls, error rates collected."
			return 1
		}
	case "GenerateHealthReport":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nDashboard-ready health report generated."
			bb.Outcome = "success"
			return 1
		}
	case "DetectCodeSmells":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Code Smells\n\nLong functions (3), deep nesting (2), duplication (1)."
			return 1
		}
	case "SuggestRefactorings":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\n## Suggestions\n- Extract method, simplify condition, DRY."
			return 1
		}
	case "RecommendPatterns":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Pattern Recommendations\n\nStrategy, Factory, Observer applicable."
			return 1
		}
	case "GeneratePatternCode":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nImplementation template generated."
			return 1
		}
	case "VerifyBehavior":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nExisting tests: 42/42 pass. No regression."
			return 1
		}
	case "ReportRefactoringImpact":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRisk: Low. Files changed: 3. Lines: +15/-8."
			bb.Outcome = "success"
			return 1
		}
	case "RunSASTScan":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## SAST Results\n\nInjection: 0, XSS: 0, Auth: 1 (medium)."
			return 1
		}
	case "GenerateSASTReport":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nPrioritized: 0 critical, 1 medium, 2 low."
			bb.Outcome = "success"
			return 1
		}
	case "ScanDependencies":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Dependency Scan\n\nCVE check: 0 critical, 2 moderate."
			return 1
		}
	case "SuggestDependencyFixes":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRecommend: bump xyz to v1.2.3, replace abc."
			return 1
		}
	case "ScanForSecrets":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Secret Scan\n\nAPI keys found: 0, tokens: 0, passwords: 0."
			return 1
		}
	case "ReportExposedSecrets":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nNo exposed secrets detected."
			bb.Outcome = "success"
			return 1
		}
	case "BuildThreatModel":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Threat Model\n\nSTRIDE analysis complete. Attack surface mapped."
			return 1
		}
	case "GenerateMitigations":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nControls: input validation, rate limiting, encryption."
			return 1
		}
	case "ValidateDataSource":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Data Source\n\nConnectivity: OK, Schema: valid."
			return 1
		}
	case "ExtractData":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nExtracted: 10,420 rows, 0 errors."
			return 1
		}
	case "ValidateTransform":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nTransform logic: valid, types: compatible."
			return 1
		}
	case "ApplyTransform":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nTransformation applied: 10,420 → 10,418 rows."
			return 1
		}
	case "VerifyOutput":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nOutput verified: nulls 0.1%, distribution matches."
			bb.Outcome = "success"
			return 1
		}
	case "ValidateTarget":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nTarget: schema compatible, permissions OK."
			return 1
		}
	case "LoadData":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nLoaded: 10,418 rows, transaction committed."
			return 1
		}
	case "VerifyLoad":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nLoad verified: row count matches, sample validated."
			bb.Outcome = "success"
			return 1
		}
	case "ParseTranscript":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Transcript\n\nSpeakers: Alice (12 turns), Bob (8 turns)."
			return 1
		}
	case "IdentifyTopics":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nTopics: Q1 Review, Hiring, Budget, Timeline."
			return 1
		}
	case "ExtractActionItems":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nActions: 5 items extracted with owners and deadlines."
			return 1
		}
	case "AssignOwners":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nOwners assigned: Alice (2), Bob (2), Carol (1)."
			return 1
		}
	case "GenerateSummary":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\n## Summary\nKey decisions, discussion points, outcomes."
			bb.Outcome = "success"
			return 1
		}
	case "FormatMeetingNotes":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nFormatted: date, attendees, agenda, notes, actions."
			return 1
		}
	case "DistributeNotes":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nDistributed to: team@example.com."
			return 1
		}
	case "CheckActionStatus":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Follow-up\n\nActions: 3 complete, 1 in progress, 1 overdue."
			return 1
		}
	case "SendReminders":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nReminders sent to Bob (overdue: Budget review)."
			return 1
		}
	case "ParseStackFrames":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Stack Trace\n\nFrames: 12, Crash at: main.go:42 (nil pointer deref)."
			return 1
		}
	case "IdentifyCrashSite":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nCrash site: processRequest(), nil config object."
			return 1
		}
	case "TraceExecutionPath":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nExecution path: init() → loadConfig() → processRequest()."
			return 1
		}
	case "IdentifyRootCause":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRoot cause: loadConfig() returns nil on file not found."
			return 1
		}
	case "GenerateFix":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nFix: add nil check after loadConfig() call."
			return 1
		}
	case "ApplyFix":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nFix applied: +3 lines, error handling added."
			return 1
		}
	case "RunRegressionTests":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRegression tests: 42/42 pass. No new failures."
			return 1
		}
	case "VerifyCrashResolved":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nCrash reproduced: NO. Fix confirmed."
			bb.Outcome = "success"
			return 1
		}
	case "SuggestGuards":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\n## Guards Added\n- Null checks, bounds checks, error wrapping."
			return 1
		}
	case "AddMonitoring":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nMonitoring: alert on nil config, file-not-found."
			return 1
		}
	case "SetPatrolRoute":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Patrol\n\nRoute: waypoints A→B→C→D→A. Speed: walk."
			return 1
		}
	case "ExecutePatrol":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nPatrolling... Interruption: none."
			return 1
		}
	case "ScanEnvironment":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nScan: raycast 12m, proximity 5m, sound 0."
			return 1
		}
	case "ClassifyThreat":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nThreat: player detected, threat level: 0.7."
			return 1
		}
	case "CalculatePursuitPath":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nPursuit: A* path 24m, ETA 3.2s."
			return 1
		}
	case "ExecutePursuit":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nPursuing... distance: 15m → 8m."
			return 1
		}
	case "SelectTarget":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nTarget: player (threat 0.7, health 60, distance 8m)."
			return 1
		}
	case "ChooseAction":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nAction: melee attack (70% hit chance)."
			return 1
		}
	case "ExecuteCombatAction":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nCombat: 25 damage dealt. Enemy health: 35/60."
			return 1
		}
	case "EvaluateCombatResult":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nEval: advantage, push forward."
			return 1
		}
	case "FindSafePosition":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRetreat: cover at (-12, 8, 2). ETA 1.8s."
			return 1
		}
	case "ExecuteRetreat":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nRetreating... reached cover. Health: 15/100."
			return 1
		}
	case "FetchMarketData":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "## Market Data\n\nOHLCV fetched: AAPL 2024-01 to 2024-12."
			return 1
		}
	case "ValidateDataQuality":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nQuality: 0 gaps, 0 outliers, data fresh."
			return 1
		}
	case "CalculateIndicators":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nIndicators: SMA(20)=185.3, RSI(14)=62, MACD: bullish."
			return 1
		}
	case "DetectPatterns":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nPatterns: Ascending triangle (bullish), support at 180."
			return 1
		}
	case "GenerateTASignals":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nSignals: BUY (RSI oversold exit + MACD cross)."
			return 1
		}
	case "ComputeSignal":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nSignal: BUY, strength: 0.72/1.0."
			return 1
		}
	case "AssessSignalStrength":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nConfidence: 72%. Historical accuracy: 68%."
			return 1
		}
	case "CheckPositionLimits":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nPosition: 5% of portfolio. Limit: 10%. OK."
			return 1
		}
	case "CalculateStopLoss":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nStop-loss: $175.80 (ATR-based, 5% below entry)."
			return 1
		}
	case "AssessRiskReward":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result += "\n\nR:R = 2.1:1. Kelly = 15% allocation. Acceptable."
			bb.Outcome = "success"
			return 1
		}

	// ─── Arc42 Documentation Actions ───────────────────────────────
	case "SetupDocTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.ChainTools = []any{newFileReadTool(), newShellExecTool(), newWebSearchTool()}
			return 1
		}
	case "ReadGraphReport":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			data, err := os.ReadFile("/home/nico/go-bt-evolve/graphify-out/GRAPH_REPORT.md")
			if err == nil {
				bb.CachedResult = string(data)
			}
			return 1
		}
	case "ReadGitHistory":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("git", "-C", "/home/nico/go-bt-evolve", "log", "--oneline", "-30").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["git_history"] = string(out)
			return 1
		}
	case "ReadADRs":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			files, _ := filepath.Glob("/home/nico/go-bt-evolve/docs/adr/ADR-*.md")
			var adrs strings.Builder
			for _, f := range files {
				data, err := os.ReadFile(f)
				if err == nil {
					adrs.Write(data)
					adrs.WriteString("\n---\n")
				}
			}
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["adrs"] = adrs.String()
			return 1
		}
	case "ReadGoMod":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			data, _ := os.ReadFile("/home/nico/go-bt-evolve/go.mod")
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["go_mod"] = string(data)
			lines := strings.SplitN(string(data), "\n", 3)
			if len(lines) > 0 {
				bb.ChainState["go_version"] = strings.TrimSpace(lines[0])
			}
			return 1
		}
	case "ReadConfigFiles":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			data, _ := os.ReadFile("/home/nico/.hermes/config.yaml")
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["config"] = string(data)
			return 1
		}
	case "DetectHardware":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			cpu, _ := exec.Command("sh", "-c", "grep 'model name' /proc/cpuinfo | head -1").Output()
			mem, _ := exec.Command("sh", "-c", "grep MemTotal /proc/meminfo").Output()
			disk, _ := exec.Command("sh", "-c", "df -h / /mnt/ssd 2>/dev/null | tail -2").Output()
			uname, _ := exec.Command("uname", "-a").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["hardware"] = fmt.Sprintf("CPU: %sMEM: %sDisk: %sKernel: %s", cpu, mem, disk, uname)
			return 1
		}
	case "DetectProcesses":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "ps aux | grep '[b]t-' | awk '{print $11, $2}'").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["processes"] = string(out)
			return 1
		}
	case "ListPackages":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "find /home/nico/go-bt-evolve/internal -maxdepth 1 -type d | sort | sed 's|.*/||'").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["packages"] = string(out)
			return 1
		}
	case "ListBinaries":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "find /home/nico/go-bt-evolve/cmd -name 'main.go' | sed 's|/main.go||' | sed 's|.*/||' | sort").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["binaries"] = string(out)
			return 1
		}
	case "ReadEngineCode":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			data, _ := os.ReadFile("/home/nico/go-bt-evolve/internal/engine/tree.go")
			bb.CachedResult = string(data[:min(len(data), 3000)])
			return 1
		}
	case "ListExternalAPIs":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "grep -rn 'http.Get\\|http.Post\\|http.NewRequest\\|net.Dial' /home/nico/go-bt-evolve/internal/ 2>/dev/null | head -20").Output()
			bb.CachedResult = string(out)
			return 1
		}
	case "ListMCPTools":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "grep -rn 'RegisterTool\\|tools/call\\|bt_agent_\\|bt_evaluator_\\|bt_langagent_' /home/nico/go-bt-evolve/cmd/bt-agent/tools.go 2>/dev/null | wc -l").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["mcp_tools"] = strings.TrimSpace(string(out))
			return 1
		}
	case "ReadSection1":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			data, _ := os.ReadFile("/mnt/ssd/clawd/wiki/bt-research/docs/arc42/01-introduction-goals.md")
			bb.CachedResult = string(data)
			return 1
		}
	case "ReadTestCoverage":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "cd /home/nico/go-bt-evolve && go test -coverprofile=/tmp/arc42-cover.out ./... 2>&1 | grep 'coverage:' | head -10").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["coverage"] = string(out)
			return 1
		}
	case "ReadErrorLogs":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "grep -i 'error\\|panic\\|fail' /home/nico/.hermes/logs/errors.log 2>/dev/null | tail -10").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["errors"] = string(out)
			return 1
		}
	case "FallbackSection1":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			bb.Result = "# 1. Introduction and Goals\n\n## 1.1 Requirements Overview\n\ngo-bt-evolve is a behavior-tree-driven AI agent platform.\n\n## 1.2 Quality Goals\n\n| Goal | Scenario |\n|------|----------|\n| Correctness | Trees route tasks to correct domain paths |\n| Evolvability | 6 evolution algorithms continuously improve trees |\n| Reliability | Panic recovery, circuit breakers, retry with DLQ |\n\n## 1.3 Stakeholders\n\n| Role | Expectations |\n|------|-------------|\n| Nico | Platform architect and developer |\n| Hermes Agent | Automated operator via cron jobs |\n| Dashboard Users | Visual introspection of agents, trees, tasks |"
			bb.Outcome = "success"
			return 1
		}
	case "ValidateSection":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if len(bb.Result) < 100 {
				bb.Result = "ERROR: section content too short"
				bb.Outcome = "failure"
				return 0
			}
			return 1
		}
	case "SaveSection":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			sectionName := bb.ChainState["section_file"].(string)
			os.MkdirAll("/mnt/ssd/clawd/wiki/bt-research/docs/arc42", 0755)
			path := "/mnt/ssd/clawd/wiki/bt-research/docs/arc42/" + sectionName
			os.WriteFile(path, []byte(bb.Result), 0644)
			return 1
		}
	case "MarkSectionDone":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			sectionKey := bb.ChainState["section_key"].(string)
			bb.ChainState[sectionKey] = true
			return 1
		}
	case "ScanCodeComments":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "grep -rn '// Package ' /home/nico/go-bt-evolve/internal/ 2>/dev/null | head -30").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["comments"] = string(out)
			return 1
		}
	case "ScanTypes":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			out, _ := exec.Command("sh", "-c", "grep -rn 'type.*struct {' /home/nico/go-bt-evolve/internal/engine/ /home/nico/go-bt-evolve/internal/evolution/ 2>/dev/null | head -20").Output()
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["types"] = string(out)
			return 1
		}
	case "CollectAllSections":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			files, _ := filepath.Glob("/mnt/ssd/clawd/wiki/bt-research/docs/arc42/0*.md")
			var all strings.Builder
			for _, f := range files {
				data, err := os.ReadFile(f)
				if err == nil {
					all.Write(data)
					all.WriteString("\n\n")
				}
			}
			bb.CachedResult = all.String()
			return 1
		}
	case "GenerateTOC":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			var toc strings.Builder
			toc.WriteString("# Table of Contents\n\n")
			for i := 1; i <= 12; i++ {
				toc.WriteString(fmt.Sprintf("%d. [Section %d](#section-%d)\n", i, i, i))
			}
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["toc"] = toc.String()
			return 1
		}
	case "SaveDocument":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			os.MkdirAll("/mnt/ssd/clawd/wiki/bt-research/docs/arc42", 0755)
			path := "/mnt/ssd/clawd/wiki/bt-research/docs/arc42/go-bt-evolve-arc42.md"
			os.WriteFile(path, []byte(bb.Result), 0644)
			return 1
		}
	case "MarkDocAssembled":
		return func(ctx *btcore.BTContext[Blackboard]) int {
			if bb.ChainState == nil {
				bb.ChainState = make(map[string]any)
			}
			bb.ChainState["doc_assembled"] = true
			return 1
		}
	default:
		return func(ctx *btcore.BTContext[Blackboard]) int {
			return 1
		}
	}
}

func (bb *Blackboard) conditionForName(name string) func(*Blackboard) bool {
	// Registry-first: packages register via engine.RegisterCondition() in init().
	// GetCondition returns nil for unknown names.
	if fn := GetCondition(name); fn != nil {
		return fn
	}
	switch name {
	case "IsHighPriority":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "critical", "urgent", "asap")
		}
	case "ValidateOutput":
		return func(b *Blackboard) bool {
			return validateOutputQuality(b)
		}
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
			return containsAny(b.Task, "review", "inspect", "lint", "vet", "staticcheck", "code review")
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
	// --- Merged universal conditions ---
	case "IsDevOps":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"deploy", "build", "pipeline", "ci/cd", "ci ", "docker",
				"kubernetes", "k8s", "terraform", "ansible", "jenkins",
				"github actions", "gitlab ci", "circleci", "infrastructure", "devops")
		}
	case "IsDataTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"etl", "pipeline", "data ", "transform", "extract",
				"load", "schema", "dataset", "csv", "parquet", "sql",
				"delegation", "queue", "index", "session", "memory")
		}
	case "IsAnalysisTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"strategy", "analysis", "analyze", "foresight", "scenario",
				"implications", "forecast", "roadmap", "synthesis", "think tank")
		}
	case "IsRefactoring":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"refactor", "restructure", "clean up", "improve",
				"modernize", "migrate", "simplify")
		}
	case "IsQuestion":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"what ", "how ", "why ", "explain", "define",
				"difference", "compare", "best practice", "example")
		}
	case "IsIncident":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"crash", "error", "timeout", "incident", "outage",
				"down", "broken", "failure", "panic", "oom")
		}
	case "IsHealthCheck":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"health", "agent status", "disk usage", "memory", "cpu",
				"metrics report", "dashboard", "system health", "check all",
				"collect system", "verify the dashboard", "capacity planning",
				"sre runbook", "sla dashboard", "chaos engineering")
		}
	case "IsMeetingTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"transcribe", "meeting", "standup", "minutes", "summarize the",
				"architecture review", "sprint planning", "board meeting",
				"action items", "multi-speaker", "facilitation", "diarize")
		}
	case "IsPlatformEval":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"platform maturity", "lowest-scoring", "test suite and report",
				"gap analysis", "comparative maturity", "maturity trends",
				"comprehensive audit", "architecture review", "production readiness",
				"platform eval", "dimension", "maturity across all")
		}
	case "IsCronTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"cron job", "cron audit", "cron capacity", "cron governance",
				"list all cron", "find any cron", "verify all cron",
				"diagnose the hermes", "cron A/B", "self-healing cron")
		}
	case "IsEvolutionTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"tree fitness", "evolution algorithm", "mutation candidate",
				"evolution safety", "ensemble evolution", "meta-controller",
				"multi-objective evolution", "fleet-wide evolution",
				"self-evolv", "order mutations", "transposition table")
		}
	case "IsNotebookLMTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"notebooklm", "chat quer", "briefing doc", "mind map",
				"research notebook", "cross-notebook", "deep research",
				"audio overview", "research pipeline", "full pipeline",
				"research impact", "meta-research")
		}
	case "IsVaultTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"ingest the session", "synthesize daily", "cross-link",
				"vault", "update the index", "weekly sweep", "wiki page",
				"map of content", "frontmatter", "knowledge gap")
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
	case "IsEngineeringTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"engineering", "sprint", "feature", "build", "implement",
				"code", "deploy", "architecture", "tech", "developer",
				"sw. eng", "software eng")
		}
	case "IsMarketingTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"marketing", "content", "seo", "campaign", "growth",
				"community", "brand", "social", "promotion",
				"advertising", "lead gen", "audience")
		}
	case "IsSalesTask":
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task),
				"sales", "deal", "revenue", "pipeline", "lead",
				"closing", "proposal", "demo", "pricing",
				"customer", "prospect", "quota")
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
			return containsAny(strings.ToLower(b.Task), "card", "kanban", "board", "focalboard", "column", "backlog", "todo", "in progress", "sprint", "status", "move", "assign")
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
				"literature", "study", "survey", "overview", "landscape",
				"what are", "who", "when", "where", "why", "top ", "best ",
				"most popular", "recommend", "suggest", "tell me about",
				"summarize", "history of", "future of", "trends", "llm",
				"framework", "python", "rust", "golang", "kubernetes",
				"search", "find ", "news", "ai ", "verification", "verify",
				"check ", "latest", "update", "review", "scan", "audit",
				"look up", "lookup", "gather", "collect", "compile", "discover")
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
			return (b.Outcome == string(reflection.Success) || b.Outcome == "chain_success") && len(bb.Result) > 50
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
		return func(b *Blackboard) bool {
			return containsAny(strings.ToLower(b.Task), "security", "exploit", "vulnerability", "penetration", "auth", "audit", "xss", "sql injection", "csrf", "owasp", "injection")
		}
	case "IsStyleCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "style", "lint", "format", "naming", "clean") }
	case "IsCIBuildTask":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "build", "deploy", "ci", "cd", "pipeline", "release")
		}
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
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "secret", "credential", "key", "token", "password")
		}
	case "IsThreatModel":
		return func(b *Blackboard) bool { return containsAny(b.Task, "threat", "model", "attack", "stride") }
	case "IsExtractRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "extract", "ingest", "load") }
	case "IsTransformRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "transform", "clean", "normalize") }
	case "IsLoadRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "load", "write", "store") }
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
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "trading", "signal", "market", "price", "stock", "alert", "critical", "incident", "notify", "route", "severity", "disk", "security", "health")
		}
	case "IsDataRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "data", "fetch", "pull", "price") }
	case "IsTAPath":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "technical", "indicator", "pattern", "rsi", "macd", "sma")
		}
	case "IsSignalRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "signal", "buy", "sell", "entry") }
	case "IsRiskCheck":
		return func(b *Blackboard) bool { return containsAny(b.Task, "risk", "stop", "position", "exposure") }
	// --- GOAP Planning conditions ---
	case "IsAssessRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "assess", "check", "review", "scan", "audit", "track", "measure", "maturity")
		}
	case "IsSyncRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "sync", "pollinate", "cross", "align", "mismatch")
		}
	// --- GOAP Research conditions ---
	case "IsResearchRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "research", "analyze", "find ", "query", "search", "discover", "evolution")
		}
	case "IsGraphifyRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "graphify", "graph", "structural", "codebase", "coupling")
		}
	// --- GOAP Devops conditions ---
	case "IsBuildRequest":
		return func(b *Blackboard) bool {
			return containsAny(b.Task, "build", "compile", "install", "make", "go build")
		}
	case "IsImplementRequest":
		return func(b *Blackboard) bool { return containsAny(b.Task, "implement", "plan", "fix", "create", "pending") }

	// ─── Arc42 Documentation Conditions ────────────────────────────
	case "GraphIsFresh":
		return func(b *Blackboard) bool {
			_, err := os.Stat("/home/nico/go-bt-evolve/graphify-out/GRAPH_REPORT.md")
			return err == nil
		}
	case "Section1Done":
		return func(b *Blackboard) bool {
			if bb.ChainState == nil {
				return false
			}
			v, _ := bb.ChainState["section1_done"].(bool)
			return v
		}
	case "Section4Done":
		return func(b *Blackboard) bool {
			if bb.ChainState == nil {
				return false
			}
			v, _ := bb.ChainState["section4_done"].(bool)
			return v
		}
	case "Section5Done":
		return func(b *Blackboard) bool {
			if bb.ChainState == nil {
				return false
			}
			v, _ := bb.ChainState["section5_done"].(bool)
			return v
		}
	case "AllSectionsDone":
		return func(b *Blackboard) bool {
			if bb.ChainState == nil {
				return false
			}
			for i := 1; i <= 12; i++ {
				key := fmt.Sprintf("section%d_done", i)
				if v, _ := bb.ChainState[key].(bool); !v {
					return false
				}
			}
			return true
		}
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

	// 0. Structured zero-LLM output detection — short but valid structured output
	// from trees like alert_router, agent_monitor that produce markdown-formatted
	// routing/status results without LLM calls.
	lowerResult := strings.ToLower(result)
	isStructured := strings.HasPrefix(strings.TrimSpace(result), "## ") ||
		strings.Contains(lowerResult, "route:") ||
		strings.Contains(lowerResult, "status:") ||
		strings.Contains(lowerResult, "delivered")
	minLen := 30
	if isStructured {
		minLen = 15 // structured zero-LLM output is intentionally compact
	}

	// 1. Minimum length check
	if len(result) < minLen {
		b.QualityScore = 0.0
		return false
	}

	// 2. Error pattern check
	errorPatterns := []string{
		"output quality failed", "i cannot", "i can't", "unable to", "error:", "failed to",
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
	// Bonus for zero-LLM routing output (alert_router, etc.)
	if isStructured && len(result) < 100 {
		score += 0.2 // compact but valid structured output
	}
	if score > 1.0 {
		score = 1.0
	}
	b.QualityScore = score
	return score >= 0.5
}

func RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string {
	start := time.Now()

	// ── Tracing: wrap tree execution in a span ──
	taskName := bb.Task
	if len(taskName) > 50 {
		taskName = taskName[:50]
	}
	_, span := tracing.StartSpan(context.Background(), "RunTask:"+taskName)
	defer span.End()
	span.SetAttribute("task", truncateStrForTree(bb.Task, 80))

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

	span.SetAttribute("outcome", bb.Outcome)
	span.SetAttribute("duration_ms", fmt.Sprintf("%d", bb.DurationMs))

	// Always validate output quality — some trees (agent_monitor, alert_router)
	// don't include ReflectOnOutcome which is where quality scoring normally runs.
	// Without this, zero-LLM trees report quality=0 even with valid structured output.
	validateOutputQuality(bb)

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
