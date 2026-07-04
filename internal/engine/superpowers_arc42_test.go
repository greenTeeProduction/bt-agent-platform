package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// arc42SyncRunner scripts git for the docs stage: status of the arc42 file
// reports modified or clean per the `docsChanged` flag.
type arc42SyncRunner struct {
	calls       []string
	docsChanged bool
}

func (r *arc42SyncRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	if name == "git" && len(args) >= 1 && args[0] == "status" && r.docsChanged {
		res.Output = " M " + arc42DocRelPath + "\n"
	}
	if name == "git" && len(args) >= 1 && args[0] == "diff" {
		res.Output = " internal/engine/foo.go | 10 +++++-----\n"
	}
	return res
}

func arc42TestRun(t *testing.T, changed []string) *SuperpowersRun {
	t.Helper()
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wt, "docs", "arc42"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, arc42DocRelPath), []byte("# arc42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &SuperpowersRun{
		ID:           "run-arc42",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: wt,
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		ChangedFiles: changed,
		Tasks:        []SuperpowersTask{{Index: 1, Objective: "add auction messages", Status: "done"}},
	}
}

func TestArc42SyncUpdatesDocAndChangedFiles(t *testing.T) {
	run := arc42TestRun(t, []string{"internal/engine/foo.go", "internal/engine/foo_test.go"})
	runner := &arc42SyncRunner{docsChanged: true}
	claude := &scriptedClaudeRunner{}

	changed, note := syncArc42Docs(context.Background(), claude, runner, run)
	if !changed || !strings.Contains(note, arc42DocRelPath) {
		t.Fatalf("changed=%v note=%q", changed, note)
	}
	if !containsStr(run.ChangedFiles, arc42DocRelPath) {
		t.Fatalf("doc must join the run's changed files: %v", run.ChangedFiles)
	}
	if len(claude.prompts) != 1 || !strings.Contains(claude.prompts[0], "add auction messages") {
		t.Fatalf("prompt must carry the run's objectives: %d prompts", len(claude.prompts))
	}
	if !strings.Contains(claude.prompts[0], "internal/engine/foo.go") || strings.Contains(claude.prompts[0], "foo_test.go") {
		t.Fatalf("prompt lists production files only:\n%s", claude.prompts[0])
	}
	if _, err := os.Stat(filepath.Join(run.ArtifactDir, "verification", "arc42-sync.txt")); err != nil {
		t.Fatalf("docs pass must leave evidence: %v", err)
	}
}

func TestArc42SyncSkipsWithoutProductionChanges(t *testing.T) {
	run := arc42TestRun(t, []string{"internal/engine/foo_test.go", "docs/x.md"})
	claude := &scriptedClaudeRunner{}
	changed, note := syncArc42Docs(context.Background(), claude, &arc42SyncRunner{}, run)
	if changed || !strings.Contains(note, "skipped") {
		t.Fatalf("test-only changes must skip: changed=%v note=%q", changed, note)
	}
	if len(claude.prompts) != 0 {
		t.Fatal("no Claude call on skip")
	}
}

func TestArc42SyncNoImpactIsClean(t *testing.T) {
	run := arc42TestRun(t, []string{"internal/engine/foo.go"})
	changed, note := syncArc42Docs(context.Background(), &scriptedClaudeRunner{}, &arc42SyncRunner{docsChanged: false}, run)
	if changed || note != "no documentation impact" {
		t.Fatalf("clean doc must report no impact: changed=%v note=%q", changed, note)
	}
	if containsStr(run.ChangedFiles, arc42DocRelPath) {
		t.Fatal("unchanged doc must not join changed files")
	}
}

func TestArc42SyncSkipsDryRunAndMainRepoModes(t *testing.T) {
	run := arc42TestRun(t, []string{"internal/engine/foo.go"})
	run.WorktreePath = run.RepoDir
	if changed, note := syncArc42Docs(context.Background(), &scriptedClaudeRunner{}, &arc42SyncRunner{}, run); changed || note != "" {
		t.Fatalf("main-repo mode must be a silent no-op: %v %q", changed, note)
	}
}
