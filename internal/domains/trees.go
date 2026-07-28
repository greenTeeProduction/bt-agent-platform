// Package domains provides domain-specific behavior trees for code review,
// DevOps/CI, security, data pipelines, refactoring, incident response,
// alert routing, and general knowledge tasks. Each tree encodes expert
// decision logic for its domain with keyword-based condition routing.
package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// seq creates a Sequence node with a stage description and children.
func seq(name, desc string, children ...evolution.SerializableNode) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Sequence", Name: name, Description: desc, Children: children}
}

// sel creates a Selector node with a routing description and children.
func sel(name, desc string, children ...evolution.SerializableNode) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Selector", Name: name, Description: desc, Children: children}
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
// name labels the agent in the node Description, systemPrompt is the system
// message, task is the user prompt, tools is the list of tool names to make
// available.
func chainAgent(name, systemPrompt string, tools []string) evolution.SerializableNode {
	ti := make([]any, len(tools))
	for i, t := range tools {
		ti[i] = t
	}
	return evolution.SerializableNode{
		Type:        "ChainAction",
		Name:        "agent:{{.Task}}",
		Description: name + ": delegate the task to an LLM chain agent with the configured tools",
		Metadata: map[string]any{
			"system_msg": systemPrompt,
			"tools":      ti,
			"max_tokens": float64(15),
		},
	}
}

// retry wraps a child with retry decorator.
func retryW(name string, child evolution.SerializableNode, maxRetries int) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Retry",
		Name:        name,
		Description: "Retry decorator: re-run " + child.Name + " on failure up to the retry limit",
		Children:    []evolution.SerializableNode{child},
		MaxRetries:  maxRetries,
	}
}

// outcome builds the standard OutcomeSelector pattern.
// Uses MarkSuccessful instead of WasSuccessful — the WasSuccessful condition
// does unreliable keyword matching on LLM output and causes false failures.
// Quality gates (PreGate validation, output length checks) catch real failures.
func outcome() evolution.SerializableNode {
	return sel("OutcomeSelector", "Route the outcome: mark success, retry with self-correction, or escalate to an external LLM",
		act("MarkSuccessful", "Mark task as successful"),
		retryW("RetrySelfCorrect", act("SelfCorrect", "Fix and retry"), 3),
		act("EscalateToDeepSeek", "Escalate to external LLM"),
	)
}

// --- Hermes Update Tree ---

// HermesUpdateTree returns a zero-LLM maintenance tree that checks for
// Hermes Agent updates and applies them via hermes update. Follows the
// same pattern as AgentMonitorTree: shell exec in Action nodes, minimal
// LLM overhead.
func HermesUpdateTree() *evolution.SerializableNode {
	guard := func(label, condition string) evolution.TypedEdge {
		return evolution.TypedEdge{Type: evolution.EdgeGuard, Label: label, Condition: condition, ChildIndex: -1}
	}
	qualityGate := func(label string) evolution.TypedEdge {
		return evolution.TypedEdge{Type: evolution.EdgeQualityGate, Label: label, ChildIndex: -1}
	}

	return &evolution.SerializableNode{
		Type: "Sequence", Name: "HermesUpdate_Main",
		Description: "Daily Hermes update: version check, git fetch, hermes update, report",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Description: "Input validation — task must be update-related",
				Edges:       []evolution.TypedEdge{guard("input-validation", "task must be non-empty and update-related")},
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task",
						Edges: []evolution.TypedEdge{guard("non-empty-task", "task string must not be empty")},
					},
					{Type: "Condition", Name: "IsUpdateTask", Description: "Has update/upgrade/hermes/maintenance keywords",
						Edges: []evolution.TypedEdge{guard("is-update-task", "task contains update/upgrade/hermes/maintenance keywords")},
					},
				},
			},
			{Type: "Action", Name: "HermesUpdateAgent",
				Description: "Check version, git fetch, run hermes update if behind, report",
				Edges:       []evolution.TypedEdge{qualityGate("update-report-completeness")},
			},
			outcome(),
		},
	}
}

// --- CodeReview Tree ---

func CodeReviewTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "CodeReview_Main",
		Description: "Code review pipeline: validate input, route to bug/security/style review, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			act("SetupDefaultTools", "Populate bb.ChainTools with real system tools"),
			act("DiscoverAvailableTools", "Record available real tools before review"),
			seq("PreGate", "Validate the task is non-empty and code-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsCodeTask", "Has code-related keywords")),
			sel("StrategyRouter", "Route to bug detection, security review, or style review by task keywords, falling back to the general code review agent",
				seq("BugDetection", "Detect bug keywords and run the bug-detection LLM agent over the code",
					cond("IsBugCheck", "Detect bug/fix/error keywords"),
					chainAgent("BugDetectionAgent", "Analyze the code for bugs: null derefs, off-by-one errors, race conditions, logic errors. Use file_read to inspect code. Report findings with line numbers and suggested fixes.",
						[]string{"file_read", "shell_exec"}),
				),
				seq("SecurityReview", "Detect security keywords and run the security-review LLM agent",
					cond("IsSecurityCheck", "Detect security/exploit/vuln keywords"),
					chainAgent("SecurityReviewAgent", "Review code for security vulnerabilities: OWASP Top 10, injection, auth bypass, unsafe operations. Use file_read to inspect files, shell_exec to grep for patterns. Report each finding with severity.",
						[]string{"file_read", "shell_exec"}),
				),
				seq("StyleReview", "Detect style keywords, check code style, and suggest corrections",
					cond("IsStyleCheck", "Detect style/lint/format keywords"),
					act("CheckCodeStyle", "Verify naming, formatting, idiomatic patterns"),
					act("SuggestStyleFixes", "Generate style corrections"),
				),
				seq("ExecutionPath", "Fallback: run the general code-review LLM agent on the task",
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
	return &evolution.SerializableNode{Type: "Sequence", Name: "DevOpsCI_Main",
		Description: "CI/CD pipeline: validate input, route to build/test/lint/deploy, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and CI/build-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsCIBuildTask", "Detect build/deploy/CI keywords")),
			sel("StrategyRouter", "Route to build, test, lint, or deploy path by task keywords, falling back to the general CI/CD agent",
				seq("BuildPath", "Compile the project, parse build errors, and suggest fixes",
					cond("NeedsBuild", "Detect build/compile keywords"),
					act("RunBuild", "Execute build command, capture output"),
					act("CheckBuildErrors", "Parse build output for errors/warnings"),
					act("FixBuildIssues", "Suggest fixes for compilation errors"),
				),
				seq("TestPath", "Run the test suite and analyze failures, flakes, and coverage gaps",
					cond("NeedsTestRun", "Detect test keywords"),
					act("RunTests", "Execute test suite, capture results"),
					act("AnalyzeTestResults", "Parse failures, flaky tests, coverage gaps"),
				),
				seq("LintPath", "Run the linter and categorize issues by severity",
					cond("NeedsLinting", "Detect lint/static analysis keywords"),
					act("RunLinter", "Execute linting tool"),
					act("AnalyzeLintOutput", "Categorize issues by severity"),
				),
				seq("DeployPath", "Deploy, verify health, and roll back on failure",
					cond("NeedsDeploy", "Detect deploy/release keywords"),
					act("RunDeploy", "Execute deployment script"),
					act("VerifyDeploy", "Health check endpoint, smoke test"),
					act("RollbackOnFailure", "Revert if health check fails"),
				),
				seq("ExecutionPath", "Fallback: run the general CI/CD LLM agent on the task",
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
	// Short helpers for typed edges
	guard := func(label, condition string) evolution.TypedEdge {
		return evolution.TypedEdge{Type: evolution.EdgeGuard, Label: label, Condition: condition, ChildIndex: -1}
	}
	effect := func(label, effectDesc string) evolution.TypedEdge {
		return evolution.TypedEdge{Type: evolution.EdgeEffect, Label: label, Effect: effectDesc, ChildIndex: -1}
	}
	qualityGate := func(label string) evolution.TypedEdge {
		return evolution.TypedEdge{Type: evolution.EdgeQualityGate, Label: label, ChildIndex: -1}
	}
	recovery := func(label string) evolution.TypedEdge {
		return evolution.TypedEdge{Type: evolution.EdgeRecovery, Label: label, ChildIndex: -1}
	}
	approval := func(label string) evolution.TypedEdge {
		return evolution.TypedEdge{Type: evolution.EdgeApproval, Label: label, ChildIndex: -1}
	}

	healthCheckPath := evolution.SerializableNode{
		Type: "Sequence", Name: "HealthCheckPath",
		Description: "Health check: scheduler, cron, disk, memory, BT processes, load, duplicate detection",
		Edges:       []evolution.TypedEdge{qualityGate("health-report-completeness")},
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "IsHealthCheck", Description: "Detect health/status/ping/cron/scheduler keywords",
				Edges: []evolution.TypedEdge{guard("is-health-check", "task contains health/status/cron/scheduler keywords")},
			},
			{Type: "Action", Name: "HealthCheckAgent", Description: "Full health check with thresholds, scheduler, cron, duplicates",
				Edges: []evolution.TypedEdge{
					effect("health-check-report", "writes health.disk, health.memory, health.processes, health.load, health.scheduler, health.cron, health.overall_status"),
				},
			},
		},
	}

	metricsPath := evolution.SerializableNode{
		Type: "Sequence", Name: "MetricsCollectionPath",
		Description: "Numeric metrics collection for capacity planning",
		Edges:       []evolution.TypedEdge{qualityGate("metrics-report-completeness")},
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "IsMetricsRequest", Description: "Detect metrics/stats/report keywords",
				Edges: []evolution.TypedEdge{guard("is-metrics-request", "task contains metrics/stats/report keywords")},
			},
			{Type: "Action", Name: "MetricsCollectionAgent", Description: "Read-only numeric metrics snapshot",
				Edges: []evolution.TypedEdge{
					effect("metrics-report", "writes metrics.disk_mb, metrics.memory_mb, metrics.process_count"),
				},
			},
		},
	}

	restartPath := evolution.SerializableNode{
		Type: "Sequence", Name: "RestartPath",
		Description: "Restart dead BT agents with approval gating",
		Edges: []evolution.TypedEdge{
			recovery("restart-dead-agents"),
			approval("restart-approval-required"),
		},
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "IsRestartRequest", Description: "Detect restart/dead/revive keywords",
				Edges: []evolution.TypedEdge{guard("is-restart-request", "task contains restart/dead/revive keywords")},
			},
			{Type: "HumanApprovalGate", Name: "ApproveRestart", Description: "Requires human approval before restarting agents",
				Edges: []evolution.TypedEdge{approval("human-approval-restart")},
				Metadata: map[string]any{
					"phase":             "pre",
					"side_effect_class": "local_reversible",
					"hitl_prompt":       "Agent monitor will restart dead bt-* processes via systemctl.",
					"auto_approve":      true,
				},
			},
			{Type: "Action", Name: "RestartDeadAgents", Description: "Restart dead bt-* processes via systemctl, clear stale in_flight",
				Edges: []evolution.TypedEdge{
					recovery("restart-action"),
					effect("restart-report", "writes restart.restarted, restart.failed, restart.stale_in_flight"),
				},
			},
		},
	}

	return &evolution.SerializableNode{
		Type: "Sequence", Name: "AgentMonitor_Main",
		Description: "Zero-LLM health/monitoring/restart agent with typed edge validation",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Description: "Input validation guard — task must be non-empty and monitor-related",
				Edges:       []evolution.TypedEdge{guard("input-validation", "task must be non-empty and monitor-related")},
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task", Edges: []evolution.TypedEdge{guard("non-empty-task", "task string must not be empty")}},
					{Type: "Condition", Name: "IsMonitorTask", Description: "Has monitor/health/status keywords", Edges: []evolution.TypedEdge{guard("is-monitor-task", "task contains monitor/health/status keywords")}},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Description: "Route to health check, metrics collection, or restart path",
				Children:    []evolution.SerializableNode{healthCheckPath, metricsPath, restartPath},
			},
			outcome(),
		},
	}
}

