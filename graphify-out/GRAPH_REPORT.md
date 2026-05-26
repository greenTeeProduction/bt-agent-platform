# Graph Report - go-bt-evolve  (2026-05-26)

## Corpus Check
- 114 files · ~117,863 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1407 nodes · 3438 edges · 66 communities (55 shown, 11 thin omitted)
- Extraction: 66% EXTRACTED · 34% INFERRED · 0% AMBIGUOUS · INFERRED: 1163 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `ba2622df`
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

## God Nodes (most connected - your core abstractions)
1. `BuildTree()` - 70 edges
2. `RunTask()` - 64 edges
3. `DefaultTree()` - 53 edges
4. `main()` - 52 edges
5. `GoDeveloperTree()` - 50 edges
6. `TestIntegration_AllTreesExecute()` - 31 edges
7. `DefaultMock()` - 29 edges
8. `Registry` - 29 edges
9. `ApplyMutations()` - 29 edges
10. `SelectorOptimizer` - 28 edges

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

## Communities (66 total, 11 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.05
Nodes (84): EvaluateGAIA(), max1(), EvaluateSWEVerified(), main(), main(), DemoChainTree(), TestChainAction_Agent_DirectAnswer(), TestChainAction_Agent_NoTools() (+76 more)

### Community 1 - "Community 1"
Cohesion: 0.05
Nodes (69): buildGoapStepPrompt(), floatField(), getStringSlice(), planStepsToStrings(), registerGoapNodes(), stringField(), worldStateFromMap(), init() (+61 more)

### Community 2 - "Community 2"
Cohesion: 0.05
Nodes (66): main(), DeepeningResult, makeRecords(), TestCloneTree_Independent(), TestEvaluateTree_AllFailures(), TestEvaluateTree_EmptyRecords(), TestEvaluateTree_GoDevVsDefault(), TestEvaluateTree_Mixed() (+58 more)

### Community 3 - "Community 3"
Cohesion: 0.09
Nodes (59): TestBTPG_QualityMetrics_AllDomainTrees(), TestBFCL_AllDomainTrees_Accuracy(), resolveTree(), act(), AgentMonitorTree(), AllDomainTrees(), CodeReviewTree(), cond() (+51 more)

### Community 4 - "Community 4"
Cohesion: 0.05
Nodes (33): Definition, InputSpec, Instance, OutputSpec, QualitySpec, Registry, NewRegistry(), State (+25 more)

### Community 5 - "Community 5"
Cohesion: 0.07
Nodes (39): BTOptimizer, ChildStats, collectSelectors(), conditionOverlap(), extractCondition(), findMainSelector(), isDefaultPath(), pathEntropy() (+31 more)

### Community 6 - "Community 6"
Cohesion: 0.06
Nodes (47): cmdCreate(), cmdDelete(), cmdList(), cmdLogs(), cmdRun(), cmdSchedule(), cmdTemplates(), cmdTest() (+39 more)

### Community 7 - "Community 7"
Cohesion: 0.08
Nodes (30): AgentRunner, Checkpoint, NewHistory(), TestHistory_AllStats(), TestHistory_Cleanup(), TestScheduler_RemoveNonexistent(), TestScheduler_UnknownAgent(), RunContext (+22 more)

### Community 8 - "Community 8"
Cohesion: 0.08
Nodes (32): AntiPattern, TestExpertKnowledge_New(), DesignPattern, coreHeuristics(), hasNodeMatching(), hasNodeType(), knownAntiPatterns(), maxDepth() (+24 more)

### Community 9 - "Community 9"
Cohesion: 0.1
Nodes (32): collectNames(), contains(), TestBuildTree_Minimal(), TestBuildTree_UnknownType(), TestTree_AllDomain(), TestTree_AllEvolution(), TestTree_AllFinance(), TestTree_DefaultStructure() (+24 more)

### Community 10 - "Community 10"
Cohesion: 0.06
Nodes (33): dart:convert, _ActivityCard, _ApproveButton, BTStudioApp, build, _buildBody, _buildOverview, Card (+25 more)

### Community 11 - "Community 11"
Cohesion: 0.15
Nodes (27): History, splitLines(), RunRecord, RunStats, ChainConfig, ChainKind, BuildChainAction(), buildChainActionFn() (+19 more)

