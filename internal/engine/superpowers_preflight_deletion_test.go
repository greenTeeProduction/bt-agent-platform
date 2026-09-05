package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// runInDir runs a bash command in dir and fails the test on error.
func runInDir(t *testing.T, dir, command string) string {
	t.Helper()
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %q in %s failed: %v\n%s", command, dir, err, out)
	}
	return string(out)
}

// TestScheduledGoapFusionBuildTreeMaterialized_HandlesDeletedTrackedFiles pins
// the guard against the LATENT BUG the 2026-07-03 cicd-move incident predicted
// and the 2026-07-10 bt-scalability-probe untracking triggered: on a bare main
// repo, `git checkout -f HEAD -- .` rewrites files present in HEAD but neither
// removes on-disk files a commit deleted nor drops their stale index entries.
// A landing that deletes a tracked file then wedges EVERY subsequent cycle —
// the guard's own verification diff reports the phantom deletion forever. The
// guard must sync the index to HEAD after materializing so a deleting commit
// cannot wedge the fleet; the deleted file remains on disk as untracked, which
// the tracked-files contract deliberately ignores.
func TestScheduledGoapFusionBuildTreeMaterialized_HandlesDeletedTrackedFiles(t *testing.T) {
	// The pre-commit hook exports GIT_DIR/GIT_INDEX_FILE while running tests;
	// inherited by this test's git commands (and the guard's own shell), they
	// would silently operate on the OUTER repository instead of the throwaway
	// one. Scrub them for the test's duration (t.Setenv registers the restore).
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_PREFIX", "GIT_COMMON_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v)
			os.Unsetenv(k)
		}
	}

	dir := t.TempDir()

	// A repo with two tracked, materialized files at commit one.
	runInDir(t, dir, "git init -q . && git config user.email t@t.local && git config user.name t")
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doomed.txt"), []byte("doomed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, "git add -A && git commit -qm one")

	// The deleting commit lands ELSEWHERE (a clone plays the run worktree) and
	// reaches this repo as a pure ref update — exactly how the daemon's apply
	// moves the bare master. Index and on-disk tree stay frozen at commit one.
	runInDir(t, dir, "git clone -q . clone && cd clone && git config user.email t@t.local && git config user.name t && git rm -q doomed.txt && git commit -qm two")
	runInDir(t, dir, "git config core.bare true && git fetch -q ./clone master:master")

	prev := goapFusionRepo
	goapFusionRepo = dir
	t.Cleanup(func() { goapFusionRepo = prev })

	fn := GetAction("VerifyScheduledGoapFusionBuildTreeMaterialized")
	if fn == nil {
		t.Fatal("missing action VerifyScheduledGoapFusionBuildTreeMaterialized")
	}
	bb := &Blackboard{Task: "verify build tree materialized after deleting commit"}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("guard must pass after materializing a HEAD that deletes tracked files (index sync); got %d\nResult: %s", code, bb.Result)
	}

	// The guard's own verification must be clean afterwards.
	diff := runInDir(t, dir, "git --git-dir=.git --work-tree=. diff --name-only HEAD --")
	if strings.TrimSpace(diff) != "" {
		t.Fatalf("tracked files still differ from HEAD after the guard ran:\n%s", diff)
	}
	// keep.txt is still tracked and materialized; doomed.txt may remain on disk
	// but must be untracked (ignored by the contract).
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("tracked keep.txt vanished: %v", err)
	}
}
