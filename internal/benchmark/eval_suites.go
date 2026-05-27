package benchmark

// AllSuites returns all evaluation suites for the BT platform.
// Covers the top 20 use cases for automated recurring task optimization.
func AllSuites() []Suite {
	return []Suite{
		PlatformEvalSuite(),       // 1. Platform self-evaluation + maturity
		GoDevSuite(),              // 2. Go development
		CodeReviewSuite(),         // 3. Code review
		DevOpsSuite(),             // 4. DevOps CI/CD
		SecurityAuditSuite(),      // 5. Security audit
		DataPipelineSuite(),       // 6. Data pipeline
		DeepResearchSuite(),       // 7. Deep research
		ThinkTankSuite(),          // 8. Think tank analysis
		RefactoringSuite(),        // 9. Refactoring
		KnowledgeQASuite(),        // 10. Knowledge QA
		KanbanWorkflowSuite(),     // 11. Kanban workflow
		IncidentInvestigationSuite(), // 12. Incident investigation
		FinanceSuite(),            // 13. Financial analysis
		HealthMonitoringSuite(),   // 14. Health monitoring
		CronJobManagementSuite(),  // 15. Cron job management
		SelfEvolutionSuite(),      // 16. Self-evolution
		MeetingNotesSuite(),       // 17. Meeting notes
		StartupSimulationSuite(),  // 18. Startup simulation
		NotebookLMResearchSuite(), // 19. NotebookLM research
		VaultManagementSuite(),    // 20. Vault management
	}
}

// ─── Eval Suite 1: Platform Maturity Self-Evaluation ────────────────────────