// --- Refactoring Tree ---

func RefactoringTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "Refactoring_Main",
		Description: "Refactoring pipeline: validate input, route to smell/pattern/verification paths, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and refactor-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsRefactorTask", "Detect refactor/improve/clean keywords")),
			sel("StrategyRouter", "Route to smell detection, pattern application, or verification by task keywords, falling back to the general refactoring agent",
				seq("SmellDetection", "Identify code smells and suggest matching refactorings",
					cond("IsSmellCheck", "Detect smell/cruft/duplicate keywords"),
					act("DetectCodeSmells", "Identify long functions, deep nesting, duplication"),
					act("SuggestRefactorings", "Extract method, simplify condition, DRY"),
				),
				seq("PatternApplication", "Recommend design patterns and generate implementation templates",
					cond("IsPatternRequest", "Detect pattern/design/architecture keywords"),
					act("RecommendPatterns", "Suggest strategy, factory, observer, etc."),
					act("GeneratePatternCode", "Produce implementation template"),
				),
				seq("VerificationPath", "Verify behavior is preserved and report refactoring impact",
					cond("NeedsVerification", "Detect verify/test/check keywords"),
					act("VerifyBehavior", "Run existing tests, check no regression"),
					act("ReportRefactoringImpact", "Summary of changes and risk assessment"),
				),
				seq("ExecutionPath", "Fallback: run the general refactoring LLM agent on the task",
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
	return &evolution.SerializableNode{Type: "Sequence", Name: "SecurityAudit_Main",
		Description: "Security audit pipeline: validate input, route to SAST/dependency/secret/threat paths, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and security-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsSecurityTask", "Detect security/audit/threat keywords")),
			sel("StrategyRouter", "Route to SAST scan, dependency scan, secret detection, or threat modeling by task keywords, falling back to the general security audit agent",
				seq("SASTPath", "Static-analysis scan for injection, XSS, and auth flaws with a prioritized report",
					cond("IsSASTRequest", "Detect SAST/static analysis keywords"),
					act("RunSASTScan", "Analyze source for injection, XSS, auth flaws"),
					act("GenerateSASTReport", "Prioritized findings with severity"),
				),
				seq("DependencyScan", "Check dependencies against CVE databases and suggest fixes",
					cond("IsDepScanRequest", "Detect dependency/package/CVE keywords"),
					act("ScanDependencies", "Check CVE database for known vulns"),
					act("SuggestDependencyFixes", "Recommend version bumps or alternatives"),
				),
				seq("SecretDetection", "Scan for exposed API keys, tokens, and passwords with remediation steps",
					cond("IsSecretScan", "Detect secret/credential/key keywords"),
					act("ScanForSecrets", "Search for API keys, tokens, passwords"),
					act("ReportExposedSecrets", "Flag with remediation steps"),
				),
				seq("ThreatModeling", "Build a STRIDE threat model and generate mitigations",
					cond("IsThreatModel", "Detect threat/model/attack keywords"),
					act("BuildThreatModel", "STRIDE analysis, attack surface mapping"),
					act("GenerateMitigations", "Controls and countermeasures"),
				),
				seq("ExecutionPath", "Fallback: run the general security-audit LLM agent on the task",
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
	return &evolution.SerializableNode{Type: "Sequence", Name: "DataPipeline_Main",
		Description: "ETL pipeline: validate input, route to extract/transform/load with anti-fabrication checks, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			act("SetupDataPipelineTools", "Register real file/shell/calculator tools"),
			act("DiscoverAvailableTools", "Record available real tools before any tool use"),
			seq("PreGate", "Validate the task is non-empty and data/ETL-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsDataTask", "Detect data/ETL/pipeline/delegation/queue/index/session/memory/extract/process keywords")),
			sel("StrategyRouter", "Route to extract, transform, or load path by task keywords, falling back to the anti-fabrication execution path",
				seq("ExtractPath", "Validate the source and extract data, reporting only observed metrics",
					cond("IsExtractRequest", "Detect extract/ingest/load keywords"),
					act("ValidateDataSource", "Check actual source file exists and capture observed metrics"),
					act("ExtractData", "Report observed extraction metrics only"),
					act("VerifyOutput", "Reject canned/fabricated counts"),
				),
				seq("TransformPath", "Validate prerequisites and apply the transform, rejecting fabricated counts",
					cond("IsTransformRequest", "Detect transform/clean/normalize keywords"),
					act("ValidateDataSource", "Check actual source file exists before transform"),
					act("ValidateTransform", "Check transformation prerequisites"),
					act("ApplyTransform", "Dry-run transform unless explicit source/target exists"),
					act("VerifyOutput", "Reject canned/fabricated counts"),
				),
				seq("LoadPath", "Validate source and target and load data without unverified writes",
					cond("IsLoadRequest", "Detect load/write/store keywords"),
					act("ValidateDataSource", "Check actual source file exists before load"),
					act("ValidateTarget", "Check target path is explicit"),
					act("LoadData", "Dry-run load unless explicit write content exists"),
					act("VerifyLoad", "Confirm no unverified writes are claimed"),
				),
				seq("ExecutionPath", "Fallback: report blocked honestly when no data source is provided",
					act("ValidateDataSource", "If no source is provided, report blocked honestly instead of fabricating"),
					act("VerifyOutput", "Anti-fabrication evidence gate"),
				),
			),
			act("ReflectOnOutcome", "Reflect on pipeline quality"),
			outcome(),
			act("UpdateBehaviorTree", "Evolve"),
		}}
}

