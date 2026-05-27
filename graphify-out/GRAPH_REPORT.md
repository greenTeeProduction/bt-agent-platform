# Graph Report - go-bt-evolve  (2026-05-27)

## Corpus Check
- 130 files · ~145,971 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1857 nodes · 4308 edges · 72 communities (61 shown, 11 thin omitted)
- Extraction: 65% EXTRACTED · 35% INFERRED · 0% AMBIGUOUS · INFERRED: 1499 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `19ac4e19`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- [[_COMMUNITY_Community 0|Community 0]]
- [[_COMMUNITY_Community 1|Community 1]]
- [[_COMMUNITY_Community 2|Community 2]]
- [[_COMMUNITY_Community 3|Community 3]]
- [[_COMMUNITY_Community 4|Community 4]]
- [[_COMMUNITY_Community 5|Community 5]]
- [[_COMMUNITY_Community 6|Community 6]]
- [[_COMMUNITY_Community 7|Community 7]]
- [[_COMMUNITY_Community 8|Community 8]]
- [[_COMMUNITY_Community 9|Community 9]]
- [[_COMMUNITY_Community 10|Community 10]]
- [[_COMMUNITY_Community 11|Community 11]]
- [[_COMMUNITY_Community 12|Community 12]]
- [[_COMMUNITY_Community 13|Community 13]]
- [[_COMMUNITY_Community 14|Community 14]]
- [[_COMMUNITY_Community 15|Community 15]]
- [[_COMMUNITY_Community 16|Community 16]]
- [[_COMMUNITY_Community 17|Community 17]]
- [[_COMMUNITY_Community 18|Community 18]]
- [[_COMMUNITY_Community 19|Community 19]]
- [[_COMMUNITY_Community 20|Community 20]]
- [[_COMMUNITY_Community 21|Community 21]]
- [[_COMMUNITY_Community 22|Community 22]]
- [[_COMMUNITY_Community 23|Community 23]]
- [[_COMMUNITY_Community 24|Community 24]]
- [[_COMMUNITY_Community 25|Community 25]]
- [[_COMMUNITY_Community 26|Community 26]]
- [[_COMMUNITY_Community 27|Community 27]]
- [[_COMMUNITY_Community 28|Community 28]]
- [[_COMMUNITY_Community 29|Community 29]]
- [[_COMMUNITY_Community 30|Community 30]]
- [[_COMMUNITY_Community 31|Community 31]]
- [[_COMMUNITY_Community 32|Community 32]]
- [[_COMMUNITY_Community 33|Community 33]]
- [[_COMMUNITY_Community 34|Community 34]]
- [[_COMMUNITY_Community 35|Community 35]]
- [[_COMMUNITY_Community 36|Community 36]]
- [[_COMMUNITY_Community 37|Community 37]]
- [[_COMMUNITY_Community 38|Community 38]]
- [[_COMMUNITY_Community 39|Community 39]]
- [[_COMMUNITY_Community 40|Community 40]]
- [[_COMMUNITY_Community 41|Community 41]]
- [[_COMMUNITY_Community 42|Community 42]]
- [[_COMMUNITY_Community 43|Community 43]]
- [[_COMMUNITY_Community 44|Community 44]]
- [[_COMMUNITY_Community 45|Community 45]]
- [[_COMMUNITY_Community 46|Community 46]]
- [[_COMMUNITY_Community 47|Community 47]]
- [[_COMMUNITY_Community 48|Community 48]]
- [[_COMMUNITY_Community 49|Community 49]]
- [[_COMMUNITY_Community 50|Community 50]]
- [[_COMMUNITY_Community 51|Community 51]]
- [[_COMMUNITY_Community 52|Community 52]]
- [[_COMMUNITY_Community 53|Community 53]]
- [[_COMMUNITY_Community 54|Community 54]]
- [[_COMMUNITY_Community 55|Community 55]]
- [[_COMMUNITY_Community 56|Community 56]]
- [[_COMMUNITY_Community 57|Community 57]]
- [[_COMMUNITY_Community 58|Community 58]]
- [[_COMMUNITY_Community 59|Community 59]]
- [[_COMMUNITY_Community 60|Community 60]]
- [[_COMMUNITY_Community 61|Community 61]]
- [[_COMMUNITY_Community 62|Community 62]]
- [[_COMMUNITY_Community 63|Community 63]]
- [[_COMMUNITY_Community 64|Community 64]]
- [[_COMMUNITY_Community 65|Community 65]]
- [[_COMMUNITY_Community 66|Community 66]]
- [[_COMMUNITY_Community 67|Community 67]]
- [[_COMMUNITY_Community 68|Community 68]]
- [[_COMMUNITY_Community 71|Community 71]]

