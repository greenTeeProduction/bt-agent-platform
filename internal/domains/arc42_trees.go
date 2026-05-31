// Package domains provides domain-specific behavior trees including
// arc42 documentation generation trees for the go-bt-evolve platform.
package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// ─── Arc42 Documentation Generator Trees ────────────────────────────────

// Arc42Trees returns all 13 arc42 generation trees (12 sections + 1 assembly).
func Arc42Trees() map[string]*evolution.SerializableNode {
	return map[string]*evolution.SerializableNode{
		"arc42:section1":  section1IntroGoals(),
		"arc42:section2":  section2Constraints(),
		"arc42:section3":  section3ContextScope(),
		"arc42:section4":  section4SolutionStrategy(),
		"arc42:section5":  section5BuildingBlocks(),
		"arc42:section6":  section6RuntimeView(),
		"arc42:section7":  section7Deployment(),
		"arc42:section8":  section8Concepts(),
		"arc42:section9":  section9Decisions(),
		"arc42:section10": section10Quality(),
		"arc42:section11": section11Risks(),
		"arc42:section12": section12Glossary(),
		"arc42:assemble":  assembleDoc(),
	}
}

func section1IntroGoals() *evolution.SerializableNode {
	return tree(
		seq("Sec1_Main",
			// PreGate: check prerequisites
			seq("PreGate",
				cond("GraphIsFresh", "graphify has been run"),
				act("SetupDocTools", "populate bb.ChainTools with arc42 tools"),
			),
			// Strategy: generate content
			sel("StrategyRouter",
				seq("GenerateIntro",
					act("ReadGraphReport", "load graphify-out/GRAPH_REPORT.md"),
					act("ReadGitHistory", "git log --oneline -30"),
					act("ReadADRs", "read docs/adr/INDEX.md"),
					chain("llm_call:Generate arc42 Section 1 — Introduction and Goals for the go-bt-evolve platform.\n\n1.1 Requirements Overview: Summarize what the platform does — BT execution engine, 41 trees across 7 categories, MCP servers, dashboard at :9800.\n1.2 Quality Goals (top 3): correctness (trees route correctly), evolvability (Stockfish/Pareto/MAP-Elites), reliability (panic recovery, circuit breakers).\n1.3 Stakeholders table: Nico (architect), Hermes Agent (operator), Dashboard users, Cron watchers.\n\nContext from codebase:\nGraph: {{.CachedResult}}\nGit: {{.ChainState.git_history}}\nADRs: {{.ChainState.adrs}}\n\nFormat as arc42 markdown with proper headings.", 2048),
				),
				seq("UseFallback",
					act("FallbackSection1", "use hardcoded template data if LLM unavailable"),
				),
			),
			// PostGate: validate and save
			act("ValidateSection", "check required subsections 1.1, 1.2, 1.3 exist"),
			act("SaveSection", "write to 01-introduction-goals.md"),
			act("MarkSectionDone", "mark section1_done in world state"),
		),
	)
}

func section2Constraints() *evolution.SerializableNode {
	return tree(
		seq("Sec2_Main",
			seq("PreGate",
				cond("GraphIsFresh", "graphify has been run"),
			),
			sel("StrategyRouter",
				seq("GenerateConstraints",
					act("ReadGoMod", "read go.mod for version constraints"),
					act("ReadConfigFiles", "read config for LLM provider constraints"),
					act("DetectHardware", "read /proc/cpuinfo, /proc/meminfo"),
					chain("llm_call:Generate arc42 Section 2 — Architecture Constraints.\n\nTechnical constraints:\n- Go version: {{.ChainState.go_version}}\n- Platform: {{.ChainState.hardware}}\n- MCP transport: stdio only (ADR-002)\n- Persistence: JSON files (ADR-003)\n- LLM: Ollama qwen3.6:35b + DeepSeek v4 fallback\n\nOrganizational constraints:\n- Single developer (Nico)\n- Jetson ARM64 — no x86-specific optimizations\n- Git-versioned trees with conventional commits\n\nConventions:\n- Skill-based documentation\n- Behavior-tree-first execution\n- File-based persistence over SQL\n\nFormat as markdown table with constraint | type | explanation.", 2048),
				),
			),
			act("ValidateSection", "check constraints table present"),
			act("SaveSection", "write to 02-constraints.md"),
			act("MarkSectionDone", "mark section2_done"),
		),
	)
}

