package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

// This fixture uses a real temporary Git repo and real Go test/build commands.
// It exercises delivery attribution; it is not model or task-impact evidence.
func researchDeliveryFixture(t *testing.T, tasks []SuperpowersTask) *SuperpowersRun {
	t.Helper()
	repo := t.TempDir()
	run := &SuperpowersRun{ID: "delivery-" + research.Key(t.Name()), RepoDir: repo,
		ArtifactDir: t.TempDir(), Mode: SuperpowersModeApply, ApplyStatus: "committed", Tasks: tasks, StartedAt: time.Now().UTC()}
	command := func(name string, args ...string) CommandResult {
		t.Helper()
		res := (execCommandRunner{}).Run(context.Background(), repo, name, args...)
		if res.Err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, res.Err, res.Output)
		}
		return res
	}
	command("git", "init", "-q", "--initial-branch=main")
	command("git", "config", "user.name", "Delivery Test")
	command("git", "config", "user.email", "delivery@example.invalid")
	command("git", "config", "core.hooksPath", "/dev/null")
	files := map[string]string{"go.mod": "module example.invalid/delivery\n\ngo 1.26.5\n", "fixture_test.go": "package fixture\nimport \"testing\"\nfunc TestFixture(t *testing.T) { if 2+2 != 4 { t.Fatal(\"arithmetic\") } }\n"}
	for i := range run.Tasks {
		task := &run.Tasks[i]
		task.Index = i + 1
		task.Status = "done"
		if len(task.Files) == 0 {
			task.Files = extractGoFilePaths(stripGoapGoalTransientNotes(task.Objective))
		}
		if len(task.Files) == 0 {
			task.Files = []string{"fixture.go"}
		}
		for _, file := range task.Files {
			if !filepath.IsLocal(file) {
				t.Fatalf("invalid fixture file %s", file)
			}
			files[file] = "package fixture\n"
		}
	}
	for file, body := range files {
		path := filepath.Join(repo, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct{ name, arg string }{{"main-focused-tests", "test"}, {"main-build", "build"}} {
		res := command("/usr/local/go/bin/go", check.arg, "./...")
		run.Verification = append(run.Verification, VerificationCheck{Name: check.name, Command: res.Command, Passed: true, Output: res.Output})
	}
	command("git", "add", ".")
	command("git", "-c", "commit.gpgsign=false", "commit", "-qm", "superpowers: apply verified run "+run.ID)
	run.AppliedCommit = strings.TrimSpace(command("git", "rev-parse", "HEAD").Output)
	return run
}

func seedResearchDelivery(t *testing.T, tasks []SuperpowersTask) *SuperpowersRun {
	t.Helper()
	seedGoalBudget(t)
	run := researchDeliveryFixture(t, tasks)
	if err := recordImplementedGoals(run); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestResearchDeliveryLinksSourceToRealCommitWithoutClaimingImpact(t *testing.T) {
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	goal := "Fix declared task evidence (files: internal/evidence.go)"
	bb := &Blackboard{User: "owner-a", ChainState: map[string]any{}}
	recordGoapResearchSource(bb, []goapResearchGoal{{Goal: goal}}, "review", "GOAL: "+goal)
	run := researchDeliveryFixture(t, []SuperpowersTask{{Title: "task evidence", Objective: "Implement the complete, verified change for this goal: [P0] NotebookLM research: " + goal}})
	run.User = bb.User
	if err := recordImplementedGoals(run); err != nil {
		t.Fatal(err)
	}
	if err := recordImplementedGoals(run); err != nil {
		t.Fatalf("idempotent delivery: %v", err)
	}
	s, err := research.OpenTraces(researchTracePath(bb.User), bb.User)
	if err != nil {
		t.Fatal(err)
	}
	g := s.Goals[researchGoalTraceID(goal)]
	if g == nil || len(g.Sources) != 1 || len(g.Deliveries) != 1 || g.Deliveries[0].Commit != run.AppliedCommit {
		t.Fatalf("broken source/delivery link: %+v", g)
	}
	if s.Summary()["measured_impact"] != "not_linked" {
		t.Fatal("commit must not imply measured impact")
	}
	if s.Summary()["source_linked_deliveries"] != 1 {
		t.Fatalf("source predating execution was not linked: %+v", g)
	}
	other, err := ResearchDeliveryStatus("owner-b")
	if err != nil || other["delivered_goals"] != 0 {
		t.Fatalf("owner leak: %v %v", other, err)
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	if dest := os.Getenv("BT_RESEARCH_TRACE_EVIDENCE"); dest != "" {
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			t.Fatal(err)
		}
		bundle := (execCommandRunner{}).Run(context.Background(), run.RepoDir, "git", "bundle", "create", dest+".bundle", "HEAD")
		if bundle.Err != nil {
			t.Fatalf("retain fixture Git objects: %+v", bundle)
		}
		runData, err := json.MarshalIndent(run, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest+".run.json", runData, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResearchDeliveryDoesNotRetroactivelyCreditLaterResearch(t *testing.T) {
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	goal := "Fix evidence (files: evidence.go)"
	run := researchDeliveryFixture(t, []SuperpowersTask{{Title: goal, Objective: goal}})
	if err := recordImplementedGoals(run); err != nil {
		t.Fatal(err)
	}
	recordGoapResearchSource(&Blackboard{ChainState: map[string]any{}}, []goapResearchGoal{{Goal: goal}}, "late-review", goal)
	if err := recordImplementedGoals(run); err != nil {
		t.Fatal(err)
	}
	s, err := research.OpenTraces(researchTracePath(""), "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Summary()["source_linked_deliveries"] != 0 {
		t.Fatal("later observation received delivery credit")
	}
	if s.Delivered(researchGoalTraceID("Fix evidence (files: other.go)")) {
		t.Fatal("delivery crossed file scope")
	}
}

func TestResearchDeliveryRejectsUnlandedCommit(t *testing.T) {
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	run := researchDeliveryFixture(t, []SuperpowersTask{{Title: "Fix evidence", Objective: "Fix evidence"}})
	// Leave the run commit only on a side branch; the active checkout's HEAD
	// is an unrelated root. Merely finding the Git object must not suffice.
	for _, args := range [][]string{{"checkout", "--orphan", "unrelated"}, {"-c", "commit.gpgsign=false", "commit", "-qm", "unrelated change"}} {
		res := (execCommandRunner{}).Run(context.Background(), run.RepoDir, "git", args...)
		if res.Err != nil {
			t.Fatalf("fixture: %+v", res)
		}
	}
	if err := recordImplementedGoals(run); err == nil || !strings.Contains(err.Error(), "not landed") {
		t.Fatalf("unlanded commit accepted: %v", err)
	}
}

func TestResearchDeliveryReconciliationHoldsOnCorruptJournal(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "pending-run")
	if err := os.Mkdir(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "run.json"), []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reconcileResearchDeliveries(context.Background(), root, ""); err == nil {
		t.Fatal("unreadable pending journal allowed new planning")
	}
}

func TestResearchDeliveryNoopAndDryRunHaveNoCreditOrPendingReceipt(t *testing.T) {
	isolateBtFusionKnowledge(t)
	for _, status := range []string{"dry_run", "applied_no_commit", "main_repo", "no_changes"} {
		run := &SuperpowersRun{ID: status, ArtifactDir: t.TempDir(), Mode: SuperpowersModeApply, ApplyStatus: status}
		if status == "dry_run" {
			run.Mode = SuperpowersModeDryRun
		}
		recordSuperpowersResearchDelivery(run)
		if run.ResearchDeliveryPending || run.ResearchDeliveryError != "" {
			t.Fatalf("no delivery must not need a receipt: %+v", run)
		}
	}
	s, err := research.OpenTraces(researchTracePath(""), "")
	if err != nil || len(s.Goals) != 0 {
		t.Fatalf("no-op created credit: %v %v", s, err)
	}
}

func TestResearchDeliveryRejectsUnsupportedCompletion(t *testing.T) {
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	base := researchDeliveryFixture(t, []SuperpowersTask{{Title: "Fix evidence", Objective: "Fix evidence", Files: []string{"evidence.go"}}})
	for name, mutate := range map[string]func(*SuperpowersRun){
		"dry-run":         func(r *SuperpowersRun) { r.Mode = SuperpowersModeDryRun },
		"pending-patch":   func(r *SuperpowersRun) { r.ApplyStatus = "pending_patch" },
		"no-op":           func(r *SuperpowersRun) { r.AppliedCommit = "" },
		"other-run":       func(r *SuperpowersRun) { r.ID = "another-run" },
		"invented-commit": func(r *SuperpowersRun) { r.AppliedCommit = strings.Repeat("a", 40) },
		"failed-check": func(r *SuperpowersRun) {
			r.Verification = append(r.Verification, VerificationCheck{Name: "main-build", Command: "go build", Passed: false})
		},
		"no-checks": func(r *SuperpowersRun) { r.Verification = nil },
		"unrelated-files": func(r *SuperpowersRun) {
			r.Tasks = []SuperpowersTask{{Title: "wrong", Objective: "other", Status: "done", Files: []string{"missing.go"}}}
			r.ChangedFiles = []string{"missing.go"}
		},
		"unfinished-task": func(r *SuperpowersRun) {
			r.Tasks = []SuperpowersTask{{Title: "unfinished", Objective: "Fix evidence", Status: "pending", Files: []string{"evidence.go"}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := *base
			mutate(&r)
			if err := recordImplementedGoals(&r); err == nil {
				t.Fatal("unsupported completion accepted")
			}
		})
	}
	s, err := research.OpenTraces(researchTracePath(""), "")
	if err != nil || len(s.Goals) != 0 {
		t.Fatalf("rejected delivery created evidence: %v %v", s, err)
	}
}

func TestResearchDeliveryPersistenceFailureDoesNotReplayCode(t *testing.T) {
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	run := researchDeliveryFixture(t, []SuperpowersTask{{Title: "Fix evidence", Objective: "Fix evidence"}})
	runsDir := t.TempDir()
	run.ArtifactDir = filepath.Join(runsDir, run.ID)
	if err := os.Mkdir(researchTracePath(""), 0o750); err != nil {
		t.Fatal(err)
	}
	recordSuperpowersResearchDelivery(run)
	if run.ResearchDeliveryError == "" || run.ApplyStatus != "committed" {
		t.Fatalf("lost delivery status: %+v", run)
	}
	stored, err := readSuperpowersRunJSON(filepath.Join(run.ArtifactDir, "run.json"))
	if err != nil || stored.ResearchDeliveryError == "" || !stored.ResearchDeliveryPending {
		t.Fatalf("failure not durable: %v %v", stored, err)
	}
	if err := reconcileResearchDeliveries(context.Background(), runsDir, ""); err == nil {
		t.Fatal("new planning must wait for receipt repair")
	}
	if err := os.Remove(researchTracePath("")); err != nil {
		t.Fatal(err)
	}
	if err := reconcileResearchDeliveries(context.Background(), runsDir, ""); err != nil {
		t.Fatal(err)
	}
	stored, err = readSuperpowersRunJSON(filepath.Join(run.ArtifactDir, "run.json"))
	if err != nil || stored.ResearchDeliveryPending || stored.ResearchDeliveryError != "" {
		t.Fatalf("receipt repair not acknowledged: %v %v", stored, err)
	}
	res := (execCommandRunner{}).Run(context.Background(), run.RepoDir, "git", "rev-list", "--count", "HEAD")
	if res.Err != nil || strings.TrimSpace(res.Output) != "1" {
		t.Fatalf("attribution repair changed history: %+v", res)
	}
}