### Community 12 - "Community 12"
Cohesion: 0.15
Nodes (9): AgentFactory, skillName(), Factory, AutoCreateTree(), determineCategory(), extractKeywords(), NewFactory(), truncateTask() (+1 more)

### Community 13 - "Community 13"
Cohesion: 0.1
Nodes (22): DebateTurn, Fellow, DefaultFellows(), NewThinkTank(), Report, ResearchFinding, ReviewComment, Scenario (+14 more)

### Community 14 - "Community 14"
Cohesion: 0.13
Nodes (5): agentTestMockLLM, chainMockLLM, mockLLM, retryMockLLM, Client

### Community 15 - "Community 15"
Cohesion: 0.12
Nodes (17): EvolvedAgent, mockOllamaServer(), newTestClient(), TestClient_AnalyzeComplexity(), TestClient_Generate(), TestClient_GeneratePlan(), TestClient_Reflect(), ContentItem (+9 more)

### Community 16 - "Community 16"
Cohesion: 0.15
Nodes (21): ABDelta, ABTest, AgentMonitorSuite(), AllSuites(), CodeReviewSuite(), containsStr(), detectPath(), DevOpsSuite() (+13 more)

### Community 17 - "Community 17"
Cohesion: 0.14
Nodes (22): chi2CDF(), cloneTree(), cohensD(), fishersExact(), GoDevSuite(), minF(), QuickValidate(), RunABTest() (+14 more)

### Community 18 - "Community 18"
Cohesion: 0.15
Nodes (8): TestUseCase_CatalogSearchExport(), TestUseCase_EmptyRegistry(), TestUseCase_FullAgentLifecycle(), parseChainConfig(), TestChainAction_ParseConfig(), Ensemble, Plan, TestKnowledgeGraph()

### Community 19 - "Community 19"
Cohesion: 0.17
Nodes (20): avgBranchingFactor(), BTPGQualityScore(), BTPGTreeSummary(), BuiltinBTPGTasks(), countNodes(), EvaluateBTPG(), isEdgeCaseTask(), maxDepth() (+12 more)

### Community 20 - "Community 20"
Cohesion: 0.19
Nodes (17): DefaultMock(), singleTaskSuite(), tasksForTree(), TestAgentMonitor(), TestAllDomainTrees(), TestCodeReviewTree(), TestCrashInvestigator(), TestDevOpsTree() (+9 more)

### Community 21 - "Community 21"
Cohesion: 0.14
Nodes (6): Approval, sortTasks(), Priority, Task, TaskStatus, Workflow

### Community 22 - "Community 22"
Cohesion: 0.34
Nodes (15): extractSection(), extractBulletPoints(), extractFirstLine(), extractListSection(), extractSection(), findNextSection(), parseDebateTranscript(), parseProbability() (+7 more)

### Community 23 - "Community 23"
Cohesion: 0.21
Nodes (12): TestCheckConfidence_ConditionExists(), TestNewPopulation_Evolution(), TestPopulation_Diversity(), Individual, collectNodeNames(), Crossover(), hashTree(), NewPopulation() (+4 more)

### Community 24 - "Community 24"
Cohesion: 0.12
Nodes (13): BFCLEntry, BFCLEvalResult, BFCLFunction, BFCLMetrics, BFCLSuite, BuiltinBFCLSimple(), BuiltinGAIADev(), BuiltinSWELite() (+5 more)

### Community 25 - "Community 25"
Cohesion: 0.19
Nodes (14): BuiltinBFCLV3(), EvaluateBFCLV3(), isToolMatch(), LoadBFCLV3Entries(), LoadBFCLV3MultiTurn(), minInt(), TestBFCLV3_LongContext(), TestBFCLV3_MultiTurn_Basic() (+6 more)

### Community 26 - "Community 26"
Cohesion: 0.16
Nodes (5): mockAgentTool, toolStub, GetReflectionsTool, RunTaskTool, truncateStr()

### Community 27 - "Community 27"
Cohesion: 0.22
Nodes (8): extractMutableParams(), getFloatMeta(), setFloatMeta(), toFloat64(), LocalSearcher, LocalSearchStrategy, mutableParam, tabuEntry

### Community 28 - "Community 28"
Cohesion: 0.13
Nodes (6): ActionFunc, ActionRegistry, Agent, AgentCallbacks, AgentRun, AgentState

