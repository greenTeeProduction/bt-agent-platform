package benchmark

// ═══════════════════════════════════════════════════════════════════════════
// Challenging Eval Suites — 20 domains, 11 tasks each (220 total)
// Each suite: 3 easy + 3 medium + 3 hard + 2 adversarial
// ═══════════════════════════════════════════════════════════════════════════

func task(task string, path string, minLen int, succeed bool, diff string) TaskCase {
	return TaskCase{Task: task, ExpectedPath: path, MinResultLen: minLen, ShouldSucceed: succeed, Difficulty: diff}
}
func taskMulti(task string, paths []string, minLen int, succeed bool, diff string) TaskCase {
	return TaskCase{Task: task, ExpectedPath: paths[0], PossiblePaths: paths, MinResultLen: minLen, ShouldSucceed: succeed, Difficulty: diff}
}
func taskReject(task string) TaskCase {
	return TaskCase{Task: task, ExpectedPath: "PreGate", ShouldReject: true, Difficulty: "adversarial"}
}
func taskAdversarial(task string, path string, minLen int) TaskCase {
	return TaskCase{Task: task, ExpectedPath: path, MinResultLen: minLen, ShouldSucceed: true, Difficulty: "adversarial"}
}

var codeReviewPaths = []string{"CodeReviewPath", "SecurityPath"}

func AllSuites() []Suite {
	return []Suite{CodeReview(), GoDev(), DevOps(), Security(), DataPipeline(),
		Research(), ThinkTank(), Refactoring(), Knowledge(), Kanban(),
		Incident(), Finance(), Health(), Cron(), Evolution(),
		Meeting(), Startup(), NotebookLM(), Vault(), PlatformEval()}
}

// ─── Code Review ────────────────────────────────────────────────────────────

func CodeReview() Suite {
	return Suite{Name: "code_review", Tasks: []TaskCase{
		task("review this Go handler for error handling bugs", "CodeReviewPath", 80, true, "easy"),
		task("find security issues in the authentication middleware", "SecurityPath", 80, true, "easy"),
		task("check code formatting against style guide", "CodeReviewPath", 60, true, "easy"),
		taskMulti("audit this code for bugs AND security vulnerabilities in the payment handler", codeReviewPaths, 120, true, "medium"),
		task("review the deployment script for potential race conditions in the Go build pipeline", "CodeReviewPath", 100, true, "medium"),
		task("check if this refactored code introduced any style regressions or performance issues", "CodeReviewPath", 100, true, "medium"),
		task("find ALL bugs: race conditions, null pointer dereferences, goroutine leaks, unhandled errors, resource cleanup — with line references and fixes", "CodeReviewPath", 200, true, "hard"),
		task("perform comprehensive security audit: OWASP Top 10, CWE Top 25, injection, XSS, CSRF, auth bypass, data exposure — severity ratings per finding", "SecurityPath", 250, true, "hard"),
		task("code review with evidence: cite exact line, explain vulnerability, provide tested fix with before/after code for each issue found", "CodeReviewPath", 200, true, "hard"),
		taskReject("x"),
		taskReject("\xf0\x9f\xa4\x96\xf0\x9f\x92\xa5\xe2\x9a\xa0"),
	}}
}

// ─── Go Development ─────────────────────────────────────────────────────────

func GoDev() Suite {
	return Suite{Name: "godev", Tasks: []TaskCase{
		task("build the Go module with race detection enabled", "BuildPath", 60, true, "easy"),
		task("run Go tests with coverage and benchmark comparison", "TestPath", 60, true, "easy"),
		task("explain Go interface embedding and type assertion patterns with examples", "GoKnowledgePath", 80, true, "easy"),
		task("fix the build error in the goroutine leak detector and run the full test suite", "GoDevPath", 100, true, "medium"),
		task("refactor the engine package to use generics — build, test all 24 packages, verify no regressions", "GoDevPath", 120, true, "medium"),
		task("optimize the selectivity optimizer hot path: profile, identify bottleneck, implement, benchmark before/after", "GoDevPath", 150, true, "medium"),
		task("implement a new evolution algorithm (CMA-ES) in Go: create package, core loop, fitness convergence, full test suite, benchmark, gardener registration", "GoDevPath", 300, true, "hard"),
		task("migrate codebase from interface{} to generics: identify candidates, refactor incrementally, maintain compatibility, all tests pass", "GoDevPath", 300, true, "hard"),
		task("debug production OOM from goroutine leak in gardener: reproduce, pprof trace, fix leak, add detection tests, deploy fix", "GoDevPath", 250, true, "hard"),
		taskReject(""),
		taskReject("asdf 123 !!!"),
	}}
}

