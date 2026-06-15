# Fusion-Style BT Agent Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Build a behavior-tree agent that reproduces OpenRouter Fusion’s multi-model deliberation pipeline: route only suitable prompts into a parallel model panel, let a judge compare responses into structured analysis, then synthesize the final answer from the analysis.

**Architecture:** Add a first-class `fusion` execution path to the Go BT platform rather than shelling out to OpenRouter’s `openrouter:fusion` tool. The runtime will expose a reusable `internal/fusion` package, a `ChainFusion` chain executor, a Fusion BT tree, and tests that prove trigger routing, parallel panel execution, structured judge JSON, blind-spot detection, and no-op fallback behavior.

**Tech Stack:** Go, existing BT engine `SerializableNode`/`ChainAction`, existing `llm.LLM` interface, OpenAI-compatible HTTP client for OpenRouter-compatible panel models, existing blackboard and chain tools, `go test`.

---

## Research Summary: What OpenRouter Fusion Does

Sources researched:
- `https://openrouter.ai/docs/guides/features/plugins/fusion`
- `https://openrouter.ai/docs/guides/features/server-tools/fusion`
- `https://openrouter.ai/docs/guides/routing/routers/fusion-router`

Core behavior to match:

1. **Fusion is a model-deliberation tool, not just routing.** The outer model can invoke a tool that runs a panel of models and returns analysis for final synthesis.
2. **The outer model decides whether to invoke Fusion.** Fusion is intended for research-heavy, expert-critique, compare/contrast, high-stakes, multi-perspective prompts; short tactical prompts should answer directly. A forced mode exists via `tool_choice: "required"`.
3. **Panel models run in parallel.** Default “Quality” preset is:
   - `~anthropic/claude-opus-latest`
   - `~openai/gpt-latest`
   - `~google/gemini-pro-latest`
   Panel size is configurable from 1–8 models.
4. **Panel models have web tools.** Each panel model gets `openrouter:web_search` and `openrouter:web_fetch` with bounded tool loops.
5. **Judge compares, does not merge.** The judge receives all panel responses and produces structured JSON analysis.
6. **Structured analysis fields:**
   - `consensus`
   - `contradictions`
   - `partial_coverage`
   - `unique_insights`
   - `blind_spots`
7. **Judge also has web tools.** The judge can search/fetch before returning analysis.
8. **Outer model writes final answer.** Final answer is produced after the outer model receives the structured analysis and raw panel responses.
9. **Important config knobs:**
   - `analysis_models`: 1–8 panel models
   - `model`: judge model
   - `max_tool_calls`: 1–16, default 8
   - `max_completion_tokens`
   - `reasoning`
   - `temperature`
   - `enabled`

BT platform mapping:

- **outer model decision** → `ShouldUseFusion` condition + force flag
- **panel parallelism** → `internal/fusion.RunPanel` goroutines and/or BT `Parallel` node
- **web search/fetch loops** → adapter over existing `web_search`/`http_get`/future `web_fetch` chain tools
- **judge analysis** → `internal/fusion.Judge` returning typed JSON
- **final answer** → `ChainFusion` synthesis step storing `bb.Result`
- **raw/structured artifacts** → `bb.ChainState["fusion_analysis"]`, `bb.ChainState["fusion_responses"]`, blackboard keys under `fusion/*`

---

## Current Codebase Anchors

Read first before implementation:

- `graphify-out/GRAPH_REPORT.md` — required codebase map. Note: current report is stale relative to HEAD; run `graphify update .` after code changes.
- `internal/engine/tree.go`
  - `Blackboard` fields: `Task`, `Result`, `Outcome`, `LLM`, `ChainTools`, `ChainState`, `Results`, `BB`
  - `BuildTree` / `BuildAndValidate`
  - `ChainAction` routing via `BuildChainAction`
- `internal/engine/chains.go`
  - `ChainConfig`
  - `ChainKind`
  - existing chain executors: `llm_call`, `map_reduce`, `refine`, `agent`, `tool_action`
- `internal/engine/blackboard_tools.go`
  - blackboard tool pattern: tools implement `Name()`, `Description()`, `Call(string) string`
- `internal/llm/ollama.go`
  - `LLM` interface