// --- MeetingNotes Tree ---

func MeetingNotesTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "MeetingNotes_Main",
		Description: "Meeting notes pipeline: validate input, route to transcribe/actions/notes/follow-up, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and meeting-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsMeetingTask", "Detect meeting/notes/transcript keywords")),
			sel("StrategyRouter", "Route to transcription, action extraction, note generation, or follow-up by task keywords, falling back to the general meeting notes agent",
				seq("TranscribePath", "Parse the transcript into speaker turns and topic segments",
					cond("HasTranscript", "Transcript or audio file provided"),
					act("ParseTranscript", "Extract speaker turns, timestamps"),
					act("IdentifyTopics", "Segment by topic shifts"),
				),
				seq("ExtractActions", "Extract action items and assign owners",
					cond("IsActionExtraction", "Detect action/todo/next keywords"),
					act("ExtractActionItems", "Identify commitments, deadlines, owners"),
					act("AssignOwners", "Map actions to participants"),
				),
				seq("GenerateNotes", "Summarize decisions, format structured notes, and distribute them",
					cond("IsSummaryRequest", "Detect summary/notes/minutes keywords"),
					act("GenerateSummary", "Key decisions, discussion points, outcomes"),
					act("FormatMeetingNotes", "Structured format: date, attendees, agenda, notes, actions"),
					act("DistributeNotes", "Email or share to relevant channels"),
				),
				seq("FollowUpPath", "Check action status and remind overdue owners",
					cond("IsFollowUp", "Detect follow-up/reminder keywords"),
					act("CheckActionStatus", "Verify completion of previous actions"),
					act("SendReminders", "Notify overdue action owners"),
				),
				seq("ExecutionPath", "Fallback: run the general meeting-notes LLM agent on the task",
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
	return &evolution.SerializableNode{Type: "Sequence", Name: "CrashInvestigator_Main",
		Description: "Crash investigation pipeline: validate input, route to parse/root-cause/fix/prevention paths, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and crash-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsCrashTask", "Detect crash/error/stack/panic keywords")),
			sel("StrategyRouter", "Route to stack-trace parsing, root-cause analysis, fix-and-verify, or prevention by task keywords, falling back to the general crash investigator agent",
				seq("ParseStackTrace", "Parse stack frames and locate the exact crash site",
					cond("HasStackTrace", "Stack trace or error log provided"),
					act("ParseStackFrames", "Extract file, line, function from each frame"),
					act("IdentifyCrashSite", "Locate exact crash point"),
				),
				seq("RootCauseAnalysis", "Trace the execution path, pinpoint the root cause, and generate a fix",
					cond("IsRootCauseRequest", "Detect root cause/why/debug keywords"),
					act("TraceExecutionPath", "Reconstruct code flow leading to crash"),
					act("IdentifyRootCause", "Pinpoint null deref, OOB, race, logic error"),
					act("GenerateFix", "Produce minimal code fix"),
				),
				seq("FixAndVerify", "Apply the generated fix and verify the crash no longer reproduces",
					cond("HasProposedFix", "Fix has been generated"),
					act("ApplyFix", "Apply code change"),
					act("RunRegressionTests", "Verify fix doesn't break existing tests"),
					act("VerifyCrashResolved", "Confirm original crash no longer reproduces"),
				),
				seq("PreventionPath", "Suggest guards and monitoring to prevent recurrence",
					cond("IsPreventionRequest", "Detect prevent/harden/guard keywords"),
					act("SuggestGuards", "Add null checks, bounds checks, error handling"),
					act("AddMonitoring", "Suggest alerts for similar patterns"),
				),
				seq("ExecutionPath", "Fallback: run the general crash-investigator LLM agent on the task",
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
	return &evolution.SerializableNode{Type: "Sequence", Name: "GameAI_Main",
		Description: "Game AI pipeline: validate input, route to patrol/detect/chase/combat/retreat states, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and game/NPC-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsGameTask", "Detect game/NPC/AI/behavior keywords")),
			sel("StrategyRouter", "Route to patrol, detect, chase, combat, or retreat state by task keywords, falling back to the general game AI agent",
				seq("PatrolPath", "Define a patrol route and execute it, watching for interruptions",
					cond("IsPatrolState", "Detect patrol/idle/wander keywords"),
					act("SetPatrolRoute", "Define waypoints or random wander"),
					act("ExecutePatrol", "Move along route, detect interruptions"),
				),
				seq("DetectPath", "Scan the environment and classify threats",
					cond("IsDetectState", "Detect detect/spot/see/hear keywords"),
					act("ScanEnvironment", "Raycast, proximity check, sound detection"),
					act("ClassifyThreat", "Friend/foe/neutral, threat level assessment"),
				),
				seq("ChasePath", "Compute a pursuit path and follow the target",
					cond("IsChaseState", "Detect chase/pursue/follow keywords"),
					act("CalculatePursuitPath", "Pathfinding to target, speed matching"),
					act("ExecutePursuit", "Follow target, maintain distance"),
				),
				seq("CombatPath", "Select a target, choose and execute a combat action, and evaluate the result",
					cond("IsCombatState", "Detect attack/fight/combat/shoot keywords"),
					act("SelectTarget", "Prioritize by threat, distance, health"),
					act("ChooseAction", "Attack, dodge, use ability, take cover"),
					act("ExecuteCombatAction", "Perform selected action"),
					act("EvaluateCombatResult", "Damage dealt, health change, reposition"),
				),
				seq("RetreatPath", "Find a safe position and retreat, using healing",
					cond("IsRetreatState", "Detect retreat/flee/escape/heal keywords"),
					act("FindSafePosition", "Locate cover or exit point"),
					act("ExecuteRetreat", "Move to safe position, use healing"),
				),
				seq("ExecutionPath", "Fallback: run the general game-AI LLM agent on the task",
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
	return &evolution.SerializableNode{Type: "Sequence", Name: "TradingSignal_Main",
		Description: "Trading signal pipeline: validate input, route to data/TA/signal/risk paths, reflect on the outcome, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and trading-related before routing",
				cond("ValidateInput", "Non-empty"), cond("IsTradingTask", "Detect trading/signal/market/price keywords")),
			sel("StrategyRouter", "Route to data collection, technical analysis, signal generation, or risk management by task keywords, falling back to the general trading signal agent",
				seq("DataCollectionPath", "Fetch market data and validate its quality",
					cond("IsDataRequest", "Detect data/fetch/pull/price keywords"),
					act("FetchMarketData", "Pull OHLCV, order book, volume data"),
					act("ValidateDataQuality", "Check for gaps, outliers, stale data"),
				),
				seq("TechnicalAnalysis", "Calculate indicators, detect chart patterns, and generate TA signals",
					cond("IsTAPath", "Detect technical/indicator/pattern keywords"),
					act("CalculateIndicators", "SMA, EMA, RSI, MACD, Bollinger, ATR"),
					act("DetectPatterns", "Head & shoulders, double top, flags, wedges"),
					act("GenerateTASignals", "Buy/sell signals from indicator crossovers"),
				),
				seq("SignalGeneration", "Combine TA signals into a weighted signal with a confidence score",
					cond("IsSignalRequest", "Detect signal/buy/sell/entry keywords"),
					act("ComputeSignal", "Weighted combination of TA signals"),
					act("AssessSignalStrength", "Confidence score, historical accuracy"),
				),
				seq("RiskManagement", "Check position limits, compute stop-loss, and assess risk/reward",
					cond("IsRiskCheck", "Detect risk/stop-loss/position keywords"),
					act("CheckPositionLimits", "Verify within exposure limits"),
					act("CalculateStopLoss", "ATR-based or percentage-based stop"),
					act("AssessRiskReward", "R:R ratio, Kelly criterion check"),
				),
				seq("ExecutionPath", "Fallback: run the general trading-signal LLM agent on the task",
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

func GoapPlanningTree(withCheckpointVerifier bool) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "GoapPlanning_Main",
		Description: "GOAP planning pipeline: assess or synchronize via chain agents, then reflect, report, and evolve",
		Children: []evolution.SerializableNode{
			act("SetupUniversalTools", "Give chain agents access to web_search, file_read, shell_exec"),
			seq("PreGate", "Validate the task is non-empty before routing", cond("ValidateInput", "Non-empty")),
			sel("StrategyRouter", "Try the real GOAP A* planner first, falling back to assessment or synchronization by task keywords, then the general planning agent",
				*evolution.GOAPPlanningTree(),
				seq("AssessPath", "Run the planning-assessment chain agent over the current state",
					cond("IsAssessRequest", "Detect assess/check/review/scan/audit keywords"),
					chainAgent("PlanningAssessAgent",
						"You are a planning assessment agent. TASK: {{.Task}}. Assess the current state: scan files, check configurations, review logs. Use web_search for external research, file_read to read local files, shell_exec to run diagnostic commands. Produce a structured assessment report with findings and recommendations.",
						[]string{"web_search", "file_read", "shell_exec"}),
				),
				seq("SyncPath", "Run the synchronization chain agent to compare two systems and report mismatches",
					cond("IsSyncRequest", "Detect sync/pollinate/cross/align keywords"),
					chainAgent("PlanningSyncAgent",
						"You are a synchronization agent. TASK: {{.Task}}. Compare two systems (skills vs trees, configs vs reality, vault vs platform). Use file_read to read files, web_search for reference, shell_exec to run diff/comparison commands. Report mismatches with specific file paths and suggested fixes.",
						[]string{"web_search", "file_read", "shell_exec"}),
				),
				seq("ExecutionPath", "Fallback: run the general planning chain agent on the task",
					chainAgent("PlanningAgent",
						"You are a planning agent. {{.Task}} Think step by step. Use web_search for research, file_read to read/write files, shell_exec to run commands. Produce a complete, actionable output.",
						[]string{"web_search", "file_read", "shell_exec"}),
				),
			),
			act("ReflectOnOutcome", "Reflect on planning quality"),
			outcome(),
			act("UpdateBehaviorTree", "Evolve"),
		}}
	if withCheckpointVerifier {
		return evolution.WrapWithCheckpointVerifier(tree, 3, "has_result=true,task_status=completed")
	}
	return tree
}

// --- GoapResearch Tree ---

func GoapResearchTree(withCheckpointVerifier bool) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "GoapResearch_Main",
		Description: "GOAP research pipeline: web research or graphify codebase analysis via chain agents, then reflect, report, and evolve",
		Children: []evolution.SerializableNode{
			act("SetupResearchTools", "Give chain agents access to web_search, knowledge_graph, calculator"),
			seq("PreGate", "Validate the task is non-empty before routing", cond("ValidateInput", "Non-empty")),
			sel("StrategyRouter", "Try the real GOAP A* planner first, falling back to web research or graphify codebase analysis by task keywords, then the general research agent",
				*evolution.GOAPResearchTree(),
				seq("ResearchPath", "Run the web/knowledge-graph research chain agent",
					cond("IsResearchRequest", "Detect research/analyze/find/query/search keywords"),
					chainAgent("ResearchAgent",
						"You are a research agent. TASK: {{.Task}}. Search the web for the latest information, query the knowledge graph for structured data, perform calculations if needed. Produce a well-structured research note with sources.",
						[]string{"web_search", "knowledge_graph", "calculator"}),
				),
				seq("GraphifyPath", "Run the graphify codebase-analysis chain agent",
					cond("IsGraphifyRequest", "Detect graphify/graph/structural/codebase keywords"),
					chainAgent("GraphifyAgent",
						"You are a codebase analysis agent. TASK: {{.Task}}. Run graphify commands to analyze code structure: graphify update . to refresh, graphify query for insights, graphify path A B for relationships. Use file_read to read GRAPH_REPORT.md and source files. Produce a structural analysis with findings.",
						[]string{"shell_exec", "file_read", "graphify"}),
				),
				seq("ExecutionPath", "Fallback: run the general research chain agent on the task",
					chainAgent("ResearchAgent",
						"You are a research agent. {{.Task}} Use web_search for research, knowledge_graph for structured queries, calculator for analysis. Produce a complete answer.",
						[]string{"web_search", "knowledge_graph", "calculator"}),
				),
			),
			act("ReflectOnOutcome", "Reflect on research quality"),
			outcome(),
			act("UpdateBehaviorTree", "Evolve"),
		}}
	if withCheckpointVerifier {
		return evolution.WrapWithCheckpointVerifier(tree, 3, "has_result=true,task_status=completed")
	}
	return tree
}

// --- GoapDevops Tree ---

func GoapDevopsTree(withCheckpointVerifier bool) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "GoapDevops_Main",
		Description: "GOAP DevOps pipeline: build or implement via chain agents, then reflect, report, and evolve",
		Children: []evolution.SerializableNode{
			act("SetupDevTools", "Give chain agents access to go_build, go_test, go_vet, web_search"),
			seq("PreGate", "Validate the task is non-empty before routing", cond("ValidateInput", "Non-empty")),
			sel("StrategyRouter", "Try the real GOAP A* planner first, falling back to build or implementation by task keywords, then the general DevOps agent",
				*evolution.GOAPDevOpsTree(),
				seq("BuildPath", "Run the build/test/vet chain agent and report results",
					cond("IsBuildRequest", "Detect build/compile/install keywords"),
					chainAgent("DevopsBuildAgent",
						"You are a build and deployment agent. TASK: {{.Task}}. Use go_build to compile Go code, go_test to run tests, go_vet for static analysis, web_search for documentation. Report build results with any errors.",
						[]string{"go_build", "go_test", "go_vet", "web_search"}),
				),
				seq("ImplementPath", "Run the implementation chain agent against the vault plan and verify with build/test",
					cond("IsImplementRequest", "Detect implement/plan/fix/create keywords"),
					chainAgent("DevopsImplementAgent",
						"You are an implementation agent. TASK: {{.Task}}. Read implementation plans from the vault, use go_build to compile changes, go_test to verify, go_vet to check quality. Use file_read to read/write code and web_search for reference. Complete the implementation task.",
						[]string{"go_build", "go_test", "go_vet", "file_read", "web_search"}),
				),
				seq("ExecutionPath", "Fallback: run the general DevOps chain agent on the task",
					chainAgent("DevopsAgent",
						"You are a DevOps agent. {{.Task}} Use go_build, go_test, go_vet for Go development, file_read for reading/writing, web_search for reference. Complete the task step by step.",
						[]string{"go_build", "go_test", "go_vet", "file_read", "web_search"}),
				),
			),
			act("ReflectOnOutcome", "Reflect on devops quality"),
			outcome(),
			act("UpdateBehaviorTree", "Evolve"),
		}}
	if withCheckpointVerifier {
		return evolution.WrapWithCheckpointVerifier(tree, 3, "has_result=true,task_status=completed")
	}
	return tree
}