func section3ContextScope() *evolution.SerializableNode {
	return tree(
		seq("Sec3_Main",
			seq("PreGate", cond("GraphIsFresh", "")),
			sel("StrategyRouter",
				seq("BusinessContext",
					act("ListExternalAPIs", "scan for HTTP clients, MCP client code"),
					act("ListMCPTools", "count tools per MCP server"),
					chain("llm_call:Generate arc42 Section 3 — Context and Scope.\n\n3.1 Business Context:\nList all communication partners as a table (Partner | Inputs | Outputs):\n- Hermes Agent → tasks, delegated work → results, reflections\n- BT Dashboard users → HTTP requests → HTML/JSON responses\n- Ollama qwen3.6:35b → prompts → completions\n- DeepSeek API → escalated prompts → completions\n- cron job system → scheduled triggers → health reports\n\n3.2 Technical Context:\nDocument interfaces:\n- bt-agent MCP: stdio JSON-RPC 2.0, 32+ tools\n- bt-evaluator MCP: stdio JSON-RPC 2.0\n- bt-langagent MCP: stdio JSON-RPC 2.0\n- bt-dashboard: HTTP :9800, REST API\n- Ollama: HTTP :11434, OpenAI-compatible\n- DeepSeek: HTTPS api.deepseek.com\n\nExternal APIs found: {{.CachedResult}}\nMCP tools: {{.ChainState.mcp_tools}}", 2048),
				),
			),
			act("ValidateSection", "check 3.1 and 3.2 exist"),
			act("SaveSection", "write to 03-context-scope.md"),
			act("MarkSectionDone", "mark section3_done"),
		),
	)
}

func section4SolutionStrategy() *evolution.SerializableNode {
	return tree(
		seq("Sec4_Main",
			seq("PreGate", cond("Section1Done", "section 1 must be complete")),
			sel("StrategyRouter",
				seq("GenerateSolution",
					act("ReadSection1", "read 01-introduction-goals.md for quality goals"),
					act("ReadADRs", "read all ADRs for architectural decisions"),
					chain("llm_call:Generate arc42 Section 4 — Solution Strategy.\n\nCreate a table: Quality Goal | Scenario | Solution Approach | Details.\n\nQuality goals from section 1:\n{{.CachedResult}}\n\nADRs:\n{{.ChainState.adrs}}\n\nKey solution approaches:\n1. Behavior Trees for execution (ADR-001) — Sequence/Selector/Action/Condition/ChainAction\n2. MCP for external interfaces (ADR-002) — JSON-RPC 2.0 over stdio\n3. File-based persistence (ADR-003) — atomic writes, git-friendly\n4. Stockfish evolution for mutation ordering — TT cache, killer moves, alpha-beta\n5. GOAP for planning — world state + actions + goals\n6. ChainAction nodes for LLM integration — 10 chain types\n7. Dashboard for introspection — 8 tabs, embed FS, REST API\n\nFormat as markdown table.", 2048),
				),
			),
			act("ValidateSection", "check solution table present"),
			act("SaveSection", "write to 04-solution-strategy.md"),
			act("MarkSectionDone", "mark section4_done"),
		),
	)
}