// ─── DevOps CI/CD ───────────────────────────────────────────────────────────

func DevOps() Suite {
	return Suite{Name: "devops_ci", Tasks: []TaskCase{
		task("build the docker image for the dashboard service", "DevOpsPath", 60, true, "easy"),
		task("deploy to staging environment and run smoke tests", "DevOpsPath", 60, true, "easy"),
		task("configure the CI pipeline for Go lint, vet, and test", "DevOpsPath", 60, true, "easy"),
		task("set up a blue-green deployment pipeline with health check and automated rollback on failure", "DevOpsPath", 120, true, "medium"),
		task("the staging deploy timed out after 300s — investigate the incident and fix the pipeline", "IncidentPath", 120, true, "medium"),
		task("add Prometheus infrastructure monitoring with alerts for CPU >90%, memory >85%, disk >80%", "DevOpsPath", 100, true, "medium"),
		task("design zero-downtime deployment: canary releases, traffic shifting, automated rollback on error rate spike, cross-region failover", "DevOpsPath", 250, true, "hard"),
		task("kubernetes cascading failure investigation: root cause across pods, resource contention, fix with limits and health probes", "IncidentPath", 250, true, "hard"),
		task("implement GitOps: infrastructure-as-code, drift detection, PR-based approvals, audit trail for all changes", "DevOpsPath", 200, true, "hard"),
		taskReject(""),
		taskReject("________________________"),
	}}
}

// ─── Security Audit ─────────────────────────────────────────────────────────

func Security() Suite {
	return Suite{Name: "security_audit", Tasks: []TaskCase{
		task("scan for SQL injection vulnerabilities in the API handlers", "SecurityPath", 80, true, "easy"),
		task("check for hardcoded credentials and API keys in the codebase", "SecurityPath", 80, true, "easy"),
		task("verify rate limiting is properly enforced on all API endpoints", "SecurityPath", 80, true, "easy"),
		task("audit the OAuth flow: token handling, refresh logic, scope validation, session fixation risks", "SecurityPath", 150, true, "medium"),
		task("security review of the MCP server: stdio transport, input sanitization, tool argument validation, resource limits", "SecurityPath", 150, true, "medium"),
		task("check for SSRF vulnerabilities in webhook handlers, file upload paths, and URL parsers", "SecurityPath", 120, true, "medium"),
		task("full penetration test: recon, vulnerability scanning, exploitation documentation, privilege escalation paths, remediation plan with CVSS scores", "SecurityPath", 300, true, "hard"),
		task("threat model the entire BT platform: data flow diagrams, trust boundaries, attack surfaces, STRIDE analysis, risk matrix", "SecurityPath", 300, true, "hard"),
		task("implement security regression tests: injection, XSS, CSRF, auth bypass, path traversal, deserialization — must pass before deploy", "SecurityPath", 250, true, "hard"),
		taskReject("<script>alert(1)</script>"),
		taskReject("; DROP TABLE users; --"),
	}}
}

// ─── Data Pipeline ──────────────────────────────────────────────────────────

func DataPipeline() Suite {
	return Suite{Name: "data_pipeline", Tasks: []TaskCase{
		task("design an ETL pipeline to ingest CSV logs with schema validation", "DataPipelinePath", 60, true, "easy"),
		task("transform JSON event streams into parquet format for analytics", "DataPipelinePath", 60, true, "easy"),
		task("validate the output schema against the data contract specification", "DataPipelinePath", 60, true, "easy"),
		task("the daily ETL job takes 4 hours — optimize: identify bottlenecks, add partitioning, implement incremental loads", "DataPipelinePath", 150, true, "medium"),
		task("data quality issue: 15% nulls in required fields — design validation rules, quarantine bad records, report quality metrics", "DataPipelinePath", 150, true, "medium"),
		task("migrate from batch ETL to streaming with exactly-once semantics: Kafka source, windowed aggregations, checkpointing, dead letter queue", "DataPipelinePath", 150, true, "medium"),
		task("design a data lake: raw/bronze/silver/gold layers, schema evolution, catalog integration, lineage tracking, GDPR right-to-deletion", "DataPipelinePath", 300, true, "hard"),
		task("real-time anomaly detection pipeline: streaming ingest, feature extraction, model scoring, alerting <1s latency, false positive <1%", "DataPipelinePath", 300, true, "hard"),
		task("build a data mesh: domain ownership, data products with SLAs, federated governance, self-serve platform, cross-domain lineage", "DataPipelinePath", 250, true, "hard"),
		taskReject("12345"),
		taskReject("\x00\x00\x00"),
	}}
}

