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
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/gardener"
)

func TestLiveFactoryEvolutionPromotesAndRollsBackMeasuredVersion(t *testing.T) {
	model := benchmark.RealLLM(t)
	deps := taskFactoryDeps(t)
	const user = "evolution-benchmark"
	request := strings.Replace(factorySumRequest, `"task":`, `"user":"`+user+`","task":`, 1)
	created := invokeFactory(t, deps, "bt_factory_create", request)
	if created["error"] != nil {
		t.Fatal(created)
	}
	id := created["tree_id"].(string)
	base := domains.ResolveTreeIDForUser(user, id)
	// Controlled fault injection: the worker computes the right value under
	// the wrong field and has no recovery. No model response is fabricated.
	gate := &base.Children[len(base.Children)-1]
	gate.Children = gate.Children[:1]
	gate.Children[0].Name = "llm_call:Compute seventeen plus twenty-five. Return only a JSON object with the field wrong_total containing the calculated sum. Use exactly that field name."
	if _, err := evolution.SaveNamedTree(deps.personaStore.Workspace(user).TreesDir(), id, base); err != nil {
		t.Fatal(err)
	}
	baseVersion, _ := evolution.TreeVersion(base)
	probeData, _ := json.Marshal(base)
	var probe evolution.SerializableNode
	if err := json.Unmarshal(probeData, &probe); err != nil {
		t.Fatal(err)
	}
	if evolution.ApplyMutations(&probe, []evolution.MutationOp{{Operation: "add_contract_recovery", Target: gate.Name}}) != 1 {
		t.Fatal("recovery proposal unavailable")
	}
	t.Logf("recovery governance before=%.2f after=%.2f", evolution.AssessGovernance(base).Score, evolution.AssessGovernance(&probe).Score)
	if !benchmark.QuickValidateCandidate(base, &probe, benchmark.SuiteForTree(id), model) {
		t.Fatal("recovery proposal failed its live smoke comparison")
	}
	agents, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = agents.Create(agent.Definition{Name: "evolving_sum", Tree: id, Metadata: map[string]string{"user": user}}); err != nil {
		t.Fatal(err)
	}
	records, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := &agent.RunDeps{Registry: agents, LLM: model, RefStore: records, ResolveTree: domains.ResolveTreeID, ResolveTreeForUser: domains.ResolveTreeIDForUser}
	options := agent.RunOptions{DisableBlackboard: true, SkipSLORecording: true}
	before, err := runner.RunOnce(t.Context(), "evolving_sum", "Execute the saved calculation.", options)
	if before == nil || before.Outcome == "success" {
		t.Fatalf("controlled baseline unexpectedly passed: %+v %v", before, err)
	}
	registry := gardener.NewRegistryWithUsers(deps.treeStore.Dir(), deps.personaStore.Root())
	tracker, err := gardener.NewMetricsTracker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	garden := gardener.NewGardener(gardener.Config{Registry: registry, RefStore: records, MetricsTracker: tracker, MaxMutations: 1, UserExperienceRoot: deps.personaStore.Root()})
	cfg := gardener.DefaultEvolveV2Config()
	cfg.MCTSStructuralSearch = false
	cfg.BlocksEnabled = false
	cfg.Specialists = nil
	cycles, err := garden.RunCycleV2(cfg)
	attempts, _ := filepath.Glob(filepath.Join(deps.treeStore.Dir(), "runtime-versions", "*", "attempts", "*.json"))
	for _, path := range attempts {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		t.Logf("rejected real qualification: %s", data)
		if report := os.Getenv("BT_PROMOTION_REPORT"); report != "" {
			if writeErr := os.WriteFile(report+".rejected.json", data, 0600); writeErr != nil {
				t.Fatal(writeErr)
			}
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	var measured gardener.CycleMetrics
	for _, cycle := range cycles {
		if cycle.TreeName == id {
			measured = cycle
		}
	}
	if !measured.Improved || measured.BaseFitness != 0 || measured.NewFitness != 100 || measured.Qualification == "" || measured.BaselineVersion != baseVersion {
		t.Fatalf("missing measured evolution: %+v", measured)
	}
	store, err := agentexec.RuntimeReleaseStore()
	if err != nil {
		t.Fatal(err)
	}
	active, release, err := store.Resolve(id, user)
	if err != nil || release == nil || release.Version != measured.CandidateVersion {
		t.Fatalf("missing active release: %+v %v", release, err)
	}
	proofs, err := filepath.Glob(filepath.Join(deps.treeStore.Dir(), "runtime-versions", "*", "qualifications", release.Qualification+".json"))
	if err != nil || len(proofs) != 1 {
		t.Fatalf("qualified paired outputs missing: %v %v", proofs, err)
	}
	proof, err := os.ReadFile(proofs[0])
	if err != nil {
		t.Fatal(err)
	}
	var qualification evolution.RuntimeQualification
	if err = json.Unmarshal(proof, &qualification); err != nil || qualification.Validate(base, active) != nil {
		t.Fatalf("qualification cannot be independently revalidated: %s %v", proof, err)
	}
	after, err := runner.RunOnce(t.Context(), "evolving_sum", "Execute the saved calculation.", options)
	if err != nil || after.Outcome != "success" || after.TreeVersion != release.Version {
		t.Fatalf("runtime did not adopt measured version: %+v %v", after, err)
	}
	output := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(after.Output), "```json\n"), "```"))
	var actual struct {
		Total int `json:"total"`
	}
	if err = json.Unmarshal([]byte(output), &actual); err != nil || actual.Total != 17+25 {
		t.Fatalf("runtime output failed independent arithmetic check: %q %v", after.Output, err)
	}
	history, err := records.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	adoption := evolution.FilterByTreeVersion(history, id, user, release.Version)
	if len(adoption) != 1 || len(adoption[0].ResultChecks) != 2 {
		t.Fatalf("missing actual adoption/gateway evidence: %+v", adoption)
	}
	restarted := gardener.NewRegistryWithUsers(deps.treeStore.Dir(), deps.personaStore.Root())
	var restartVersion string
	for _, entry := range restarted.List() {
		if entry.Tree != nil && entry.Tree.Name == id {
			restartVersion, _ = evolution.TreeVersion(entry.Tree)
		}
	}
	if restartVersion != release.Version {
		t.Fatal("registry restart reloaded the stale proposal file")
	}
	if err = restarted.RollbackTree(id, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	rolled, err := runner.RunOnce(context.Background(), "evolving_sum", "Execute the saved calculation.", options)
	if rolled == nil || rolled.TreeVersion != baseVersion || rolled.Outcome == "success" {
		t.Fatalf("runtime did not restore exact predecessor: %+v %v", rolled, err)
	}
	_, rollback, err := store.Resolve(id, user)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("BT_PROMOTION_REPORT"); path != "" {
		data, err := json.MarshalIndent(map[string]any{"controlled_fault": "worker returns wrong_total; no mocked model outputs", "tree_id": id, "user": user, "baseline": base, "candidate": active, "qualification": qualification, "cycle": measured, "release": release, "before": before, "after": after, "adoption": adoption, "rollback": rollback, "after_rollback": rolled, "independent_contract_passed": true}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("measured task passes %.0f%% -> %.0f%%; runtime adopted %s, rollback restored %s", measured.BaseFitness, measured.NewFitness, release.Version, baseVersion)
}
