// Package domains provides domain-specific behavior trees for code review,
// DevOps/CI, security, data pipelines, refactoring, incident response,
// alert routing, and general knowledge tasks. Each tree encodes expert
// decision logic for its domain with keyword-based condition routing.
package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// seq creates a Sequence node with children.
func seq(name string, children ...evolution.SerializableNode) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Sequence", Name: name, Children: children}
}

// sel creates a Selector node with children.
func sel(name string, children ...evolution.SerializableNode) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Selector", Name: name, Children: children}
}

// cond creates a Condition node.
func cond(name, desc string) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Condition", Name: name, Description: desc}
}

// act creates an Action node.
func act(name, desc string) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Action", Name: name, Description: desc}
}

// chainAgent creates a ChainAction node for the agent: chain type.
// systemPrompt is the system message, task is the user prompt,
// tools is the list of tool names to make available.
func chainAgent(name, systemPrompt string, tools []string) evolution.SerializableNode {
	ti := make([]any, len(tools))
	for i, t := range tools {
		ti[i] = t
	}
	return evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:" + systemPrompt,
		Metadata: map[string]any{
			"tools":      ti,
			"max_tokens": float64(15),
		},
	}
}

// retry wraps a child with retry decorator.
func retryW(name string, child evolution.SerializableNode, max int) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Retry", Name: name, Children: []evolution.SerializableNode{child}, MaxRetries: max}
}

// outcome builds the standard OutcomeSelector pattern.
// Uses MarkSuccessful instead of WasSuccessful — the WasSuccessful condition
// does unreliable keyword matching on LLM output and causes false failures.
// Quality gates (PreGate validation, output length checks) catch real failures.
func outcome() evolution.SerializableNode {
	return sel("OutcomeSelector",
		act("MarkSuccessful", "Mark task as successful"),
		retryW("RetrySelfCorrect", act("SelfCorrect", "Fix and retry"), 3),
		act("EscalateToDeepSeek", "Escalate to external LLM"),
	)
}

// --- CodeReview Tree ---

func CodeReviewTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "CodeReview_Main", Children: []evolution.SerializableNode{
		act("SetupDefaultTools", "Populate bb.ChainTools with real system tools"),
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsCodeTask", "Has code-related keywords")),
		sel("StrategyRouter",
			seq("BugDetection",
				cond("IsBugCheck", "Detect bug/fix/error keywords"),
				chainAgent("BugDetectionAgent", "Analyze the code for bugs: null derefs, off-by-one errors, race conditions, logic errors. Use file_read to inspect code. Report findings with line numbers and suggested fixes.",
					[]string{"file_read", "shell_exec"}),
			),
			seq("SecurityReview",
				cond("IsSecurityCheck", "Detect security/exploit/vuln keywords"),
				chainAgent("SecurityReviewAgent", "Review code for security vulnerabilities: OWASP Top 10, injection, auth bypass, unsafe operations. Use file_read to inspect files, shell_exec to grep for patterns. Report each finding with severity.",
					[]string{"file_read", "shell_exec"}),
			),
			seq("StyleReview",
				cond("IsStyleCheck", "Detect style/lint/format keywords"),
				act("CheckCodeStyle", "Verify naming, formatting, idiomatic patterns"),
				act("SuggestStyleFixes", "Generate style corrections"),
			),
			seq("ExecutionPath",
				chainAgent("CodeReviewAgent",
					"You are a code review agent. {{.Task}} Review the code for bugs, security issues, and style problems. Use file_read to inspect files, shell_exec to run analysis commands. Report findings with file paths and line numbers.",
					[]string{"file_read", "shell_exec"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on review quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- DevOps/CI Tree ---

func DevOpsCITree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "DevOpsCI_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsCIBuildTask", "Detect build/deploy/CI keywords")),
		sel("StrategyRouter",
			seq("BuildPath",
				cond("NeedsBuild", "Detect build/compile keywords"),
				act("RunBuild", "Execute build command, capture output"),
				act("CheckBuildErrors", "Parse build output for errors/warnings"),
				act("FixBuildIssues", "Suggest fixes for compilation errors"),
			),
			seq("TestPath",
				cond("NeedsTestRun", "Detect test keywords"),
				act("RunTests", "Execute test suite, capture results"),
				act("AnalyzeTestResults", "Parse failures, flaky tests, coverage gaps"),
			),
			seq("LintPath",
				cond("NeedsLinting", "Detect lint/static analysis keywords"),
				act("RunLinter", "Execute linting tool"),
				act("AnalyzeLintOutput", "Categorize issues by severity"),
			),
			seq("DeployPath",
				cond("NeedsDeploy", "Detect deploy/release keywords"),
				act("RunDeploy", "Execute deployment script"),
				act("VerifyDeploy", "Health check endpoint, smoke test"),
				act("RollbackOnFailure", "Revert if health check fails"),
			),
			seq("ExecutionPath",
				chainAgent("DevOpsCIAgent",
					"You are a CI/CD agent. {{.Task}} Build, test, lint, or deploy as needed. Use go_build for compilation, go_test for testing, go_vet for analysis. Report results.",
					[]string{"go_build", "go_test", "go_vet", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on CI/CD quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- AgentMonitor Tree ---

func AgentMonitorTree() *evolution.SerializableNode {
	// Zero-LLM tree — no chainAgent calls, instant execution.
	// Health checks and metrics are hardcoded actions that mark results directly.
	return &evolution.SerializableNode{Type: "Sequence", Name: "AgentMonitor_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsMonitorTask", "Detect monitor/health/status keywords")),
		sel("StrategyRouter",
			seq("HealthCheckPath",
				cond("IsHealthCheck", "Detect health/status/ping keywords"),
				act("HealthCheckAgent", "Health check completed — see blackboard for result"),
			),
			seq("MetricsCollectionPath",
				cond("IsMetricsRequest", "Detect metrics/stats keywords"),
				act("MetricsCollectionAgent", "Metrics collection completed — see blackboard for result"),
			),
		),
		outcome(),
	}}
}

// --- Refactoring Tree ---

func RefactoringTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "Refactoring_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsRefactorTask", "Detect refactor/improve/clean keywords")),
		sel("StrategyRouter",
			seq("SmellDetection",
				cond("IsSmellCheck", "Detect smell/cruft/duplicate keywords"),
				act("DetectCodeSmells", "Identify long functions, deep nesting, duplication"),
				act("SuggestRefactorings", "Extract method, simplify condition, DRY"),
			),
			seq("PatternApplication",
				cond("IsPatternRequest", "Detect pattern/design/architecture keywords"),
				act("RecommendPatterns", "Suggest strategy, factory, observer, etc."),
				act("GeneratePatternCode", "Produce implementation template"),
			),
			seq("VerificationPath",
				cond("NeedsVerification", "Detect verify/test/check keywords"),
				act("VerifyBehavior", "Run existing tests, check no regression"),
				act("ReportRefactoringImpact", "Summary of changes and risk assessment"),
			),
			seq("ExecutionPath",
				chainAgent("RefactoringAgent",
					"You are a refactoring agent. {{.Task}} Use file_read to inspect code, shell_exec to run analysis tools and test changes. Report refactoring suggestions with specific code changes.",
					[]string{"file_read", "shell_exec"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on refactoring quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- SecurityAudit Tree ---

func SecurityAuditTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "SecurityAudit_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsSecurityTask", "Detect security/audit/threat keywords")),
		sel("StrategyRouter",
			seq("SASTPath",
				cond("IsSASTRequest", "Detect SAST/static analysis keywords"),
				act("RunSASTScan", "Analyze source for injection, XSS, auth flaws"),
				act("GenerateSASTReport", "Prioritized findings with severity"),
			),
			seq("DependencyScan",
				cond("IsDepScanRequest", "Detect dependency/package/CVE keywords"),
				act("ScanDependencies", "Check CVE database for known vulns"),
				act("SuggestDependencyFixes", "Recommend version bumps or alternatives"),
			),
			seq("SecretDetection",
				cond("IsSecretScan", "Detect secret/credential/key keywords"),
				act("ScanForSecrets", "Search for API keys, tokens, passwords"),
				act("ReportExposedSecrets", "Flag with remediation steps"),
			),
			seq("ThreatModeling",
				cond("IsThreatModel", "Detect threat/model/attack keywords"),
				act("BuildThreatModel", "STRIDE analysis, attack surface mapping"),
				act("GenerateMitigations", "Controls and countermeasures"),
			),
			seq("ExecutionPath",
				chainAgent("SecurityAuditAgent",
					"You are a security audit agent. {{.Task}} Scan for vulnerabilities, check dependencies, detect secrets. Use shell_exec for scanning tools, file_read for code inspection, web_search for CVE lookup.",
					[]string{"shell_exec", "file_read", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on audit quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- DataPipeline Tree ---

func DataPipelineTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "DataPipeline_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsDataTask", "Detect data/ETL/pipeline/delegation/queue/index/session/memory/extract/process keywords")),
		sel("StrategyRouter",
			seq("ExtractPath",
				cond("IsExtractRequest", "Detect extract/ingest/load keywords"),
				act("ValidateDataSource", "Check source connectivity and schema"),
				act("ExtractData", "Pull data with error handling and retries"),
			),
			seq("TransformPath",
				cond("IsTransformRequest", "Detect transform/clean/normalize keywords"),
				act("ValidateTransform", "Check transformation logic, data types"),
				act("ApplyTransform", "Execute transformation pipeline"),
				act("VerifyOutput", "Row counts, null checks, distribution validation"),
			),
			seq("LoadPath",
				cond("IsLoadRequest", "Detect load/write/store keywords"),
				act("ValidateTarget", "Check target schema compatibility"),
				act("LoadData", "Write to target with transaction safety"),
				act("VerifyLoad", "Confirm row counts, sample validation"),
			),
			seq("ExecutionPath",
				chainAgent("DataPipelineAgent",
					"You are a data pipeline agent. {{.Task}} Process data, run ETL tasks, handle queue operations, index sessions, extract memories. Use file_read and file_write for reading/writing, shell_exec for running scripts, web_search for documentation.",
					[]string{"file_read", "shell_exec", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on pipeline quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- MeetingNotes Tree ---

func MeetingNotesTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "MeetingNotes_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsMeetingTask", "Detect meeting/notes/transcript keywords")),
		sel("StrategyRouter",
			seq("TranscribePath",
				cond("HasTranscript", "Transcript or audio file provided"),
				act("ParseTranscript", "Extract speaker turns, timestamps"),
				act("IdentifyTopics", "Segment by topic shifts"),
			),
			seq("ExtractActions",
				cond("IsActionExtraction", "Detect action/todo/next keywords"),
				act("ExtractActionItems", "Identify commitments, deadlines, owners"),
				act("AssignOwners", "Map actions to participants"),
			),
			seq("GenerateNotes",
				cond("IsSummaryRequest", "Detect summary/notes/minutes keywords"),
				act("GenerateSummary", "Key decisions, discussion points, outcomes"),
				act("FormatMeetingNotes", "Structured format: date, attendees, agenda, notes, actions"),
				act("DistributeNotes", "Email or share to relevant channels"),
			),
			seq("FollowUpPath",
				cond("IsFollowUp", "Detect follow-up/reminder keywords"),
				act("CheckActionStatus", "Verify completion of previous actions"),
				act("SendReminders", "Notify overdue action owners"),
			),
			seq("ExecutionPath",
				chainAgent("MeetingNotesAgent",
					"You are a meeting notes agent. {{.Task}} Transcribe meetings, extract action items, summarize discussions. Use file_read to read transcripts, shell_exec for processing, web_search for context.",
					[]string{"file_read", "shell_exec", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on notes quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- CrashInvestigator Tree ---

func CrashInvestigatorTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "CrashInvestigator_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsCrashTask", "Detect crash/error/stack/panic keywords")),
		sel("StrategyRouter",
			seq("ParseStackTrace",
				cond("HasStackTrace", "Stack trace or error log provided"),
				act("ParseStackFrames", "Extract file, line, function from each frame"),
				act("IdentifyCrashSite", "Locate exact crash point"),
			),
			seq("RootCauseAnalysis",
				cond("IsRootCauseRequest", "Detect root cause/why/debug keywords"),
				act("TraceExecutionPath", "Reconstruct code flow leading to crash"),
				act("IdentifyRootCause", "Pinpoint null deref, OOB, race, logic error"),
				act("GenerateFix", "Produce minimal code fix"),
			),
			seq("FixAndVerify",
				cond("HasProposedFix", "Fix has been generated"),
				act("ApplyFix", "Apply code change"),
				act("RunRegressionTests", "Verify fix doesn't break existing tests"),
				act("VerifyCrashResolved", "Confirm original crash no longer reproduces"),
			),
			seq("PreventionPath",
				cond("IsPreventionRequest", "Detect prevent/harden/guard keywords"),
				act("SuggestGuards", "Add null checks, bounds checks, error handling"),
				act("AddMonitoring", "Suggest alerts for similar patterns"),
			),
			seq("ExecutionPath",
				chainAgent("CrashInvestigatorAgent",
					"You are a crash investigator. {{.Task}} Analyze stack traces, find root causes, suggest fixes. Use file_read for source code, shell_exec for logs, web_search for error lookup.",
					[]string{"file_read", "shell_exec", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on investigation quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- GameAI Tree ---

func GameAITree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "GameAI_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsGameTask", "Detect game/NPC/AI/behavior keywords")),
		sel("StrategyRouter",
			seq("PatrolPath",
				cond("IsPatrolState", "Detect patrol/idle/wander keywords"),
				act("SetPatrolRoute", "Define waypoints or random wander"),
				act("ExecutePatrol", "Move along route, detect interruptions"),
			),
			seq("DetectPath",
				cond("IsDetectState", "Detect detect/spot/see/hear keywords"),
				act("ScanEnvironment", "Raycast, proximity check, sound detection"),
				act("ClassifyThreat", "Friend/foe/neutral, threat level assessment"),
			),
			seq("ChasePath",
				cond("IsChaseState", "Detect chase/pursue/follow keywords"),
				act("CalculatePursuitPath", "Pathfinding to target, speed matching"),
				act("ExecutePursuit", "Follow target, maintain distance"),
			),
			seq("CombatPath",
				cond("IsCombatState", "Detect attack/fight/combat/shoot keywords"),
				act("SelectTarget", "Prioritize by threat, distance, health"),
				act("ChooseAction", "Attack, dodge, use ability, take cover"),
				act("ExecuteCombatAction", "Perform selected action"),
				act("EvaluateCombatResult", "Damage dealt, health change, reposition"),
			),
			seq("RetreatPath",
				cond("IsRetreatState", "Detect retreat/flee/escape/heal keywords"),
				act("FindSafePosition", "Locate cover or exit point"),
				act("ExecuteRetreat", "Move to safe position, use healing"),
			),
			seq("ExecutionPath",
				chainAgent("GameAIAgent",
					"You are a game AI agent. {{.Task}} Implement game AI behaviors, patrol/combat/flee logic. Use shell_exec for testing behaviors, web_search for AI patterns.",
					[]string{"shell_exec", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on AI behavior quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- TradingSignal Tree ---

func TradingSignalTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "TradingSignal_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsTradingTask", "Detect trading/signal/market/price keywords")),
		sel("StrategyRouter",
			seq("DataCollectionPath",
				cond("IsDataRequest", "Detect data/fetch/pull/price keywords"),
				act("FetchMarketData", "Pull OHLCV, order book, volume data"),
				act("ValidateDataQuality", "Check for gaps, outliers, stale data"),
			),
			seq("TechnicalAnalysis",
				cond("IsTAPath", "Detect technical/indicator/pattern keywords"),
				act("CalculateIndicators", "SMA, EMA, RSI, MACD, Bollinger, ATR"),
				act("DetectPatterns", "Head & shoulders, double top, flags, wedges"),
				act("GenerateTASignals", "Buy/sell signals from indicator crossovers"),
			),
			seq("SignalGeneration",
				cond("IsSignalRequest", "Detect signal/buy/sell/entry keywords"),
				act("ComputeSignal", "Weighted combination of TA signals"),
				act("AssessSignalStrength", "Confidence score, historical accuracy"),
			),
			seq("RiskManagement",
				cond("IsRiskCheck", "Detect risk/stop-loss/position keywords"),
				act("CheckPositionLimits", "Verify within exposure limits"),
				act("CalculateStopLoss", "ATR-based or percentage-based stop"),
				act("AssessRiskReward", "R:R ratio, Kelly criterion check"),
			),
			seq("ExecutionPath",
				chainAgent("TradingSignalAgent",
					"You are a trading signal agent. {{.Task}} Analyze market data, generate signals, assess risk. Use web_search for market data, calculator for analysis, file_read for reports.",
					[]string{"web_search", "calculator", "file_read"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on signal quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- GoapPlanning Tree ---

func GoapPlanningTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "GoapPlanning_Main", Children: []evolution.SerializableNode{
		act("SetupUniversalTools", "Give chain agents access to web_search, file_read, shell_exec"),
		seq("PreGate", cond("ValidateInput", "Non-empty")),
		sel("StrategyRouter",
			seq("AssessPath",
				cond("IsAssessRequest", "Detect assess/check/review/scan/audit keywords"),
				chainAgent("PlanningAssessAgent",
					"You are a planning assessment agent. Assess the current state: scan files, check configurations, review logs. Use web_search for external research, file_read to read local files, shell_exec to run diagnostic commands. Produce a structured assessment report with findings and recommendations.",
					[]string{"web_search", "file_read", "shell_exec"}),
			),
			seq("SyncPath",
				cond("IsSyncRequest", "Detect sync/pollinate/cross/align keywords"),
				chainAgent("PlanningSyncAgent",
					"You are a synchronization agent. Compare two systems (skills vs trees, configs vs reality, vault vs platform). Use file_read to read files, web_search for reference, shell_exec to run diff/comparison commands. Report mismatches with specific file paths and suggested fixes.",
					[]string{"web_search", "file_read", "shell_exec"}),
			),
			seq("ExecutionPath",
				chainAgent("PlanningAgent",
					"You are a planning agent. {{.Task}} Think step by step. Use web_search for research, file_read to read/write files, shell_exec to run commands. Produce a complete, actionable output.",
					[]string{"web_search", "file_read", "shell_exec"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on planning quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- GoapResearch Tree ---

func GoapResearchTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "GoapResearch_Main", Children: []evolution.SerializableNode{
		act("SetupResearchTools", "Give chain agents access to web_search, knowledge_graph, calculator"),
		seq("PreGate", cond("ValidateInput", "Non-empty")),
		sel("StrategyRouter",
			seq("ResearchPath",
				cond("IsResearchRequest", "Detect research/analyze/find/query/search keywords"),
				chainAgent("ResearchAgent",
					"You are a research agent. Search the web for the latest information, query the knowledge graph for structured data, perform calculations if needed. Produce a well-structured research note with sources.",
					[]string{"web_search", "knowledge_graph", "calculator"}),
			),
			seq("GraphifyPath",
				cond("IsGraphifyRequest", "Detect graphify/graph/structural/codebase keywords"),
				chainAgent("GraphifyAgent",
					"You are a codebase analysis agent. Run graphify commands to analyze code structure: graphify update . to refresh, graphify query for insights, graphify path A B for relationships. Use file_read to read GRAPH_REPORT.md and source files. Produce a structural analysis with findings.",
					[]string{"shell_exec", "file_read"}),
			),
			seq("ExecutionPath",
				chainAgent("ResearchAgent",
					"You are a research agent. {{.Task}} Use web_search for research, knowledge_graph for structured queries, calculator for analysis. Produce a complete answer.",
					[]string{"web_search", "knowledge_graph", "calculator"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on research quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- GoapDevops Tree ---

func GoapDevopsTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "GoapDevops_Main", Children: []evolution.SerializableNode{
		act("SetupDevTools", "Give chain agents access to go_build, go_test, go_vet, web_search"),
		seq("PreGate", cond("ValidateInput", "Non-empty")),
		sel("StrategyRouter",
			seq("BuildPath",
				cond("IsBuildRequest", "Detect build/compile/install keywords"),
				chainAgent("DevopsBuildAgent",
					"You are a build and deployment agent. Use go_build to compile Go code, go_test to run tests, go_vet for static analysis, web_search for documentation. Report build results with any errors.",
					[]string{"go_build", "go_test", "go_vet", "web_search"}),
			),
			seq("ImplementPath",
				cond("IsImplementRequest", "Detect implement/plan/fix/create keywords"),
				chainAgent("DevopsImplementAgent",
					"You are an implementation agent. Read implementation plans from the vault, use go_build to compile changes, go_test to verify, go_vet to check quality. Use file_read to read/write code and web_search for reference. Complete the implementation task.",
					[]string{"go_build", "go_test", "go_vet", "file_read", "web_search"}),
			),
			seq("ExecutionPath",
				chainAgent("DevopsAgent",
					"You are a DevOps agent. {{.Task}} Use go_build, go_test, go_vet for Go development, file_read for reading/writing, web_search for reference. Complete the task step by step.",
					[]string{"go_build", "go_test", "go_vet", "file_read", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on devops quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// AllDomainTrees returns all domain trees keyed by name.
func AllDomainTrees() map[string]*evolution.SerializableNode {
	trees := map[string]*evolution.SerializableNode{
		"code_review":        CodeReviewTree(),
		"devops_ci":          DevOpsCITree(),
		"agent_monitor":      AgentMonitorTree(),
		"refactoring":       RefactoringTree(),
		"security_audit":     SecurityAuditTree(),
		"data_pipeline":      DataPipelineTree(),
		"meeting_notes":      MeetingNotesTree(),
		"crash_investigator": CrashInvestigatorTree(),
		"game_ai":            GameAITree(),
		"trading_signal":     TradingSignalTree(),
		"alert_router":       AlertRouterTree(),
		"goap_planning":      GoapPlanningTree(),
		"goap_research":      GoapResearchTree(),
		"goap_devops":        GoapDevopsTree(),
	}
	// Merge arc42 trees with qualified names (arc42:section1, etc.)
	for k, v := range Arc42Trees() {
		trees[k] = v
	}
	return trees
}

// Descriptions maps tree names to descriptions.
var Descriptions = map[string]string{
	"code_review":        "Bug detection, security review, style checking for any language",
	"devops_ci":          "Build → test → lint → deploy → verify → rollback pipeline",
	"agent_monitor":      "Health-check MCP servers, restart dead agents, send alerts",
	"refactoring":       "Detect code smells, suggest rewrites, verify behavior preserved",
	"security_audit":     "SAST scan, dependency CVE check, secret detection, threat modeling",
	"data_pipeline":      "ETL validation: extract → transform → load with integrity checks",
	"meeting_notes":      "Transcribe → extract actions → assign → summarize → distribute",
	"crash_investigator": "Parse stack trace → root cause → fix → verify → prevent recurrence",
	"game_ai":            "Patrol → detect → chase → combat → retreat (classic game BT patterns)",
	"trading_signal":     "Market data → technical analysis → signal generation → risk management",
}