- `internal/llm/deepseek.go`
  - existing OpenAI-compatible POST `/chat/completions` style client
- `internal/llm/provider.go`
  - provider config currently supports `ollama`, `deepseek`, `acp`
- `internal/config/config.go`
  - config/env loading pattern
- `internal/evolution/tree_builders.go`
  - reusable tree builder helpers
- `internal/evolution/*_trees.go`
  - examples of domain tree definitions

Known constraint:
- `go test ./internal/engine` currently has a pre-existing import-cycle setup failure. For fusion work, add tests in packages that can run cleanly (`internal/fusion`, `internal/llm`, `internal/evolution`) and only run narrowly scoped engine tests that compile.

---

## Acceptance Criteria

1. A BT tree can execute a prompt through a Fusion-style deliberation pipeline.
2. The system can skip Fusion for short/tactical prompts unless forced.
3. Panel model count validates 1–8.
4. `max_tool_calls` validates 1–16 and is enforced in any tool loop.
5. Panel calls execute concurrently and retain per-model errors without failing the entire run when at least one model succeeds.
6. Judge output is typed JSON with:
   - `consensus`
   - `contradictions`
   - `partial_coverage`
   - `unique_insights`
   - `blind_spots`
7. Final answer uses judge analysis and raw responses, not only one panel answer.
8. Raw panel responses and judge analysis are stored in `bb.ChainState` and, when available, the scoped blackboard.
9. Tests cover trigger routing, parallel panel execution, judge JSON parsing, partial panel failures, forced Fusion, and direct fallback.
10. `go build ./...` passes.

---

## Proposed Data Model

Create package `internal/fusion`.

Core types:

```go
package fusion

import "time"

type Config struct {
    Enabled             bool     `json:"enabled"`
    Force               bool     `json:"force"`
    AnalysisModels      []string `json:"analysis_models"`
    JudgeModel          string   `json:"model"`
    MaxToolCalls        int      `json:"max_tool_calls"`
    MaxCompletionTokens int      `json:"max_completion_tokens"`
    Temperature         *float64 `json:"temperature,omitempty"`
    Timeout             time.Duration
}

type Response struct {
    Model       string        `json:"model"`
    Content     string        `json:"content"`
    Error       string        `json:"error,omitempty"`
    DurationMS  int64         `json:"duration_ms"`
    ToolCalls   int           `json:"tool_calls,omitempty"`
}

type Analysis struct {
    Consensus       []string          `json:"consensus"`
    Contradictions  []Contradiction   `json:"contradictions"`
    PartialCoverage []CoveragePoint   `json:"partial_coverage"`
    UniqueInsights  []UniqueInsight   `json:"unique_insights"`
    BlindSpots      []string          `json:"blind_spots"`
}

type Contradiction struct {
    Topic   string   `json:"topic"`
    Stances []Stance `json:"stances"`
}

type Stance struct {
    Model  string `json:"model"`
    Stance string `json:"stance"`
}

type CoveragePoint struct {
    Models []string `json:"models"`
    Point  string   `json:"point"`
}

type UniqueInsight struct {
    Model   string `json:"model"`
    Insight string `json:"insight"`
}

type Result struct {
    Status    string     `json:"status"`
    Analysis  Analysis   `json:"analysis"`
    Responses []Response `json:"responses"`
    Final     string     `json:"final,omitempty"`
}
```

---

## Task 1: Add Fusion Config Validation

**Objective:** Create `internal/fusion.Config`, defaults, and validation matching OpenRouter Fusion’s knobs.

**Files:**
- Create: `internal/fusion/config.go`
- Create: `internal/fusion/config_test.go`

**Step 1: Write failing tests**

Test cases:

```go
func TestConfig_DefaultsQualityPreset(t *testing.T)
func TestConfig_ValidatesPanelSize(t *testing.T)
func TestConfig_ValidatesMaxToolCalls(t *testing.T)
func TestConfig_Disabled(t *testing.T)
```

Expected behavior:
- `DefaultConfig()` returns enabled=true, max_tool_calls=8, 3 quality preset models.
- 0 panel models defaults to quality preset.
- >8 panel models returns validation error.
- max_tool_calls <1 or >16 returns validation error.

**Step 2: Run tests to verify failure**

