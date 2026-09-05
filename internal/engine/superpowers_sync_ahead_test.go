package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// bareSyncRunner fakes a bare main repo for the worktree-sync ancestry flow:
// rev-parse --is-bare-repository answers true, FETCH_HEAD/master resolve to
// distinct SHAs, and fail forces exit-1 on any command containing a key.
// Regression context: until 2026-07-03 the bare sync ran a plain
// `git fetch origin master:master`, so a locally landed but not yet pushed
// commit (local master ahead of origin) made every scheduled goap-fusion
// cycle die on a rejected non-fast-forward — ~2.5min of Claude research
// burned per attempt, x3 retries, then dead-letter.
type bareSyncRunner struct {
	calls []string
	fail  map[string]string // command substring -> forced output
}

func (r *bareSyncRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	for key, out := range r.fail {
		if strings.Contains(cmd, key) {
			res.Err = errors.New("exit status 1")
			res.Output = out
			return res
		}
	}
	switch {
	case strings.HasPrefix(cmd, "git rev-parse --is-bare-repository"):
		res.Output = "true\n"
	case strings.HasPrefix(cmd, "git rev-parse FETCH_HEAD"):
		res.Output = "or1g1n0\n"
	case strings.HasPrefix(cmd, "git rev-parse master"):
		res.Output = "l0cal00\n"
	}
	return res
}

func (r *bareSyncRunner) joined() string { return strings.Join(r.calls, "\n") }

func TestBareSyncLocalAheadOfOriginProceedsWithoutRefUpdate(t *testing.T) {
	// Origin's tip is already an ancestor of local master (local is ahead or
	// equal): the sync goal — local has everything origin has — is met. The
	// cycle must proceed; the apply stage's push carries the ahead commits.
	runner := &bareSyncRunner{}
	if err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo"); err != nil {
		t.Fatalf("local-ahead sync must not fail the cycle: %v", err)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git fetch origin master") {
		t.Fatalf("sync must still fetch origin's tip; calls:\n%s", joined)
	}
	for _, forbidden := range []string{"master:master", "git update-ref"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("local-ahead sync must not move master (%q ran); calls:\n%s", forbidden, joined)
		}
	}
}

func TestBareSyncLocalBehindFastForwardsViaGuardedUpdateRef(t *testing.T) {
	runner := &bareSyncRunner{fail: map[string]string{
		"merge-base --is-ancestor or1g1n0 master": "", // origin NOT contained locally
	}}
	if err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo"); err != nil {
		t.Fatalf("local-behind sync must fast-forward, not fail: %v", err)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git update-ref refs/heads/master or1g1n0 l0cal00") {
		t.Fatalf("behind sync must ff master to origin tip guarded by the old value; calls:\n%s", joined)
	}
}

func TestBareSyncDivergedMasterFailsWithClearMessage(t *testing.T) {
	runner := &bareSyncRunner{fail: map[string]string{
		"merge-base --is-ancestor": "", // neither direction is an ancestor
	}}
	err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo")
	if err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("diverged masters must fail with a divergence message, got: %v", err)
	}
	if strings.Contains(runner.joined(), "git update-ref") {
		t.Fatalf("diverged sync must never move master; calls:\n%s", runner.joined())
	}
}

func TestBareSyncFetchFailureStillSurfaces(t *testing.T) {
	runner := &bareSyncRunner{fail: map[string]string{
		"fetch origin master": "network unreachable",
	}}
	err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo")
	if err == nil || !strings.Contains(err.Error(), "could not fetch origin master") {
		t.Fatalf("fetch failure must surface, got: %v", err)
	}
}