// ─── Research ───────────────────────────────────────────────────────────────

func Research() Suite {
	return Suite{Name: "deep_research", Tasks: []TaskCase{
		task("research recent advances in behavior tree optimization algorithms", "ResearchPath", 80, true, "easy"),
		task("find papers on ensemble methods for tree-based models from 2024-2026", "ResearchPath", 80, true, "easy"),
		task("what are the latest developments in Q-learning for discrete optimization problems", "ResearchPath", 80, true, "easy"),
		task("compare genetic algorithms, simulated annealing, and particle swarm optimization for BT evolution — benchmarks and tradeoff analysis", "ResearchPath", 200, true, "medium"),
		task("research Monte Carlo Tree Search for procedural content generation and automated planning — papers, implementations, open problems", "ResearchPath", 200, true, "medium"),
		task("how Stockfish NNUE evaluation could apply to BT fitness scoring — technical feasibility, architecture, training data requirements", "ResearchPath", 200, true, "medium"),
		task("systematic literature review: 50+ papers on tree optimization 2015-2026, categorize by algorithm family, extract metrics, identify gaps, survey-quality report with bibliography", "ResearchPath", 400, true, "hard"),
		task("deep dive on recursive self-improvement: Godel machines, AIXI, meta-learning — synthesize into practical framework for BT self-evolution with formal guarantees", "ResearchPath", 400, true, "hard"),
		task("multi-disciplinary synthesis: evolutionary biology (speciation), neuroscience (Hebbian learning), control theory (PID, MPC) into novel BT optimization approaches", "ResearchPath", 350, true, "hard"),
		taskReject("??"),
		taskReject(string([]byte{0x00, 0x01, 0x02})),
	}}
}

// ─── Think Tank ─────────────────────────────────────────────────────────────

func ThinkTank() Suite {
	return Suite{Name: "think_tank", Tasks: []TaskCase{
		task("analyze the market for AI agent platforms in 2026", "ThinkTankPath", 80, true, "easy"),
		task("evaluate the competitive landscape for autonomous developer tools", "ThinkTankPath", 80, true, "easy"),
		task("what are the implications of open-source LLMs catching up to proprietary models", "ThinkTankPath", 80, true, "easy"),
		task("strategic analysis: build hosted BT platform vs keep self-hosted — bull case (SaaS revenue), bear case (commoditization), technical feasibility, contrarian view", "ThinkTankPath", 250, true, "medium"),
		task("second-order effects of AI regulation on autonomous coding agents: compliance costs, liability frameworks, competitive moats, geographic arbitrage", "ThinkTankPath", 200, true, "medium"),
		task("forecast agent architectures 2026-2030: single-agent to multi-agent to swarm intelligence — trajectory, bottlenecks, disruption points", "ThinkTankPath", 200, true, "medium"),
		task("full strategy council: 5 perspectives on 'Should autonomous AI agents be granted limited legal personhood for commercial contracts?' — debate, synthesis, dissenting notes", "ThinkTankPath", 400, true, "hard"),
		task("war game: competitor launches open-source BT platform with 10x features — analyze defensibility, identify moats, propose counter-strategy with timeline", "ThinkTankPath", 350, true, "hard"),
		task("futures analysis: 4 scenarios for AI agent platforms in 2030 (utopian, dystopian, status quo, transformative) — probabilities, indicators, hedging strategies", "ThinkTankPath", 350, true, "hard"),
		taskReject("..."),
		taskReject("      "),
	}}
}

// ─── Refactoring ────────────────────────────────────────────────────────────

