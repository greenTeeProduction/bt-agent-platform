package evolution

// MergedTree builds a universal behavior tree that combines the best patterns
// from all 46 existing trees across 6 categories. It routes any task through
// the most appropriate domain-specific path with quality gates and self-improvement.
//
// Structure:
//   PreGate (6 universal validators)
//   StrategyRouter (21 ranked paths from all domains)
//   QualityGate (output validation)
//   OutcomeSelector (success/retry/escalate)
//   SelfImprove (adapt on failure patterns)
func MergedTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "Merged_Main",
		Children: []SerializableNode{
			// ─── PreGate: Universal input validation ─────────────────────
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "HasClearTask", Description: "Task has context, verb, clear goal (>5 chars, alphabetic)"},
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task"},
					{Type: "Action", Name: "SetupUniversalTools", Description: "web_search, calculator, code_exec, file_ops"},
				},
			},

			// ─── StrategyRouter: 13 domain paths ranked by specificity ──
			{
				Type: "Selector",
				Name: "StrategyRouter",
				Children: []SerializableNode{
					// Path 1: Code Review (most specific)
					{
						Type: "Sequence", Name: "CodeReviewPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsCodeReview", Description: "review/audit/bug/security/style keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Review this code for bugs, security issues, and style problems: {{.Task}}. Provide fixes with before/after examples.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 2: Go Development
					{
						Type: "Sequence", Name: "GoDevPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsGoRelated", Description: "go/golang/.go/goroutine/channel keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Complete this Go development task: {{.Task}}. Use available tools. Provide complete solution.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 3: Finance / Business
					{
						Type: "Sequence", Name: "FinancePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsFinanceTask", Description: "dcf/lbo/valuation/earnings/pitch/kyc/audit keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Complete this financial analysis task: {{.Task}}. Use available tools for research and computation. Provide structured output.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 4: DevOps / CI/CD
					{
						Type: "Sequence", Name: "DevOpsPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsDevOps", Description: "deploy/build/pipeline/ci/cd/docker/kubernetes keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Handle this DevOps task: {{.Task}}. Execute builds, manage deployments, configure pipelines.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 5: Security Audit
					{
						Type: "Sequence", Name: "SecurityPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsSecurityCheck", Description: "security/exploit/vulnerability/penetration/auth keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Perform security analysis: {{.Task}}. Check OWASP Top 10, injection, auth bypass, misconfig. Report findings with severity.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 6: Data Pipeline
					{
						Type: "Sequence", Name: "DataPipelinePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsDataTask", Description: "etl/pipeline/data/transform/extract/load/schema keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Design or fix this data pipeline: {{.Task}}. Consider ETL flow, schema, transformations, error handling.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 7: Research / Analysis
					{
						Type: "Sequence", Name: "ResearchPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsResearchQuery", Description: "research/investigate/analyze/study/explore/find keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Research this topic thoroughly: {{.Task}}. Use web search, synthesize findings, cite sources. Provide executive summary + details.",
								Metadata: map[string]any{"max_tokens": float64(2048)},
							},
						},
					},
					// Path 8: Think Tank Analysis
					{
						Type: "Sequence", Name: "ThinkTankPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsAnalysisTask", Description: "strategy/analysis/foresight/scenario/implications/forecast keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Analyze from multiple perspectives (bull, bear, technical, macro, contrarian): {{.Task}}. Identify theses, antitheses, synthesize into recommendation.",
								Metadata: map[string]any{"max_tokens": float64(2048)},
							},
						},
					},
					// Path 9: Refactoring
					{
						Type: "Sequence", Name: "RefactoringPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsRefactoring", Description: "refactor/restructure/clean/improve/modernize/migrate keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Refactor this code: {{.Task}}. Improve structure, readability, performance. Preserve behavior. Use idiomatic patterns.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 10: Knowledge / Question
					{
						Type: "Sequence", Name: "KnowledgePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsQuestion", Description: "what/how/why/explain/define/difference/best practice keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Answer this question comprehensively: {{.Task}}. Provide examples, context, and references.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 11: Kanban / Workflow
					{
						Type: "Sequence", Name: "WorkflowPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsKanbanTask", Description: "kanban/task/card/board/backlog/sprint/status keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Manage this workflow task: {{.Task}}. Create/update/move cards, check DoR/DoD gates, report status.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 12: Monitoring / Crash Investigation
					{
						Type: "Sequence", Name: "IncidentPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsIncident", Description: "crash/error/timeout/incident/outage/down/broken/failure keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Investigate this incident: {{.Task}}. Find root cause, assess impact, propose fix and prevention.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 13: Health Monitoring
					{
						Type: "Sequence", Name: "HealthPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsHealthCheck", Description: "health/monitoring/capacity/alert keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Monitor system health: {{.Task}}. Check agents, disk, memory, CPU, cron jobs, dashboard. Provide health report with status indicators.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 14: Meeting Notes
					{
						Type: "Sequence", Name: "MeetingPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsMeetingTask", Description: "transcribe/meeting/standup/minutes keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Process meeting: {{.Task}}. Transcribe, extract action items, summarize decisions, assign owners, generate minutes.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 15: Platform Evaluation
					{
						Type: "Sequence", Name: "PlatformEvalPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsPlatformEval", Description: "platform maturity/dimension/gap analysis keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Evaluate the platform: {{.Task}}. Score dimensions, identify gaps, estimate effort, rank by ROI, produce improvement plan.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 16: Cron Job Management
					{
						Type: "Sequence", Name: "CronPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsCronTask", Description: "cron job/audit/capacity/governance keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Manage cron jobs: {{.Task}}. List, audit, optimize schedules, detect failures, propose improvements.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 17: Self-Evolution
					{
						Type: "Sequence", Name: "EvolutionPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsEvolutionTask", Description: "tree fitness/mutation/evolution/ensemble keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Evolve the platform: {{.Task}}. Evaluate fitness, order mutations, apply improvements, validate, commit.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 18: NotebookLM Research
					{
						Type: "Sequence", Name: "NotebookLMPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsNotebookLMTask", Description: "notebooklm/chat query/mind map/research pipeline keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Run NotebookLM research: {{.Task}}. Query notebooks, generate reports, mind maps, artifacts. Save to vault.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 19: Vault Management
					{
						Type: "Sequence", Name: "VaultPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsVaultTask", Description: "vault/ingest/synthesize/cross-link/index keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Manage the vault: {{.Task}}. Ingest, synthesize, cross-link, update indices, run sweeps, maintain knowledge graph.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
						},
					},
					// Path 20: Telegram Clarify — ensure button questions
					{
						Type: "Sequence", Name: "TelegramClarifyPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsTelegram", Description: "telegram platform/messaging/button keywords"},
							{
								Type: "ChainAction",
								Name: "llm_call:Validate this Telegram response: {{.Task}}. If the response contains a question to the user, it MUST use the clarify() tool with multiple-choice buttons (1-4 choices). Check: does the response ask the user something? If yes, was clarify() called with concrete choices? If not, rewrite the response to use clarify(question=..., choices=[...]).",
								Metadata: map[string]any{"max_tokens": float64(400)},
							},
						},
					},
					// Path 21: General (catch-all)
					{
						Type: "Sequence", Name: "GeneralPath",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "llm_call:Complete this task: {{.Task}}. Use available tools. Provide a thorough, complete solution.",
								Metadata: map[string]any{"max_tokens": float64(1024)},
							},
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

			// ─── Reflection ─────────────────────────────────────────────
			{
				Type: "Action",
				Name: "ReflectOnOutcome",
				Description: "Reflect: what went well, what to improve, pattern detection",
			},

			// ─── Outcome Selector ───────────────────────────────────────
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful", Description: "Exit if task succeeded with quality output"},
					{
						Type: "ChainAction",
						Name: "llm_call:Self-correct the previous task. Analyze what went wrong, fix the issues, and produce a corrected solution.",
						Metadata: map[string]any{"max_tokens": float64(1024)},
					},
					{
						Type: "ChainAction",
						Name: "llm_call:Escalate to DeepSeek v4 Pro for difficult task: {{.Task}}. Previous attempt failed. Provide expert-level solution.",
						Metadata: map[string]any{"max_tokens": float64(2048)},
					},
				},
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

// ─── Condition handlers for merged tree ─────────────────────────────────────

// Add these to engine/tree.go conditionForName:

// IsDevOps detects DevOps/CI/CD tasks.
// case "IsDevOps":
//     return func(b *Blackboard) bool {
//         return containsAny(strings.ToLower(b.Task),
//             "deploy", "build", "pipeline", "ci/cd", "ci ", "docker",
//             "kubernetes", "k8s", "terraform", "ansible", "jenkins",
//             "github actions", "gitlab ci", "circleci", "infrastructure")
//     }

// IsDataTask detects data pipeline/ETL tasks.
// case "IsDataTask":
//     return func(b *Blackboard) bool {
//         return containsAny(strings.ToLower(b.Task),
//             "etl", "pipeline", "data ", "transform", "extract",
//             "load", "schema", "dataset", "csv", "parquet", "sql")
//     }

// IsAnalysisTask detects think-tank/strategy analysis tasks.
// case "IsAnalysisTask":
//     return func(b *Blackboard) bool {
//         return containsAny(strings.ToLower(b.Task),
//             "strategy", "analysis", "analyze", "foresight", "scenario",
//             "implications", "forecast", "roadmap", "synthesis")
//     }

// IsRefactoring detects refactoring tasks.
// case "IsRefactoring":
//     return func(b *Blackboard) bool {
//         return containsAny(strings.ToLower(b.Task),
//             "refactor", "restructure", "clean up", "improve",
//             "modernize", "migrate", "simplify")
//     }

// IsQuestion detects knowledge questions.
// case "IsQuestion":
//     return func(b *Blackboard) bool {
//         return containsAny(strings.ToLower(b.Task),
//             "what ", "how ", "why ", "explain", "define",
//             "difference", "compare", "best practice", "example")
//     }

// IsIncident detects crash/incident tasks.
// case "IsIncident":
//     return func(b *Blackboard) bool {
//         return containsAny(strings.ToLower(b.Task),
//             "crash", "error", "timeout", "incident", "outage",
//             "down", "broken", "failure", "panic", "oom")
//     }

// SetupUniversalTools populates bb.ChainTools with universal tool set.
// case "SetupUniversalTools":
//     return func(ctx *btcore.BTContext[Blackboard]) int {
//         bb := ctx.State
//         bb.ChainTools = []any{
//             toolStub{name: "web_search", desc: "Search the web", call: func(q string) string { return "" }},
//             toolStub{name: "code_exec", desc: "Execute code", call: func(q string) string { return "" }},
//             toolStub{name: "file_ops", desc: "Read/write files", call: func(q string) string { return "" }},
//             toolStub{name: "calculator", desc: "Compute math", call: func(q string) string { return "" }},
//         }
//         return 1
//     }