## God Nodes (most connected - your core abstractions)
1. `BuildTree()` - 71 edges
2. `RunTask()` - 65 edges
3. `DefaultTree()` - 56 edges
4. `contains()` - 54 edges
5. `main()` - 54 edges
6. `NewKnowledgeGraph()` - 52 edges
7. `GoDeveloperTree()` - 50 edges
8. `NewStore()` - 32 edges
9. `DefaultMock()` - 31 edges
10. `TestIntegration_AllTreesExecute()` - 31 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `NewFactory()`  [INFERRED]
  cmd/bt-agent/main.go → internal/knowledge/factory.go
- `main()` --calls--> `AutoCreateTree()`  [INFERRED]
  cmd/bt-agent/main.go → internal/knowledge/factory.go
- `main()` --calls--> `NewKnowledgeGraph()`  [INFERRED]
  cmd/bt-agent/main.go → internal/knowledge/graph.go
- `main()` --calls--> `BuildKnowledgeGraph()`  [INFERRED]
  cmd/bt-dashboard/main.go → internal/knowledge/registry.go
- `main()` --calls--> `BuildKnowledgeGraph()`  [INFERRED]
  cmd/bt-agent/main.go → internal/knowledge/registry.go

## Communities (72 total, 11 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.05
Nodes (86): detectPath(), RunSuite(), EvaluateBTPG(), EvaluateGAIA(), max1(), EvaluateSWEVerified(), main(), main() (+78 more)

### Community 1 - "Community 1"
Cohesion: 0.05
Nodes (69): cloneTree(), buildGoapStepPrompt(), floatField(), getStringSlice(), planStepsToStrings(), registerGoapNodes(), stringField(), worldStateFromMap() (+61 more)

### Community 2 - "Community 2"
Cohesion: 0.06
Nodes (70): TestBTPG_QualityMetrics_AllDomainTrees(), TestBFCL_AllDomainTrees_Accuracy(), resolveTree(), act(), AgentMonitorTree(), AllDomainTrees(), CodeReviewTree(), cond() (+62 more)

### Community 3 - "Community 3"
Cohesion: 0.06
Nodes (50): BTOptimizer, ChildStats, TestBTOptimizer_New(), TestDTAnalyzer_New(), TestLocalSearcher_New(), TestSelectorOptimizer_New(), collectSelectors(), conditionOverlap() (+42 more)

### Community 4 - "Community 4"
Cohesion: 0.05
Nodes (38): Catalog, extractYAMLField(), inferTree(), splitTags(), TestInferTree(), CatalogEntry, TestCheckConfidence_ConditionExists(), TestCrossover_Single() (+30 more)

### Community 5 - "Community 5"
Cohesion: 0.05
Nodes (56): avgBranchingFactor(), BTPGQualityScore(), BTPGTreeSummary(), countNodes(), isEdgeCaseTask(), maxDepth(), TestBTPG_NilTree(), TestBTPG_QualityMetrics_CodeReviewTree() (+48 more)

### Community 6 - "Community 6"
Cohesion: 0.05
Nodes (30): agentTestMockLLM, chainMockLLM, mockLLM, retryMockLLM, Evaluator, EvaluatorConfig, Analyzer, extractJSON() (+22 more)

### Community 7 - "Community 7"
Cohesion: 0.06
Nodes (56): authMiddleware(), main(), Debug(), Error(), Info(), L(), Warn(), auditKey (+48 more)

### Community 8 - "Community 8"
Cohesion: 0.07
Nodes (50): TestBreed(), TestBreed_FromArchetype(), TestBreed_FromArchetype_Fallback(), TestBreed_NoParents(), TestBreed_TooFewParents(), TestBuildFromArchetype(), TestConnect(), TestConnect_Duplicate() (+42 more)