```bash
cd /home/nico/go-bt-evolve
go test ./internal/fusion -run TestConfig -count=1
```

Expected: FAIL because package/files do not exist.

**Step 3: Implement config**

Create `internal/fusion/config.go` with:

```go
func DefaultConfig() Config
func (c Config) Normalize() Config
func (c Config) Validate() error
```

Use quality preset strings from OpenRouter docs:

```go
var QualityPreset = []string{
    "~anthropic/claude-opus-latest",
    "~openai/gpt-latest",
    "~google/gemini-pro-latest",
}
```

**Step 4: Verify**

```bash
go test ./internal/fusion -run TestConfig -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/fusion/config.go internal/fusion/config_test.go
git commit -m "feat: add fusion config validation"
```

---

## Task 2: Add OpenRouter/OpenAI-Compatible Model Client

**Objective:** Add a reusable OpenAI-compatible chat client that can call arbitrary OpenRouter models by model name.

**Files:**
- Create: `internal/llm/openai_compat.go`
- Create: `internal/llm/openai_compat_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/llm/provider.go`

**Why:** Existing `DeepSeekClient` is OpenAI-compatible but hardwired to DeepSeek config. Fusion needs dynamic per-call model selection for 1–8 panel models plus judge model.

**Step 1: Write tests**

Use `httptest.Server` to assert request shape:

```go
func TestOpenAICompat_Generate_SendsChatCompletion(t *testing.T)
func TestOpenAICompat_WithModel_OverridesModel(t *testing.T)
func TestOpenAICompat_ErrorResponse(t *testing.T)
func TestOpenAICompat_ReadsOpenRouterDefaults(t *testing.T)
```

Expected request:

```json
{
  "model": "anthropic/claude-3.5-sonnet",
  "messages": [
    {"role":"system","content":"..."},
    {"role":"user","content":"..."}
  ],
  "stream": false
}
```

**Step 2: Implement client**

Create:

```go
type OpenAICompatConfig struct {
    APIKey  string
    BaseURL string
    Model   string
    Timeout time.Duration
    AppName string
    SiteURL string
}

type OpenAICompatClient struct { ... }

func NewOpenAICompatClient(cfg OpenAICompatConfig) *OpenAICompatClient
func (c *OpenAICompatClient) Generate(prompt string) (string, error)
func (c *OpenAICompatClient) GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error)
```

Headers:
- `Authorization: Bearer <key>`
- `Content-Type: application/json`
- Optional OpenRouter attribution:
  - `HTTP-Referer`
  - `X-Title`

Config additions:

```go
OpenRouterHost  string `json:"openrouter_host" env:"OPENROUTER_HOST" default:"https://openrouter.ai/api/v1"`
OpenRouterKey   string `json:"openrouter_key,omitempty" env:"OPENROUTER_API_KEY" default:""`
OpenRouterModel string `json:"openrouter_model" env:"BT_OPENROUTER_MODEL" default:"openrouter/auto"`
```

Provider additions:
- Add `openrouter` to valid providers in `provider.go`.
- Build `OpenAICompatClient` with OpenRouter defaults.

**Step 3: Verify**

```bash
go test ./internal/llm -run 'TestOpenAICompat|TestNewProvider' -count=1
go test ./internal/config -run 'Test.*OpenRouter|TestLoad' -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/llm/openai_compat.go internal/llm/openai_compat_test.go internal/config/config.go internal/llm/provider.go
git commit -m "feat: add OpenRouter-compatible LLM provider"
```

---

## Task 3: Implement Parallel Panel Runner

**Objective:** Run 1–8 analysis models concurrently, preserving errors per model and returning successful responses.

**Files:**
- Create: `internal/fusion/panel.go`
- Create: `internal/fusion/panel_test.go`

**Step 1: Define abstraction**

Avoid depending directly on `internal/llm` in tests:

```go
type ModelCaller interface {
    GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error)
}
```

**Step 2: Write tests**

Tests:

```go
func TestRunPanel_RunsModelsConcurrently(t *testing.T)
func TestRunPanel_PreservesPerModelErrors(t *testing.T)
func TestRunPanel_FailsWhenAllModelsFail(t *testing.T)
func TestRunPanel_StoresDurations(t *testing.T)
```

