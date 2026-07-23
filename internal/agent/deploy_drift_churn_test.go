package agent

import (
	"errors"
	"testing"
)

// Gap 4 of the 2026-07-23 fleet review: 9 restarts in 8h. Three churn
// sources, each pinned here — revision bumps with identical trees (PR-merge
// double-bumps) re-adopted content already running; a daemon that cannot
// restart itself rebuilt the same head every 20-minute tick; and the fleet
// sweep re-restarted siblings that had already adopted the head themselves.

// A merge commit of an already-adopted landing moves the revision but not the
// tree: rebuilding produces identical code with a different stamp, and
// restarting adopts nothing. Tree-identical revisions are not drift.
func TestDriftStatus_TreeIdenticalRevisionIsNotStale(t *testing.T) {
	prevHead, prevTree := driftHeadFn, driftTreeFn
	t.Cleanup(func() { driftHeadFn, driftTreeFn = prevHead, prevTree })

	driftHeadFn = func(string) (string, error) { return "merge456", nil }
	driftTreeFn = func(_ string, rev string) (string, error) { return "tree-same", nil }

	head, stale, err := DriftStatus("/r", "landing123")
	if err != nil || head != "merge456" {
		t.Fatalf("head=%q err=%v", head, err)
	}
	if stale {
		t.Fatal("revision bump with an identical tree must not be reported stale — it re-adopts already-running content")
	}

	// Different trees = genuine drift.
	driftTreeFn = func(_ string, rev string) (string, error) { return "tree-of-" + rev, nil }
	_, stale, err = DriftStatus("/r", "landing123")
	if err != nil || !stale {
		t.Fatalf("differing trees must stay stale; stale=%v err=%v", stale, err)
	}

	// Tree resolution failure falls back to the revision comparison.
	driftTreeFn = func(string, string) (string, error) { return "", errors.New("no such object") }
	_, stale, err = DriftStatus("/r", "landing123")
	if err != nil || !stale {
		t.Fatalf("tree-resolution failure must fall back to revision drift; stale=%v err=%v", stale, err)
	}
}

// RecordSuccess marks the head as built: the binaries on disk already come
// from it, so a later tick must not rebuild — only adoption may still be
// pending. A head change or a recorded failure (smoke-test rollback) clears
// the mark.
func TestRebuildBackoff_RecordSuccessMarksHeadBuilt(t *testing.T) {
	b := NewRebuildBackoff()
	if b.BuiltAt("h1") {
		t.Fatal("nothing built yet")
	}
	b.RecordSuccess("h1")
	if !b.BuiltAt("h1") {
		t.Fatal("RecordSuccess must mark the head built")
	}
	if b.BuiltAt("h2") {
		t.Fatal("a different head is not built")
	}
	b.RecordFailure("h1")
	if b.BuiltAt("h1") {
		t.Fatal("a recorded failure (e.g. smoke-test rollback) must clear the built mark so a fresh rebuild can run")
	}
}