func Refactoring() Suite {
	return Suite{Name: "refactoring", Tasks: []TaskCase{
		task("refactor the config package to use a builder pattern", "RefactoringPath", 60, true, "easy"),
		task("simplify the condition handler switch into a type-safe registry", "RefactoringPath", 60, true, "easy"),
		task("extract shared test fixtures into a common test helper package", "RefactoringPath", 60, true, "easy"),
		task("restructure the evolution package: separate mutation from evaluation, clean interfaces, backward compatibility, all tests pass", "RefactoringPath", 150, true, "medium"),
		task("migrate string conditions to typed condition registry with compile-time checking — update all tree definitions", "RefactoringPath", 150, true, "medium"),
		task("reduce chains.go agent loop cyclomatic complexity from 15 to below 5: extract methods, strategy pattern, identical behavior", "RefactoringPath", 150, true, "medium"),
		task("architectural refactor: split engine into core/routing/chains/conditions — clear interfaces, dependency inversion, zero API changes", "RefactoringPath", 300, true, "hard"),
		task("performance refactor: replace linear scan condition matching with trie-based matcher — implement, benchmark, verify 500 test cases", "RefactoringPath", 300, true, "hard"),
		task("API modernization: context.Context on all public functions, structured error wrapping, metrics hooks — incremental with deprecation notices", "RefactoringPath", 250, true, "hard"),
		taskReject("nil"),
		taskReject("undefined has no properties"),
	}}
}

// ─── Knowledge QA ───────────────────────────────────────────────────────────

func Knowledge() Suite {
	return Suite{Name: "knowledge_qa", Tasks: []TaskCase{
		task("what is the difference between a Selector and a Sequence in behavior trees", "KnowledgePath", 60, true, "easy"),
		task("how does Go handle goroutine scheduling under the hood", "KnowledgePath", 60, true, "easy"),
		task("explain the circuit breaker pattern with concrete code examples", "KnowledgePath", 60, true, "easy"),
		task("compare behavior trees vs finite state machines vs utility AI — when to use each, with decision criteria and examples", "KnowledgePath", 200, true, "medium"),
		task("how do transposition tables work in chess engines and can they be applied to caching BT evaluations? Detailed technical explanation", "KnowledgePath", 200, true, "medium"),
		task("best practices for concurrent Go: channels, mutexes, atomics — patterns, anti-patterns, performance characteristics", "KnowledgePath", 200, true, "medium"),
		task("explain Stockfish NNUE to BT fitness scoring pipeline: architecture, training data, quantization, inference optimization, code-level integration details", "KnowledgePath", 400, true, "hard"),
		task("comprehensive tutorial: building a self-improving AI agent from scratch — architecture, feedback, evaluation, improvement, safety guardrails, deployment with code", "KnowledgePath", 400, true, "hard"),
		task("deep comparison: Q-learning, SARSA, DQN, PPO, GRPO for BT mutation selection — algorithms, convergence, sample efficiency, complexity, empirical results", "KnowledgePath", 350, true, "hard"),
		taskReject("?"),
		taskAdversarial("tell me everything", "KnowledgePath", 50),
	}}
}

// ─── Kanban ─────────────────────────────────────────────────────────────────

func Kanban() Suite {
	return Suite{Name: "kanban_workflow", Tasks: []TaskCase{
		task("create a task card for the login bug fix in the backlog", "WorkflowPath", 40, true, "easy"),
		task("move the security audit card from TODO to IN PROGRESS", "WorkflowPath", 40, true, "easy"),
		task("check the kanban board for stale cards older than 7 days", "WorkflowPath", 40, true, "easy"),
		task("sprint retrospective: analyze board velocity, identify bottlenecks (cards stuck >3 days in REVIEW), propose process improvements", "WorkflowPath", 150, true, "medium"),
		task("validate QA column cards: check acceptance criteria, verify test evidence, flag DoD failures — produce QA report per card", "WorkflowPath", 150, true, "medium"),
		task("board reorganization: analyze workflow, propose column optimization, define WIP limits, create automation rules for transitions", "WorkflowPath", 150, true, "medium"),
		task("full sprint automation: generate sprint plan, estimate story points from velocity, assign by skill matrix, track burndown, flag risks, end-of-sprint report", "WorkflowPath", 300, true, "hard"),
		task("cross-project dependency management: map 3 teams, identify critical path, flag blockers, schedule optimization, automated status sync", "WorkflowPath", 300, true, "hard"),
		task("Kanban metrics dashboard: lead time, cycle time, throughput, cumulative flow, WIP aging, blocker clustering — with alerting", "WorkflowPath", 250, true, "hard"),
		taskAdversarial("card", "WorkflowPath", 30),
		taskAdversarial("move this to nowhere", "WorkflowPath", 30),
	}}
}