// --- AuctionDemo Tree ---

// AuctionDemoTree returns a domain tree that exercises auction-based A2A task
// allocation end to end — milestone 5/5 of the "Auction-based A2A task
// allocation for multi-agent coordination" program. The entire announce → bid →
// award flow is driven by the engine's single AuctionDelegate action: the
// production seam whose Auctioneer fans a TaskAnnouncement out to candidate
// agents, collects their Bids, and dispatches the real task to the winning
// bidder (falling back to a delegate tree via delegate_tree_id when no eligible
// bidder responds) — see a2a.Auctioneer.RunAuction, which composes all three
// stages internally.
//
// Earlier revisions modeled the announce and bid stages as separate AnnounceTask
// and CollectBids Action nodes so the tree "read as" the full protocol, but those
// names are not registered in the engine: at tick time they resolved to the
// permissive success fallback, reporting success while surfacing no
// announcement/bid evidence into the blackboard (a silent no-op caught by
// TestAuctionDemoTreeHasNoSilentNoOps). Because RunAuction already performs the
// announce and bid stages within AuctionDelegate, the honest tree collapses to
// the single real seam rather than fronting it with hollow stages. The
// surrounding PreGate/outcome scaffolding matches the other curated domain trees
// so the demo is selectable via switch_tree.
func AuctionDemoTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "AuctionDemo_Main",
		Description: "Auction demo: validate input, run the announce→bid→award AuctionDelegate seam, reflect, and route the outcome",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the task is non-empty and auction-related before delegation",
				cond("ValidateInput", "Non-empty"), cond("IsAuctionTask", "Detect auction/allocate/bid/award/delegate keywords")),
			act("AuctionDelegate", "Run the full announce→bid→award auction: fan a TaskAnnouncement out to candidate A2A agents, collect their Bids, and award the task to the winning bidder — falling back to a delegate tree when no eligible bidder responds"),
			act("ReflectOnOutcome", "Reflect on auction allocation quality"),
			outcome(),
		}}
}

