package evolution

// MergedTree builds a universal behavior tree that combines the best patterns
// from all 46 existing trees across 6 categories. It routes any task through
// the most appropriate domain-specific path with quality gates and self-improvement.
//
// All ChainAction nodes use agent: chains with real tools (shell_exec, file_read,
// http_get) instead of llm_call: — enabling actual tool execution via ReAct agent loops.
//
// Structure:
//   PreGate (6 universal validators + tool setup)
//   StrategyRouter (21 ranked paths from all domains, all agent: chains)
//   QualityGate (output validation)
//   OutcomeSelector (success/retry/escalate)
//   SelfImprove (adapt on failure patterns)
func MergedTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "Merged_Main",
		Children: []SerializableNode{
			// ─── PreGate: Universal input validation + tool setup ───────
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "HasClearTask", Description: "Task has context, verb, clear goal (>5 chars, alphabetic)"},
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task"},
					{Type: "Action", Name: "SetupUniversalTools", Description: "shell_exec, http_get, file_read, process_check, disk_usage, memory_usage"},
				},
			},

			// ─── StrategyRouter: 21 domain paths ranked by specificity ──
			{
				Type: "Selector",
				Name: "StrategyRouter",
				Children: []SerializableNode{
					// Path 1: Code Review
					{
						Type: "Sequence", Name: "CodeReviewPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsCodeReview", Description: "review/audit/bug/security/style keywords"},
							chainAgentNode("agent:Review this code for bugs, security issues, and style problems: {{.Task}}. Use file_read to inspect files, shell_exec to grep for patterns. Provide fixes with before/after examples.", 10, "You are a code review agent. Use file_read and shell_exec to inspect code. Report bugs with line numbers and suggested fixes."),
						},
					},
					// Path 2: Go Development
					{
						Type: "Sequence", Name: "GoDevPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsGoRelated", Description: "go/golang/.go/goroutine/channel keywords"},
							chainAgentNode("agent:Complete this Go development task: {{.Task}}. Use file_read to inspect code, shell_exec to run go build/test/vet. Provide complete working solution.", 10, "You are a Go developer agent. Use shell_exec for go build/test/vet, file_read for code inspection."),
						},
					},
					// Path 3: Finance / Business
					{
						Type: "Sequence", Name: "FinancePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsFinanceTask", Description: "dcf/lbo/valuation/earnings/pitch/kyc/audit keywords"},
							chainAgentNode("agent:Complete this financial analysis task: {{.Task}}. Use shell_exec for data processing, file_read for document inspection. Provide structured output.", 10, "You are a financial analysis agent. Use available tools to gather data and compute results."),
						},
					},
					// Path 4: DevOps / CI/CD
					{
						Type: "Sequence", Name: "DevOpsPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsDevOps", Description: "deploy/build/pipeline/ci/cd/docker/kubernetes keywords"},
							chainAgentNode("agent:Handle this DevOps task: {{.Task}}. Use shell_exec to run builds, deployments, check services. Use http_get for health checks. Use file_read for config files.", 10, "You are a DevOps agent. Use shell_exec, http_get, and file_read to manage infrastructure."),
						},
					},
					// Path 5: Security Audit
					{
						Type: "Sequence", Name: "SecurityPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsSecurityCheck", Description: "security/exploit/vulnerability/penetration/auth keywords"},
							chainAgentNode("agent:Perform security analysis: {{.Task}}. Use file_read to inspect code, shell_exec to grep for patterns (sql injection, hardcoded keys, unsafe exec). Check OWASP Top 10. Report findings with severity.", 10, "You are a security audit agent. Inspect files and code for vulnerabilities. Report each finding with severity and fix."),
						},
					},
					// Path 6: Data Pipeline
					{
						Type: "Sequence", Name: "DataPipelinePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsDataTask", Description: "etl/pipeline/data/transform/extract/load/schema keywords"},
							chainAgentNode("agent:Design or fix this data pipeline: {{.Task}}. Use file_read to inspect data files/schemas, shell_exec to validate transformations. Consider ETL flow, schema, error handling.", 10, "You are a data pipeline agent. Inspect data files and validate pipeline logic."),
						},
					},
					// Path 7: Research / Analysis
					{
						Type: "Sequence", Name: "ResearchPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsResearchQuery", Description: "research/investigate/analyze/study/explore/find keywords"},
							chainAgentNode("agent:Research this topic thoroughly: {{.Task}}. Use file_read for local docs, shell_exec to search/query data, http_get for API access. Synthesize findings, cite sources. Provide executive summary + details.", 10, "You are a research agent. Gather information from all available sources and synthesize a comprehensive answer."),
						},
					},
					// Path 8: Think Tank Analysis
					{
						Type: "Sequence", Name: "ThinkTankPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsAnalysisTask", Description: "strategy/analysis/foresight/scenario/implications/forecast keywords"},
							chainAgentNode("agent:Analyze from multiple perspectives (bull, bear, technical, macro, contrarian): {{.Task}}. Use available tools to gather data. Identify theses, antitheses, synthesize into recommendation.", 10, "You are a strategic analysis agent. Consider multiple perspectives before forming conclusions."),
						},
					},
					// Path 9: Refactoring
					{
						Type: "Sequence", Name: "RefactoringPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsRefactoring", Description: "refactor/restructure/clean/improve/modernize/migrate keywords"},
							chainAgentNode("agent:Refactor this code: {{.Task}}. Use file_read to inspect code, shell_exec to run tests. Improve structure, readability, performance. Preserve behavior. Use idiomatic patterns.", 10, "You are a refactoring agent. Inspect code, run tests to verify behavior preservation, produce improved code."),
						},
					},
					// Path 10: Knowledge / Question
					{
						Type: "Sequence", Name: "KnowledgePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsQuestion", Description: "what/how/why/explain/define/difference/best practice keywords"},
							chainAgentNode("agent:Answer this question comprehensively: {{.Task}}. Use file_read for documentation, shell_exec for data lookup. Provide examples, context, and references.", 10, "You are a knowledgeable assistant. Use available tools to find information and provide thorough answers."),
						},
					},
					// Path 11: Kanban / Workflow
					{
						Type: "Sequence", Name: "WorkflowPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsKanbanTask", Description: "kanban/task/card/board/backlog/sprint/status keywords"},
							chainAgentNode("agent:Manage this workflow task: {{.Task}}. Use file_read to inspect vault/kanban state, shell_exec for git operations. Create/update/move cards, check DoR/DoD gates, report status.", 10, "You are a workflow management agent. Inspect project state and manage tasks."),
						},
					},
					// Path 12: Monitoring / Crash Investigation
					{
						Type: "Sequence", Name: "IncidentPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsIncident", Description: "crash/error/timeout/incident/outage/down/broken/failure keywords"},
							chainAgentNode("agent:Investigate this incident: {{.Task}}. Use shell_exec to check processes/logs, http_get for health endpoints, disk_usage/memory_usage for resource state. Find root cause, assess impact, propose fix and prevention.", 10, "You are an incident investigator agent. Use system tools to diagnose failures and find root causes."),
						},
					},
					// Path 13: Health Monitoring
					{
						Type: "Sequence", Name: "HealthPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsHealthCheck", Description: "health/monitoring/capacity/alert keywords"},
							chainAgentNode("agent:Monitor system health: {{.Task}}. Use disk_usage for / and /mnt/ssd, memory_usage for RAM, process_check for bt-agent/bt-dashboard/bt-gardener, http_get on localhost:9800/api/health. Provide health report with status indicators.", 10, "You are a system health monitor agent. Use real system tools to check disk, memory, processes, and services."),
						},
					},
					// Path 14: Meeting Notes
					{
						Type: "Sequence", Name: "MeetingPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsMeetingTask", Description: "transcribe/meeting/standup/minutes keywords"},
							chainAgentNode("agent:Process meeting: {{.Task}}. Use file_read for transcripts/notes. Transcribe, extract action items, summarize decisions, assign owners, generate minutes.", 10, "You are a meeting processing agent. Extract structured information from meeting materials."),
						},
					},
					// Path 15: Platform Evaluation
					{
						Type: "Sequence", Name: "PlatformEvalPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsPlatformEval", Description: "platform maturity/dimension/gap analysis keywords"},
							chainAgentNode("agent:Evaluate the platform: {{.Task}}. Use file_read for docs/code, shell_exec for tests/metrics, http_get for API endpoints. Score dimensions, identify gaps, estimate effort, rank by ROI, produce improvement plan.", 10, "You are a platform evaluation agent. Inspect code, docs, and metrics to assess maturity."),
						},
					},
					// Path 16: Cron Job Management
					{
						Type: "Sequence", Name: "CronPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsCronTask", Description: "cron job/audit/capacity/governance keywords"},
							chainAgentNode("agent:Manage cron jobs: {{.Task}}. Use shell_exec to list/inspect cron state, file_read for job configs. List, audit, optimize schedules, detect failures, propose improvements.", 10, "You are a cron job management agent. Inspect job configurations and optimize schedules."),
						},
					},
					// Path 17: Self-Evolution
					{
						Type: "Sequence", Name: "EvolutionPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsEvolutionTask", Description: "tree fitness/mutation/evolution/ensemble keywords"},
							chainAgentNode("agent:Evolve the platform: {{.Task}}. Use shell_exec for go test/go build, file_read for tree definitions/metrics. Evaluate fitness, order mutations, apply improvements, validate, commit.", 10, "You are an evolution agent. Test, measure, and improve behavior trees."),
						},
					},
					// Path 18: NotebookLM Research
					{
						Type: "Sequence", Name: "NotebookLMPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsNotebookLMTask", Description: "notebooklm/chat query/mind map/research pipeline keywords"},
							chainAgentNode("agent:Run NotebookLM research: {{.Task}}. Use file_read for vault docs, shell_exec for nlm CLI, http_get for API calls. Query notebooks, generate reports, mind maps, artifacts. Save to vault.", 10, "You are a NotebookLM integration agent. Manage research notebooks and generate artifacts."),
						},
					},
					// Path 19: Vault Management
					{
						Type: "Sequence", Name: "VaultPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsVaultTask", Description: "vault/ingest/synthesize/cross-link/index keywords"},
							chainAgentNode("agent:Manage the vault: {{.Task}}. Use file_read for vault contents, shell_exec for git operations. Ingest, synthesize, cross-link, update indices, run sweeps, maintain knowledge graph.", 10, "You are a vault management agent. Organize and maintain the knowledge vault."),
						},
					},
					// Path 20: Telegram Clarify
					{
						Type: "Sequence", Name: "TelegramClarifyPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsTelegram", Description: "telegram platform/messaging/button keywords"},
							chainAgentNode("agent:Validate this Telegram response: {{.Task}}. If the response contains a question to the user, ensure it would be presented with multiple-choice buttons (2-4 choices). Check: does the response ask the user something? If yes, suggest concrete choices. If not, confirm the response is complete and actionable.", 5, "You validate Telegram message formatting. Ensure questions to users include concrete multiple-choice options."),
						},
					},
					// Path 21: General (catch-all)
					{
						Type: "Sequence", Name: "GeneralPath",
						Children: []SerializableNode{
							chainAgentNode("agent:Complete this task: {{.Task}}. Use available tools (shell_exec, file_read, http_get, process_check, disk_usage, memory_usage). Provide a thorough, complete solution.", 10, "You are a general-purpose AI agent. Use all available tools to complete the task thoroughly."),
						},
					},
				},
			},

			// ─── Quality Gate ───────────────────────────────────────────
			{
				Type: "Action",
				Name: "ValidateOutput",
				Description: "Check output quality: min length, structure, error patterns",
			},

			// ─── Outcome ──────────────────────────────────────────────
			{
				Type: "Action",
				Name: "MarkSuccessful",
				Description: "Mark task as successful — quality gates already validated output",
			},

			// ─── Self-Improvement ───────────────────────────────────────
			{
				Type: "Action",
				Name: "UpdateBehaviorTree",
				Description: "Adapt tree on 3+ consecutive failures; save reflections",
			},
		},
	}
}

// chainAgentNode creates a ChainAction node for the agent: chain type.
func chainAgentNode(name string, maxIter int, systemMsg string) SerializableNode {
	return SerializableNode{
		Type: "ChainAction",
		Name: name,
		Metadata: map[string]any{
			"max_iterations": float64(maxIter),
			"system_msg":     systemMsg,
			"tools":          []any{"shell_exec", "http_get", "file_read", "process_check", "disk_usage", "memory_usage"},
		},
	}
}