### Community 29 - "Community 29"
Cohesion: 0.21
Nodes (13): BuiltinToolBench(), EvaluateToolBench(), formatAvailableAPIs(), TestToolBench_APISelection(), TestToolBench_EmptyEntries(), TestToolBench_EvaluateWithCodeReviewTree(), TestToolBench_EvaluateWithGoDevTree(), TestToolBench_IndividualEntries() (+5 more)

### Community 30 - "Community 30"
Cohesion: 0.22
Nodes (8): maxInt(), CompanyOrchestrator, EngineerTree(), MarketingTree(), SalesTree(), StartupTrees(), clamp(), safeDiv()

### Community 31 - "Community 31"
Cohesion: 0.18
Nodes (10): expandTemplate(), replaceAll(), trimQuotes(), Pipeline, PipelineResult, Runner, Step, StepKind (+2 more)

### Community 32 - "Community 32"
Cohesion: 0.23
Nodes (13): TestBuildKG(), TestConnect(), TestDiscover_CodeReview(), TestDiscover_Finance(), TestDiscover_Research(), TestDiscover_Unknown(), TestListByCategory(), TestNewKG() (+5 more)

### Community 33 - "Community 33"
Cohesion: 0.26
Nodes (13): buildTauBenchTask(), BuiltinTauBenchAirline(), BuiltinTauBenchRetail(), DefaultTauBenchEntries(), EvaluateTauBench(), matchActions(), minLen(), TestTauBench_ActionMatching() (+5 more)

### Community 34 - "Community 34"
Cohesion: 0.19
Nodes (13): airlineTools(), LoadTauBenchTasks(), retailTools(), TestTauBench_TaskLoading(), TestTauBench_ToolDefinitions(), toolsForDomain(), TauBenchAction, TauBenchEntry (+5 more)

### Community 35 - "Community 35"
Cohesion: 0.25
Nodes (5): Catalog, extractYAMLField(), inferTree(), splitTags(), CatalogEntry

### Community 36 - "Community 36"
Cohesion: 0.16
Nodes (6): Capability, Edge, itoa(), KnowledgeGraph, Relation, TreeMeta

### Community 37 - "Community 37"
Cohesion: 0.2
Nodes (5): GardenerRecommendTool, GardenerRunCycleTool, main(), truncateStr(), NewGardener()

### Community 38 - "Community 38"
Cohesion: 0.42
Nodes (11): readMessages(), TestInitialize(), TestNotification_Initialized(), TestParseError(), TestRegisterMultipleTools(), testServer(), TestToolsCall_BadParams(), TestToolsCall_Success() (+3 more)

### Community 39 - "Community 39"
Cohesion: 0.2
Nodes (8): NewEvaluator(), DefaultDeepSeekConfig(), NewDeepSeekClient(), DeepSeekClient, DeepSeekConfig, deepseekMsg, deepseekRequest, deepseekResponse

### Community 40 - "Community 40"
Cohesion: 0.35
Nodes (10): NewAnalyzer(), NewAgentFactory(), TestAnalyzer_EmptyResponse_Error(), TestAnalyzer_NoStrategyPath_Error(), TestAnalyzer_ParsesTreeSpec(), TestExtractJSON(), TestFactory_CreateFromContent(), TestFactory_CreateFromFile() (+2 more)

### Community 41 - "Community 41"
Cohesion: 0.25
Nodes (10): NewCatalog(), TestCatalog_Export(), TestCatalog_ListInstalled(), TestCatalog_ListTemplates(), TestCatalog_Search(), TestCatalog_SkillToAgent(), TestInferTree(), TestCatalog_EmptyTemplates() (+2 more)

### Community 42 - "Community 42"
Cohesion: 0.22
Nodes (6): BestOfN(), jaccardSimilarity(), StackedEnsemble(), tokenSet(), topKOutputs(), TreeRunner

### Community 43 - "Community 43"
Cohesion: 0.29
Nodes (9): TestBTOptimizer_New(), TestDTAnalyzer_New(), NewBTOptimizer(), NewDTAnalyzer(), TestBTOptimizer_ReorderSelectors(), TestDTAnalyzer_BestSplit(), TestDTAnalyzer_Entropy(), TestDTAnalyzer_Gini() (+1 more)

