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

// retry wraps a child with retry decorator.
func retryW(name string, child evolution.SerializableNode, max int) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Retry", Name: name, Children: []evolution.SerializableNode{child}, MaxRetries: max}
}

// outcome builds the standard OutcomeSelector pattern.
func outcome() evolution.SerializableNode {
	return sel("OutcomeSelector",
		cond("WasSuccessful", "Exit if success"),
		retryW("RetrySelfCorrect", act("SelfCorrect", "Fix and retry"), 3),
		act("EscalateToDeepSeek", "Escalate to external LLM"),
	)
}

// --- CodeReview Tree ---

func CodeReviewTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "CodeReview_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsCodeTask", "Has code-related keywords")),
		sel("StrategyRouter",
			seq("BugDetection",
				cond("IsBugCheck", "Detect bug/fix/error keywords"),
				act("ScanForBugs", "Analyze code for null derefs, off-by-one, race conditions"),
				act("SuggestBugFixes", "Generate fix with before/after"),
			),
			seq("SecurityReview",
				cond("IsSecurityCheck", "Detect security/exploit/vuln keywords"),
				act("ScanForVulns", "Check OWASP Top 10, injection, auth bypass"),
				act("SuggestSecurityFixes", "Generate secure alternative"),
			),
			seq("StyleReview",
				cond("IsStyleCheck", "Detect style/lint/format keywords"),
				act("CheckCodeStyle", "Verify naming, formatting, idiomatic patterns"),
				act("SuggestStyleFixes", "Generate style corrections"),
			),
			seq("ExecutionPath", act("AnalyzeTask", "LLM analyze"), act("ExecutePlan", "LLM execute")),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
		),
		act("ReflectOnOutcome", "Reflect on CI/CD quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- AgentMonitor Tree ---

func AgentMonitorTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "AgentMonitor_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsMonitorTask", "Detect monitor/health/status keywords")),
		sel("StrategyRouter",
			seq("HealthCheckPath",
				cond("IsHealthCheck", "Detect health/status/ping keywords"),
				act("CheckAllAgents", "Ping all registered MCP servers"),
				act("IdentifyDeadAgents", "Flag agents not responding"),
			),
			seq("MetricsCollectionPath",
				cond("IsMetricsRequest", "Detect metrics/stats keywords"),
				act("CollectAgentMetrics", "Gather uptime, tool calls, error rates"),
				act("GenerateHealthReport", "Produce dashboard-ready report"),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
		),
		act("ReflectOnOutcome", "Reflect on audit quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// --- DataPipeline Tree ---

func DataPipelineTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "DataPipeline_Main", Children: []evolution.SerializableNode{
		seq("PreGate", cond("ValidateInput", "Non-empty"), cond("IsDataTask", "Detect data/ETL/pipeline keywords")),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
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
			seq("ExecutionPath", act("AnalyzeTask", "LLM"), act("ExecutePlan", "LLM")),
		),
		act("ReflectOnOutcome", "Reflect on signal quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
}

// AllDomainTrees returns all domain trees keyed by name.
func AllDomainTrees() map[string]*evolution.SerializableNode {
	return map[string]*evolution.SerializableNode{
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
	}
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
