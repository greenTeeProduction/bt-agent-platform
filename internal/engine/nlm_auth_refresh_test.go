package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// fakeNlmAuth scripts nlmAuthRun: returns queued outputs in order and
// records the invoked subcommands.
type fakeNlmAuth struct {
	outputs []string
	calls   []string
}

func (f *fakeNlmAuth) run(_ time.Duration, args ...string) string {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(f.outputs) == 0 {
		return ""
	}
	out := f.outputs[0]
	f.outputs = f.outputs[1:]
	return out
}

func withFakeNlmAuth(t *testing.T, f *fakeNlmAuth) {
	t.Helper()
	orig := nlmAuthRun
	nlmAuthRun = f.run
	t.Cleanup(func() { nlmAuthRun = orig })
}

func runAuthAction(t *testing.T) (*Blackboard, int) {
	t.Helper()
	fn := GetAction("CheckNotebookLMAuthAndRefresh")
	if fn == nil {
		t.Fatal("CheckNotebookLMAuthAndRefresh not registered")
	}
	bb := &Blackboard{Task: "research", ChainState: map[string]any{}}
	return bb, fn(btcore.NewBTContext(context.Background(), bb))
}

// Expired credentials must trigger a refresh attempt — observed live
// 2026-07-03: "Credentials have expired. Run 'nlm login'" went straight to
// failure because only "stale"/"not_configured" refreshed, and the
// notebooklm-researcher dead-lettered every 2h window without ever trying
// the login it was telling the operator to run.
func TestNlmAuthExpiredRefreshesAndRecoversOnCleanRecheck(t *testing.T) {
	f := &fakeNlmAuth{outputs: []string{
		"✗ Authentication failed: Credentials have expired.\nRun 'nlm login' to re-authenticate.",
		"Login OK",
		"✓ Authenticated as user@example.com",
	}}
	withFakeNlmAuth(t, f)

	bb, code := runAuthAction(t)
	if code != 1 {
		t.Fatalf("recovered auth = %d, want 1; result:\n%s", code, bb.Result)
	}
	if len(f.calls) != 3 || f.calls[1] != "login" {
		t.Fatalf("expected check → login → re-check, got %v", f.calls)
	}
}

// The verdict must come from the RE-CHECK, not the concatenated transcript —
// the old code grepped the combined string, so the original failure text
// poisoned even a successful refresh.
func TestNlmAuthStillExpiredAfterRefreshFails(t *testing.T) {
	f := &fakeNlmAuth{outputs: []string{
		"✗ Authentication failed: Credentials have expired.",
		"browser required",
		"✗ Authentication failed: Credentials have expired.",
	}}
	withFakeNlmAuth(t, f)

	// Unrecovered auth needs the USER (interactive browser login): failing
	// here only dead-letters the guardian every tick without changing
	// anything, so it degrades with success and a loud instruction instead.
	bb, code := runAuthAction(t)
	if code != 1 {
		t.Fatalf("unrecovered auth = %d, want 1 (degrade, not dead-letter)", code)
	}
	if bb.Outcome != "nlm_auth_needs_user" {
		t.Fatalf("Outcome = %q, want nlm_auth_needs_user", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "nlm login") {
		t.Fatalf("result must instruct the user: %s", bb.Result)
	}
}

func TestNlmAuthHealthySkipsRefresh(t *testing.T) {
	f := &fakeNlmAuth{outputs: []string{"✓ Authenticated as user@example.com"}}
	withFakeNlmAuth(t, f)

	_, code := runAuthAction(t)
	if code != 1 {
		t.Fatalf("healthy auth = %d, want 1", code)
	}
	if len(f.calls) != 1 {
		t.Fatalf("healthy auth must not refresh, calls: %v", f.calls)
	}
}