Concurrency proof:
- fake 3 models each sleep 100ms
- total elapsed should be <220ms, not ~300ms

**Step 3: Implement**

```go
func RunPanel(ctx context.Context, caller ModelCaller, cfg Config, prompt string, tools []Tool) ([]Response, error)
```

For v1, implement model calls without tool loops. Keep `tools []Tool` in signature so Task 5 can add web-search/fetch loops without API churn.

Panel system prompt must instruct:
- answer independently
- cite uncertainty
- identify assumptions
- do not defer to other panelists
- if web tools are unavailable, say so explicitly

**Step 4: Verify**

```bash
go test ./internal/fusion -run TestRunPanel -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/fusion/panel.go internal/fusion/panel_test.go
git commit -m "feat: run fusion panel models concurrently"
```

---

## Task 4: Implement Judge Structured Analysis

**Objective:** Compare panel responses and return typed JSON analysis.

**Files:**
- Create: `internal/fusion/judge.go`
- Create: `internal/fusion/judge_test.go`

**Step 1: Write tests**

Tests:

```go
func TestJudge_ParsesStructuredJSON(t *testing.T)
func TestJudge_RejectsInvalidJSON(t *testing.T)
func TestJudge_IncludesAllFusionFields(t *testing.T)
func TestJudgePrompt_SaysCompareNotMerge(t *testing.T)
```

**Step 2: Implement judge prompt**

Judge prompt must contain:

```text
You are the Fusion judge. Compare panel responses; do not merge them.
Return ONLY JSON with keys: consensus, contradictions, partial_coverage, unique_insights, blind_spots.
```

Input includes all model names and contents.

Implement:

```go
func Judge(ctx context.Context, caller ModelCaller, cfg Config, prompt string, responses []Response) (Analysis, error)
```

Parsing should tolerate fenced JSON:

```go
func parseAnalysisJSON(raw string) (Analysis, error)
```

**Step 3: Verify**

```bash
go test ./internal/fusion -run 'TestJudge|TestParseAnalysis' -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/fusion/judge.go internal/fusion/judge_test.go
git commit -m "feat: add fusion judge structured analysis"
```

---

## Task 5: Add Optional Web Search / Fetch Tool Loop

**Objective:** Approximate Fusion’s `openrouter:web_search` and `openrouter:web_fetch` loops using BT platform tools.

**Files:**
- Create: `internal/fusion/tools.go`
- Create: `internal/fusion/tools_test.go`
- Modify: `internal/fusion/panel.go`
- Modify: `internal/fusion/judge.go`
- Modify: `internal/engine/chains.go` if needed to adapt existing `bb.ChainTools`

**Step 1: Define fusion tool interface**

```go
type Tool interface {
    Name() string
    Description() string
    Call(input string) string
}
```

**Step 2: Tool loop protocol**

Use the same ReAct-compatible format as `execAgent`:

```text
Thought: ...
Action: web_search
Action Input: ...
```

or

```text
Final Answer: ...
```

Bound by `cfg.MaxToolCalls`.

**Step 3: Tests**

```go
func TestToolLoop_StopsAtMaxToolCalls(t *testing.T)
func TestToolLoop_ExecutesWebSearchAndFetch(t *testing.T)
func TestToolLoop_FinalAnswerWithoutTools(t *testing.T)
func TestToolLoop_ToolErrorsAreIncluded(t *testing.T)
```

**Step 4: Implementation rule**

If no web tools exist, the panel/judge still runs, but the system prompt says:

```text
Web tools unavailable in this run; state uncertainty explicitly.
```

This keeps behavior honest instead of pretending web access exists.

**Step 5: Verify**

```bash
go test ./internal/fusion -run 'TestToolLoop|TestRunPanel|TestJudge' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/fusion/tools.go internal/fusion/tools_test.go internal/fusion/panel.go internal/fusion/judge.go internal/engine/chains.go
git commit -m "feat: add bounded fusion web tool loops"
```

---

## Task 6: Implement Final Synthesis

**Objective:** Produce the final user-facing answer from original prompt, structured analysis, and raw panel responses.

**Files:**
- Create: `internal/fusion/synthesize.go`
- Create: `internal/fusion/synthesize_test.go`

**Step 1: Tests**