// wrapWithErrorHandler wraps a tree root in a ClaudeErrorHandler decorator so
// any failure that bubbles to the root can grow a Claude-proposed recovery
// node (engine/error_handler_node.go). Pure data — domains must not import
// engine (test-build cycle, see goap_fusion_wire_seam_test.go).
func wrapWithErrorHandler(name string, tree *evolution.SerializableNode) *evolution.SerializableNode {
	if tree == nil || tree.Type == "ClaudeErrorHandler" {
		return tree
	}
	return &evolution.SerializableNode{
		Type:        "ClaudeErrorHandler",
		Name:        name + "_ErrorHandler",
		Description: "Self-extending Claude error handler: on root failure, propose and gate a recovery node here (see engine/error_handler_node.go).",
		Children:    []evolution.SerializableNode{*tree},
	}
}

// AllDomainTrees returns all domain trees keyed by name.
func AllDomainTrees() map[string]*evolution.SerializableNode {
	trees := map[string]*evolution.SerializableNode{
		"code_review":               CodeReviewTree(),
		"devops_ci":                 DevOpsCITree(),
		"agent_monitor":             AgentMonitorTree(),
		"refactoring":               RefactoringTree(),
		"security_audit":            SecurityAuditTree(),
		"data_pipeline":             DataPipelineTree(),
		"meeting_notes":             MeetingNotesTree(),
		"crash_investigator":        CrashInvestigatorTree(),
		"game_ai":                   GameAITree(),
		"trading_signal":            TradingSignalTree(),
		"alert_router":              AlertRouterTree(),
		"goap_planning":             GoapPlanningTree(true),
		"goap_research":             GoapResearchTree(true),
		"goap_devops":               GoapDevopsTree(true),
		"goap_fusion":               GoapFusionTree(true),
		"goap_fusion_loop":          GoapFusionLoopTree(),
		"bt_fusion":                 BTFusionTree(),
		"bt_manager":                BTManagerTree(),
		"notebooklm":                NotebookLMTree(),
		"notebooklm_consumer":       NotebookLMConsumerTree(),
		"notebooklm_plan_implement": evolution.NotebooklmPlanImplementTree(),
		"superpowers_workflow":      SuperpowersWorkflowTree(),
		"hermes_update":             HermesUpdateTree(),
		"auction_demo":              AuctionDemoTree(),
		"arc42_seeder":              Arc42SeederTree(),
		"arc42:docsync":             Arc42DocsyncTree(),
		"self_review":               SelfReviewTree(),
	}
	// Merge arc42 trees with qualified names (arc42:section1, etc.)
	for k, v := range Arc42Trees() {
		trees[k] = v
	}
	// Every domain tree root gets the self-extending error handler.
	for k, v := range trees {
		trees[k] = wrapWithErrorHandler(k, v)
	}
	return trees
}

