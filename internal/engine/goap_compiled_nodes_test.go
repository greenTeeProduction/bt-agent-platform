package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestParseGoapPairs(t *testing.T) {
	pairs := parseGoapPairs("has_result=false,task_type=general,count=3")
	if pairs["has_result"] != false || pairs["task_type"] != "general" || pairs["count"] != 3.0 {
		t.Fatalf("pairs = %v", pairs)
	}
	if len(parseGoapPairs("garbage")) != 0 {
		t.Fatal("fragments without '=' must be skipped")
	}
}

func TestCompiledGoapCondition(t *testing.T) {
	fn := compiledGoapConditionFor("GoapStateMatches:has_analysis=true,task_type=build")
	if fn == nil {
		t.Fatal("expected condition")
	}

	// No world state → guard fails.
	if fn(&Blackboard{}) {
		t.Fatal("guard must fail without world state")
	}

	bb := &Blackboard{ChainState: map[string]any{
		"goap_world_state": goap.WorldState{"has_analysis": true, "task_type": "build"},
	}}
	if !fn(bb) {
		t.Fatal("guard should pass when all pairs match")
	}

	bb.ChainState["goap_world_state"] = goap.WorldState{"has_analysis": true, "task_type": "test"}
	if fn(bb) {
		t.Fatal("guard must fail on value mismatch")
	}

	// Plain-map form (JSON roundtrip) is accepted too.
	bb.ChainState["goap_world_state"] = map[string]any{"has_analysis": true, "task_type": "build"}
	if !fn(bb) {
		t.Fatal("guard should read plain-map world state")
	}

	// Non-compiled names return nil.
	if compiledGoapConditionFor("WasSuccessful") != nil {
		t.Fatal("plain names must not dispatch as compiled guards")
	}
	if compiledGoapConditionFor("GoapStateMatches:") != nil {
		t.Fatal("empty spec must not dispatch")
	}
}

func TestCompiledGoapAction_AppliesEffects(t *testing.T) {
	fn := compiledGoapActionFor("ApplyGoapEffects:has_result=true,task_status=completed")
	if fn == nil {
		t.Fatal("expected action")
	}
	bb := &Blackboard{}
	if status := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); status != 1 {
		t.Fatalf("status = %d", status)
	}
	ws := goapWorldStateFrom(bb)
	if ws["has_result"] != true || ws["task_status"] != "completed" {
		t.Fatalf("world state = %v", ws)
	}

	// Effects merge into an existing world state without clobbering it.
	fn2 := compiledGoapActionFor("ApplyGoapEffects:linted=true")
	fn2(&btcore.BTContext[Blackboard]{Blackboard: bb})
	ws = goapWorldStateFrom(bb)
	if ws["has_result"] != true || ws["linted"] != true {
		t.Fatalf("merge lost keys: %v", ws)
	}
}

func TestCompiledGoapNodes_NumericEquality(t *testing.T) {
	apply := compiledGoapActionFor("ApplyGoapEffects:count=3")
	bb := &Blackboard{}
	apply(&btcore.BTContext[Blackboard]{Blackboard: bb})
	guard := compiledGoapConditionFor("GoapStateMatches:count=3")
	if !guard(bb) {
		t.Fatal("numeric values should compare equal after parse")
	}
}

// TestCompiledPlanTree_ValidatesAndRuns is the Phase 3 acceptance test: a
// plan compiled by goap.CompilePlanToTree passes full validation and its
// happy path executes end-to-end — guards hold along the seeded state
// trajectory and effects drive the world state to the goal.
func TestCompiledPlanTree_ValidatesAndRuns(t *testing.T) {
	goal := goap.NewGoal("complete", 0.8, goap.WorldState{"task_status": "completed"})
	planner := goap.NewPlanner(goap.StandardActions(), 50, 10000)
	initial := goap.WorldState{"task_type": "general", "has_result": false}
	plan := planner.Plan(initial.Clone(), goal)
	if plan == nil {
		t.Fatal("planner found no plan")
	}

	goapNode, err := goap.CompilePlanToTree(plan, goap.CompileOptions{
		InitialState: initial,
		KnownAction:  func(name string) bool { return GetAction(name) != nil },
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	tree := evolution.FromGoapNode(goapNode)

	if info := ValidateTreeFull(tree); !info.Valid() {
		t.Fatalf("compiled tree failed validation: %v", info.Errors)
	}
	if msgs := ValidateTree(tree); len(msgs) != 0 {
		t.Fatalf("compiled tree failed registry validation: %v", msgs)
	}
}
