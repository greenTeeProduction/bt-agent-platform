package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

func assertUnqualifiedProposal(t *testing.T, store *evolution.TreeStore, kg *knowledge.KnowledgeGraph, id string, out map[string]any) *evolution.SerializableNode {
	t.Helper()
	if out["persisted"] != false || out["qualified"] != false || out["proposal_saved"] != true || out["qualification_error"] == nil {
		t.Fatalf("unqualified search was not retained as a proposal: %v", out)
	}
	data, err := os.ReadFile(out["proposal_file"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Candidate *evolution.SerializableNode `json:"candidate"`
	}
	if err = json.Unmarshal(data, &proposal); err != nil || proposal.Candidate == nil {
		t.Fatalf("proposal not retained: %v", err)
	}
	for _, name := range []string{id, id + "-evolved"} {
		if loaded, err := store.LoadNamed(name); err != nil || loaded != nil {
			t.Fatalf("unqualified proposal became runnable as %s: %v", name, err)
		}
		if tree, _, err := evolution.NewRuntimeReleaseStore(filepath.Join(store.Dir(), "runtime-versions")).Resolve(name, ""); err != nil || tree != nil {
			t.Fatalf("unqualified runtime authority: %s %v", name, err)
		}
	}
	if kg != nil {
		if kg.Trees[id+"-evolved"] != nil {
			t.Fatal("unqualified descendant registered for discovery")
		}
		if meta := kg.Trees[id]; meta != nil && (meta.StructuralFitness != 0 || meta.EvolvedCount != 0) {
			t.Fatal("unpublished estimate improved runtime discovery")
		}
	}
	return proposal.Candidate
}

func TestManualEvolutionCannotBorrowFailureEvidence(t *testing.T) {
	deps := taskFactoryDeps(t)
	created := invokeFactory(t, deps, "bt_factory_create", factorySumRequest)
	id := created["tree_id"].(string)
	base, err := publicationBaseline(deps, id, "")
	if err != nil {
		t.Fatal(err)
	}
	version, _ := evolution.TreeVersion(base)
	deps.refStore, err = evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, change := range []func(*evolution.Record){
		func(r *evolution.Record) { r.TreeName = "other" },
		func(r *evolution.Record) { r.User = "other" },
		func(r *evolution.Record) { r.TreeVersion = "older" },
		func(r *evolution.Record) { r.ExecutionVersions = []string{"different_expansion"} },
		func(r *evolution.Record) { r.EvidenceKind = evolution.EvidenceCompilation },
		func(_ *evolution.Record) {}, func(_ *evolution.Record) {},
	} {
		r := evolution.Record{TaskID: fmt.Sprintf("scope-%d", i), TreeName: id, TreeVersion: version, ExecutionVersions: []string{version}, EvidenceKind: evolution.EvidenceExecution, Outcome: evolution.Failure}
		change(&r)
		if err := deps.refStore.Save(&r); err != nil {
			t.Fatal(err)
		}
	}
	out := invokeFactory(t, deps, "bt_evolve", fmt.Sprintf(`{"tree":%q}`, id))
	if out["evolved"] != false || out["matching_failures"] != float64(2) || out["proposal_saved"] != nil {
		t.Fatalf("borrowed unrelated evidence: %v", out)
	}
}

// Both entrypoints use the actual benchmark model and real agent executions.
// The controlled fault changes worker instructions; no LLM output is supplied.
func TestLiveManualAndGeneticPublication(t *testing.T) {
	for _, tool := range []string{"bt_evolve", "bt_evolve_genetic"} {
		t.Run(tool, func(t *testing.T) {
			model := benchmark.RealLLM(t)
			deps := taskFactoryDeps(t)
			user := ""
			request := factorySumRequest
			if tool == "bt_evolve" {
				user = "manual-benchmark"
				request = strings.Replace(request, `"task":`, `"user":"`+user+`","task":`, 1)
			}
			created := invokeFactory(t, deps, "bt_factory_create", request)
			id := created["tree_id"].(string)
			feedbackPath := filepath.Join(t.TempDir(), "feedback.json")
			deps.kg.ConfigureFeedbackPersistence(feedbackPath, time.Nanosecond)
			base, err := publicationBaseline(deps, id, user)
			if err != nil {
				t.Fatal(err)
			}
			gate := &base.Children[len(base.Children)-1]
			gate.Children = gate.Children[:1]
			gate.Children[0].Name = "llm_call:Compute seventeen plus twenty-five. Return only a JSON object with the field wrong_total containing the calculated sum. Use exactly that field name."
			dir := deps.treeStore.Dir()
			if user != "" {
				dir = deps.personaStore.Workspace(user).TreesDir()
			}
			if _, err = evolution.SaveNamedTree(dir, id, base); err != nil {
				t.Fatal(err)
			}
			baseVersion, _ := evolution.TreeVersion(base)
			deps.refStore, err = evolution.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			registry, err := agent.NewRegistry(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = registry.Create(agent.Definition{Name: "publication_sum", Tree: id, Metadata: map[string]string{"user": user}}); err != nil {
				t.Fatal(err)
			}
			runner := &agent.RunDeps{Registry: registry, LLM: model, RefStore: deps.refStore, ResolveTree: domains.ResolveTreeID, ResolveTreeForUser: domains.ResolveTreeIDForUser}
			opts := agent.RunOptions{DisableBlackboard: true, SkipSLORecording: true}
			for range 3 {
				before, err := runner.RunOnce(t.Context(), "publication_sum", "Execute the saved task.", opts)
				if before == nil || before.Outcome == "success" || before.TreeVersion != baseVersion {
					t.Fatalf("baseline did not fail as declared: %+v %v", before, err)
				}
			}
			args := fmt.Sprintf(`{"tree":%q,"user":%q,"population":4,"generations":1}`, id, user)
			out := invokeFactory(t, deps, tool, args)
			evidence := map[string]any{"tool": tool, "result": out}
			defer func() {
				if path := os.Getenv("BT_MANUAL_PUBLICATION_REPORT"); path != "" {
					evidence["test_passed"] = !t.Failed()
					data, err := json.MarshalIndent(evidence, "", "  ")
					if err != nil {
						t.Error(err)
						return
					}
					if err = os.WriteFile(path+"."+tool+".json", data, 0600); err != nil {
						t.Error(err)
					}
				}
			}()
			if out["qualified"] != true || out["persisted"] != true {
				t.Fatalf("measured candidate not published: %v", out)
			}
			fitness := invokeFactory(t, deps, "bt_get_fitness", args)
			if fitness["total_tasks"] != float64(0) {
				t.Fatalf("candidate inherited predecessor history: %v", fitness)
			}
			inspected := invokeFactory(t, deps, "bt_get_tree", args)
			encoded, _ := json.Marshal(inspected)
			var inspectedTree evolution.SerializableNode
			if err = json.Unmarshal(encoded, &inspectedTree); err != nil {
				t.Fatal(err)
			}
			inspectedVersion, _ := evolution.TreeVersion(&inspectedTree)
			if inspectedVersion != out["candidate_version"] {
				t.Fatal("inspection returned stale legacy definition")
			}
			if user != "" {
				template, err := loadAutomationTemplate(deps, user, id)
				if err != nil {
					t.Fatal(err)
				}
				templateVersion, err := evolution.TreeVersion(template)
				if err != nil || templateVersion != inspectedVersion {
					t.Fatalf("automation reused stale pre-evolution task: %s %v", templateVersion, err)
				}
				evidence["automation_template_version"] = templateVersion
			}
			after, err := runner.RunOnce(t.Context(), "publication_sum", "Execute the saved task.", opts)
			evidence["after"] = after
			if err != nil || after == nil || after.Outcome != "success" || after.TreeVersion != out["candidate_version"] {
				t.Fatalf("publication not adopted: %+v %v", after, err)
			}
			contract := evolution.ResultContract{JSONFields: map[string]json.RawMessage{"total": json.RawMessage(`42`)}}
			if err := contract.Verify(after.Output); err != nil {
				t.Fatalf("independent arithmetic check: %v", err)
			}
			fitness = invokeFactory(t, deps, "bt_get_fitness", args)
			evidence["fitness"] = fitness
			if fitness["total_tasks"] != float64(1) || fitness["successes"] != float64(1) {
				t.Fatalf("actual adopted execution missing: %v", fitness)
			}
			if user == "" {
				restarted := knowledge.NewKnowledgeGraph()
				restarted.Register(&knowledge.TreeMeta{ID: id, Name: id})
				if err := restarted.LoadFeedback(feedbackPath); err != nil {
					t.Fatal(err)
				}
				meta := restarted.Trees[id]
				if out["feedback_error"] != nil || meta.EvolvedCount != 1 || meta.StructuralFitness != evolution.AssessGovernance(&inspectedTree).Score {
					t.Fatalf("qualified feedback did not survive restart: %+v", meta)
				}
				evidence["persisted_feedback"] = meta
			} else if len(deps.kg.Trees) != 0 {
				t.Fatal("personal evolution credited the shared graph")
			}
		})
	}
}
