package engine

import (
	"context"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestVerifyGoapBuildDelegatesOnBareRepo pins the bare-main-repo contract:
// with no working tree there is nothing current to build (on-disk files are
// pre-conversion leftovers, and go's VCS stamping dies on `git status`), and
// the run was already verified inside its worktree by the apply step — so
// VerifyGoapBuild must pass through with a delegation note instead of
// failing the whole cycle after a successful apply (observed live: run
// 20260702T234913 committed and pushed 714c4c3, then reported
// "Verification Failed / error obtaining VCS status").
//
// Skips when goapFusionRepo is not a bare repo (CI checkouts): the non-bare
// path runs real multi-minute builds.
func TestVerifyGoapBuildDelegatesOnBareRepo(t *testing.T) {
	out, err := runGoapShell("git rev-parse --is-bare-repository")
	if err != nil || strings.TrimSpace(out) != "true" {
		t.Skipf("goapFusionRepo is not a bare repo here (out=%q err=%v)", strings.TrimSpace(out), err)
	}
	fn := GetAction("VerifyGoapBuild")
	if fn == nil {
		t.Fatal("VerifyGoapBuild not registered")
	}
	bb := &Blackboard{Task: "verify build"}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyGoapBuild on bare repo = %d, want 1 (delegate to apply-stage verification)", code)
	}
	if !strings.Contains(bb.Result, "Verification Delegated") {
		t.Fatalf("expected delegation note, got: %s", bb.Result[:min(len(bb.Result), 200)])
	}
}