```go
func TestSynthesize_UsesAnalysisAndResponses(t *testing.T)
func TestSynthesize_FallsBackToJudgeAnalysisWhenOuterModelFails(t *testing.T)
func TestSynthesize_IncludesContradictions(t *testing.T)
```

**Step 2: Implement**

```go
func Synthesize(ctx context.Context, caller ModelCaller, cfg Config, originalPrompt string, result Result) (string, error)
```

Prompt should require:
- identify consensus as high-confidence
- explicitly call out contradictions and uncertainty
- include unique insights when useful
- mention blind spots if important
- no fabricated citations

**Step 3: Verify**

```bash
go test ./internal/fusion -run TestSynthesize -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/fusion/synthesize.go internal/fusion/synthesize_test.go
git commit -m "feat: synthesize final answer from fusion analysis"
```

---

## Task 7: Add End-to-End Fusion Runner

**Objective:** Provide one public entry point that executes the full Fusion pipeline.

**Files:**
- Create: `internal/fusion/run.go`
- Create: `internal/fusion/run_test.go`

**Step 1: Implement**

```go
func Run(ctx context.Context, caller ModelCaller, cfg Config, prompt string, tools []Tool) (Result, error)
```

Sequence:
1. Normalize/validate config.
2. `RunPanel` in parallel.
3. Fail only if all panel responses fail.
4. `Judge` successful responses.
5. `Synthesize` final answer.
6. Return `Result{Status:"ok", Analysis, Responses, Final}`.

**Step 2: Tests**

```go
func TestRun_EndToEnd(t *testing.T)
func TestRun_AllPanelFailures(t *testing.T)
func TestRun_PartialPanelFailureStillSucceeds(t *testing.T)
func TestRun_DisabledReturnsError(t *testing.T)
```

**Step 3: Verify**

```bash
go test ./internal/fusion -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/fusion/run.go internal/fusion/run_test.go
git commit -m "feat: add end-to-end fusion runner"
```

---

## Task 8: Add ChainKind `fusion` to BT Engine

**Objective:** Make Fusion executable as a BT `ChainAction`.

**Files:**
- Modify: `internal/engine/chains.go`
- Create or modify: `internal/engine/chains_fusion_test.go`

**Step 1: Add chain kind**

In `internal/engine/chains.go`:

```go
const ChainFusion ChainKind = "fusion"
```

Add switch case:

```go
case ChainFusion:
    return execFusion(cfg, bb)
```

**Step 2: Implement `execFusion`**

Responsibilities:
- Expand prompt via `expandTemplate(cfg.Prompt, bb)`.
- Parse Fusion config from `cfg.Params`:
  - `analysis_models` CSV
  - `model`
  - `max_tool_calls`
  - `force`
  - `enabled`
- Adapt `bb.LLM` to `fusion.ModelCaller` when possible.
  - If `bb.LLM` is a generic OpenRouter client, use per-model calls.
  - Otherwise use fallback wrapper that calls the same `bb.LLM.Generate` and labels models as configured. This allows unit tests and local-only mode.
- Convert `bb.ChainTools` into `[]fusion.Tool` if they implement `Name/Description/Call`.
- Store:
  - `bb.ChainState["fusion_analysis"]`
  - `bb.ChainState["fusion_responses"]`
  - `bb.ChainState["fusion_models"]`
  - `bb.Result = result.Final`
  - `bb.Outcome = "chain_success"`

**Step 3: Tests**

```go
func TestExecFusion_StoresAnalysisAndResponses(t *testing.T)
func TestExecFusion_ParsesConfigParams(t *testing.T)
func TestExecFusion_NoLLMReturnsFailure(t *testing.T)
func TestExecFusion_PartialPanelFailureStillSucceeds(t *testing.T)
```

If `internal/engine` package import cycle blocks tests, put tests in `internal/fusion` and add a focused `go test ./internal/engine -run TestExecFusion -count=1` only after checking it compiles.

**Step 4: Verify**

```bash
go test ./internal/fusion -count=1
go test ./internal/engine -run TestExecFusion -count=1
```

Expected:
- Fusion tests pass.
- If engine package still hits unrelated import cycle, document it and verify with `go build ./...`.

**Step 5: Commit**

```bash
git add internal/engine/chains.go internal/engine/chains_fusion_test.go
git commit -m "feat: expose fusion as BT chain action"
```