func section5BuildingBlocks() *evolution.SerializableNode {
	return tree(
		seq("Sec5_Main",
			seq("PreGate",
				cond("Section1Done", ""),
				cond("Section4Done", ""),
			),
			sel("StrategyRouter",
				// Level 1: overall system decomposition
				seq("Level1_Overall",
					act("ListPackages", "find internal/ -maxdepth 1 -type d"),
					act("ListBinaries", "find cmd/ -name main.go"),
					chain("llm_call:Generate arc42 Section 5 — Building Block View.\n\n5.1 Whitebox Overall System:\nDecompose go-bt-evolve into layers:\n\nPackages: {{.ChainState.packages}}\nBinaries: {{.ChainState.binaries}}\n\nLayer model:\n1. Entrypoints (cmd/): bt-agent, bt-dashboard, bt-evaluator, bt-langagent, bt-gardener, bt-agent-cli, benchcmp\n2. Service Layer (internal/agent, internal/dashboard, internal/workflow, internal/thinktank, internal/startup)\n3. Core Engine (internal/engine): tree building, blackboard, chains (10 types), registry\n4. Evolution Engine (internal/evolution): Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert Knowledge\n5. Knowledge Layer (internal/knowledge, internal/factory): graph, embeddings, tree discovery/creation\n6. Infrastructure (internal/security, internal/reliability, internal/metrics, internal/tracing, internal/config, internal/log, internal/mcp, internal/a2a, internal/reflection)\n\nProvide a responsibility table and an ASCII layer diagram.", 2048),
				),
				// Level 2: Engine whitebox
				seq("Level2_Engine",
					act("ReadEngineCode", "read internal/engine/tree.go structure"),
					chain("llm_call:Generate Level 2 whitebox for internal/engine:\n- BuildTree(): constructs go-bt Command from SerializableNode\n- RunTask(): executes tree tick loop with panic recovery\n- Blackboard: shared state (Task, Plan, Result, Outcome, ChainState, ChainTools)\n- Chains: 10 chain types (llm_call, agent, rag_query, tool_call, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_action)\n- Registry: actionForName/conditionForName — 50+ registered handlers\n- Tree validation, output quality, reflection recording\n\nCode: {{.CachedResult}}", 1024),
				),
				// Level 2: Evolution whitebox
				seq("Level2_Evolution",
					chain("llm_call:Generate Level 2 whitebox for internal/evolution:\n- Stockfish: TranspositionTable + mutation ordering + alpha-beta\n- Pareto: MultiFitness, ParetoFront, ParetoPopulation\n- MAP-Elites: BehavioralDescriptor, MAPElitesGrid\n- Island Model: IslandModel with migration\n- Q-Learning: State→Action epsilon-greedy\n- Expert Knowledge: 6 design patterns, 5 anti-patterns\n- Mutate: 10 mutation operators (add_before, add_after, wrap_retry, prune, etc.)\n- Types: SerializableNode, Individual, Population", 1024),
				),
				// Level 2: Dashboard whitebox
				seq("Level2_Dashboard",
					chain("llm_call:Generate Level 2 whitebox for bt-dashboard:\n- cmd/bt-dashboard/main.go: HTTP server on :9800, embed FS for static files\n- 8 tabs: Overview, ThinkTank, Company, Tasks, Tree View, Evolution, Agents, MindMap\n- API endpoints: /api/summary, /api/tree, /api/agents, /api/tasks, /api/sprint, /api/openapi.json\n- internal/dashboard/: agents.go, executor.go, tasks.go, metrics.go\n- AgentExecutor: shells out to hermes chat for BT task delegation", 1024),
				),
				// Level 3: Chain types detail
				seq("Level3_Chains",
					chain("llm_call:Generate Level 3 detail for chain types:\n10 ChainAction types in internal/engine/chains.go:\n1. llm_call — single LLM invocation with {{.Task}} template\n2. agent — ReAct loop (Thought→Action→Observation→Final Answer)\n3. rag_query — retrieval-augmented QA using bb.KgResults\n4. tool_call — named tool invocation via LLM reasoning\n5. structured_output — JSON output with schema constraint\n6. refine — iterative self-improvement (2 passes)\n7. map_reduce — decompose → process → combine\n8. conversation — multi-turn with memory\n9. retrieval_qa — two-phase retrieve-then-answer\n10. tool_action — direct tool invocation\n\nEach ChainAction reads config from node Name (chain_type:prompt) and Metadata.", 1024),
				),
			),
			act("ValidateSection", "check levels 1, 2, 3 present"),
			act("SaveSection", "write to 05-building-blocks.md"),
			act("MarkSectionDone", "mark section5_done"),
		),
	)
}

