package engine

import (
	"context"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestVerifyFusionBuildDelegatesOnBareRepo pins bt_fusion's verification to
// the same bare-repo contract as VerifyGoapBuild: with no working tree there
// is nothing bt-fusion changed to build (it writes a vault report), go's VCS
// stamping dies on git status (observed live: 12s failures after an
// otherwise clean run), and `go build -o bt-agent` would overwrite the LIVE
// daemon binary in place if it ever succeeded. Skips on non-bare checkouts.
func TestVerifyFusionBuildDelegatesOnBareRepo(t *testing.T) {
	out, err := runGoapShell("git rev-parse --is-bare-repository")
	if err != nil || strings.TrimSpace(out) != "true" {
		t.Skipf("main repo is not bare here (out=%q err=%v)", strings.TrimSpace(out), err)
	}
	fn := GetAction("VerifyFusionBuild")
	if fn == nil {
		t.Fatal("VerifyFusionBuild not registered")
	}
	bb := &Blackboard{Task: "fusion", ChainState: map[string]any{}}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyFusionBuild on bare repo = %d, want 1 (delegate)", code)
	}
	if !strings.Contains(bb.Result, "Verification Delegated") {
		t.Fatalf("expected delegation note, got: %s", bb.Result)
	}
}
