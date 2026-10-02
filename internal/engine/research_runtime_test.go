package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/research"
	"github.com/nico/go-bt-evolve/internal/util"
)

// Real Git objects exercise the code-containment join. Declared run records
// exercise the attribution protocol; these are not real-model impact evidence.
func TestResearchRuntimeJoinsOnlyQualifiedCodeAndResults(t *testing.T) {
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	const user = "runtime-owner"
	const goal = "Fix execution evidence (files: evidence.go)"
	recordGoapResearchSource(&Blackboard{User: user, ChainState: map[string]any{}}, []goapResearchGoal{{Goal: goal}}, "review", goal)
	delivered := researchDeliveryFixture(t, []SuperpowersTask{{Title: goal, Objective: goal, Files: []string{"evidence.go"}}})
	delivered.User = user
	if err := recordImplementedGoals(delivered); err != nil {
		t.Fatal(err)
	}
	traces, err := research.OpenTraces(researchTracePath(user), user)
	if err != nil {
		t.Fatal(err)
	}
	receipt := traces.Goals[researchGoalTraceID(goal)].Deliveries[0]
	version := "sha256:" + strings.Repeat("a", 64)
	started := time.Now().UTC()
	valid := evolution.Record{TaskID: "observed", RunID: "observed-run", User: user, Task: "Compute the total", TreeName: "factory:sum", TreeVersion: version, ExecutionVersions: []string{version}, EvidenceKind: evolution.EvidenceExecution, StartedAt: started, Timestamp: started.Add(time.Second).UnixMilli(), Outcome: evolution.Success, Result: `{"total":42}`, Build: util.BuildProvenance{Module: "github.com/nico/go-bt-evolve", Revision: delivered.AppliedCommit, Known: true}}
	valid.ResultChecks = []evolution.ResultCheck{{Passed: true, OutputDigest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(valid.Result))), Contract: &evolution.ResultContract{JSONFields: map[string]json.RawMessage{"total": json.RawMessage(`42`)}}}}
	for name, tc := range map[string]struct {
		change            func(*evolution.Record)
		adopted, verified int
	}{
		"exact":             {func(*evolution.Record) {}, 1, 1},
		"wrong-owner":       {func(r *evolution.Record) { r.User = "someone-else" }, 0, 0},
		"dirty":             {func(r *evolution.Record) { r.Build.Dirty = true }, 0, 0},
		"unknown-build":     {func(r *evolution.Record) { r.Build.Known = false }, 0, 0},
		"local-replacement": {func(r *evolution.Record) { r.Build.LocalReplacements = true }, 0, 0},
		"earlier-run":       {func(r *evolution.Record) { r.StartedAt = receipt.RecordedAt.Add(-time.Second) }, 0, 0},
		"mixed-tree":        {func(r *evolution.Record) { r.ExecutionVersions = []string{version, "other"} }, 0, 0},
		"user-feedback":     {func(r *evolution.Record) { r.UserFeedback = "positive" }, 0, 0},
		"wrong-result":      {func(r *evolution.Record) { r.Result = `{"total":41}` }, 1, 0},
		"failed-run":        {func(r *evolution.Record) { r.Outcome = evolution.Failure }, 1, 0},
		"flag-only":         {func(r *evolution.Record) { r.ResultChecks = []evolution.ResultCheck{{Passed: true}} }, 1, 0},
	} {
		t.Run(name, func(t *testing.T) {
			store, err := evolution.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			record := valid
			tc.change(&record)
			if err = store.Save(&record); err != nil {
				t.Fatal(err)
			}
			report, err := ResearchRuntimeStatus(t.Context(), user, store, nil)
			if err != nil {
				t.Fatal(err)
			}
			if report["adopted_execution_count"] != tc.adopted || report["verified_result_count"] != tc.verified || report["qualified_tree_execution_count"] != 0 {
				t.Fatalf("incorrect attribution: %+v", report)
			}
			if report["measured_impact"] != "causal_research_impact_not_established" {
				t.Fatal("correlation claimed as causal impact")
			}
			if tc.adopted > 0 {
				observations := report["runtime_observations"].(map[string][]ResearchRuntimeObservation)[researchGoalTraceID(goal)]
				if len(observations) != 1 || len(observations[0].SourceIDs) != 1 {
					t.Fatalf("missing original source link: %+v", observations)
				}
			}
		})
	}
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "reflection-corrupt.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResearchRuntimeStatus(t.Context(), user, store, nil); err == nil {
		t.Fatal("corrupt evidence silently omitted")
	}

	git := func(args ...string) string {
		t.Helper()
		result := (execCommandRunner{}).Run(t.Context(), delivered.RepoDir, "git", args...)
		if result.Err != nil {
			t.Fatalf("git %v: %+v", args, result)
		}
		return strings.TrimSpace(result.Output)
	}
	commit := func(file, content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(delivered.RepoDir, file), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		git("add", file)
		git("-c", "commit.gpgsign=false", "commit", "-qm", "descendant")
		return git("rev-parse", "HEAD")
	}
	unrelated := commit("unrelated.txt", "unrelated change")
	if ok, err := runningBuildContainsDelivery(t.Context(), receipt, unrelated); err != nil || !ok {
		t.Fatalf("unchanged delivered code rejected: %v %v", ok, err)
	}
	changed := commit("evidence.go", "package fixture\n// changed delivered code\n")
	if ok, err := runningBuildContainsDelivery(t.Context(), receipt, changed); err != nil || ok {
		t.Fatalf("changed code inherited delivery credit: %v %v", ok, err)
	}
	wrongTree := receipt
	wrongTree.Tree = strings.Repeat("b", 40)
	if _, err := runningBuildContainsDelivery(t.Context(), wrongTree, unrelated); err == nil {
		t.Fatal("mismatched delivered Git tree accepted")
	}
	git("checkout", "--orphan", "unrelated-root")
	git("-c", "commit.gpgsign=false", "commit", "-qm", "unrelated root")
	if ok, err := runningBuildContainsDelivery(t.Context(), receipt, git("rev-parse", "HEAD")); err != nil || ok {
		t.Fatalf("unrelated build accepted: %v %v", ok, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ResearchRuntimeStatus(ctx, user, store, nil); err == nil {
		t.Fatal("canceled/corrupt report accepted")
	}
}
