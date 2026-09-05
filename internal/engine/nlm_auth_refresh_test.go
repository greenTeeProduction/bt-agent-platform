package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/notebooklmauth"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestNotebookLMAuthEntrypointsSharePolicy(t *testing.T) {
	for _, status := range []string{"valid", "auth_required", "network_error", "auth_error", "cooldown", "in_progress"} {
		t.Run(status, func(t *testing.T) {
			parent := t.Context()
			calls := 0
			old := nlmAuthEnsure
			nlmAuthEnsure = func(ctx context.Context) notebooklmauth.Result {
				if ctx != parent {
					t.Error("caller context was lost")
				}
				calls++
				return notebooklmauth.Result{Status: status, Detail: "policy verdict"}
			}
			t.Cleanup(func() { nlmAuthEnsure = old })
			for _, name := range []string{"CheckNotebookLMAuth", "CheckNotebookLMAuthAndRefresh"} {
				bb := &Blackboard{}
				got := GetAction(name)(btcore.NewBTContext(parent, bb))
				want, outcome := -1, status
				if status == "valid" {
					want, outcome = 1, "success"
				}
				if got != want || bb.Outcome != outcome || !strings.Contains(bb.Result, status) || bb.ChainState["nlm_auth"] == nil {
					t.Fatalf("%s: code=%d outcome=%s result=%s", name, got, bb.Outcome, bb.Result)
				}
			}
			out := newNotebookLMAuthRefreshTool().CallContext(parent, "")
			if !strings.Contains(out, status) || calls != 3 {
				t.Fatalf("raw tool bypassed policy: %s; calls=%d", out, calls)
			}
		})
	}
}