// A tick that finds the head already built (the gardener's every-20-minutes
// case, or a deferred adoption after an in-flight job) must skip the rebuild
// but still proceed to adoption — the old flow returned at the backoff guard
// and a rebuilt-but-unrestarted daemon rebuilt the same head forever
// (00:16/00:36/00:56 on 2026-07-23).
func TestDriftWatchOnce_AlreadyBuiltHeadSkipsRebuildStillAdopts(t *testing.T) {
	prevHead, prevRebuild, prevRestart, prevSmoke := driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn = prevHead, prevRebuild, prevRestart, prevSmoke
	})
	adoptionStampDir = t.TempDir()
	t.Cleanup(func() { adoptionStampDir = "" })

	driftHeadFn = func(string) (string, error) { return "def", nil }
	rebuilds := 0
	driftRebuildFn = func(string, []RebuildTarget) error { rebuilds++; return nil }
	driftSmokeTestFn = func(string) error { return nil }
	var restarted []string
	driftRestartFn = func(unit string) error { restarted = append(restarted, unit); return nil }

	backoff := NewRebuildBackoff()
	backoff.RecordSuccess("def") // a previous tick already built this head

	res, err := DriftWatchOnce(DriftWatchConfig{
		RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, AutoRestart: true,
		Targets: []RebuildTarget{{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent"}},
		Binary:  "bt-agent", Backoff: backoff,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rebuilds != 0 {
		t.Fatalf("rebuilds = %d, want 0 — the head is already built", rebuilds)
	}
	if res.Rebuilt {
		t.Fatal("res.Rebuilt must be false when the rebuild was skipped")
	}
	if !res.Restarted || len(restarted) != 1 || restarted[0] != "bt-agent" {
		t.Fatalf("adoption must still proceed on an already-built head; restarted=%v res=%+v", restarted, res)
	}
}

// The fleet sweep must not re-restart a sibling that already adopted this
// head (its own watcher self-restarted): each successful restart writes a
// per-unit adoption stamp, and a sibling whose stamp matches the head is
// skipped.
func TestDriftWatchOnce_SiblingRestartSkippedWhenAlreadyAdopted(t *testing.T) {
	prevHead, prevRebuild, prevRestart, prevSmoke := driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn = prevHead, prevRebuild, prevRestart, prevSmoke
	})
	adoptionStampDir = t.TempDir()
	t.Cleanup(func() { adoptionStampDir = "" })

	driftHeadFn = func(string) (string, error) { return "def", nil }
	driftRebuildFn = func(string, []RebuildTarget) error { return nil }
	driftSmokeTestFn = func(string) error { return nil }
	var restarted []string
	driftRestartFn = func(unit string) error { restarted = append(restarted, unit); return nil }

	// The gardener already self-adopted head "def".
	writeAdoptionStamp("bt-gardener", "def")

	targets := []RebuildTarget{
		{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent", Unit: "bt-agent"},
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: "/bin/bt-gardener", Unit: "bt-gardener"},
		{Name: "bt-dashboard", Pkg: "./cmd/bt-dashboard", OutPath: "/bin/bt-dashboard", Unit: "bt-dashboard"},
	}
	res, err := DriftWatchOnce(DriftWatchConfig{
		RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, AutoRestart: true,
		RestartSiblings: true, Targets: targets, Binary: "bt-agent", Backoff: NewRebuildBackoff(),
	})
	if err != nil || !res.Restarted {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for _, u := range restarted {
		if u == "bt-gardener" {
			t.Fatalf("bt-gardener already adopted head def and must not be re-restarted; restarted=%v", restarted)
		}
	}
	if len(restarted) != 2 { // bt-dashboard + self
		t.Fatalf("restarted=%v, want exactly bt-dashboard and bt-agent", restarted)
	}

	// Every successful restart must stamp its unit at the head, so the next
	// sweep (and other watchers) see the adoption.
	for _, unit := range []string{"bt-agent", "bt-dashboard"} {
		if got := adoptionStampHead(unit); got != "def" {
			t.Fatalf("adoption stamp for %s = %q, want def", unit, got)
		}
	}
}

// An unstubbed test process must see inert stamps: the resolver returns ""
// under go test unless a test sets adoptionStampDir, so neither cross-test
// stamp leakage (the order-dependent restart-test failure that triggered the
// 01d8dcf stale-index revert cascade, 2026-07-23) nor live-home writes are
// possible from tests that forget isolation.
func TestAdoptionStamps_InertUnderTestWithoutOptIn(t *testing.T) {
	if adoptionStampDir != "" {
		t.Fatalf("precondition: adoptionStampDir override unexpectedly set: %q", adoptionStampDir)
	}
	writeAdoptionStamp("bt-gardener", "somehead")
	if got := adoptionStampHead("bt-gardener"); got != "" {
		t.Fatalf("unstubbed stamps must be inert under go test, read back %q", got)
	}
}
