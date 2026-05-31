// Package finance converts all 10 Anthropic financial agent workflows into behavior
// trees, encoding domain-specific decision logic from the financial-services repository.
//
// Trees (10 total, 20-39 nodes each):
//
//	pitch_agent — comps, precedents, LBO, DCF → branded pitch deck
//	earnings_reviewer — earnings call analysis → model update → note draft
//	market_researcher — industry overview, competitive landscape, investment ideas
//	model_builder — 3-statement, DCF, LBO models live in Excel
//	meeting_prep — client briefing pack generation
//	valuation_reviewer — GP package review → valuation → LP reporting
//	gl_reconciler — break identification → root cause → sign-off
//	month_end_closer — accruals, roll-forwards, variance analysis
//	statement_auditor — LP statement audit with verification checks
//	kyc_screener — document parsing, sanctions screening, risk scoring
//
// The package provides 48 finance-specific engine nodes (19 conditions, 29 actions)
// covering DCF modeling, LBO verification, KYC rules, GL reconciliation, and more.
package finance

import "github.com/nico/go-bt-evolve/internal/evolution"

// AnthropicFinanceTrees maps each of the 10 Anthropic financial agents to a behavior tree.
// These trees encode the workflow decision logic described in the financial-services repo.

// PitchAgentTree — comps, precedents, LBO → branded pitch deck
func PitchAgentTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "PitchAgent_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Check if task involves financial analysis"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					// Path 1: Comparable company analysis
					{
						Type: "Sequence", Name: "CompsPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsCompsRequest", Description: "Detect comparable company / multiples keywords"},
							{Type: "Action", Name: "FetchCompsData", Description: "Pull comps from financial data providers"},
							{Type: "Action", Name: "BuildCompsTable", Description: "Build valuation multiples table in Excel"},
							{Type: "Action", Name: "ValidateComps", Description: "Check ranges, outliers, sector alignment"},
						},
					},
					// Path 2: Precedent transaction analysis
					{
						Type: "Sequence", Name: "PrecedentsPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsPrecedentsRequest", Description: "Detect precedent / transaction keywords"},
							{Type: "Action", Name: "FetchPrecedentsData", Description: "Pull comparable transactions"},
							{Type: "Action", Name: "BuildPrecedentsTable", Description: "Build transaction comps with premiums"},
							{Type: "Action", Name: "AnalyzePremiums", Description: "Calculate control premiums and synergies"},
						},
					},
					// Path 3: LBO model
					{
						Type: "Sequence", Name: "LBOPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsLBORequest", Description: "Detect LBO / leveraged buyout keywords"},
							{Type: "Action", Name: "BuildLBOModel", Description: "Fill LBO template with deal assumptions"},
							{Type: "Action", Name: "VerifyLBOModel", Description: "Check Sources=Uses, IRR/MOIC returns, debt paydown"},
							{Type: "Action", Name: "FormatLBOOutput", Description: "Apply blue/grey palette, formula color conventions"},
						},
					},
					// Path 4: DCF valuation
					{
						Type: "Sequence", Name: "DCFPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsDCFRequest", Description: "Detect DCF / discounted cash flow keywords"},
							{Type: "Action", Name: "BuildDCFModel", Description: "Build 3-scenario DCF with WACC calculation"},
							{Type: "Action", Name: "BuildSensitivityTables", Description: "Populate 3 sensitivity tables (5×5 odd dimensions)"},
							{Type: "Action", Name: "VerifyDCFModel", Description: "Recalculate, verify no #REF!/#DIV/0!"},
						},
					},
					// Path 5: Deck assembly
					{
						Type: "Sequence", Name: "DeckAssemblyPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsDeckRequest", Description: "Detect pitch deck / presentation keywords"},
							{Type: "Action", Name: "AssemblePitchDeck", Description: "Combine analysis into branded PowerPoint deck"},
							{Type: "Action", Name: "QCDeck", Description: "Quality check: consistent fonts, correct charts, source footnotes"},
						},
					},
					// Fallback: generic execution
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM: analyze task"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM: generate and execute plan"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on analysis quality"},
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful", Description: "Exit if success"},
					{
						Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3,
						Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix and retry"}},
					},
					{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate complex financial model"},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Adapt on failures"},
		},
	}
}

