package engine

import (
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestActionForName_BulkCoverage exercises all 200+ switch cases in actionForName.
// This ensures every action resolves to a non-nil function that executes without panic.
// Many of these are fallback stubs used by domain trees (finance, research, etc.)
// that do NOT register via RegisterAction().
func TestActionForName_BulkCoverage(t *testing.T) {
	// Collect ALL action names that exist in the switch statement.
	// These are the set-difference between tree.go's actionForName switch
	// and the registered actions in registry.go + arc42_nodes.go.
	actionNames := []string{
		// --- Tool setup actions ---
		"SetupDevTools",
		"SetupUniversalTools",
		"SetupResearchTools",
		"SetupStartupTools",
		"InitTranspositionTable",
		"LoadCachedFitness",
		"StoreInTranspositionTable",
		"hasCachedFitness",

		// --- Go developer actions ---
		"ReviewGoCode",
		"SuggestImprovements",
		"CompileGoCode",
		"FixBuildErrors",
		"RunGoTests",
		"AnalyzeTestResults",
		"ExplainGoConcept",

		// --- Finance: pitch_agent (comps, precedents, LBO, DCF, deck) ---
		"FetchCompsData",
		"BuildCompsTable",
		"ValidateComps",
		"FetchPrecedentsData",
		"BuildPrecedentsTable",
		"AnalyzePremiums",
		"BuildLBOModel",
		"VerifyLBOModel",
		"FormatLBOOutput",
		"BuildDCFModel",
		"BuildSensitivityTables",
		"VerifyDCFModel",
		"AssemblePitchDeck",
		"QCDeck",

		// --- Finance: earnings_reviewer ---
		"IngestEarningsData",
		"ExtractKeyMetrics",
		"CompareVsConsensus",
		"UpdateFinancialModel",
		"RollForwardProjections",
		"VerifyModelIntegrity",
		"DraftEarningsNote",
		"QCResearchNote",

		// --- Finance: market_researcher ---
		"ResearchIndustry",
		"BuildIndustryOverview",
		"MapCompetitors",
		"BuildPeerComparison",
		"ScreenForIdeas",
		"RankAndPrioritize",

		// --- Finance: model_builder ---
		"Build3StatementModel",
		"VerifyModelBalance",

		// --- Finance: meeting_prep ---
		"GatherClientContext",
		"BuildBriefingPack",
		"QCBriefingPack",

		// --- Finance: valuation_reviewer ---
		"IngestGPPackage",
		"RunValuationTemplate",
		"StageLPReporting",

		// --- Finance: gl_reconciler ---
		"CompareGLEntries",
		"IdentifyBreaks",
		"TraceRootCause",
		"RouteForSignOff",

		// --- Finance: month_end_closer ---
		"CalculateAccruals",
		"RunRollForward",
		"AnalyzeVariance",
		"PrepareClosePackage",

		// --- Finance: statement_auditor ---
		"IngestLPStatements",
		"ValidateCalculations",
		"CheckDisclosures",
		"GenerateAuditReport",

		// --- Finance: kyc_screener ---
		"ParseOnboardingDocs",
		"RunKYCRulesEngine",
		"FlagGaps",
		"GenerateKYCReport",

		// --- Research actions ---
		"AskClarifyingQuestions",
		"RefineQueryWithAnswers",
		"ProceedDirectly",
		"DecomposeQuery",
		"AssessComplexity",
		"ExecuteSingleSearch",
		"SpawnResearchThreads",
		"SpawnDeepThreads",
		"SearchBroadFirst",
		"FilterAndRankSources",
		"ExtractKeyFindings",
		"CrossReferenceFacts",
		"TargetedDeepDive",
		"PivotOnDeadEnds",
		"CoverageComplete",
		"IterateSearch",
		"StructureReport",
		"DraftSections",
		"GenerateVisualizations",
		"AddCitations",
		"AddReasoningChain",
		"FlagRemainingGaps",

		// --- Domain: code review ---
		"ScanForBugs",
		"SuggestBugFixes",
		"ScanForVulns",
		"SuggestSecurityFixes",
		"CheckCodeStyle",
		"SuggestStyleFixes",

		// --- Domain: devops/ci ---
		"RunBuild",
		"CheckBuildErrors",
		"FixBuildIssues",
		"RunTests",
		"RunLinter",
		"AnalyzeLintOutput",
		"RunDeploy",
		"VerifyDeploy",
		"RollbackOnFailure",

		// --- Domain: agent monitor ---
		"CheckAllAgents",
		"IdentifyDeadAgents",
		"RestartDeadAgents",
		"VerifyRestart",
		"SendAlert",
		"EscalateToOperator",
		"CollectAgentMetrics",
		"GenerateHealthReport",

		// --- Domain: refactoring ---
		"DetectCodeSmells",
		"SuggestRefactorings",
		"RecommendPatterns",
		"GeneratePatternCode",
		"VerifyBehavior",
		"ReportRefactoringImpact",

		// --- Domain: security ---
		"RunSASTScan",
		"GenerateSASTReport",
		"ScanDependencies",
		"SuggestDependencyFixes",
		"ScanForSecrets",
		"ReportExposedSecrets",
		"BuildThreatModel",
		"GenerateMitigations",

		// --- Domain: data pipeline ---
		"ValidateDataSource",
		"ExtractData",
		"ValidateTransform",
		"ApplyTransform",
		"VerifyOutput",
		"ValidateTarget",
		"LoadData",
		"VerifyLoad",

		// --- Domain: meeting notes ---
		"ParseTranscript",
		"IdentifyTopics",
		"ExtractActionItems",
		"AssignOwners",
		"GenerateSummary",
		"FormatMeetingNotes",
		"DistributeNotes",
		"CheckActionStatus",
		"SendReminders",

		// --- Domain: incident response ---
		"ParseStackFrames",
		"IdentifyCrashSite",
		"TraceExecutionPath",
		"IdentifyRootCause",
		"GenerateFix",
		"ApplyFix",
		"RunRegressionTests",
		"VerifyCrashResolved",
		"SuggestGuards",
		"AddMonitoring",

		// --- Domain: game AI ---
		"SetPatrolRoute",
		"ExecutePatrol",
		"ScanEnvironment",
		"ClassifyThreat",
		"CalculatePursuitPath",
		"ExecutePursuit",
		"SelectTarget",
		"ChooseAction",
		"ExecuteCombatAction",
		"EvaluateCombatResult",
		"FindSafePosition",
		"ExecuteRetreat",

		// --- Domain: trading ---
		"FetchMarketData",
		"ValidateDataQuality",
		"CalculateIndicators",
		"DetectPatterns",
		"GenerateTASignals",
		"ComputeSignal",
		"AssessSignalStrength",
		"CheckPositionLimits",
		"CalculateStopLoss",
		"AssessRiskReward",

		// --- Arc42 documentation actions (not registered — fallback in tree.go) ---
		"FallbackSection1",

		// --- Knowledge graph actions ---
		"ApplyKnowledge",
		"QueryKG",
		"UseCachedResult",

		// --- Arc42 doc assembly actions ---
		"CollectAllSections",
		"GenerateTOC",
		"MarkDocAssembled",
		"MarkSectionDone",
		"ReadSection1",
		"SaveDocument",
		"SaveSection",
		"ScanCodeComments",
		"ScanTypes",
		"ValidateSection",

		// --- Arc42 doc reading actions ---
		"ReadADRs",
		"ReadConfigFiles",
		"ReadEngineCode",
		"ReadErrorLogs",
		"ReadGitHistory",
		"ReadGoMod",
		"ReadGraphReport",
		"ReadTestCoverage",

		// --- Arc42 system discovery actions ---
		"DetectHardware",
		"DetectProcesses",
		"ListBinaries",
		"ListExternalAPIs",
		"ListMCPTools",
		"ListPackages",

		// --- Tool setup (unregistered) ---
		"SetupDefaultTools",
		"SetupDocTools",
	}

	bb := &Blackboard{
		Task:   "review Go code for performance issues",
		LLM:    &MockLLM{},
		Result: "initial result for concatenation tests",
		ChainState: map[string]any{
			"section_file":       "09-test-scenarios.md",
			"section_key":        "section9_done",
			"doc_title":          "Go BT Evolve Arc42",
			"arc42_section_file": "09-test-scenarios.md",
			"arc42_section_data": "# Test Section Content\n\nThis is a test section with enough content to pass validation.\n\n## Overview\n\nMore content here to ensure we exceed the minimum length threshold.\n",
		},
	}

	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	for _, name := range actionNames {
		t.Run(name, func(t *testing.T) {
			fn := bb.actionForName(name)
			if fn == nil {
				t.Fatalf("actionForName(%q) returned nil", name)
			}
			// Execute without panic — catch any panics
			result := func() int {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("action %q panicked: %v", name, r)
					}
				}()
				return fn(ctx)
			}()
			if result != 1 && result != -1 {
				t.Errorf("action %q returned unexpected status %d (expected 1 or -1)", name, result)
			}
		})
	}
}