### Community 44 - "Community 44"
Cohesion: 0.27
Nodes (4): itoa(), maxTreeDepth(), QTable, ReinforcementLearner

### Community 45 - "Community 45"
Cohesion: 0.33
Nodes (5): TestGenerator_BuildsCorrectTree(), TestGenerator_NoSelfCorrect_NoFallback(), GeneratedAgent, Generator, NewGenerator()

### Community 46 - "Community 46"
Cohesion: 0.28
Nodes (4): newConfig(), newTestStores(), TestNewEvolvedAgent(), mockLLM

### Community 47 - "Community 47"
Cohesion: 0.39
Nodes (8): NewEvolvedAgent(), NewCreateAgentTool(), NewEvolveTool(), NewFitnessTool(), NewGetReflectionsTool(), NewGetTreeTool(), NewReflectTool(), NewRunTaskTool()

### Community 48 - "Community 48"
Cohesion: 0.43
Nodes (7): DefaultLLM(), TestBFCL_CodeReview_Routing(), TestBFCL_Multiple_Routing(), TestBFCL_Relevance_NoFalsePositives(), TestBFCL_Simple_RoutingAccuracy(), TestGAIA_DeepResearch(), TestSWELite_IssueAnalysis()

### Community 49 - "Community 49"
Cohesion: 0.46
Nodes (6): Analyzer, extractJSON(), truncate(), SkillSpec, TreeNode, TreeSpec

### Community 52 - "Community 52"
Cohesion: 0.33
Nodes (4): BuiltinSWEVerifiedSample(), SWEVerifiedEntry, SWEVerifiedMetrics, SWEVerifiedResult

### Community 53 - "Community 53"
Cohesion: 0.47
Nodes (3): Blackboard, containsAny(), truncateStrForTree()

### Community 57 - "Community 57"
Cohesion: 0.6
Nodes (4): buildEvolvedPrompt(), toolDescriptions(), toolNames(), Config

## Knowledge Gaps
- **144 isolated node(s):** `TauBenchEntry`, `TauBenchAction`, `TauBenchTool`, `TauBenchParam`, `TauBenchMetrics` (+139 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **11 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `main()` connect `Community 6` to `Community 0`, `Community 2`, `Community 3`, `Community 4`, `Community 7`, `Community 8`, `Community 10`, `Community 12`, `Community 13`, `Community 15`, `Community 18`, `Community 19`, `Community 23`, `Community 32`, `Community 37`, `Community 40`, `Community 41`, `Community 47`, `Community 50`?**
  _High betweenness centrality (0.189) - this node is a cross-community bridge._
- **Why does `truncate()` connect `Community 0` to `Community 6`, `Community 47`, `Community 49`, `Community 18`, `Community 50`, `Community 21`, `Community 22`?**
  _High betweenness centrality (0.049) - this node is a cross-community bridge._
- **Why does `BuildTree()` connect `Community 0` to `Community 33`, `Community 3`, `Community 4`, `Community 6`, `Community 8`, `Community 9`, `Community 45`, `Community 14`, `Community 48`, `Community 16`, `Community 19`, `Community 53`, `Community 25`, `Community 29`, `Community 30`?**
  _High betweenness centrality (0.044) - this node is a cross-community bridge._
- **Are the 68 inferred relationships involving `BuildTree()` (e.g. with `EvaluateTauBench()` and `EvaluateBFCLV3()`) actually correct?**
  _`BuildTree()` has 68 INFERRED edges - model-reasoned connections that need verification._
- **Are the 63 inferred relationships involving `RunTask()` (e.g. with `EvaluateTauBench()` and `EvaluateBFCLV3()`) actually correct?**
  _`RunTask()` has 63 INFERRED edges - model-reasoned connections that need verification._
- **Are the 52 inferred relationships involving `DefaultTree()` (e.g. with `TestTree_DefaultStructure()` and `TestOutcome_Success()`) actually correct?**
  _`DefaultTree()` has 52 INFERRED edges - model-reasoned connections that need verification._
- **Are the 37 inferred relationships involving `main()` (e.g. with `NewStore()` and `NewTreeStore()`) actually correct?**
  _`main()` has 37 INFERRED edges - model-reasoned connections that need verification._