### Community 9 - "Community 9"
Cohesion: 0.05
Nodes (45): TestClient_AnalyzeComplexity_FallbackOnError(), TestClient_AnalyzeComplexity_High(), TestClient_Generate_ConnectionRefused(), TestClient_GeneratePlan_FallbackOnError(), TestClient_Reflect_FallbackOnError(), TestClient_Reflect_FallbackSections(), TestClient_Reflect_OnlyWentWell(), TestDeepSeekClient_AnalyzeComplexity() (+37 more)

### Community 10 - "Community 10"
Cohesion: 0.08
Nodes (29): AgentRunner, Checkpoint, NewHistory(), TestHistory_AllStats(), TestHistory_Cleanup(), TestScheduler_RemoveNonexistent(), TestScheduler_UnknownAgent(), RunContext (+21 more)

### Community 11 - "Community 11"
Cohesion: 0.09
Nodes (39): ABDelta, ABTest, AgentMonitorSuite(), AllSuites(), chi2CDF(), CodeReviewSuite(), cohensD(), containsStr() (+31 more)

### Community 12 - "Community 12"
Cohesion: 0.07
Nodes (11): GardenerRunCycleTool, mockAgentTool, toolStub, CreateAgentTool, EvolveTool, FitnessTool, GetReflectionsTool, GetTreeTool (+3 more)

### Community 13 - "Community 13"
Cohesion: 0.09
Nodes (28): TestTT_NonExistentDir(), NewTranspositionTable(), perTreeStats(), setupGardener(), TestEvolveTree_BloatGuard(), TestEvolveTree_MultipleTrees(), TestEvolveTree_NilTree(), TestEvolveTree_WithRealTree() (+20 more)

### Community 14 - "Community 14"
Cohesion: 0.13
Nodes (28): History, splitLines(), RunRecord, RunStats, Duration(), ChainConfig, ChainKind, BuildChainAction() (+20 more)

### Community 15 - "Community 15"
Cohesion: 0.06
Nodes (33): dart:convert, _ActivityCard, _ApproveButton, BTStudioApp, build, _buildBody, _buildOverview, Card (+25 more)

### Community 16 - "Community 16"
Cohesion: 0.11
Nodes (19): Definition, InputSpec, Instance, OutputSpec, QualitySpec, Registry, NewRegistry(), State (+11 more)

### Community 17 - "Community 17"
Cohesion: 0.09
Nodes (28): collectNames(), TestBuildTree_Minimal(), TestBuildTree_UnknownType(), TestTree_AllDomain(), TestTree_AllEvolution(), TestTree_AllFinance(), TestTree_DefaultStructure(), TestTree_GoDevStructure() (+20 more)

### Community 18 - "Community 18"
Cohesion: 0.14
Nodes (13): skillName(), TestAutoCreateTree_ConfidenceThreshold(), TestAutoCreateTree_Existing(), TestAutoCreateTree_New(), TestDetermineCategory(), TestExtractKeywords(), TestTruncateTask(), Factory (+5 more)

### Community 19 - "Community 19"
Cohesion: 0.12
Nodes (13): parseChainConfig(), TestChainAction_ParseConfig(), TestEnsemble_New(), Ensemble, BestOfN(), jaccardSimilarity(), NewEnsemble(), StackedEnsemble() (+5 more)

### Community 20 - "Community 20"
Cohesion: 0.13
Nodes (26): airlineTools(), buildTauBenchTask(), BuiltinTauBenchAirline(), BuiltinTauBenchRetail(), DefaultTauBenchEntries(), EvaluateTauBench(), LoadTauBenchTasks(), matchActions() (+18 more)

### Community 21 - "Community 21"
Cohesion: 0.12
Nodes (25): NewCircuitBreaker(), NewConcurrencyLimiter(), NewDeadLetterQueue(), NewPriorityQueue(), TestCircuitBreaker_Closed(), TestCircuitBreaker_FailsInHalfOpen(), TestCircuitBreaker_HalfOpen(), TestCircuitBreaker_OpensAfterThreshold() (+17 more)