func PlatformEvalSuite() Suite {
	return Suite{
		Name: "platform_eval",
		Tasks: []TaskCase{
			{Task: "evaluate the current platform maturity across all 10 dimensions", ExpectedPath: "AnalysisPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "identify the lowest-scoring dimension and propose concrete improvement", ExpectedPath: "GapAnalysisPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "run the full test suite and report coverage metrics", ExpectedPath: "ValidationPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "audit all behavior trees for max_tokens issues and structural problems", ExpectedPath: "AuditPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 5: Security Audit ───────────────────────────────────────────

func SecurityAuditSuite() Suite {
	return Suite{
		Name: "security_audit",
		Tasks: []TaskCase{
			{Task: "scan this code for OWASP Top 10 vulnerabilities and injection risks", ExpectedPath: "VulnerabilityPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "audit the authentication flow for session hijacking and token reuse", ExpectedPath: "AuthAuditPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "check for exposed secrets, credentials, and API keys in the codebase", ExpectedPath: "SecretScanPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "threat model the API endpoints for CSRF and privilege escalation", ExpectedPath: "ThreatModelPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "review rate limiting and input sanitization implementation", ExpectedPath: "InfraSecurityPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 6: Data Pipeline ────────────────────────────────────────────

func DataPipelineSuite() Suite {
	return Suite{
		Name: "data_pipeline",
		Tasks: []TaskCase{
			{Task: "design an ETL pipeline to transform CSV data into a normalized schema", ExpectedPath: "DesignPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "extract data from the PostgreSQL database and load into the warehouse", ExpectedPath: "ExtractPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "transform raw JSON logs into structured parquet files", ExpectedPath: "TransformPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "validate the data schema against the contract and report mismatches", ExpectedPath: "ValidatePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "optimize the SQL query for the daily aggregation job", ExpectedPath: "OptimizePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 7: Deep Research ────────────────────────────────────────────

func DeepResearchSuite() Suite {
	return Suite{
		Name: "deep_research",
		Tasks: []TaskCase{
			{Task: "research the latest advances in behavior tree optimization algorithms for 2026", ExpectedPath: "LiteratureReviewPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "investigate how Stockfish-style transposition tables can improve BT caching", ExpectedPath: "DeepDivePath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "what are the tradeoffs between genetic algorithms and Q-learning for tree evolution", ExpectedPath: "ComparativePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "find the top 5 papers on memetic algorithms applied to decision trees", ExpectedPath: "DiscoveryPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "synthesize research findings into a weekly report with actionable recommendations", ExpectedPath: "SynthesisPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 8: Think Tank Analysis ──────────────────────────────────────

func ThinkTankSuite() Suite {
	return Suite{
		Name: "think_tank",
		Tasks: []TaskCase{
			{Task: "analyze the strategic implications of AI agent frameworks becoming commodities", ExpectedPath: "MacroAnalysisPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "evaluate the competitive landscape for autonomous coding agents in 2026", ExpectedPath: "MarketAnalysisPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "assess the technical feasibility of recursive self-improving behavior trees", ExpectedPath: "TechFeasibilityPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "identify blind spots in the current platform architecture from a contrarian view", ExpectedPath: "ContrarianPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "synthesize bull, bear, technical, macro, and contrarian perspectives into recommendation", ExpectedPath: "SynthesisPath", ShouldSucceed: true, MinResultLen: 60},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 9: Refactoring ──────────────────────────────────────────────

func RefactoringSuite() Suite {
	return Suite{
		Name: "refactoring",
		Tasks: []TaskCase{
			{Task: "refactor the engine package to reduce cyclomatic complexity", ExpectedPath: "StructuralPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "restructure the evolution module to separate mutation strategies from evaluation", ExpectedPath: "ModularPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "migrate deprecated API calls to the new type contract system", ExpectedPath: "MigrationPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "simplify the condition handler switch statement with a registry pattern", ExpectedPath: "SimplifyPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "improve test readability by extracting shared mock fixtures", ExpectedPath: "TestImprovePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 10: Knowledge QA ────────────────────────────────────────────

func KnowledgeQASuite() Suite {
	return Suite{
		Name: "knowledge_qa",
		Tasks: []TaskCase{
			{Task: "what is the best practice for error handling in Go", ExpectedPath: "AnswerPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "how do behavior tree selectors differ from finite state machines", ExpectedPath: "ExplainPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "why use circuit breakers for MCP server connections", ExpectedPath: "RationalePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "explain the difference between Stockfish evaluation and genetic algorithm fitness", ExpectedPath: "ComparePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "give examples of ensemble methods applied to tree optimization", ExpectedPath: "ExamplePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 11: Kanban Workflow ─────────────────────────────────────────

func KanbanWorkflowSuite() Suite {
	return Suite{
		Name: "kanban_workflow",
		Tasks: []TaskCase{
			{Task: "create a new task card in the backlog for the security audit feature", ExpectedPath: "CreatePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "move the bugfix card from TODO to IN PROGRESS and assign to engineer", ExpectedPath: "TransitionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check the board for stale cards older than 7 days in any column", ExpectedPath: "MonitorPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "validate all cards in QA column have acceptance criteria checked", ExpectedPath: "QAPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "generate the daily standup report from board status", ExpectedPath: "ReportPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 12: Incident Investigation ──────────────────────────────────

func IncidentInvestigationSuite() Suite {
	return Suite{
		Name: "incident_investigation",
		Tasks: []TaskCase{
			{Task: "investigate the timeout error in the production deployment and find root cause", ExpectedPath: "RCAPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "analyze the crash dump from the OOM kill on the gardener process", ExpectedPath: "CrashAnalysisPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "trace the incident timeline from logs and identify the triggering event", ExpectedPath: "TimelinePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "propose a fix and prevention plan for the circuit breaker failure", ExpectedPath: "RemediationPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "assess the blast radius: which services and cron jobs were affected", ExpectedPath: "ImpactPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 14: Health Monitoring ───────────────────────────────────────

func HealthMonitoringSuite() Suite {
	return Suite{
		Name: "health_monitoring",
		Tasks: []TaskCase{
			{Task: "check health of all BT agents and MCP servers", ExpectedPath: "AgentCheckPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "collect system metrics: disk usage, memory, CPU load, process count", ExpectedPath: "MetricsPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "verify all cron jobs are running and report any failures", ExpectedPath: "CronPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "check the dashboard health endpoint and API responsiveness", ExpectedPath: "DashboardPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "generate a system health report with green/yellow/red status for all components", ExpectedPath: "ReportPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 15: Cron Job Management ─────────────────────────────────────

func CronJobManagementSuite() Suite {
	return Suite{
		Name: "cron_management",
		Tasks: []TaskCase{
			{Task: "list all cron jobs and identify any with error status", ExpectedPath: "AuditPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "check for overlapping cron jobs that duplicate functionality", ExpectedPath: "DedupPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "verify all cron job scripts exist and are executable", ExpectedPath: "ValidatePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "pause the failing hermes-update job and diagnose the delivery error", ExpectedPath: "FixPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "optimize cron job schedules to reduce noise from 1-minute polling jobs", ExpectedPath: "OptimizePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 16: Self-Evolution ──────────────────────────────────────────

func SelfEvolutionSuite() Suite {
	return Suite{
		Name: "self_evolution",
		Tasks: []TaskCase{
			{Task: "analyze the current behavior tree for structural weaknesses and dead paths", ExpectedPath: "AnalyzePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "order mutations by expected fitness improvement using Stockfish heuristics", ExpectedPath: "OrderPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "apply the top-ranked add_before mutation to add a quality gate", ExpectedPath: "MutatePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "validate the mutated tree against all benchmark suites", ExpectedPath: "ValidatePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "commit the evolution with fitness delta and rollback capability", ExpectedPath: "CommitPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 17: Meeting Notes ───────────────────────────────────────────

func MeetingNotesSuite() Suite {
	return Suite{
		Name: "meeting_notes",
		Tasks: []TaskCase{
			{Task: "transcribe the team standup meeting and extract action items", ExpectedPath: "TranscribePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "summarize the architecture review meeting into key decisions and tradeoffs", ExpectedPath: "SummarizePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "extract todos and assignees from the sprint planning discussion", ExpectedPath: "ExtractPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "generate meeting minutes with decisions, action items, and next steps", ExpectedPath: "MinutesPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "cross-reference meeting action items with existing kanban cards", ExpectedPath: "CrossRefPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 18: Startup Simulation ──────────────────────────────────────

func StartupSimulationSuite() Suite {
	return Suite{
		Name: "startup_simulation",
		Tasks: []TaskCase{
			{Task: "run a sprint simulation with the engineer and PM trees for feature development", ExpectedPath: "SprintPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "execute a quarterly review with CEO and CTO strategic planning", ExpectedPath: "QuarterPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "simulate a fundraising round with pitch agent and financial models", ExpectedPath: "FundraisePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "generate the company state summary with MRR, runway, and team metrics", ExpectedPath: "SummaryPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "run competitive analysis across 3 competitor products", ExpectedPath: "AnalysisPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 19: NotebookLM Research ─────────────────────────────────────

func NotebookLMResearchSuite() Suite {
	return Suite{
		Name: "notebooklm_research",
		Tasks: []TaskCase{
			{Task: "run 5 daily chat queries on the BT optimization notebook across rotating topics", ExpectedPath: "QueryPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "generate a briefing doc report from the latest research findings", ExpectedPath: "ReportPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "create a mind map from the behavior tree algorithm research", ExpectedPath: "MindMapPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run deep research on the weekly priority topic from the backlog", ExpectedPath: "DeepResearchPath", ShouldSucceed: true, MinResultLen: 50},
			{Task: "save all findings to the vault with proper cross-references and tags", ExpectedPath: "SavePath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}

// ─── Eval Suite 20: Vault Management ────────────────────────────────────────

func VaultManagementSuite() Suite {
	return Suite{
		Name: "vault_management",
		Tasks: []TaskCase{
			{Task: "ingest the session transcript and extract key insights for the vault", ExpectedPath: "IngestPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "synthesize the daily research findings into a wiki note with frontmatter", ExpectedPath: "SynthesizePath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "cross-link orphan pages that have fewer than 2 incoming links", ExpectedPath: "CrossLinkPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "update the _index.md map of content with new pages from this week", ExpectedPath: "IndexPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "run the weekly sweep: review 7 days of notes, extract patterns, prune stale content", ExpectedPath: "SweepPath", ShouldSucceed: true, MinResultLen: 40},
			{Task: "", ExpectedPath: "", ShouldSucceed: false},
		},
	}
}