func section6RuntimeView() *evolution.SerializableNode {
	return tree(
		seq("Sec6_Main",
			seq("PreGate", cond("Section5Done", "")),
			sel("StrategyRouter",
				seq("TaskExecution",
					chain("llm_call:Generate arc42 Section 6 — Runtime View.\n\n6.1 Task Execution Scenario:\nSequence: Hermes → MCP Server (stdio) → bt-agent → RunTask() → BuildTree() → Tick loop → chainAction → Ollama → result → reflection.\n\n6.2 Evolution Cycle:\nGardener cron → ev_evaluate → ev_order_mutations → apply top mutation → ev_evaluate → compare fitness → accept/rollback → git commit.\n\n6.3 Sprint Execution:\nDashboard POST /api/sprint → Create tasks → goroutine → orch.RunSprint() → engineer tree → Ollama (2-4 min) → mark done → poll /api/sprint/status.\n\n6.4 Error Recovery:\nChainAction panic → SafeGo recover → RecordFailure → CircuitBreaker check → RetryWithBackoff (1s/2s/4s) → DeadLetterQueue if exhausted.\n\nFor each scenario, describe the step-by-step interaction between building blocks.", 2048),
				),
			),
			act("ValidateSection", "check 4 scenarios present"),
			act("SaveSection", "write to 06-runtime-view.md"),
			act("MarkSectionDone", "mark section6_done"),
		),
	)
}

func section7Deployment() *evolution.SerializableNode {
	return tree(
		seq("Sec7_Main",
			seq("PreGate", cond("Section5Done", "")),
			sel("StrategyRouter",
				seq("Deployment",
					act("DetectHardware", "read /proc/cpuinfo, /proc/meminfo, df -h"),
					act("DetectProcesses", "ps aux | grep bt-"),
					chain("llm_call:Generate arc42 Section 7 — Deployment View.\n\n7.1 Infrastructure Level 1:\nHardware: {{.ChainState.hardware}}\nDescribe: Jetson ARM64 (12 cores, 61GB RAM, 57GB eMMC + 1.8TB NVMe)\nProcesses:\n- hermes-gateway → spawns [bt-agent, bt-evaluator, bt-langagent] via stdio MCP\n- bt-dashboard → systemd user service on :9800\n- Ollama → localhost:11434 (qwen3.6:35b)\n- External: api.deepseek.com\n\n7.2 Infrastructure Level 2:\n7.2.1 Process tree: gateway PID → 3 MCP children, dashboard independent\n7.2.2 Storage: ~/.go-bt-evolve/ (agents, logs, history, reflections, scheduler, DLQ), /mnt/ssd/hermes/ (cron outputs, analysis)\n7.2.3 Network: all localhost except DeepSeek API and Tailscale (100.123.73.66)\n\nProcesses: {{.ChainState.processes}}", 2048),
				),
			),
			act("ValidateSection", "check 7.1 and 7.2 present"),
			act("SaveSection", "write to 07-deployment.md"),
			act("MarkSectionDone", "mark section7_done"),
		),
	)
}

func section8Concepts() *evolution.SerializableNode {
	return tree(
		seq("Sec8_Main",
			seq("PreGate", cond("Section5Done", "")),
			sel("StrategyRouter",
				seq("Concepts",
					chain("llm_call:Generate arc42 Section 8 — Crosscutting Concepts.\n\nDocument 8 crosscutting concepts for go-bt-evolve:\n\n8.1 Behavior Tree Execution Model — All agents use Sequence/Selector/Action/Condition/ChainAction nodes. ADR-001.\n\n8.2 ChainAction Nodes — 10 chain types wrap LLM calls. Each reads config from node Name (chain_type:prompt) and Metadata. Template variables: {{.Task}}, {{.Plan}}, {{.Result}}.\n\n8.3 MCP Protocol Layer — All tools exposed via JSON-RPC 2.0 stdio. 3 servers (bt-agent, bt-evaluator, bt-langagent). ADR-002.\n\n8.4 File-Based Persistence — JSON files with atomic writes (write .tmp → rename). ADR-003. Used for agents, scheduler, reflections, DLQ, tree store, TT.\n\n8.5 Evolution Pipeline — Common pattern: evaluate → order mutations → apply → re-evaluate → accept/rollback. 6 algorithms share this.\n\n8.6 Error Resiliency — SafeGo + CircuitBreaker (3-state) + RetryWithBackoff + DeadLetterQueue. Applied across all goroutines.\n\n8.7 Quality Gates — Output validation (min length, error patterns), max_tokens audit, HasClearTask PreGate. Mutation safety gates aspirational.\n\n8.8 Tool Protocol — ChainAction nodes use tool stubs (Name/Description/Call). Tools populated at PreGate via SetupDefaultTools/SetupDevTools/SetupResearchTools.\n\nFor each concept, describe: what, why, where (files), how it affects building blocks.", 3072),
				),
			),
			act("ValidateSection", "check 8 subsections present"),
			act("SaveSection", "write to 08-crosscutting-concepts.md"),
			act("MarkSectionDone", "mark section8_done"),
		),
	)
}