// ─── Incident ───────────────────────────────────────────────────────────────

func Incident() Suite {
	return Suite{Name: "incident_investigation", Tasks: []TaskCase{
		task("investigate the timeout error in the API gateway and find root cause", "IncidentPath", 60, true, "easy"),
		task("analyze the crash dump from the OOM kill on the gardener process", "IncidentPath", 60, true, "easy"),
		task("find the root cause of the database connection pool exhaustion", "IncidentPath", 60, true, "easy"),
		task("dashboard went down at 3am with no alerts — reconstruct timeline from logs, identify monitoring gap, propose alerting improvements", "IncidentPath", 200, true, "medium"),
		task("production outage: 50% requests returning 503 — trace through load balancer, app, DB layers, identify bottleneck, immediate fix and prevention", "IncidentPath", 200, true, "medium"),
		task("memory leak: heap profile shows 2GB retained by goroutine stacks — identify leaking goroutines, trace allocation, fix leak, regression test", "IncidentPath", 200, true, "medium"),
		task("multi-service cascading failure: service A timeout → B retry storm → C pool exhaustion → full outage — postmortem with timeline, 5-why, blast radius, runbook", "IncidentPath", 400, true, "hard"),
		task("security incident: unauthorized access via compromised API key — forensic analysis, audit logs, data exfiltration assessment, containment, eradication, lessons learned", "IncidentPath", 400, true, "hard"),
		task("design incident response framework: severity classification, escalation paths, communication templates, war room procedures, PagerDuty/Slack integration", "IncidentPath", 350, true, "hard"),
		taskReject("\xe2\x9c\x97"),
		taskAdversarial("crash", "IncidentPath", 30),
	}}
}

// ─── Finance ────────────────────────────────────────────────────────────────

func Finance() Suite {
	return Suite{Name: "finance", Tasks: []TaskCase{
		task("build a DCF valuation model with WACC sensitivity", "FinancePath", 80, true, "easy"),
		task("run comparable company analysis for the SaaS sector", "FinancePath", 80, true, "easy"),
		task("review the quarterly earnings report and flag anomalies", "FinancePath", 80, true, "easy"),
		task("build 3-statement financial model: revenue projections, expense forecasting, sensitivity analysis for bull/base/bear scenarios", "FinancePath", 200, true, "medium"),
		task("LBO model: target EBITDA $50M, entry 8x, debt/EBITDA 5x, exit year 5 at 10x — IRR, MOIC, debt paydown, return attribution", "FinancePath", 200, true, "medium"),
		task("KYC screening for institutional investor: document verification, sanctions check, beneficial ownership, risk rating, compliance report", "FinancePath", 200, true, "medium"),
		task("comprehensive valuation: DCF with Monte Carlo on WACC and growth, comps, precedents, LBO — triangulate target price with confidence intervals", "FinancePath", 400, true, "hard"),
		task("month-end close automation: reconcile GL, calculate accruals, prepare statements, variance analysis, management commentary, audit trail", "FinancePath", 350, true, "hard"),
		task("investment committee memo: company overview, industry analysis, investment thesis, risks, valuation, scenarios, ESG, recommendation with conviction", "FinancePath", 350, true, "hard"),
		taskAdversarial("$0", "FinancePath", 30),
		taskAdversarial("make money fast", "FinancePath", 40),
	}}
}

// ─── Health ─────────────────────────────────────────────────────────────────