// EarningsReviewerTree — earnings call + filings → model update → note draft
func EarningsReviewerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "EarningsReviewer_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Financial task check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "EarningsIngestPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsEarningsRequest", Description: "Detect earnings / quarterly / 10-Q / 8-K keywords"},
							{Type: "Action", Name: "IngestEarningsData", Description: "Pull earnings call transcript, press release, filings"},
							{Type: "Action", Name: "ExtractKeyMetrics", Description: "Extract revenue, EPS, guidance, segment data"},
							{Type: "Action", Name: "CompareVsConsensus", Description: "Compare actuals vs. analyst consensus estimates"},
						},
					},
					{
						Type: "Sequence", Name: "ModelUpdatePath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "NeedsModelUpdate", Description: "Detect model update / refresh / revise keywords"},
							{Type: "Action", Name: "UpdateFinancialModel", Description: "Update DCF/comps model with new data"},
							{Type: "Action", Name: "RollForwardProjections", Description: "Roll forward projection period"},
							{Type: "Action", Name: "VerifyModelIntegrity", Description: "Check model still balances after updates"},
						},
					},
					{
						Type: "Sequence", Name: "NoteDraftingPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsNoteRequest", Description: "Detect note / report / write-up keywords"},
							{Type: "Action", Name: "DraftEarningsNote", Description: "Draft research note: key takeaways, estimate changes, rating"},
							{Type: "Action", Name: "QCResearchNote", Description: "Check for consistency, regulatory disclaimers, formatting"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM: analyze"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM: execute"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful", Description: "Exit if success"},
					{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
					{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// MarketResearcherTree — sector/theme → industry overview, competitive landscape, ideas
func MarketResearcherTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "MarketResearcher_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "IndustryOverviewPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsIndustryRequest", Description: "Detect sector / industry / theme keywords"},
							{Type: "Action", Name: "ResearchIndustry", Description: "Gather market size, growth, trends, key players"},
							{Type: "Action", Name: "BuildIndustryOverview", Description: "Create structured industry overview document"},
						},
					},
					{
						Type: "Sequence", Name: "CompetitiveLandscapePath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsCompetitiveRequest", Description: "Detect competitive / landscape / peer keywords"},
							{Type: "Action", Name: "MapCompetitors", Description: "Map competitive landscape, market share, positioning"},
							{Type: "Action", Name: "BuildPeerComparison", Description: "Build peer comparison table with key metrics"},
						},
					},
					{
						Type: "Sequence", Name: "IdeaGenerationPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsIdeaRequest", Description: "Detect idea / opportunity / theme keywords"},
							{Type: "Action", Name: "ScreenForIdeas", Description: "Screen for investment ideas based on theme"},
							{Type: "Action", Name: "RankAndPrioritize", Description: "Rank ideas by conviction, upside, catalysts"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// ModelBuilderTree — DCF, LBO, 3-statement, comps live in Excel
func ModelBuilderTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "ModelBuilder_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "ThreeStatementPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "Is3StatementRequest", Description: "Detect 3-statement / operating model keywords"},
							{Type: "Action", Name: "Build3StatementModel", Description: "Link income statement, balance sheet, cash flow"},
							{Type: "Action", Name: "VerifyModelBalance", Description: "Check A=L+E, cash flow ties to balance sheet"},
						},
					},
					{
						Type: "Sequence", Name: "DCFModelPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsDCFRequest", Description: "Detect DCF keywords"},
							{Type: "Action", Name: "BuildDCFModel", Description: "Build DCF with WACC + sensitivity"},
							{Type: "Action", Name: "VerifyDCFModel", Description: "Recalculate, verify formulas"},
						},
					},
					{
						Type: "Sequence", Name: "LBOModelPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsLBORequest", Description: "Detect LBO keywords"},
							{Type: "Action", Name: "BuildLBOModel", Description: "Build LBO with returns analysis"},
							{Type: "Action", Name: "VerifyLBOModel", Description: "Check Sources=Uses, IRR/MOIC"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// MeetingPrepTree — briefing pack before client meetings
func MeetingPrepTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "MeetingPrep_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "BriefingPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsMeetingPrep", Description: "Detect briefing / meeting prep / client keywords"},
							{Type: "Action", Name: "GatherClientContext", Description: "Pull CRM data, recent interactions, holdings"},
							{Type: "Action", Name: "BuildBriefingPack", Description: "Assemble briefing: portfolio review, market update, talking points"},
							{Type: "Action", Name: "QC BriefingPack", Description: "Verify data accuracy, formatting, completeness"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// ValuationReviewerTree — ingests GP packages, runs valuation, stages LP reporting
func ValuationReviewerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "ValuationReviewer_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "GPIngestPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsValuationRequest", Description: "Detect valuation / GP / LP keywords"},
							{Type: "Action", Name: "IngestGPPackage", Description: "Parse GP capital account statements, cap tables"},
							{Type: "Action", Name: "RunValuationTemplate", Description: "Apply valuation methodology (market, income, NAV)"},
							{Type: "Action", Name: "StageLPReporting", Description: "Prepare LP capital account statements, performance reports"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// GLReconcilerTree — finds breaks, traces root cause, routes for sign-off
func GLReconcilerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "GLReconciler_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "ReconPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsGLReconRequest", Description: "Detect GL / reconciliation / break keywords"},
							{Type: "Action", Name: "CompareGLEntries", Description: "Compare GL to sub-ledger, bank statements"},
							{Type: "Action", Name: "IdentifyBreaks", Description: "Flag discrepancies, categorize by type"},
							{Type: "Action", Name: "TraceRootCause", Description: "Trace break to source transaction or posting error"},
							{Type: "Action", Name: "RouteForSignOff", Description: "Prepare reconciliation package for reviewer approval"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// MonthEndCloserTree — accruals, roll-forwards, variance commentary
func MonthEndCloserTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "MonthEndCloser_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "ClosePath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsMonthEndRequest", Description: "Detect month-end / close / accrual keywords"},
							{Type: "Action", Name: "CalculateAccruals", Description: "Calculate expense accruals, prepaids, deferrals"},
							{Type: "Action", Name: "RunRollForward", Description: "Roll forward balance sheet accounts"},
							{Type: "Action", Name: "AnalyzeVariance", Description: "Compare actuals vs budget, write variance commentary"},
							{Type: "Action", Name: "PrepareClosePackage", Description: "Assemble close package for controller review"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// StatementAuditorTree — audits LP statements before distribution
func StatementAuditorTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "StatementAuditor_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "AuditPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsAuditRequest", Description: "Detect audit / statement / LP keywords"},
							{Type: "Action", Name: "IngestLPStatements", Description: "Load LP capital account statements"},
							{Type: "Action", Name: "ValidateCalculations", Description: "Verify NAV, allocations, waterfall calculations"},
							{Type: "Action", Name: "CheckDisclosures", Description: "Verify regulatory disclosures and footnotes"},
							{Type: "Action", Name: "GenerateAuditReport", Description: "Produce audit findings report"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// KYCScreenerTree — parses onboarding docs, runs rules engine, flags gaps
func KYCScreenerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "KYCScreener_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty"},
					{Type: "Condition", Name: "IsFinanceTask", Description: "Finance check"},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "KYCPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsKYCRequest", Description: "Detect KYC / AML / onboarding keywords"},
							{Type: "Action", Name: "ParseOnboardingDocs", Description: "Extract entity info, beneficial owners, documents"},
							{Type: "Action", Name: "RunKYCRulesEngine", Description: "Screen against sanctions lists, PEP databases, adverse media"},
							{Type: "Action", Name: "FlagGaps", Description: "Flag missing documents, red flags, escalation items"},
							{Type: "Action", Name: "GenerateKYCReport", Description: "Produce KYC screening report with risk rating"},
						},
					},
					{
						Type: "Sequence", Name: "ExecutionPath",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "AnalyzeTask", Description: "LLM"},
							{Type: "Action", Name: "ExecutePlan", Description: "LLM"},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful", Description: "Exit"},
				{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix"}}},
				{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate"},
			}},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve"},
		},
	}
}