func section9Decisions() *evolution.SerializableNode {
	return tree(
		seq("Sec9_Main",
			seq("PreGate", cond("Section4Done", "")),
			sel("StrategyRouter",
				seq("Decisions",
					act("ReadADRs", "read all docs/adr/ADR-*.md files"),
					chain("llm_call:Generate arc42 Section 9 — Architecture Decisions.\n\nFormat the following ADRs using the Nygard format (Title, Context, Decision, Status, Consequences):\n\n{{.ChainState.adrs}}\n\nRefer to the actual ADR files at docs/adr/INDEX.md.", 2048),
				),
			),
			act("ValidateSection", "check at least 3 ADRs present"),
			act("SaveSection", "write to 09-decisions.md"),
			act("MarkSectionDone", "mark section9_done"),
		),
	)
}

func section10Quality() *evolution.SerializableNode {
	return tree(
		seq("Sec10_Main",
			seq("PreGate", cond("Section1Done", "")),
			sel("StrategyRouter",
				seq("Quality",
					act("ReadSection1", "read quality goals from section 1"),
					chain("llm_call:Generate arc42 Section 10 — Quality Requirements.\n\n10.1 Quality Tree (Q42 hashtags):\n#reliable — 100% panic recovery (SafeGo), circuit breaker, RetryWithBackoff, DLQ\n#evolvable — 6 evolution algorithms, git-versioned trees, benchmark gating\n#secure — rate limiting, API key auth, IP filtering, audit logging, CSRF, security headers, key rotation\n#testable — 71 test files, 24 passing packages, 78% avg coverage, Test Watchdog cron\n#operable — slog structured logging, Prometheus metrics, health endpoint, trace reader\n#flexible — 41 trees across 7 categories, 21-path merged tree, 10 chain types\n\n10.2 Quality Scenarios (long form SEI/Bass+21):\nScenario 1: Agent crash → SafeGo recovers in <1s, DLQ persists task\nScenario 2: 100 evolutions → no fitness drop >20% (aspirational)\nScenario 3: /api/tree returns 41 trees in <500ms\nScenario 4: Test Watchdog detects new failure within 4h\nScenario 5: bt-agent handles 3 concurrent MCP calls\n\nQuality goals from section 1: {{.CachedResult}}", 2048),
				),
			),
			act("ValidateSection", "check 10.1 and 10.2 present"),
			act("SaveSection", "write to 10-quality.md"),
			act("MarkSectionDone", "mark section10_done"),
		),
	)
}