### Community 22 - "Community 22"
Cohesion: 0.19
Nodes (21): contains(), saveRecordWithDelay(), TestBuildEvolvedPrompt(), TestCreateAgentTool_Call_NilFactory(), TestEvolveTool_Call_NotEnoughFailures(), TestEvolveTool_Call_NoTree(), TestEvolveTool_Call_Success(), TestFitnessTool_Call() (+13 more)

### Community 23 - "Community 23"
Cohesion: 0.1
Nodes (14): AgentMetrics, AgentStats, Counter, Gauge, HealthResponse, Histogram, init(), MetricsJSON() (+6 more)

### Community 24 - "Community 24"
Cohesion: 0.09
Nodes (9): Config, envBool(), envFloat(), envInt(), envStr(), Load(), TestEnvBool(), ValidationError (+1 more)

### Community 25 - "Community 25"
Cohesion: 0.11
Nodes (16): Config, TestBaseNodeCount(), TestGetRetryCount(), TestHasNodeNamed(), TestIsNodeWrapped(), TestMaxInt(), CycleMetrics, Gardener (+8 more)

### Community 26 - "Community 26"
Cohesion: 0.14
Nodes (3): DeadLetterQueue, PriorityQueue, TaskQueue

### Community 27 - "Community 27"
Cohesion: 0.21
Nodes (23): makeRecords(), TestCloneTree_Independent(), TestEvaluateTree_AllFailures(), TestEvaluateTree_EmptyRecords(), TestEvaluateTree_GoDevVsDefault(), TestEvaluateTree_Mixed(), TestEvaluateTree_NodeCountPenalty(), TestEvaluateTree_Perfect() (+15 more)

### Community 28 - "Community 28"
Cohesion: 0.12
Nodes (20): TestApplyMutations_Batch(), TestCountNodes(), TestDefaultTree_Structure(), TestMutation_AddAfter(), TestMutation_AddBefore(), TestMutation_AddFallback(), TestMutation_IncreaseRetries(), TestMutation_PruneNode() (+12 more)

### Community 29 - "Community 29"
Cohesion: 0.13
Nodes (18): BuiltinBFCLV3(), EvaluateBFCLV3(), isToolMatch(), LoadBFCLV3Entries(), LoadBFCLV3MultiTurn(), minInt(), TestBFCLV3_LongContext(), TestBFCLV3_MultiTurn_Basic() (+10 more)

### Community 30 - "Community 30"
Cohesion: 0.16
Nodes (19): main(), DeepeningResult, FitnessScore, MutationCandidate, cloneTree(), containsWord(), countSelectors(), estimatePathCoverage() (+11 more)

### Community 31 - "Community 31"
Cohesion: 0.15
Nodes (3): TranspositionTable, TreeStore, Store

### Community 32 - "Community 32"
Cohesion: 0.15
Nodes (14): DefaultFellows(), NewThinkTank(), TestDefaultFellows(), TestFellowConfidence(), TestFullAnalysis_MultipleTopics(), TestNewThinkTank(), TestOrchestrator_Debate(), TestOrchestrator_EmptyTopic() (+6 more)

### Community 33 - "Community 33"
Cohesion: 0.34
Nodes (15): extractSection(), extractBulletPoints(), extractFirstLine(), extractListSection(), extractSection(), findNextSection(), parseDebateTranscript(), parseProbability() (+7 more)

### Community 34 - "Community 34"
Cohesion: 0.15
Nodes (5): Approval, sortTasks(), Priority, TaskStatus, Workflow

