package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	btcore "github.com/rvitorper/go-bt/core"
)

func installGoapTestAction(t *testing.T, name string, action ActionFunc) {
	t.Helper()
	previous := actionRegistry[name]
	actionRegistry[name] = action
	t.Cleanup(func() {
		if previous == nil {
			delete(actionRegistry, name)
		} else {
			actionRegistry[name] = previous
		}
	})
}

// Protocol/actual filesystem tests; no model capability is claimed here.
func TestGoapObservationRetainsScopeAcrossRunningWithoutReplay(t *testing.T) {
	file := filepath.Join(t.TempDir(), "effect.txt")
	calls := 0
	installGoapTestAction(t, "ObserveRealFile", func(ctx *btcore.BTContext[Blackboard]) int {
		calls++
		if calls == 1 {
			if err := os.WriteFile(file, []byte("written"), 0600); err != nil {
				t.Error(err)
				return -1
			}
			return 0
		}
		actual, err := os.ReadFile(file)
		if err != nil {
			t.Error(err)
			return -1
		}
		if err = ctx.Blackboard.ObserveGoapFacts("actual file readback", map[string]any{"saved": string(actual) == "written"}); err != nil {
			t.Error(err)
			return -1
		}
		ctx.Blackboard.Result = "status: saved file and independently verified its contents"
		return 1
	})
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tree := &evolution.SerializableNode{Type: "GoapStep", Name: "observe_file", Metadata: map[string]any{"effects": map[string]any{"saved": true}}, Children: []evolution.SerializableNode{{Type: "Action", Name: "ObserveRealFile"}}}
	bb := &Blackboard{Task: "save and inspect a file", Reflections: store}
	command, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	RunTask(bb, command)
	if bb.Outcome != "success" || calls != 2 || goapWorldStateFrom(bb)["saved"] != true {
		t.Fatalf("lost observation: %+v calls=%d", bb, calls)
	}
	command.Run(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if calls != 2 {
		t.Fatal("terminal gate replayed its capability")
	}
	records, err := store.LoadAllStrict()
	if err != nil || len(records) != 1 || len(records[0].GoapChecks) != 1 {
		t.Fatalf("missing observation evidence: %+v %v", records, err)
	}
	if check := records[0].GoapChecks[0]; !check.Passed || check.Verify() != nil || check.Origin != "actual file readback" {
		t.Fatalf("unverifiable receipt: %+v", check)
	}
}

func TestGoapObservationRejectsStaleOrAssertedFacts(t *testing.T) {
	for _, source := range []string{"result", "file_task", "capability"} {
		t.Run(source, func(t *testing.T) {
			key := "done"
			if source == "result" {
				key = "result.done"
			}
			tree := &evolution.SerializableNode{Type: "GoapStep", Name: "must_observe", Metadata: map[string]any{"effect_source": source, "effects": map[string]any{key: true}}, Children: []evolution.SerializableNode{{Type: "AlwaysSucceed", Name: "no_effect"}}}
			bb := &Blackboard{Task: "perform a task", Result: `{"done":true}`, Results: []string{`{"done":true}`}, ChainState: map[string]any{"world_state": map[string]any{"done": true}, "goap_world_state": goap.WorldState{key: false}}}
			command, err := BuildAndValidate(tree, bb)
			if err != nil {
				t.Fatal(err)
			}
			RunTask(bb, command)
			if bb.Outcome == "success" || goapWorldStateFrom(bb)[key] != false {
				t.Fatalf("stale/public state became observed: %+v", bb)
			}
		})
	}
}

func TestGoapObservationFailureAfterFileCommitIsUncertainAndNotReplayed(t *testing.T) {
	root := t.TempDir()
	previous := ArtifactRootFn
	ArtifactRootFn = func(string) (string, error) { return root, nil }
	t.Cleanup(func() { ArtifactRootFn = previous })
	calls := 0
	installGoapTestAction(t, "PrepareObservedFile", func(ctx *btcore.BTContext[Blackboard]) int { calls++; ctx.Blackboard.Result = `{"total":42}`; return 1 })
	fileTask := evolution.SerializableNode{Type: "FileTask", Name: "write_file", Metadata: map[string]any{"user": "alice", "task": "write a result", "file_task": evolution.FileTaskSpec{Output: "result.json"}, "result_contract": map[string]any{"json_fields": map[string]any{"total": 42}}}, Children: []evolution.SerializableNode{{Type: "Action", Name: "PrepareObservedFile"}}}
	tree := &evolution.SerializableNode{Type: "GoapStep", Name: "inconsistent_expected_effect", Metadata: map[string]any{"effect_source": "file_task", "effects": map[string]any{"saved": false}, "effect_bindings": map[string]string{"saved": "verified"}}, Children: []evolution.SerializableNode{fileTask}}
	bb := &Blackboard{Task: "write a result", User: "alice"}
	command, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	RunTask(bb, command)
	if bb.Outcome != "uncertain" || len(bb.EvidenceEffects()) != 1 {
		t.Fatalf("committed failure disposition: %s %+v", bb.Outcome, bb.EvidenceEffects())
	}
	if _, err := os.ReadFile(filepath.Join(root, "result.json")); err != nil {
		t.Fatal(err)
	}
	command.Run(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if calls != 1 {
		t.Fatal("failed observation replayed committed work")
	}
}

func TestGoapSetupPreservesObservedStateAndOriginalCapabilities(t *testing.T) {
	actions := []goap.Action{{Name: "custom", Effects: goap.WorldState{"custom_done": true}}}
	bb := &Blackboard{ChainState: map[string]any{"goap_world_state": goap.WorldState{"already_saved": true}}}
	node := &evolution.SerializableNode{Type: "Action", Name: "SetupGoapTools", Metadata: map[string]any{"goap_actions": actions, "goap_goals": []*goap.Goal{goap.NewGoal("custom", 1, goap.WorldState{"custom_done": true})}}}
	command, err := BuildAndValidate(node, bb)
	if err != nil {
		t.Fatal(err)
	}
	RunTask(bb, command)
	if goapWorldStateFrom(bb)["already_saved"] != true {
		t.Fatal("setup discarded an observed fact")
	}
	var actual []goap.Action
	if err := decodeGoapValue(bb.ChainState["goap_actions"], &actual); err != nil || len(actual) != 1 || actual[0].Name != "custom" {
		t.Fatal("definition capabilities did not reach the planner")
	}
}

func TestGoapFinalGoalCannotBeMissingOrUnsatisfied(t *testing.T) {
	for _, metadata := range []map[string]any{nil, {"goap_final_goal": goap.NewGoal("saved", 1, goap.WorldState{"saved": true})}} {
		bb := &Blackboard{Task: "verify saved output", ChainState: map[string]any{"goap_world_state": goap.WorldState{"saved": false}}}
		command, err := BuildAndValidate(&evolution.SerializableNode{Type: "Action", Name: "VerifyGoapGoal", Metadata: metadata}, bb)
		if err != nil {
			t.Fatal(err)
		}
		RunTask(bb, command)
		if bb.Outcome == "success" {
			t.Fatal("missing or unsatisfied goal passed")
		}
	}
}

func TestGoapFailedReplanClearsPreviousExecutablePlan(t *testing.T) {
	goal := goap.NewGoal("saved", 1, goap.WorldState{"saved": true})
	bb := &Blackboard{Task: "save a report", ChainState: map[string]any{"goap_actions": []goap.Action{{Name: "unrelated", Effects: goap.WorldState{"other": true}}}, "goap_current_goal": map[string]any{"name": goal.Name, "conditions": goal.Conditions}, "goap_world_state": goap.WorldState{"saved": false}, "goap_plan": &goap.Plan{Goal: goal}, "goap_step_index": 0, "goap_steps": []string{"stale"}}}
	if got := GetAction("PlanGoapActions")(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("unreachable goal was replaced: %d", got)
	}
	for _, key := range []string{"goap_plan", "goap_step_index", "goap_steps"} {
		if _, ok := bb.ChainState[key]; ok {
			t.Fatalf("stale %s retained", key)
		}
	}
}