// ExpectedDomainIDs converts a domain tree registry (as returned by
// AllDomainTrees) into the "domain:<name>" ID form knowledge.KnowledgeGraph's
// ExpectedDomains expects, so every process wiring the live registry into
// CoverageGaps shares one canonical conversion instead of duplicating it.
func ExpectedDomainIDs(registry map[string]*evolution.SerializableNode) []string {
	ids := make([]string, 0, len(registry))
	for name := range registry {
		ids = append(ids, "domain:"+name)
	}
	return ids
}

// Descriptions maps tree names to descriptions.
var Descriptions = map[string]string{
	"code_review":               "Bug detection, security review, style checking for any language",
	"devops_ci":                 "Build → test → lint → deploy → verify → rollback pipeline",
	"agent_monitor":             "Health-check MCP servers, restart dead agents, send alerts",
	"refactoring":               "Detect code smells, suggest rewrites, verify behavior preserved",
	"security_audit":            "SAST scan, dependency CVE check, secret detection, threat modeling",
	"data_pipeline":             "ETL validation: extract → transform → load with integrity checks",
	"meeting_notes":             "Transcribe → extract actions → assign → summarize → distribute",
	"crash_investigator":        "Parse stack trace → root cause → fix → verify → prevent recurrence",
	"game_ai":                   "Patrol → detect → chase → combat → retreat (classic game BT patterns)",
	"trading_signal":            "Market data → technical analysis → signal generation → risk management",
	"alert_router":              "Route any alert (critical/security/trading/disk/health) by severity and type to the right channel — keyword-matching only, no LLM, instant execution",
	"goap_planning":             "Goal-driven planning: assess → plan → execute with GOAP agent routing",
	"goap_research":             "Research pipeline: web search → knowledge graph → synthesis with GOAP agent routing",
	"goap_devops":               "Build, test, lint, deploy pipeline with GOAP goal-driven agent selection — routes to build/implement/investigate agents based on task keywords",
	"goap_fusion":               "GOAP-driven BT platform improvement: reads NotebookLM vault research, runs graphify codebase analysis, identifies gaps, prioritizes goals, implements safe tree improvements, verifies with build/test",
	"goap_fusion_loop":          "Continuous self-improving GOAP fusion loop: grills NotebookLM with critical review, identifies gaps, implements improvements via Claude Code, verifies, and repeats forever — autonomous BT framework self-evolution",
	"bt_fusion":                 "Research behavior-tree/self-improving-agent patterns and expand this Go BT platform with evidence-backed fusion reports",
	"bt_manager":                "Post-execution meta-agent: analyze failures, detect degraded agents, apply targeted tree mutations — self-healing for the BT fleet",
	"notebooklm":                "NotebookLM operations: research→import→query, vault ingest, studio content creation (podcasts/briefings), sync-back to vault. Deterministic nlm CLI tool stubs with anti-fabrication evidence gate",
	"notebooklm_consumer":       "Consume NotebookLM research outputs: read synthesis files, compute source trends, write structured summaries back to vault",
	"notebooklm_plan_implement": "Research→Grill→Plan→Implement→Verify→Deploy pipeline: NotebookLM deep research, critical review, implementation plan generation, subagent delegation, test verification, and build/deploy",
	"superpowers_workflow":      "Production Superpowers workflow v2: skill routing, grill gate, TDD task loop with review cycles, debug branch, finish options",
	"hermes_update":             "Zero-LLM daily Hermes Agent maintenance: version check, git fetch, run hermes update when behind, report",
	"arc42:docsync":             "Per-section arc42 + README documentation sync: SyncArc42Section01..12 + SyncReadme, each a bounded guideline-constrained Claude pass that updates its file only when the last change affects it",
	"arc42_seeder":              "Seed the next multi-cycle improvement program from the LIVE arc42 quality goals (reads docs/arc42 at runtime, never a copy): one goal targeted per run, grounded goal-named milestones, persisted to programs.json for the goap-fusion loop",
	"self_review":               "Proactive self-review agent: gathers the autonomous commits landed since the last review, runs a read-only Claude Code review over their diffs, and seeds a self-fix:self-review:<sig> code-fix program per CONFIRMED defect for the goap-fusion loop to implement",
	"auction_demo":              "Auction-based A2A task allocation demo: announce a TaskAnnouncement → collect Bids → award to the winning bidder via the AuctionDelegate seam, falling back to a delegate tree when no eligible bidder responds",
	"arc42:section1":            "Generate arc42 section 1 (Introduction and Goals): requirements overview, quality goals, stakeholders — via LLM with template fallback",
	"arc42:section2":            "Generate arc42 section 2 (Architecture Constraints): technical, organizational, and convention constraints as a table",
	"arc42:section3":            "Generate arc42 section 3 (Context and Scope): business and technical context tables for all communication partners and interfaces",
	"arc42:section4":            "Generate arc42 section 4 (Solution Strategy): key technology, decomposition, and quality-achievement decisions",
	"arc42:section5":            "Generate arc42 section 5 (Building Block View): static decomposition of the platform into packages and modules",
	"arc42:section6":            "Generate arc42 section 6 (Runtime View): key runtime scenarios and interaction sequences",
	"arc42:section7":            "Generate arc42 section 7 (Deployment View): infrastructure, nodes, and deployment mapping",
	"arc42:section8":            "Generate arc42 section 8 (Crosscutting Concepts): patterns and concepts applied across the architecture",
	"arc42:section9":            "Generate arc42 section 9 (Architecture Decisions): ADR summaries and decision records",
	"arc42:section10":           "Generate arc42 section 10 (Quality Requirements): quality tree and concrete quality scenarios",
	"arc42:section11":           "Generate arc42 section 11 (Risks and Technical Debt): known risks and technical debt inventory",
	"arc42:section12":           "Generate arc42 section 12 (Glossary): domain and technical term definitions",
}