### Community 35 - "Community 35"
Cohesion: 0.11
Nodes (18): 1. Prerequisites, 2. Install, 3. Run tests, 4. Start the dashboard, 5. Run your first task, API Endpoints, Architecture, code:bash (git clone https://github.com/nico/go-bt-evolve.git) (+10 more)

### Community 36 - "Community 36"
Cohesion: 0.3
Nodes (17): readMessages(), TestInitialize(), TestNotification_Initialized(), TestParseError(), TestRegisterMultipleTools(), testServer(), TestSetSecurity_ApiKeyAccepted(), TestSetSecurity_ApiKeyRejected() (+9 more)

### Community 37 - "Community 37"
Cohesion: 0.12
Nodes (13): BFCLEntry, BFCLEvalResult, BFCLFunction, BFCLMetrics, BFCLSuite, BuiltinBFCLSimple(), BuiltinGAIADev(), BuiltinSWELite() (+5 more)

### Community 38 - "Community 38"
Cohesion: 0.24
Nodes (15): DefaultMock(), singleTaskSuite(), tasksForTree(), TestAgentMonitor(), TestAllDomainTrees(), TestCodeReviewTree(), TestCrashInvestigator(), TestDevOpsTree() (+7 more)

### Community 39 - "Community 39"
Cohesion: 0.24
Nodes (15): DefaultLLM(), BuiltinBTPGTasks(), TestBTPG_BuiltinTasks_Content(), TestBTPG_EdgeCaseRobustness(), TestBTPG_EmptyTasks(), TestBTPG_QualityMetrics_StaticAnalysis(), TestBTPG_TaskExecution(), TestBTPG_TaskExecution_FiveTasks() (+7 more)

### Community 40 - "Community 40"
Cohesion: 0.17
Nodes (12): AgentDefinition, ContentType, Schema, MustParseAgentDefinition(), ParseAgentDefinition(), intPtr(), TestAgentDefinition_JSON_MarshalRoundtrip(), TestSchemaValidation() (+4 more)

### Community 41 - "Community 41"
Cohesion: 0.22
Nodes (14): buildTreeJSON(), handleAnalyze(), handleChat(), handleDefaultCompany(), handleFellows(), handleSprintExecute(), handleSprintStatus(), handleSummary() (+6 more)

### Community 42 - "Community 42"
Cohesion: 0.13
Nodes (9): TestConfig_AllFields(), TestEvolvedAgent_Run(), TestEvolvedAgent_Run_WithAutoEvolve(), TestEvolvedAgent_StructFields(), TestNewEvolvedAgent_AllToolsRegistered(), newConfig(), TestNewEvolvedAgent(), mockLLM (+1 more)

### Community 43 - "Community 43"
Cohesion: 0.16
Nodes (12): NewCatalog(), TestCatalog_Export(), TestCatalog_ListInstalled(), TestCatalog_ListTemplates(), TestCatalog_Search(), TestCatalog_SkillToAgent(), TestCatalog_EmptyTemplates(), TestCatalog_InstallAndSearch() (+4 more)

### Community 44 - "Community 44"
Cohesion: 0.17
Nodes (10): expandTemplate(), replaceAll(), trimQuotes(), Pipeline, PipelineResult, Runner, Step, StepKind (+2 more)

### Community 45 - "Community 45"
Cohesion: 0.13
Nodes (6): ActionFunc, ActionRegistry, Agent, AgentCallbacks, AgentRun, AgentState

### Community 46 - "Community 46"
Cohesion: 0.21
Nodes (13): BuiltinToolBench(), EvaluateToolBench(), formatAvailableAPIs(), TestToolBench_APISelection(), TestToolBench_EmptyEntries(), TestToolBench_EvaluateWithCodeReviewTree(), TestToolBench_EvaluateWithGoDevTree(), TestToolBench_IndividualEntries() (+5 more)

### Community 47 - "Community 47"
Cohesion: 0.19
Nodes (11): Blackboard, TestValidateOutput_Empty(), TestValidateOutput_ErrorPattern(), TestValidateOutput_FromResults(), TestValidateOutput_Good(), TestValidateOutput_Short(), TestValidateOutput_Structured(), TestIntegration_QualityGatesFullFlow() (+3 more)

### Community 48 - "Community 48"
Cohesion: 0.2
Nodes (12): init(), Init(), NewDefaultCompany(), CompanyState, Decision, QuarterResult, companySummary(), TestCompanySimulation_Quarter() (+4 more)

### Community 49 - "Community 49"
Cohesion: 0.33
Nodes (12): cmdCreate(), cmdDelete(), cmdList(), cmdLogs(), cmdRun(), cmdSchedule(), cmdTemplates(), cmdTest() (+4 more)

### Community 50 - "Community 50"
Cohesion: 0.31
Nodes (12): NewEvolvedAgent(), TestEvolvedAgent_Run_Error(), TestNewEvolvedAgent_MinimalConfig(), TestNewEvolvedAgent_NilBlackboard(), TestToolNames_And_Descriptions(), NewCreateAgentTool(), NewEvolveTool(), NewFitnessTool() (+4 more)

### Community 51 - "Community 51"
Cohesion: 0.26
Nodes (7): CompanyOrchestrator, EngineerTree(), MarketingTree(), SalesTree(), StartupTrees(), clamp(), safeDiv()

### Community 52 - "Community 52"
Cohesion: 0.17
Nodes (10): CircuitState, DeadLetterEntry, JobState, Priority, PriorityTask, Backoff(), RetryWithBackoff(), TestBackoff() (+2 more)

### Community 53 - "Community 53"
Cohesion: 0.15
Nodes (12): ADR-001: Behavior Trees as Core Execution Model, ADR-002: MCP as External Interface, ADR-003: File-Based Persistence over SQL, Consequences, Consequences, Consequences, Context, Context (+4 more)

### Community 54 - "Community 54"
Cohesion: 0.22
Nodes (4): GardenerRecommendTool, GardenerStatusTool, main(), truncateStr()

### Community 55 - "Community 55"
Cohesion: 0.22
Nodes (8): NewSchedulerState(), NewTaskQueue(), TestNewSchedulerState_NonexistentPath(), TestSchedulerState_Delete(), TestSchedulerState_SaveLoad(), TestTaskQueue_EnqueueDequeue(), TestTaskQueue_Peek(), TestTaskQueue_Persistence()

### Community 56 - "Community 56"
Cohesion: 0.22
Nodes (5): NewWorkerPool(), TestWorkerPool_Stats(), TestWorkerPool_Submit(), WorkerPool, Task

### Community 57 - "Community 57"
Cohesion: 0.22
Nodes (8): DebateTurn, Fellow, Report, ResearchFinding, ReviewComment, Scenario, Synthesis, ThinkTank

### Community 62 - "Community 62"
Cohesion: 0.47
Nodes (5): buildEvolvedPrompt(), toolDescriptions(), toolNames(), Config, TestToolNames()

## Knowledge Gaps
- **175 isolated node(s):** `TauBenchEntry`, `TauBenchAction`, `TauBenchTool`, `TauBenchParam`, `TauBenchMetrics` (+170 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **11 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `main()` connect `Community 49` to `Community 0`, `Community 2`, `Community 4`, `Community 5`, `Community 6`, `Community 7`, `Community 8`, `Community 9`, `Community 10`, `Community 12`, `Community 13`, `Community 15`, `Community 17`, `Community 18`, `Community 19`, `Community 27`, `Community 30`, `Community 32`, `Community 39`, `Community 41`, `Community 43`, `Community 48`, `Community 50`, `Community 71`?**
  _High betweenness centrality (0.203) - this node is a cross-community bridge._
- **Why does `truncate()` connect `Community 0` to `Community 33`, `Community 34`, `Community 6`, `Community 41`, `Community 12`, `Community 49`, `Community 50`, `Community 19`?**
  _High betweenness centrality (0.155) - this node is a cross-community bridge._
- **Why does `Task` connect `Community 56` to `Community 34`?**
  _High betweenness centrality (0.115) - this node is a cross-community bridge._
- **Are the 69 inferred relationships involving `BuildTree()` (e.g. with `EvaluateTauBench()` and `EvaluateBFCLV3()`) actually correct?**
  _`BuildTree()` has 69 INFERRED edges - model-reasoned connections that need verification._
- **Are the 64 inferred relationships involving `RunTask()` (e.g. with `EvaluateTauBench()` and `EvaluateBFCLV3()`) actually correct?**
  _`RunTask()` has 64 INFERRED edges - model-reasoned connections that need verification._
- **Are the 55 inferred relationships involving `DefaultTree()` (e.g. with `TestTree_DefaultStructure()` and `TestOutcome_Success()`) actually correct?**
  _`DefaultTree()` has 55 INFERRED edges - model-reasoned connections that need verification._
- **Are the 51 inferred relationships involving `contains()` (e.g. with `matchActions()` and `isToolMatch()`) actually correct?**
  _`contains()` has 51 INFERRED edges - model-reasoned connections that need verification._