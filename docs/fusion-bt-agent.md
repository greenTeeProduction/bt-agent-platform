# Fusion BT Agent

The Fusion BT agent implements native multi-model deliberation inspired by OpenRouter Fusion.

## Tree IDs

- `fusion`
- `fusion_deliberation`

Both resolve to `evolution.FusionDeliberationTree()`.

## Runtime behavior

1. `PreGate`
   - `ValidateInput`
   - `AssignComplexity`
   - `SetupDefaultTools`
2. `StrategyRouter`
   - `FusionPath` when `ShouldUseFusion` returns true
   - `DirectPath` fallback when Fusion is not useful
3. `fusion` chain executor
   - runs 1–8 panel models in parallel
   - each panel can use available tools through a bounded Action/Action Input loop
   - judge model returns strict JSON with:
     - `consensus`
     - `contradictions`
     - `partial_coverage`
     - `unique_insights`
     - `blind_spots`
   - final synthesis uses judge analysis plus raw panel outputs

## Routing triggers

`ShouldUseFusion` returns true for:

- `ChainState["fusion_force"] == true|"true"|"1"|"yes"`
- high complexity tasks
- long prompts
- prompts mentioning research, compare/contrast, expert disagreement, critique, risk, high-stakes accuracy, tradeoffs, or pros/cons

Short tactical prompts use the direct fallback path.

## Configuration

Fusion chain params:

```yaml
params:
  analysis_models: "~anthropic/claude-opus-latest,~openai/gpt-latest,~google/gemini-pro-latest"
  model: "~anthropic/claude-opus-latest"
  max_tool_calls: "8"
  force: "false"
  enabled: "true"
```

OpenRouter provider env vars:

```bash
export BT_LLM_PROVIDER=openrouter
export OPENROUTER_API_KEY=...
export BT_OPENROUTER_MODEL=openrouter/auto
export OPENROUTER_HOST=https://openrouter.ai/api/v1
```

## Blackboard outputs

The Fusion chain stores:

- `bb.Result`: final synthesis
- `bb.Results[]`: final synthesis appended
- `bb.Outcome`: `chain_success` or `chain_failed`
- `bb.ChainState["fusion_status"]`
- `bb.ChainState["fusion_responses"]`
- `bb.ChainState["fusion_analysis"]`
- `bb.ChainState["fusion_models"]`
- `bb.ChainState["fusion_panel_count"]`
- `bb.ChainState["fusion_success_count"]`

When a durable blackboard handle is attached, it also writes:

- `fusion/input`
- `fusion/responses.json`
- `fusion/analysis.json`
- `fusion/final.md`

## Agent template

`agents/templates/fusion-research-agent.yaml` registers a reusable built-in agent template with `tree: fusion`.

## Verification commands

```bash
/usr/local/go/bin/go test ./internal/fusion ./internal/llm ./internal/config ./internal/evolution ./internal/domains -run 'TestConfig|TestOpenAICompat|TestRunPanel|TestJudge|TestToolLoop|TestSynthesize|TestRun_EndToEnd|TestFusionDeliberationTree|TestResolveTreeID_Fusion|TestEnvOverride_OpenRouter|TestValidate_OpenRouter' -count=1
/usr/local/go/bin/go test ./cmd/bt-agent -count=1
/usr/local/go/bin/go build ./cmd/bt-agent
```
