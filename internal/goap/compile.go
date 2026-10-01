package goap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
)

// CompileOptions parameterizes the plan→BT compiler (ADR-133 Phase 3).
type CompileOptions struct {
	// TreeName names the root node; default "goap_plan_<goalslug>".
	TreeName string
	// InitialState seeds the tree's GOAP world state in the PreGate so the
	// compiled precondition guards hold along the happy path exactly as the
	// planner proved them. Defaults to DefaultInitialState().
	InitialState WorldState
	// LLMPrompts maps action names to prompt templates (the previously dead
	// GOAPTreeDefinition.LLMPrompts / BlackboardBridge.LLMActions concept):
	// a step with a registered prompt compiles to a ChainAction using it.
	LLMPrompts map[string]string
	// KnownAction reports whether an engine action with this name is
	// registered; matching steps compile to plain Action nodes (finally
	// consuming the engine registry). Nil → no steps map to engine actions.
	KnownAction func(name string) bool
	// StyleHints is appended to generated step prompts (persona preferences:
	// output style, language, verbosity).
	StyleHints string
	// MaxTokens for generated ChainAction steps; default 1024.
	MaxTokens int
	// DisableReplan omits the dynamic GOAP replan fallback path.
	DisableReplan bool
	// Provenance is merged into the root node metadata (user, source
	// pattern, parent goals — recorded for evolution lineage).
	Provenance map[string]any
}

// CompilePlanToTree compiles a GOAP plan into a persistent, evolvable
// behavior tree. Each GoapStep checks preconditions, executes its child, and
// advances runtime state only after fresh observations satisfy expected effects.
// A final goal check precedes the standard reflection/output scaffold. The
// optional replan path retains the original plan's capabilities and observed
// state; it cannot introduce alternate capabilities absent from that plan.
func CompilePlanToTree(plan *Plan, opts CompileOptions) (*SerializableNode, error) {
	if plan == nil || plan.Goal == nil {
		return nil, fmt.Errorf("goap: compile requires a plan with a goal")
	}
	if len(plan.Steps) == 0 {
		return nil, fmt.Errorf("goap: plan for %q has no steps to compile", plan.Goal.Name)
	}

	initial := opts.InitialState
	if initial == nil {
		initial = DefaultInitialState()
	}
	name := opts.TreeName
	if name == "" {
		name = "goap_plan_" + goalSlug(plan.Goal.Name)
	}

	planPath := SerializableNode{
		Type: "Sequence",
		Name: "PlanPath",
	}
	for i, step := range plan.Steps {
		planPath.Children = append(planPath.Children, compileStep(i, step, opts))
	}
	planPath.Children = append(planPath.Children, SerializableNode{Type: "Action", Name: "VerifyGoapGoal", Metadata: map[string]any{"goap_final_goal": plan.Goal}})

	router := SerializableNode{
		Type:     "Selector",
		Name:     "StrategyRouter",
		Children: []SerializableNode{planPath},
	}
	if !opts.DisableReplan {
		router.Children = append(router.Children, replanPath(plan, opts))
	}

	root := &SerializableNode{
		Type:     "Sequence",
		Name:     name,
		Metadata: provenanceMetadata(plan, opts),
		Children: []SerializableNode{
			preGate(initial),
			router,
			{Type: "Action", Name: "ReflectOnOutcome"},
			outcomeSelector(opts),
		},
	}
	return root, nil
}

// compileStep wraps an executable in a gate that observes effects. Predicted
// planner effects remain expectations, never unconditional runtime state writes.
func compileStep(index int, step Action, opts CompileOptions) SerializableNode {
	executable := executableNode(step, opts)
	metadata := map[string]any{"preconditions": step.Preconditions, "effects": step.Effects}
	for _, key := range []string{"effect_source", "effect_bindings"} {
		if value, ok := step.Metadata[key]; ok {
			metadata[key] = value
		}
	}
	return SerializableNode{Type: "GoapStep", Name: fmt.Sprintf("Step_%d_%s", index+1, step.Name), Metadata: metadata, Children: []SerializableNode{executable}}
}

// CompileActionStep uses the same execution/effect boundary for dynamic plans.
func CompileActionStep(index int, step Action, opts CompileOptions) SerializableNode {
	return compileStep(index, step, opts)
}

// executableNode picks the execution strategy for a step: a registered
// engine action when one matches, an explicit LLM prompt when configured,
// else a generated prompt shaped by the step's semantics and style hints.
func executableNode(step Action, opts CompileOptions) SerializableNode {
	if raw, ok := step.Metadata["execution"]; ok {
		data, err := json.Marshal(raw)
		var node SerializableNode
		if err == nil {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			err = decoder.Decode(&node)
		}
		if err != nil {
			return SerializableNode{Type: "InvalidGOAPExecutor", Name: step.Name}
		}
		return node
	}

	if opts.KnownAction != nil && opts.KnownAction(step.Name) {
		return SerializableNode{Type: "Action", Name: step.Name}
	}

	prompt := ""
	if opts.LLMPrompts != nil {
		prompt = opts.LLMPrompts[step.Name]
	}
	if prompt == "" {
		prompt = stepPrompt(step, opts.StyleHints)
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	return SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:" + prompt,
		Metadata: map[string]any{
			"max_tokens":  float64(maxTokens),
			"goap_step":   step.Name,
			"plan_source": "goap_compiler",
		},
	}
}