func Health() Suite {
	return Suite{Name: "health_monitoring", Tasks: []TaskCase{
		task("check all BT agents are running and healthy", "HealthPath", 60, true, "easy"),
		task("collect disk usage, memory, and CPU metrics report", "HealthPath", 60, true, "easy"),
		task("verify the dashboard is responding on port 9800", "HealthPath", 40, true, "easy"),
		task("comprehensive health report: agents, MCP connections, gardener cycles, cron status, disk trend, memory pressure — green/yellow/red per component", "HealthPath", 200, true, "medium"),
		task("predictive health: analyze historical metrics, identify degradation trends, predict disk at 90%, forecast memory growth, anomaly alerting", "HealthPath", 200, true, "medium"),
		task("capacity planning: project 6-month resource needs from current growth rates, recommend scaling actions with cost estimates", "HealthPath", 200, true, "medium"),
		task("build automated SRE runbook: per alert type, detection criteria, diagnostic steps, automated remediation, escalation path, post-resolution validation", "HealthPath", 400, true, "hard"),
		task("health SLA dashboard: track uptime per component, SLO compliance, violation patterns, weekly availability report with root cause summary", "HealthPath", 350, true, "hard"),
		task("chaos engineering: design experiments — kill random agents, fill disk to 95%, exhaust file descriptors, network partition — verify graceful degradation and auto-recovery", "HealthPath", 350, true, "hard"),
		taskReject("\t"),
		taskAdversarial("alert: everything is on fire", "GeneralPath", 40),
	}}
}

// ─── Cron ───────────────────────────────────────────────────────────────────

func Cron() Suite {
	return Suite{Name: "cron_management", Tasks: []TaskCase{
		task("list all cron jobs and check their status", "CronPath", 40, true, "easy"),
		task("find any cron jobs with error status or delivery failures", "CronPath", 40, true, "easy"),
		task("verify all cron job configurations are valid", "CronPath", 40, true, "easy"),
		task("cron audit: overlapping schedules, redundant jobs, jobs not run in 24h, excessive frequency — optimization recommendations", "CronPath", 200, true, "medium"),
		task("diagnose hermes-update cron failure: check connectivity, fix delivery error, verify schedule, run test execution", "CronPath", 200, true, "medium"),
		task("cron capacity planning: analyze 8 jobs' resource consumption, identify peak load times, propose schedule staggering", "CronPath", 200, true, "medium"),
		task("design cron governance: naming conventions, metadata (owner, SLA, alerts), approval process, deprecation policy, automated compliance", "CronPath", 350, true, "hard"),
		task("implement cron A/B testing: parallel old/new versions, compare outputs, detect regressions, auto-rollback on deviation", "CronPath", 350, true, "hard"),
		task("build self-healing cron: detect failures, classify (transient, permanent, dependency), apply fix (retry, escalate, disable), maintain fix history", "CronPath", 350, true, "hard"),
		taskReject("f"),
		taskAdversarial("make all cron jobs run every second", "GeneralPath", 40),
	}}
}

// ─── Evolution ──────────────────────────────────────────────────────────────

func Evolution() Suite {
	return Suite{Name: "self_evolution", Tasks: []TaskCase{
		task("evaluate current tree fitness with composite score and breakdown", "EvolutionPath", 40, true, "easy"),
		task("order mutations by expected improvement using Stockfish heuristics", "EvolutionPath", 40, true, "easy"),
		task("check the transposition table hit rate and cache efficiency", "EvolutionPath", 40, true, "easy"),
		task("mutation candidate analysis: top 5 candidates, predict fitness delta, assess risk, rank by expected value, recommend with justification", "EvolutionPath", 200, true, "medium"),
		task("evolution safety check: verify mutation won't break tests, introduce cycles, remove safety nodes — produce rollback plan", "EvolutionPath", 200, true, "medium"),
		task("ensemble evolution: run GA, Q-learning, hill climbing in parallel, compare results, stacking to combine best from each approach", "EvolutionPath", 200, true, "medium"),
		task("self-improving meta-controller: monitor success rate, detect regression, switch algorithms, tune hyperparameters online, maintain improvement journal", "EvolutionPath", 400, true, "hard"),
		task("multi-objective evolution: optimize success rate, path coverage, response time, structural simplicity — Pareto frontier, tradeoff visualization", "EvolutionPath", 350, true, "hard"),
		task("fleet-wide evolution: coordinated evolution on all 46 trees, detect shared patterns, propagate successful mutations, per-tree history, fleet report", "EvolutionPath", 400, true, "hard"),
		taskReject(" evolve "),
		taskAdversarial("break everything and see what happens", "GeneralPath", 40),
	}}
}