func section11Risks() *evolution.SerializableNode {
	return tree(
		seq("Sec11_Main",
			seq("PreGate", cond("Section1Done", "")),
			sel("StrategyRouter",
				seq("Risks",
					act("ReadGraphReport", "load graphify isolated nodes + god nodes"),
					act("ReadTestCoverage", "go test -coverprofile"),
					act("ReadErrorLogs", "read recent errors from hermes logs"),
					chain("llm_call:Generate arc42 Section 11 — Risks and Technical Debt.\n\nPrioritized risk table:\n\nR1 (HIGH): Mutation Death Spiral — 97.3% mutations regress. No quality gates enforced. Mitigation: implement pre/post fitness comparison gates.\n\nR2 (HIGH): Single Point of Failure — bt-agent is sole task execution path. Mitigation: add worker pool for horizontal scaling.\n\nR3 (MEDIUM): Dead Code — 327 isolated nodes in graphify. Mitigation: Dead Code Sweeper cron removes weekly.\n\nR4 (MEDIUM): Package Sprawl — 36 packages for 136 source files (3.8 ratio). Mitigation: consolidate to ~22 packages.\n\nR5 (MEDIUM): Dashboard Untested — 910-line main.go, 0 tests. Mitigation: add handler tests.\n\nR6 (MEDIUM): MCP+A2A Duplication — two servers with overlapping auth. Mitigation: extract shared server base.\n\nR7 (LOW): DeepSeek API dependency — escalation depends on external API. Mitigation: local fallback always available.\n\nR8 (LOW): Evolution Engine Sprawl — 13 communities for evolution, overlapping strategies. Mitigation: Strategy interface consolidation.\n\nGraph data: {{.CachedResult}}\nCoverage: {{.ChainState.coverage}}\nErrors: {{.ChainState.errors}}\n\nAlso list known technical debt: duplicate utilities (fixed 2026-05-31), silent error suppression (fixed 2026-05-31), DefaultTree god node (extracted 2026-05-31).", 2048),
				),
			),
			act("ValidateSection", "check risk table present"),
			act("SaveSection", "write to 11-risks-debt.md"),
			act("MarkSectionDone", "mark section11_done"),
		),
	)
}

func section12Glossary() *evolution.SerializableNode {
	return tree(
		seq("Sec12_Main",
			seq("PreGate", cond("Section1Done", "")),
			sel("StrategyRouter",
				seq("Glossary",
					act("ScanCodeComments", "grep package comments from internal/"),
					act("ScanTypes", "grep type.*struct from internal/"),
					act("ReadADRs", "extract defined terms from ADRs"),
					chain("llm_call:Generate arc42 Section 12 — Glossary.\n\nCreate a glossary table (Term | Definition) with 35-40 domain and technical terms from the go-bt-evolve platform. Include:\n\nCore BT terms: Behavior Tree, Blackboard, Sequence, Selector, Action, Condition, ChainAction, SerializableNode, PreGate, StrategyRouter, OutcomeSelector\n\nExecution: Tick, BuildTree, RunTask, LLM Interface, Chain Type\n\nEvolution: Stockfish Evolution, Pareto Front, MAP-Elites, Island Model, Q-Learning, Transposition Table, Mutation, Fitness Score, Expert Knowledge\n\nInfrastructure: MCP, A2A, ADR, GOAP, Dashboard, Dead Letter Queue, Circuit Breaker, SafeGo, RetryWithBackoff\n\nTypes from code: {{.ChainState.types}}\nComments: {{.ChainState.comments}}\nADR terms: {{.ChainState.adrs}}\n\nFormat as a markdown table.", 2048),
				),
			),
			act("ValidateSection", "check glossary table with 30+ terms"),
			act("SaveSection", "write to 12-glossary.md"),
			act("MarkSectionDone", "mark section12_done"),
		),
	)
}

func assembleDoc() *evolution.SerializableNode {
	return tree(
		seq("Assemble_Main",
			seq("PreGate",
				cond("AllSectionsDone", "all 12 sections must be complete"),
			),
			sel("StrategyRouter",
				seq("Assemble",
					act("CollectAllSections", "read all 12 section markdown files"),
					act("GenerateTOC", "extract headings for table of contents"),
					chain("llm_call:Generate the final arc42 document by merging all 12 sections with proper frontmatter.\n\nSections:\n{{.CachedResult}}\n\nAdd:\n- YAML frontmatter with title, date, version, status\n- arc42 version reference\n- Table of Contents\n- All 12 sections in order\n- Document metadata footer\n\nOutput as a single markdown file.", 2048),
				),
			),
			act("SaveDocument", "write to go-bt-evolve-arc42.md"),
			act("MarkDocAssembled", "mark doc_assembled = true"),
		),
	)
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func tree(root evolution.SerializableNode) *evolution.SerializableNode {
	return &root
}

func chain(prompt string, maxTokens int) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type: "ChainAction",
		Name: prompt,
		Metadata: map[string]any{
			"max_tokens": float64(maxTokens),
		},
	}
}
