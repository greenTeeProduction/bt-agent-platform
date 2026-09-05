package goap

import (
	"strings"
	"testing"
)

// compileTestPlan builds a two-step plan with preconditions and effects.
func compileTestPlan() *Plan {
	goal := NewGoal("ship feature", 0.7, WorldState{"task_status": "completed"})
	return &Plan{
		Goal: goal,
		Steps: []Action{
			{
				Name:          "analyze_requirements",
				Cost:          1.0,
				Preconditions: WorldState{"has_result": false},
				Effects:       WorldState{"has_analysis": true},
			},
			{
				Name:          "execute_general",
				Cost:          1.0,
				Preconditions: WorldState{"task_type": "general"},
				Effects:       WorldState{"has_result": true, "task_status": "completed"},
			},
		},
		Cost: 2.0,
	}
}

func findChild(n *SerializableNode, name string) *SerializableNode {
	if n == nil {
		return nil
	}
	if n.Name == name {
		return n
	}
	for i := range n.Children {
		if found := findChild(&n.Children[i], name); found != nil {
			return found
		}
	}
	return nil
}

func TestCompilePlanToTree_Scaffold(t *testing.T) {
	tree, err := CompilePlanToTree(compileTestPlan(), CompileOptions{})
	if err != nil {
		t.Fatalf("CompilePlanToTree: %v", err)
	}
	if tree.Type != "Sequence" {
		t.Fatalf("root type = %s", tree.Type)
	}
	if len(tree.Children) != 4 {
		t.Fatalf("root children = %d, want 4 (PreGate, StrategyRouter, Reflect, OutcomeSelector)", len(tree.Children))
	}
	if tree.Children[0].Name != "PreGate" ||
		tree.Children[1].Name != "StrategyRouter" ||
		tree.Children[2].Name != "ReflectOnOutcome" ||
		tree.Children[3].Name != "OutcomeSelector" {
		t.Fatalf("unexpected scaffold: %s %s %s %s",
			tree.Children[0].Name, tree.Children[1].Name, tree.Children[2].Name, tree.Children[3].Name)
	}

	// StrategyRouter: compiled plan path first, replan fallback last.
	router := &tree.Children[1]
	if router.Type != "Selector" || len(router.Children) != 2 {
		t.Fatalf("router = %s with %d children", router.Type, len(router.Children))
	}
	if router.Children[0].Name != "PlanPath" || router.Children[1].Name != "GoapReplanPath" {
		t.Fatalf("router children: %s, %s", router.Children[0].Name, router.Children[1].Name)
	}

	// PreGate seeds the initial world state.
	seed := findChild(&tree.Children[0], "ApplyGoapEffects:has_result=false,task_type=general")
	if seed == nil {
		t.Fatalf("PreGate missing initial-state seed; children: %+v", tree.Children[0].Children)
	}
}

func TestCompilePlanToTree_StepGuardsAndEffects(t *testing.T) {
	tree, err := CompilePlanToTree(compileTestPlan(), CompileOptions{})
	if err != nil {
		t.Fatalf("CompilePlanToTree: %v", err)
	}
	step1 := findChild(tree, "Step_1_analyze_requirements")
	if step1 == nil {
		t.Fatal("missing step 1 sequence")
	}
	if len(step1.Children) != 3 {
		t.Fatalf("step 1 children = %d, want guard + exec + effects", len(step1.Children))
	}
	if step1.Children[0].Type != "Condition" || step1.Children[0].Name != "GoapStateMatches:has_result=false" {
		t.Fatalf("step 1 guard = %s %q", step1.Children[0].Type, step1.Children[0].Name)
	}
	if step1.Children[2].Type != "Action" || step1.Children[2].Name != "ApplyGoapEffects:has_analysis=true" {
		t.Fatalf("step 1 effects = %s %q", step1.Children[2].Type, step1.Children[2].Name)
	}

	// Multi-effect step encodes sorted pairs.
	step2 := findChild(tree, "Step_2_execute_general")
	if step2 == nil {
		t.Fatal("missing step 2 sequence")
	}
	last := step2.Children[len(step2.Children)-1]
	if last.Name != "ApplyGoapEffects:has_result=true,task_status=completed" {
		t.Fatalf("step 2 effects = %q", last.Name)
	}
}

