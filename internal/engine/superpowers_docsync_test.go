package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Doc-drift sync: every run must leave the drift-checked documentation
// (API_REFERENCE.md package listing, GETTING_STARTED binaries, tutorial /
// troubleshooting command refs, ADR catalog) consistent with the code —
// enforced HARD by a doc-drift verification check, satisfied by a docs
// Claude pass that runs whenever the worktree's own scripts/check-doc-drift.sh
// reports drift. Regression context 2026-07-09: an external landing added
// internal/persona undocumented; every cycle commit failed the hook's drift
// check for 16 hours (docs are always "changed" via the arc42 sync stage)
// and nothing in the pipeline could write the missing documentation.

type docsyncRunner struct {
	calls       []string
	docsChanged bool
}

func (r *docsyncRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	if name == "git" && len(args) >= 1 && args[0] == "status" && r.docsChanged {
		res.Output = " M docs/API_REFERENCE.md\n"
	}
	return res
}

func withDocDrift(t *testing.T, results ...bool) *int {
	t.Helper()
	calls := 0
	old := superpowersDocDriftFn
	superpowersDocDriftFn = func(_ context.Context, dir string) (string, bool) {
		idx := calls
		calls++
		if idx >= len(results) {
			idx = len(results) - 1
		}
		if results[idx] {
			return "Documentation is fully in sync with codebase", true
		}
		return "Packages in code but NOT in docs:\n    - persona\n1 drift error(s) found", false
	}
	t.Cleanup(func() { superpowersDocDriftFn = old })
	return &calls
}

func docsyncTestRun(t *testing.T) *SuperpowersRun {
	t.Helper()
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wt, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, docDriftScriptRelPath), []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &SuperpowersRun{
		ID:           "run-docsync",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: wt,
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		ChangedFiles: []string{"internal/persona/profile.go"},
	}
}

func TestDocDriftSyncSkipsWhenInSync(t *testing.T) {
	calls := withDocDrift(t, true)
	run := docsyncTestRun(t)
	claude := &scriptedClaudeRunner{}
	changed, note := syncDriftDocs(context.Background(), claude, &docsyncRunner{}, run)
	if changed {
		t.Fatalf("in-sync docs must not report a change: %s", note)
	}
	if len(claude.prompts) != 0 {
		t.Fatal("no Claude pass when docs are already in sync")
	}
	if *calls != 1 {
		t.Fatalf("expected exactly one drift check, got %d", *calls)
	}
}

func TestDocDriftSyncFixesDriftViaClaudePass(t *testing.T) {
	withDocDrift(t, false, true) // drift found, fixed after the pass
	run := docsyncTestRun(t)
	claude := &scriptedClaudeRunner{}
	runner := &docsyncRunner{docsChanged: true}
	changed, note := syncDriftDocs(context.Background(), claude, runner, run)
	if !changed {
		t.Fatalf("expected docs update, got note: %s", note)
	}
	if len(claude.prompts) != 1 {
		t.Fatalf("expected one Claude docs pass, got %d", len(claude.prompts))
	}
	if !strings.Contains(claude.prompts[0], "persona") {
		t.Fatal("the drift report must be embedded in the docs prompt")
	}
	found := false
	for _, f := range run.ChangedFiles {
		if f == "docs/API_REFERENCE.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("updated doc must join ChangedFiles: %v", run.ChangedFiles)
	}
}

func TestDocDriftSyncStillFailingIsReported(t *testing.T) {
	withDocDrift(t, false, false)
	run := docsyncTestRun(t)
	changed, note := syncDriftDocs(context.Background(), &scriptedClaudeRunner{}, &docsyncRunner{}, run)
	if changed {
		t.Fatal("unfixed drift must not be reported as changed")
	}
	if !strings.Contains(strings.ToLower(note), "still") {
		t.Fatalf("note must say drift is still present (verification will fail the run): %s", note)
	}
}

func TestDocDriftSyncSkipsDryRunAndMissingScript(t *testing.T) {
	calls := withDocDrift(t, false)
	run := docsyncTestRun(t)
	run.Mode = SuperpowersModeDryRun
	if changed, _ := syncDriftDocs(context.Background(), &scriptedClaudeRunner{}, &docsyncRunner{}, run); changed {
		t.Fatal("dry-run must skip")
	}
	run2 := docsyncTestRun(t)
	_ = os.Remove(filepath.Join(run2.WorktreePath, docDriftScriptRelPath))
	if changed, _ := syncDriftDocs(context.Background(), &scriptedClaudeRunner{}, &docsyncRunner{}, run2); changed {
		t.Fatal("missing drift script must skip")
	}
	if *calls != 0 {
		t.Fatal("skip paths must not invoke the drift checker")
	}
}

func TestVerificationChecksIncludeDocDriftGate(t *testing.T) {
	// The gate is HARD: doc-drift joins the verification checks whenever the
	// worktree ships the drift script, so a run that leaves documentation
	// inconsistent FAILS verification — it cannot reach the landing commit.
	run := docsyncTestRun(t)
	checks := buildSuperpowersVerificationChecks(run)
	found := ""
	for _, c := range checks {
		if c.name == "doc-drift" {
			found = c.cmd
		}
	}
	if found == "" {
		t.Fatalf("doc-drift check missing from verification: %+v", checks)
	}
	if !strings.Contains(found, docDriftScriptRelPath) {
		t.Fatalf("doc-drift check must run the worktree's drift script: %s", found)
	}

	// Without the script (older bases), the check must not be added.
	run2 := docsyncTestRun(t)
	_ = os.Remove(filepath.Join(run2.WorktreePath, docDriftScriptRelPath))
	for _, c := range buildSuperpowersVerificationChecks(run2) {
		if c.name == "doc-drift" {
			t.Fatal("doc-drift check must be skipped when the script is absent")
		}
	}
}
