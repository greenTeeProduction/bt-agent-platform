package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

func TestLivePersonalFileAutomation(t *testing.T) {
	model := benchmark.RealLLM(t)
	deps := taskFactoryDeps(t)
	var err error
	deps.agentReg, err = agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps.refStore, err = evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previousStore, previousPolicy := hitl.DefaultStore, hitl.GetPolicy()
	if _, err := hitl.InitStore(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, Timeout: time.Hour})
	t.Cleanup(func() { hitl.DefaultStore = previousStore; hitl.SetPolicy(previousPolicy) })
	const user = "personal-file-benchmark"
	const task = "Read the supplied expenses JSON. Sum every amount and count the entries. Return only a JSON object with total and count."
	root := deps.personaStore.Workspace(user).ArtifactsDir()
	if err := os.MkdirAll(root, 0750); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"expenses":[{"amount":12},{"amount":9},{"amount":4}]}`)
	if err := os.WriteFile(filepath.Join(root, "expenses.json"), input, 0600); err != nil {
		t.Fatal(err)
	}
	request := knowledge.TaskRequest{Task: task, User: user, MaxTokens: 256, ResultContract: json.RawMessage(`{"json_fields":{"total":25,"count":3}}`), FileTask: &evolution.FileTaskSpec{Input: "expenses.json", Output: "reports/summary.json"}}
	raw, _ := json.Marshal(request)
	created := invokeFactory(t, deps, "bt_factory_create", string(raw))
	if created["persisted"] != true {
		t.Fatal(created)
	}
	raw, _ = json.Marshal(map[string]string{"user": user, "tree": created["tree_id"].(string), "schedule": "0 9 * * *"})
	proposal := invokeFactory(t, deps, "bt_automation_schedule", string(raw))
	if proposal["proposed"] != true {
		t.Fatal(proposal)
	}
	treeID := proposal["tree_id"].(string)
	if domains.ResolveTreeIDForUser(user, treeID) != nil {
		t.Fatal("pending tree is executable")
	}
	req, err := hitl.DefaultStore.Approve(proposal["hitl_id"].(string), "fixture-owner", "approve exact artifact task")
	if err != nil {
		t.Fatal(err)
	}
	if out := finalizeAutomationApproval(deps, req, true); out["activated"] != true {
		t.Fatal(out)
	}
	inst, err := deps.agentReg.Get(req.Context["agent_name"])
	if err != nil {
		t.Fatal(err)
	}
	runner := &agent.RunDeps{Registry: deps.agentReg, LLM: model, RefStore: deps.refStore, ResolveTree: domains.ResolveTreeID, ResolveTreeForUser: domains.ResolveTreeIDForUser}
	scheduler := agent.NewScheduler(agent.SchedulerConfig{Registry: deps.agentReg, JobStore: agent.NewFileJobStore(filepath.Join(t.TempDir(), "jobs.json"))})
	var run *agent.RunResult
	outcome, output, err := scheduler.RunNow(inst.Definition.Name, inst.Definition.Description, func(rc agent.RunContext) (string, string, *agent.RunResult, error) {
		r, e := runner.RunOnce(rc.Context, rc.AgentName, rc.Task, agent.RunOptions{DisableBlackboard: true, SkipSLORecording: true})
		run = r
		if r == nil {
			return "failure", "", nil, e
		}
		return r.Outcome, r.Output, r, e
	}, "120s")
	if err != nil || outcome != "success" || run == nil {
		t.Fatalf("actual scheduled task failed: %+v %v", run, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "reports/summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var actual struct{ Total, Count int }
	if err := json.Unmarshal(data, &actual); err != nil || actual.Total != 12+9+4 || actual.Count != 3 {
		t.Fatalf("independent file check failed: %s %v", data, err)
	}
	if string(data) != output || len(run.Effects) != 1 || !run.Effects[0].Verified || !run.Effects[0].WriteCommitted || run.Effects[0].InputDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(input)) || run.Effects[0].OutputDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(data)) {
		t.Fatalf("missing observed receipt: %+v", run)
	}
	records, err := deps.refStore.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	exact := evolution.FilterByTreeVersion(records, treeID, user, proposal["tree_version"].(string))
	if len(exact) != 1 || len(exact[0].Effects) != 1 || !exact[0].Effects[0].Verified || exact[0].Task != task {
		t.Fatalf("lost task/version/effect evidence: %+v", records)
	}
	if _, exists := knowledge.GlobalGraph.Trees[treeID]; exists {
		t.Fatal("personal scheduling contaminated shared feedback")
	}
	// A real model that computes a result inconsistent with the declared
	// oracle must not replace an existing valid report. No mock LLM is used.
	request.ResultContract = json.RawMessage(`{"json_fields":{"total":999,"count":3}}`)
	raw, _ = json.Marshal(request)
	negativeCreated := invokeFactory(t, deps, "bt_factory_create", string(raw))
	if negativeCreated["persisted"] != true {
		t.Fatal(negativeCreated)
	}
	_, err = deps.agentReg.Create(agent.Definition{Name: "negative-report", Description: task, Tree: negativeCreated["tree_id"].(string), Metadata: map[string]string{"user": user}})
	if err != nil {
		t.Fatal(err)
	}
	negative, negativeErr := runner.RunOnce(context.Background(), "negative-report", task, agent.RunOptions{DisableBlackboard: true, SkipSLORecording: true})
	afterNegative, readErr := os.ReadFile(filepath.Join(root, "reports/summary.json"))
	if negativeErr == nil || negative == nil || negative.Outcome == "success" || len(negative.Effects) != 0 || readErr != nil || string(afterNegative) != string(data) {
		t.Fatalf("failed real result gate changed the output: %+v %v %s", negative, negativeErr, afterNegative)
	}
	// Approval is pinned: neither changed bytes nor a missing ledger may run.
	altered := domains.ResolveTreeIDForUser(user, treeID)
	altered.Description += " changed after consent"
	if _, err := evolution.SaveNamedTree(deps.personaStore.Workspace(user).TreesDir(), treeID, altered); err != nil {
		t.Fatal(err)
	}
	calls := model.(*benchmark.LiveModel).Evidence().Calls
	denied, err := runner.RunOnce(context.Background(), inst.Definition.Name, task, agent.RunOptions{DisableBlackboard: true, SkipSLORecording: true})
	if err == nil || denied.Outcome == "success" || model.(*benchmark.LiveModel).Evidence().Calls != calls {
		t.Fatal("changed task version executed through fallback")
	}
	if err := os.Remove(deps.personaStore.Workspace(user).AutomationsPath()); err != nil {
		t.Fatal(err)
	}
	if domains.ResolveTreeIDForUser(user, treeID) != nil {
		t.Fatal("missing ledger bypassed tracked consent")
	}
	evidence := model.(*benchmark.LiveModel).Evidence()
	if evidence.Calls == 0 || evidence.Errors != 0 {
		t.Fatalf("missing successful real inference: %+v", evidence)
	}
	report := map[string]any{"created": created, "proposal": proposal, "run": run, "record": exact[0], "input": json.RawMessage(input), "readback": json.RawMessage(data), "model": evidence, "independent_readback_passed": true, "negative_run": negative, "negative_preserved_existing_file": true, "changed_version_blocked": true, "missing_ledger_blocked": true, "dispatch": "Scheduler.RunNow -> production RunOnce; cron wall-clock dispatch not observed"}
	if reportPath := os.Getenv("BT_PERSONAL_FILE_REPORT"); reportPath != "" {
		encoded, _ := json.MarshalIndent(report, "", "  ")
		if err := os.MkdirAll(filepath.Dir(reportPath), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reportPath, append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real personal task: %s/%s, %d calls; independent output=%s", evidence.Backend, evidence.Model, evidence.Calls, data)
}