// stepPrompt derives an LLM prompt from a plan step's name and effects.
func stepPrompt(step Action, styleHints string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Execute plan step %q for the task: {{.Task}}.", step.Name)
	if len(step.Effects) > 0 {
		b.WriteString(" This step must achieve: ")
		b.WriteString(encodePairsReadable(step.Effects))
		b.WriteString(".")
	}
	b.WriteString(" Return only the concrete result of this step.")
	if styleHints != "" {
		b.WriteString(" ")
		b.WriteString(styleHints)
	}
	return b.String()
}

// preGate validates input and seeds only facts absent from runtime state.
// Existing observations take precedence over the declared starting assumptions.
func preGate(initial WorldState) SerializableNode {
	return SerializableNode{Type: "Sequence", Name: "PreGate", Children: []SerializableNode{
		{Type: "Condition", Name: "ValidateInput"}, {Type: "Action", Name: "SetupDefaultTools"},
		{Type: "Action", Name: "SeedGoapState", Metadata: map[string]any{"goap_initial_state": initial}},
	}}
}

// Replanning retains the original capabilities and goal and seeds only absent
// initial facts. A memory sequence prevents planning from restarting on a tick.
func replanPath(plan *Plan, opts CompileOptions) SerializableNode {
	return SerializableNode{Type: "MemSequence", Name: "GoapReplanPath", Children: []SerializableNode{
		{Type: "Action", Name: "SetupGoapTools", Metadata: map[string]any{"goap_actions": plan.Steps, "goap_goals": []*Goal{plan.Goal}, "goap_llm_prompts": opts.LLMPrompts}},
		{Type: "Action", Name: "PlanGoapActions"}, {Type: "Action", Name: "ExecuteGoapStep"},
	}}
}

// outcomeSelector mirrors the platform's standard self-correction tail.
func outcomeSelector(opts CompileOptions) SerializableNode {
	prompt := "Self-correct the previous step and fix any issues."
	if opts.StyleHints != "" {
		prompt += " " + opts.StyleHints
	}
	return SerializableNode{
		Type: "Selector",
		Name: "OutcomeSelector",
		Children: []SerializableNode{
			{Type: "Condition", Name: "WasSuccessful"},
			{
				Type:     "ChainAction",
				Name:     "llm_call:" + prompt,
				Metadata: map[string]any{"max_tokens": float64(512)},
			},
		},
	}
}

// provenanceMetadata records how the tree was manufactured so evolution and
// audits can trace lineage (goal, plan hash, steps, plus caller-supplied
// provenance like user and source pattern).
func provenanceMetadata(plan *Plan, opts CompileOptions) map[string]any {
	steps := make([]any, 0, len(plan.Steps))
	for _, s := range plan.Steps {
		steps = append(steps, s.Name)
	}
	meta := map[string]any{
		"generated_by": "goap_compiler",
		"goal":         plan.Goal.Name,
		"plan_hash":    PlanHash(plan),
		"plan_steps":   steps,
		"plan_cost":    plan.Cost,
	}
	maps.Copy(meta, opts.Provenance)
	return meta
}

// PlanHash fingerprints a plan (goal name + conditions + step names) for
// provenance and duplicate detection.
func PlanHash(plan *Plan) string {
	h := fnv.New32a()
	if plan.Goal != nil {
		_, _ = h.Write([]byte(plan.Goal.Name))
		_, _ = h.Write([]byte(plan.Goal.Conditions.String()))
	}
	for _, s := range plan.Steps {
		_, _ = h.Write([]byte("|" + s.Name))
	}
	return fmt.Sprintf("%08x", h.Sum32())
}

// encodePairs renders a world state as the sorted "k=v,k2=v2" spec used by
// GoapStateMatches / ApplyGoapEffects node names. Pairs whose key or value
// would corrupt the encoding (embedded "," or "=") are skipped.
func encodePairs(ws WorldState) string {
	if len(ws) == 0 {
		return ""
	}
	keys := slices.Sorted(maps.Keys(ws))
	var parts []string
	for _, k := range keys {
		v := fmt.Sprintf("%v", ws[k])
		if strings.ContainsAny(k, ",=") || strings.ContainsAny(v, ",=") {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

// encodePairsReadable renders a world state for prompts ("k = v, k2 = v2").
func encodePairsReadable(ws WorldState) string {
	keys := slices.Sorted(maps.Keys(ws))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s = %v", k, ws[k]))
	}
	return strings.Join(parts, ", ")
}