---

## Task 9: Add Fusion Routing Conditions

**Objective:** Match Fusion’s “use only when helpful unless forced” behavior.

**Files:**
- Create: `internal/engine/fusion_conditions.go`
- Create: `internal/engine/fusion_conditions_test.go`
- Modify: `internal/engine/registry.go`

**Step 1: Implement conditions/actions**

Add:

```go
func shouldUseFusionCond(b *Blackboard) bool
func forceFusionCond(b *Blackboard) bool
func markFusionSkippedAction(ctx *btcore.BTContext[Blackboard]) int
```

Trigger if any:
- `bb.ChainState["fusion_force"] == true`
- `Complexity == "high"`
- prompt contains research/critique/compare/high-stakes keywords:
  - `research`, `survey`, `compare`, `contrast`, `strongest arguments`, `where do experts disagree`, `critique`, `risk`, `high-stakes`, `accuracy`, `multiple perspectives`, `tradeoff`
- task length above threshold, e.g. >350 chars

Do not trigger for:
- short tactical commands
- simple code edits
- status questions
- one-hop factual asks

**Step 2: Register**

In `init()` registry:

```go
RegisterCondition("ShouldUseFusion", shouldUseFusionCond)
RegisterCondition("ForceFusion", forceFusionCond)
RegisterAction("MarkFusionSkipped", markFusionSkippedAction)
```

**Step 3: Tests**

```go
func TestShouldUseFusion_ResearchPrompt(t *testing.T)
func TestShouldUseFusion_CompareContrast(t *testing.T)
func TestShouldUseFusion_ShortTacticalFalse(t *testing.T)
func TestShouldUseFusion_ForceTrue(t *testing.T)
```

**Step 4: Verify**

```bash
go test ./internal/engine -run 'TestShouldUseFusion|TestForceFusion' -count=1
```

Expected: PASS or document unrelated import-cycle if still present.

**Step 5: Commit**

```bash
git add internal/engine/fusion_conditions.go internal/engine/fusion_conditions_test.go internal/engine/registry.go
git commit -m "feat: add fusion routing conditions"
```

---

## Task 10: Add Fusion BT Tree

**Objective:** Provide a reusable BT agent tree that behaves like OpenRouter Fusion.

**Files:**
- Create: `internal/evolution/fusion_trees.go`
- Create: `internal/evolution/fusion_trees_test.go`
- Modify: any tree catalog if present, likely `internal/evolution/default_tree.go` or domain catalog file found by search.

**Step 1: Tree shape**

```go
func FusionDeliberationTree() *SerializableNode {
    return NewTree("FusionDeliberation",
        NewPreGate(
            NewCondition("ValidateInput", "Task must be non-empty"),
            NewAction("AssignComplexity", "Classify task complexity"),
            NewAction("SetupDefaultTools", "Attach web_search/http_get tools when available"),
        ),
        NewStrategyRouter(
            NewStrategy("FusionPath",
                NewCondition("ShouldUseFusion", "Only deliberate when multiple perspectives are valuable"),
                SerializableNode{
                    Type: "ChainAction",
                    Name: "fusion:{{.Task}}",
                    Metadata: map[string]any{
                        "chain_type": "fusion",
                        "prompt": "{{.Task}}",
                        "params": map[string]any{
                            "max_tool_calls": "8",
                        },
                    },
                },
            ),
            NewStrategy("DirectPath",
                NewChainAction("llm_call:{{.Task}}", 4096),
            ),
        ),
        NewReflect(),
        NewDefaultOutcomeSelector(4096),
        NewAdapt(),
    )
}
```

Adjust exact metadata shape to match `parseChainConfig()` in `internal/engine/chains.go`.

**Step 2: Tests**

```go
func TestFusionDeliberationTree_Validates(t *testing.T)
func TestFusionDeliberationTree_HasFusionPath(t *testing.T)
func TestFusionDeliberationTree_HasDirectFallback(t *testing.T)
```

**Step 3: Verify**

```bash
go test ./internal/evolution -run TestFusionDeliberationTree -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/evolution/fusion_trees.go internal/evolution/fusion_trees_test.go
git commit -m "feat: add fusion deliberation BT tree"
```