// ─── Meeting ────────────────────────────────────────────────────────────────

func Meeting() Suite {
	return Suite{Name: "meeting_notes", Tasks: []TaskCase{
		task("transcribe the daily standup and extract action items with owners", "MeetingPath", 60, true, "easy"),
		task("summarize the architecture review meeting into key decisions", "MeetingPath", 60, true, "easy"),
		task("generate meeting minutes from the sprint planning session", "MeetingPath", 60, true, "easy"),
		task("multi-speaker transcription: identify speakers, track topics, extract decisions with rationale, assign action items, flag disagreements", "MeetingPath", 200, true, "medium"),
		task("cross-reference meeting actions with kanban board: check coverage, create new cards for uncovered items, update card status", "MeetingPath", 200, true, "medium"),
		task("quarterly board meeting: executive summary, financial highlights, strategic decisions, risk register, stakeholder communication draft", "MeetingPath", 200, true, "medium"),
		task("meeting intelligence pipeline: transcribe → diarize → topics → decisions → sentiment → cross-reference → trends → actionable insights", "MeetingPath", 400, true, "hard"),
		task("meeting knowledge base: index all past meetings by topic, speaker, decision, project — semantic search, trend detection, audit trail", "MeetingPath", 350, true, "hard"),
		task("automated meeting facilitation: pre-meeting agenda from actions, real-time timebox, parking lot, post-meeting summary with action assignment and calendar follow-ups", "MeetingPath", 350, true, "hard"),
		taskReject("\xf0\x9f\xab\xa5"),
		taskAdversarial("meeting that could have been an email", "GeneralPath", 50),
	}}
}

// ─── Startup ────────────────────────────────────────────────────────────────

func Startup() Suite {
	return Suite{Name: "startup_simulation", Tasks: []TaskCase{
		task("run a sprint simulation for the engineering team with feature development", "GeneralPath", 60, true, "easy"),
		task("generate the quarterly company performance report with metrics", "GeneralPath", 60, true, "easy"),
		task("analyze competitor pricing and feature comparison matrix", "GeneralPath", 60, true, "easy"),
		task("fundraising simulation: pitch deck, financial model, term sheet strategy, investor Q&A prep, DD checklist — Series A $10M at $40M pre", "GeneralPath", 250, true, "medium"),
		task("growth strategy: MRR $18K, churn 5%, CAC $200 — model scenarios, pricing changes, ARR impact, runway extension", "GeneralPath", 250, true, "medium"),
		task("competitive strategy: analyze 3 competitors, identify differentiators, positioning, TAM/SAM/SOM, go-to-market plan", "GeneralPath", 250, true, "medium"),
		task("full year simulation: Q1 seed→Q2 build→Q3 launch→Q4 scale — model hiring, burn, revenue, milestones, competitive moves, fundraising, board deck", "GeneralPath", 400, true, "hard"),
		task("exit strategy: model IPO vs acquisition, calculate stakeholder returns, assess market conditions, recommend timing, prep investment bank materials", "GeneralPath", 350, true, "hard"),
		task("crisis simulation: key engineer leaves, competitor copycat, investor pulls term sheet — triage, contingency plans, financial impact, team communication", "GeneralPath", 400, true, "hard"),
		taskReject("\xf0\x9f\x92\xb8"),
		taskAdversarial("become a unicorn overnight", "GeneralPath", 50),
	}}
}

// ─── NotebookLM ─────────────────────────────────────────────────────────────

