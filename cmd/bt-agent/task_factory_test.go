package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/agentexec"
	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/persona"
)

func taskFactoryDeps(t *testing.T) *mcpDeps {
	t.Helper()
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	t.Setenv("BT_REFLECTIONS_DIR", t.TempDir())
	t.Setenv("BT_MUTATED_TREES_DIR", t.TempDir())
	treeStore, err := evolution.NewTreeStore(os.Getenv("BT_REFLECTIONS_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	personas, err := persona.NewStore(agent.UsersDir())
	if err != nil {
		t.Fatal(err)
	}
	return &mcpDeps{treeStore: treeStore, kg: knowledge.NewKnowledgeGraph(), personaStore: personas}
}

func invokeFactory(t *testing.T, deps *mcpDeps, tool, args string) map[string]any {
	t.Helper()
	server := engine.NewServer("factory-test")
	registerMCPTools(server, deps)
	result, ok := server.Invoke(tool, json.RawMessage(args))
	if !ok || result == nil || len(result.Content) == 0 {
		t.Fatal("missing factory tool response")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

const factorySumRequest = `{"task":"Compute seventeen plus twenty-five. Return compact JSON with only the field total containing the calculated sum. Do not add prose or padding.","result_contract":{"json_fields":{"total":42}},"max_tokens":128}`

func TestFactoryPublishesOnlyPersistedTasks(t *testing.T) {
	deps := taskFactoryDeps(t)
	out := invokeFactory(t, deps, "bt_factory_create", factorySumRequest)
	if out["persisted"] != true || out["registered"] != true || out["qualified"] != false {
		t.Fatalf("bad publication: %v", out)
	}
	id := out["tree_id"].(string)
	loaded := agentexec.ResolveGeneratedTree(id)
	if loaded == nil {
		t.Fatal("published tree is not resolvable")
	}
	version, _ := evolution.TreeVersion(loaded)
	if version != out["tree_version"] || len(deps.kg.Trees) != 1 {
		t.Fatalf("publication disagrees with disk: %v", out)
	}
	deps.treeStore = nil
	failure := invokeFactory(t, deps, "bt_factory_create", factorySumRequest)
	if failure["error"] == nil || failure["persisted"] != false || len(deps.kg.Trees) != 1 {
		t.Fatalf("failed persistence registered tree: %v", failure)
	}
	for _, args := range []string{`{`, `{"task":""}`, `{"task":"run","result_contract":{"typo":true}}`} {
		if result := invokeFactory(t, deps, "bt_factory_create", args); result["error"] == nil {
			t.Fatalf("bad arguments accepted: %v", result)
		}
	}
}

func TestFactoryPersonalResolutionRequiresOwner(t *testing.T) {
	deps := taskFactoryDeps(t)
	args := strings.Replace(factorySumRequest, `"task":`, `"user":"alice","task":`, 1)
	out := invokeFactory(t, deps, "bt_kg_auto_create", args)
	if out["persisted"] != true || out["registered"] != false || len(deps.kg.Trees) != 0 {
		t.Fatalf("personal task leaked into shared index: %v", out)
	}
	id := out["tree_id"].(string)
	if domains.ResolveTreeID(id) != nil || domains.ResolveTreeIDForUser("bob", id) != nil {
		t.Fatal("private or missing factory tree resolved without owner")
	}
	if tree := domains.ResolveTreeIDForUser("alice", id); tree == nil || tree.Metadata["user"] != "alice" {
		t.Fatal("owner cannot resolve persisted task")
	}
	deps.personaStore = nil
	if out := invokeFactory(t, deps, "bt_factory_create", args); out["error"] == nil || out["persisted"] != false {
		t.Fatal("personal task fell back to shared store")
	}
}

func TestLiveFactoryCreatesResolvesAndExecutesTask(t *testing.T) {
	model := benchmark.RealLLM(t)
	deps := taskFactoryDeps(t)
	args := strings.Replace(factorySumRequest, `"task":`, `"user":"factory-benchmark","task":`, 1)
	created := invokeFactory(t, deps, "bt_factory_create", args)
	if created["error"] != nil {
		t.Fatal(created)
	}
	id := created["tree_id"].(string)
	registry, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Create(agent.Definition{Name: "factory_sum", Tree: id, Metadata: map[string]string{"user": "factory-benchmark"}}); err != nil {
		t.Fatal(err)
	}
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := &agent.RunDeps{Registry: registry, LLM: model, RefStore: store, ResolveTree: domains.ResolveTreeID, ResolveTreeForUser: domains.ResolveTreeIDForUser}
	run, err := runner.RunOnce(context.Background(), "factory_sum", "Execute the saved arithmetic task.", agent.RunOptions{DisableBlackboard: true, SkipSLORecording: true})
	if err != nil || run.Outcome != "success" {
		t.Fatalf("actual factory execution failed: %+v, %v", run, err)
	}
	var actual struct {
		Total int `json:"total"`
	}
	output := strings.TrimSpace(run.Output)
	output = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(output, "```json\n"), "```"))
	if err = json.Unmarshal([]byte(output), &actual); err != nil || actual.Total != 17+25 {
		t.Fatalf("independent result verification failed: %q %v", run.Output, err)
	}
	records, err := store.LoadAll()
	if err != nil || len(records) != 1 {
		t.Fatalf("missing execution evidence: %+v %v", records, err)
	}
	qualified := evolution.FilterByTreeVersion(records, id, "factory-benchmark", created["tree_version"].(string))
	if len(qualified) != 1 || len(qualified[0].ResultChecks) == 0 || run.TreeVersion != created["tree_version"] {
		t.Fatalf("unattributed execution: %+v", records)
	}
	evidence := model.(*benchmark.LiveModel).Evidence()
	if evidence.Calls == 0 || evidence.Errors != 0 {
		t.Fatalf("missing real inference: %+v", evidence)
	}
	if path := os.Getenv("BT_FACTORY_REPORT"); path != "" {
		data, err := json.MarshalIndent(map[string]any{"created": created, "tree": domains.ResolveTreeIDForUser("factory-benchmark", id), "run": run, "record": qualified[0], "model": evidence, "independent_contract_passed": true}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("factory task executed with %s:%s, calls=%d, version=%s", evidence.Backend, evidence.Model, evidence.Calls, run.TreeVersion)
}