---

## Task 11: Add Agent Template / CLI Registration

**Objective:** Make the Fusion BT agent discoverable and runnable by the BT platform.

**Files:**
- Search first:
  - `grep` equivalent via `search_files("AgentDefinition|Template|allPlatformTrees|ResolveAgentName", ...)`
- Likely modify one of:
  - `internal/agents/*`
  - `internal/dashboard/*`
  - `internal/evolution/default_tree.go`
  - `internal/evolution/tree_builders.go`
  - CLI command catalog under `cmd/*`

**Step 1: Find registration point**

Use:

```bash
graphify query "where are built-in agent trees registered and listed"
graphify query "how does ResolveAgentName map agent names to trees"
```

Fallback search:

```text
search_files("allPlatformTrees|ResolveAgentName|AgentDefinition|TemplateDir", path="/home/nico/go-bt-evolve", file_glob="*.go")
```

**Step 2: Add agent identity**

Name: `fusion-deliberation`
Description: `Multi-model panel + judge + synthesis for research-heavy or high-stakes prompts.`
Tree: `FusionDeliberationTree()`

**Step 3: Tests**

```go
func TestFusionAgent_Listed(t *testing.T)
func TestFusionAgent_ResolvesByName(t *testing.T)
```

**Step 4: Verify**

```bash
go test ./... -run 'TestFusionAgent|TestFusionDeliberationTree' -count=1
```

If full `./...` hits known engine import cycle, run targeted package tests and `go build ./...`.

**Step 5: Commit**

```bash
git add <registration files>
git commit -m "feat: register fusion deliberation agent"
```

---

## Task 12: Add Metrics and Blackboard Persistence

**Objective:** Make Fusion observable and debuggable.

**Files:**
- Modify: `internal/engine/chains.go` or new `internal/engine/fusion_state.go`
- Add tests where compile-safe.

**Step 1: Store state**

After `execFusion`:

```go
bb.ChainState["fusion_status"] = result.Status
bb.ChainState["fusion_models"] = []string{...}
bb.ChainState["fusion_panel_count"] = len(result.Responses)
bb.ChainState["fusion_success_count"] = countSuccessful(result.Responses)
bb.ChainState["fusion_analysis"] = result.Analysis
bb.ChainState["fusion_responses"] = result.Responses
```

If `bb.BB != nil`, write:

- `fusion/input`
- `fusion/responses.json`
- `fusion/analysis.json`
- `fusion/final.md`

**Step 2: Tests**

```go
func TestExecFusion_WritesChainState(t *testing.T)
func TestExecFusion_WritesScopedBlackboardWhenAvailable(t *testing.T)
```

**Step 3: Verify**

```bash
go test ./internal/fusion -count=1
go test ./internal/blackboard -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/engine/chains.go internal/engine/fusion_state.go <tests>
git commit -m "feat: persist fusion artifacts to blackboard"
```

---

## Task 13: Add End-to-End Fake-LLM BT Test

**Objective:** Prove the BT tree executes the Fusion flow without hitting real APIs.

**Files:**
- Create: `internal/evolution/fusion_integration_test.go` or compile-safe package equivalent.

**Step 1: Fake model caller**

Use deterministic fake responses:

- model A: consensus point + unique A
- model B: consensus point + contradiction
- judge: JSON containing all five fields
- synthesizer: final answer referencing consensus and contradiction

**Step 2: Execute tree**

Create `engine.Blackboard` with fake LLM/OpenRouter client, task:

```text
Compare the strongest arguments for and against carbon taxes. Where do experts disagree?
```

Run:

```go
cmd := engine.BuildTree(evolution.FusionDeliberationTree(), bb)
status := cmd.Tick(ctx)
```

Exact runner API may differ; follow existing tests in `internal/engine/chains_test.go` and `internal/benchmark/integration_test.go`.

**Step 3: Assert**

- status success
- `bb.Outcome == "chain_success"` or final success outcome
- `bb.Result` includes consensus and contradiction
- `bb.ChainState["fusion_analysis"]` exists

**Step 4: Verify**

```bash
go test ./internal/evolution -run TestFusionDeliberation_EndToEnd -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/evolution/fusion_integration_test.go
git commit -m "test: cover fusion BT end-to-end with fake models"
```