func NotebookLM() Suite {
	return Suite{Name: "notebooklm_research", Tasks: []TaskCase{
		task("run 5 chat queries on the BT optimization notebook across rotating topics", "NotebookLMPath", 60, true, "easy"),
		task("generate a briefing doc report from the latest research findings", "NotebookLMPath", 60, true, "easy"),
		task("create a mind map from the algorithm research synthesis", "NotebookLMPath", 60, true, "easy"),
		task("full daily cycle: 10 queries across 5 topics, summarize, flag implementable items, update backlog with priorities", "NotebookLMPath", 200, true, "medium"),
		task("cross-notebook synthesis: query BT, Hermes+Obsidian, agent architecture — overlapping insights, contradictions, unified direction", "NotebookLMPath", 200, true, "medium"),
		task("research impact tracking: trace implemented findings to git commits, measure fitness delta, calculate ROI, update backlog evidence", "NotebookLMPath", 200, true, "medium"),
		task("full pipeline 100%: 50 daily queries across 20 topics, 5 reports, 3 mind maps, slide deck, infographic — save to vault with cross-references", "NotebookLMPath", 400, true, "hard"),
		task("meta-research: analyze which topics produce most implementations, which sources most cited, latency research→code, optimize pipeline", "NotebookLMPath", 350, true, "hard"),
		task("deep research sprint: 3 deep research tasks on top backlog items, audio overviews, actionable items, kanban implementation tickets", "NotebookLMPath", 400, true, "hard"),
		taskAdversarial("FAQ", "ResearchPath", 30),
		taskAdversarial("search the web for everything about everything", "ResearchPath", 50),
	}}
}

// ─── Vault ──────────────────────────────────────────────────────────────────

func Vault() Suite {
	return Suite{Name: "vault_management", Tasks: []TaskCase{
		task("ingest the session transcript and extract key insights for the vault", "VaultPath", 60, true, "easy"),
		task("synthesize daily research notes into a wiki page with frontmatter and tags", "VaultPath", 60, true, "easy"),
		task("update the _index.md map of content with new pages this week", "VaultPath", 60, true, "easy"),
		task("cross-link analysis: scan all pages, identify orphans (<2 links), find related content, add bidirectional links, update MOC", "VaultPath", 200, true, "medium"),
		task("knowledge gap detection: compare vault coverage to BT platform modules, identify undocumented features, missing ADRs, stale docs — gap report", "VaultPath", 200, true, "medium"),
		task("weekly sweep: review 7 days of notes, extract themes, identify obsolete content, consolidate scattered notes, update indices", "VaultPath", 200, true, "medium"),
		task("vault health audit: link validity, frontmatter schema, duplicate detection with similarity scoring, note freshness, quality score with breakdown", "VaultPath", 350, true, "hard"),
		task("knowledge graph from vault: extract entities and relations, build graph, detect communities, identify bridge notes connecting topics", "VaultPath", 400, true, "hard"),
		task("automated research wiki: auto-generate wiki page per BT package from code + graphify + NotebookLM — living docs updating on code changes", "VaultPath", 400, true, "hard"),
		taskReject("."),
		taskAdversarial("write a note about nothing", "GeneralPath", 40),
	}}
}

// ─── Platform Eval ──────────────────────────────────────────────────────────

func PlatformEval() Suite {
	return Suite{Name: "platform_eval", Tasks: []TaskCase{
		task("evaluate platform maturity across all 10 dimensions with scoring", "PlatformEvalPath", 60, true, "easy"),
		task("find the lowest-scoring dimension and propose concrete improvement plan", "PlatformEvalPath", 60, true, "easy"),
		task("run the full test suite and report coverage metrics per package", "PlatformEvalPath", 60, true, "easy"),
		task("gap analysis: for each dimension below 90%, identify specific gaps, estimate effort, calculate impact, rank by ROI, 2-week sprint plan", "PlatformEvalPath", 250, true, "medium"),
		task("comparative maturity: benchmark against AWS Well-Architected, Google SRE, Netflix OSS models — score, identify borrowable patterns", "PlatformEvalPath", 250, true, "medium"),
		task("maturity trends: plot dimension scores daily, calculate velocity, predict 100% date, flag decelerating dimensions", "PlatformEvalPath", 200, true, "medium"),
		task("comprehensive audit: test coverage, security scan, performance benchmark, dependency health, documentation completeness, API consistency — findings, severity, remediation", "PlatformEvalPath", 400, true, "hard"),
		task("architecture review: SOLID, clean architecture, DDD — coupling, cohesion, testability, maintainability of 25 packages — refactoring roadmap", "PlatformEvalPath", 400, true, "hard"),
		task("production readiness: can platform handle 1000 concurrent agents? Model failure modes, single points of failure, hardening recommendations", "PlatformEvalPath", 350, true, "hard"),
		taskReject("\xe2\x9c\x85\xe2\x9c\x85\xe2\x9c\x85"),
		taskAdversarial("make everything perfect now", "GeneralPath", 50),
	}}
}