func TestCompilePlanToTree_ExecutableNodeSelection(t *testing.T) {
	plan := compileTestPlan()

	// Default: LLM ChainAction with a derived prompt.
	tree, _ := CompilePlanToTree(plan, CompileOptions{StyleHints: "Answer in German."})
	step := findChild(tree, "Step_1_analyze_requirements")
	exec := step.Children[1]
	if exec.Type != "ChainAction" || !strings.HasPrefix(exec.Name, "llm_call:") {
		t.Fatalf("exec node = %s %q", exec.Type, exec.Name)
	}
	if !strings.Contains(exec.Name, "analyze_requirements") || !strings.Contains(exec.Name, "Answer in German.") {
		t.Fatalf("prompt missing step name or style hints: %q", exec.Name)
	}

	// Explicit prompt template wins over the derived prompt.
	tree, _ = CompilePlanToTree(plan, CompileOptions{
		LLMPrompts: map[string]string{"analyze_requirements": "Custom prompt for {{.Task}}"},
	})
	exec = findChild(tree, "Step_1_analyze_requirements").Children[1]
	if exec.Name != "llm_call:Custom prompt for {{.Task}}" {
		t.Fatalf("explicit prompt not used: %q", exec.Name)
	}

	// Registered engine action wins over any LLM path.
	tree, _ = CompilePlanToTree(plan, CompileOptions{
		KnownAction: func(name string) bool { return name == "analyze_requirements" },
	})
	exec = findChild(tree, "Step_1_analyze_requirements").Children[1]
	if exec.Type != "Action" || exec.Name != "analyze_requirements" {
		t.Fatalf("registered action not used: %s %q", exec.Type, exec.Name)
	}
	// The other step still compiles to a ChainAction.
	exec2 := findChild(tree, "Step_2_execute_general").Children[1]
	if exec2.Type != "ChainAction" {
		t.Fatalf("unregistered step should stay ChainAction, got %s", exec2.Type)
	}
}

func TestCompilePlanToTree_Provenance(t *testing.T) {
	plan := compileTestPlan()
	tree, _ := CompilePlanToTree(plan, CompileOptions{
		Provenance: map[string]any{"user": "nico"},
	})
	meta := tree.Metadata
	if meta["generated_by"] != "goap_compiler" || meta["goal"] != "ship feature" || meta["user"] != "nico" {
		t.Fatalf("provenance = %v", meta)
	}
	if meta["plan_hash"] != PlanHash(plan) {
		t.Fatalf("plan hash mismatch: %v vs %s", meta["plan_hash"], PlanHash(plan))
	}
	steps, ok := meta["plan_steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("plan_steps = %v", meta["plan_steps"])
	}
}

func TestCompilePlanToTree_DisableReplan(t *testing.T) {
	tree, _ := CompilePlanToTree(compileTestPlan(), CompileOptions{DisableReplan: true})
	if findChild(tree, "GoapReplanPath") != nil {
		t.Fatal("replan path should be omitted")
	}
}

func TestCompilePlanToTree_Errors(t *testing.T) {
	if _, err := CompilePlanToTree(nil, CompileOptions{}); err == nil {
		t.Fatal("expected error for nil plan")
	}
	empty := &Plan{Goal: NewGoal("g", 0.5, WorldState{"a": true})}
	if _, err := CompilePlanToTree(empty, CompileOptions{}); err == nil {
		t.Fatal("expected error for empty plan")
	}
}

func TestPlanHash_Distinguishes(t *testing.T) {
	a := compileTestPlan()
	b := compileTestPlan()
	if PlanHash(a) != PlanHash(b) {
		t.Fatal("identical plans must hash equal")
	}
	b.Steps = b.Steps[:1]
	if PlanHash(a) == PlanHash(b) {
		t.Fatal("different step lists must hash differently")
	}
}

func TestEncodePairs_SkipsCorruptingValues(t *testing.T) {
	spec := encodePairs(WorldState{"ok": true, "bad,key": true, "bad": "a=b"})
	if spec != "ok=true" {
		t.Fatalf("spec = %q", spec)
	}
}
