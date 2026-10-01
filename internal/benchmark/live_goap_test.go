package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
)

// Both planning routes must perform actual dependent file tasks. Expected
// arithmetic is retained in oracles, not injected into model responses.
func TestLiveGoapPlansObserveActualPersonalFileEffects(t *testing.T) {
	model := RealLLM(t)
	var reports []map[string]any
	for _, mode := range []string{"compiled", "dynamic"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			previous := engine.ArtifactRootFn
			engine.ArtifactRootFn = func(user string) (string, error) {
				if user != "goap-owner" {
					return "", fmt.Errorf("wrong owner")
				}
				return root, nil
			}
			t.Cleanup(func() { engine.ArtifactRootFn = previous })
			if err := os.WriteFile(filepath.Join(root, "expenses.json"), []byte(`{"expenses":[12,9,4]}`), 0600); err != nil {
				t.Fatal(err)
			}
			const task = "Create my expense total report, then create a second report with twice that total."
			fileStep := func(name, input, output, prompt string, expected map[string]any) evolution.SerializableNode {
				contract := map[string]any{"json_fields": expected}
				return evolution.SerializableNode{Type: "FileTask", Name: name, Metadata: map[string]any{"user": "goap-owner", "task": task, "file_task": evolution.FileTaskSpec{Input: input, Output: output}, "result_contract": contract}, Children: []evolution.SerializableNode{
					{Type: "QualityGate", Name: name + "_values", Metadata: map[string]any{"result_contract": contract}, Children: []evolution.SerializableNode{
						{Type: "ChainAction", Name: "llm_call:" + prompt + "\nInput data:\n{{.ChainState.task_input}}", Metadata: map[string]any{"max_tokens": 128}},
					}},
				}}
			}
			first := fileStep("sum_expenses", "expenses.json", "total.json", "Sum the expenses in the input. Return JSON with exactly the field total containing the calculated sum.", map[string]any{"total": 25})
			second := fileStep("double_total", "total.json", "double.json", "Read total from the input and multiply it by two. Return JSON with exactly the field double_total containing the calculated product.", map[string]any{"double_total": 50})
			actions := []goap.Action{
				{Name: "save_total", Cost: 1, Preconditions: goap.WorldState{"report_written": false}, Effects: goap.WorldState{"report_written": true}, Metadata: map[string]any{"execution": first, "effect_source": "file_task", "effect_bindings": map[string]string{"report_written": "verified"}}},
				{Name: "save_double", Cost: 1, Preconditions: goap.WorldState{"report_written": true}, Effects: goap.WorldState{"double_written": true}, Metadata: map[string]any{"execution": second, "effect_source": "file_task", "effect_bindings": map[string]string{"double_written": "verified"}}},
			}
			goal := goap.NewGoal("both reports saved", 1, goap.WorldState{"report_written": true, "double_written": true})
			initial := goap.WorldState{"report_written": false, "double_written": false}
			var source *goap.SerializableNode
			if mode == "compiled" {
				plan := goap.NewPlanner(actions, 10, 100).Plan(initial, goal)
				var err error
				source, err = goap.CompilePlanToTree(plan, goap.CompileOptions{TreeName: "personal-goap", InitialState: initial, Provenance: map[string]any{"user": "goap-owner"}})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				node := goap.BuildSerializableTree(goap.GOAPTreeDefinition{Actions: actions, Goals: []*goap.Goal{goal}, Config: goap.DefaultGOAPConfig()})
				node.Children[0].Metadata["goap_initial_state"] = initial
				source = &node
			}
			tree := evolution.FromGoapNode(source)
			store, err := evolution.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			bb := &engine.Blackboard{Task: task, User: "goap-owner", LLM: model, Reflections: store, TreeID: "personal-goap"}
			command, err := engine.BuildAndValidate(tree, bb)
			if err != nil {
				t.Fatal(err)
			}
			before := model.(*LiveModel).Evidence().Calls
			engine.RunTask(bb, command)
			if bb.Outcome != "success" {
				t.Fatalf("GOAP failed: %s %s %+v", bb.Outcome, bb.Result, bb.ChainState)
			}
			for name, fields := range map[string]map[string]int{"total.json": {"total": 12 + 9 + 4}, "double.json": {"double_total": 2 * (12 + 9 + 4)}} {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				var actual map[string]int
				if err = json.Unmarshal(data, &actual); err != nil {
					t.Fatal(err)
				}
				for key, want := range fields {
					if actual[key] != want {
						t.Fatalf("independent artifact check: %s %s", name, data)
					}
				}
			}
			records, err := store.LoadAllStrict()
			if err != nil || len(records) != 1 {
				t.Fatalf("evidence: %+v %v", records, err)
			}
			r := records[0]
			if len(r.Effects) != 2 || len(r.GoapChecks) != 2 || !r.VerifiedFinalResult() {
				t.Fatalf("missing governed effects: %+v", r)
			}
			for _, check := range r.GoapChecks {
				if !check.Passed || check.Source != "file_task" || check.Verify() != nil {
					t.Fatalf("unverifiable observation: %+v", check)
				}
			}
			if r.Effects[0].Scope == r.Effects[1].Scope {
				t.Fatal("separate steps shared an observation scope")
			}
			calls := model.(*LiveModel).Evidence().Calls - before
			if calls < 2 {
				t.Fatal("dependent tasks did not use real inference")
			}
			reports = append(reports, map[string]any{"mode": mode, "tree": tree, "record": r, "model": model.(*LiveModel).Evidence(), "calls": calls, "independent_artifacts_verified": true})
			t.Logf("%s: two observed file effects, total=25, double_total=50, real calls=%d", mode, calls)
		})
	}
	if path := os.Getenv("BT_GOAP_REPORT"); path != "" {
		data, err := json.MarshalIndent(reports, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLiveGoapModelResultRequiresExactValueOracle(t *testing.T) {
	model := RealLLM(t)
	tree := &evolution.SerializableNode{Type: "GoapStep", Name: "calculate_total", Metadata: map[string]any{"effect_source": "result", "effects": map[string]any{"result.total": 42}}, Children: []evolution.SerializableNode{{Type: "ChainAction", Name: "llm_call:Add 19 and 23. Return only JSON with field total and the computed sum.", Metadata: map[string]any{"max_tokens": 96}}}}
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bb := &engine.Blackboard{Task: "Calculate my total", User: "goap-owner", LLM: model, Reflections: store, TreeID: "goap-result"}
	command, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	engine.RunTask(bb, command)
	records, err := store.LoadAllStrict()
	if err != nil || len(records) != 1 {
		t.Fatalf("missing result evidence: %v", err)
	}
	r := records[0]
	if !r.VerifiedFinalResult() || len(r.GoapChecks) != 1 || !r.GoapChecks[0].Passed || r.GoapChecks[0].Verify() != nil || len(r.Effects) != 0 {
		t.Fatalf("model value did not pass its exact oracle: %+v", r)
	}
	var actual map[string]int
	result := strings.TrimSpace(r.Result)
	if strings.HasPrefix(result, "```json\n") && strings.HasSuffix(result, "```") {
		result = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(result, "```json\n"), "```"))
	}
	if err = json.Unmarshal([]byte(result), &actual); err != nil || actual["total"] != 19+23 {
		t.Fatalf("independent value check: %s %v", r.Result, err)
	}
	t.Logf("real model result total=42; external effect receipts=0; model=%+v", model.(*LiveModel).Evidence())
}