// AllFinanceTrees returns all 10 finance trees keyed by name.
func AllFinanceTrees() map[string]*evolution.SerializableNode {
	return map[string]*evolution.SerializableNode{
		"pitch_agent":        PitchAgentTree(),
		"earnings_reviewer":  EarningsReviewerTree(),
		"market_researcher":  MarketResearcherTree(),
		"model_builder":      ModelBuilderTree(),
		"meeting_prep":       MeetingPrepTree(),
		"valuation_reviewer": ValuationReviewerTree(),
		"gl_reconciler":      GLReconcilerTree(),
		"month_end_closer":   MonthEndCloserTree(),
		"statement_auditor":  StatementAuditorTree(),
		"kyc_screener":       KYCScreenerTree(),
	}
}

// AgentDescriptions maps agent names to their Anthropic descriptions.
var AgentDescriptions = map[string]string{
	"pitch_agent":        "Comps, precedents, LBO → branded pitch deck",
	"earnings_reviewer":  "Earnings call + filings → model update → note draft",
	"market_researcher":  "Sector/theme → industry overview, competitive landscape, ideas shortlist",
	"model_builder":      "DCF, LBO, 3-statement, comps – live in Excel",
	"meeting_prep":       "Briefing pack before client meetings",
	"valuation_reviewer": "Ingests GP packages, runs valuation template, stages LP reporting",
	"gl_reconciler":      "Finds breaks, traces root cause, routes for sign-off",
	"month_end_closer":   "Accruals, roll-forwards, variance commentary",
	"statement_auditor":  "Audits LP statements before distribution",
	"kyc_screener":       "Parses onboarding docs, runs rules engine, flags gaps",
}