---

## Task 14: Add Real-API Smoke Test Behind Env Gate

**Objective:** Verify real OpenRouter behavior without making CI depend on paid API calls.

**Files:**
- Create: `internal/fusion/openrouter_integration_test.go`

**Step 1: Env gate**

Skip unless:

```go
if os.Getenv("OPENROUTER_API_KEY") == "" || os.Getenv("BT_TEST_REAL_FUSION") != "1" {
    t.Skip("real fusion smoke test disabled")
}
```

**Step 2: Use cheap models by default**

Use low-cost panel models for smoke:

```text
~google/gemini-flash-latest
deepseek/deepseek-v3.2
~moonshotai/kimi-latest
```

Keep prompt small.

**Step 3: Verify**

Manual command:

```bash
BT_TEST_REAL_FUSION=1 OPENROUTER_API_KEY=$OPENROUTER_API_KEY go test ./internal/fusion -run TestOpenRouterFusionSmoke -count=1 -timeout 180s
```

Expected:
- returns `Status: ok`
- at least 2 successful panel responses
- non-empty analysis fields
- final answer non-empty

**Step 4: Commit**

```bash
git add internal/fusion/openrouter_integration_test.go
git commit -m "test: add gated real OpenRouter fusion smoke test"
```

---

## Task 15: Documentation and Operator Notes

**Objective:** Document how to run and configure the Fusion BT agent.

**Files:**
- Create: `docs/fusion-bt-agent.md`
- Modify: README or agent catalog docs if present.

**Content:**

Include:
- What it does.
- When it triggers.
- How to force Fusion.
- Config env vars:
  - `OPENROUTER_API_KEY`
  - `OPENROUTER_HOST`
  - `BT_OPENROUTER_MODEL`
  - `BT_FUSION_ANALYSIS_MODELS` (if added)
  - `BT_FUSION_JUDGE_MODEL` (if added)
  - `BT_FUSION_MAX_TOOL_CALLS` (if added)
- Example task:

```bash
bt-agent run fusion-deliberation "Survey the strongest arguments for and against carbon taxes. Where do experts disagree?"
```

- Troubleshooting:
  - no key
  - all panel models failed
  - invalid judge JSON
  - web tools unavailable
  - cost controls

**Verify:**

```bash
go build ./...
go test ./internal/fusion ./internal/llm ./internal/evolution -count=1
```

**Commit:**

```bash
git add docs/fusion-bt-agent.md README.md
git commit -m "docs: document fusion BT agent"
```

---

## Final Verification Checklist

Run after all tasks:

```bash
cd /home/nico/go-bt-evolve
/usr/local/go/bin/go build ./...
/usr/local/go/bin/go test ./internal/fusion -count=1
/usr/local/go/bin/go test ./internal/llm -run 'TestOpenAICompat|TestNewProvider' -count=1
/usr/local/go/bin/go test ./internal/evolution -run 'TestFusion|Test.*Fusion' -count=1
/usr/local/go/bin/go test ./internal/blackboard -count=1
graphify update .
git status --short
```

Expected:
- build passes
- fusion/llm/evolution/blackboard targeted tests pass
- graph updated
- clean worktree after commits

If `go test ./internal/engine` still fails due to pre-existing import cycle, do not block Fusion implementation; record the exact failure in the commit notes and leave a separate issue/plan for fixing the engine test import cycle.

---

## Production Safety Notes

- **Cost guard:** default panel should be configurable and should not silently use the expensive quality preset unless `OPENROUTER_API_KEY` is present and Fusion is explicitly enabled for that agent.
- **Concurrency guard:** max 8 panel models; default 3; use context timeout per panel call.
- **Partial failure:** one failed panel model should become `Response.Error`, not kill the full run.
- **All failure:** fail fast with clear `Status: error` and no fabricated answer.
- **Judge JSON:** invalid JSON should return a clear error and store raw judge output for inspection.
- **Web tools:** if unavailable, responses must state that freshness is limited.
- **No OpenRouter Fusion shortcut:** Do not call `openrouter:fusion`; this implementation should reproduce the Fusion pipeline natively so BT can monitor/evolve every step.
- **Blackboard persistence:** preserve raw responses and analysis for auditability and future self-improvement.
