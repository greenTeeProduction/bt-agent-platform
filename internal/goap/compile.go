package goap

import (
	"fmt"
	"hash/fnv"
	"sort"
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
// behavior tree. Each plan step becomes a guarded sequence
//
//	Sequence: Step_<i>_<action>
//	  Condition: GoapStateMatches:<preconditions>   (world-state guard)
//	  <executable node>                             (engine Action | ChainAction)
//	  Action: ApplyGoapEffects:<effects>            (world-state write)
//
// wrapped in the platform's standard scaffold (PreGate → StrategyRouter →
// ReflectOnOutcome → OutcomeSelector) so gardener mutations and reflections
// operate on it unmodified. The StrategyRouter's last branch is a dynamic
// GOAP replan path: when a compiled step fails mid-plan, the planner replans
// from the world state the executed steps actually produced.
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

	router := SerializableNode{
		Type:     "Selector",
		Name:     "StrategyRouter",
		Children: []SerializableNode{planPath},
	}
	if !opts.DisableReplan {
		router.Children = append(router.Children, replanPath())
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

// compileStep turns one plan step into a guarded step sequence.
func compileStep(index int, step Action, opts CompileOptions) SerializableNode {
	seq := SerializableNode{
		Type: "Sequence",
		Name: fmt.Sprintf("Step_%d_%s", index+1, step.Name),
	}
	if spec := encodePairs(step.Preconditions); spec != "" {
		seq.Children = append(seq.Children, SerializableNode{
			Type: "Condition",
			Name: "GoapStateMatches:" + spec,
		})
	}
	seq.Children = append(seq.Children, executableNode(step, opts))
	if spec := encodePairs(step.Effects); spec != "" {
		seq.Children = append(seq.Children, SerializableNode{
			Type: "Action",
			Name: "ApplyGoapEffects:" + spec,
		})
	}
	return seq
}

// executableNode picks the execution strategy for a step: a registered
// engine action when one matches, an explicit LLM prompt when configured,
// else a generated prompt shaped by the step's semantics and style hints.
func executableNode(step Action, opts CompileOptions) SerializableNode {
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

// preGate is the standard input gate plus the world-state seed that makes
// the compiled guards sound: initial state + step effects reproduce exactly
// the state trajectory the planner verified.
func preGate(initial WorldState) SerializableNode {
	gate := SerializableNode{
		Type: "Sequence",
		Name: "PreGate",
		Children: []SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
			{Type: "Action", Name: "SetupDefaultTools"},
		},
	}
	if spec := encodePairs(initial); spec != "" {
		gate.Children = append(gate.Children, SerializableNode{
			Type: "Action",
			Name: "ApplyGoapEffects:" + spec,
		})
	}
	return gate
}

// replanPath is the dynamic fallback: seed GOAP state (idempotent), replan
// from the current world state, and execute via the engine's dynamic GOAP
// nodes — the same proven shape as goap.BuildSerializableTree.
func replanPath() SerializableNode {
	return SerializableNode{
		Type: "Sequence",
		Name: "GoapReplanPath",
		Children: []SerializableNode{
			{Type: "Action", Name: "SetupGoapTools"},
			{Type: "Action", Name: "PlanGoapActions"},
			{
				Type: "Selector",
				Name: "GoapReplanRouter",
				Children: []SerializableNode{
					{
						Type: "Sequence",
						Name: "GoapReplanExecute",
						Children: []SerializableNode{
							{Type: "Action", Name: "ExecuteGoapStep"},
							{Type: "Condition", Name: "HasMoreGoapSteps"},
						},
					},
					{Type: "Action", Name: "GoapFallback"},
				},
			},
		},
	}
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
	for k, v := range opts.Provenance {
		meta[k] = v
	}
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
	keys := make([]string, 0, len(ws))
	for k := range ws {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
	keys := make([]string, 0, len(ws))
	for k := range ws {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s = %v", k, ws[k]))
	}
	return strings.Join(parts, ", ")
}
