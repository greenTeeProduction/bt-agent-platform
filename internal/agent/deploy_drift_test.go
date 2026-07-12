package agent

import (
	"errors"
	"testing"
	"time"
)

// DriftStatus compares the running build revision against the repo HEAD.
// The git call is stubbed (driftHeadFn) so the logic is tested without a repo.
func TestDriftStatus(t *testing.T) {
	prev := driftHeadFn
	t.Cleanup(func() { driftHeadFn = prev })

	cases := []struct {
		name      string
		head      string
		headErr   error
		running   string
		wantHead  string
		wantStale bool
		wantErr   bool
	}{
		{"in sync", "abc123", nil, "abc123", "abc123", false, false},
		{"stale — HEAD moved past running binary", "def456", nil, "abc123", "def456", true, false},
		{"unstamped build cannot be compared", "def456", nil, "unknown", "def456", false, false},
		{"empty running revision cannot be compared", "def456", nil, "", "def456", false, false},
		{"git failure surfaces", "", errors.New("not a repo"), "abc123", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driftHeadFn = func(string) (string, error) { return c.head, c.headErr }
			head, stale, err := DriftStatus("/repo", c.running)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if head != c.wantHead || stale != c.wantStale {
				t.Fatalf("DriftStatus = (%q, %v), want (%q, %v)", head, stale, c.wantHead, c.wantStale)
			}
		})
	}
}

// DriftWatchOnce logs a WARN on drift and only rebuilds when AutoRebuild is set —
// the production default (flag off) must be detection-only, never a self-rebuild.
func TestDriftWatchOnce(t *testing.T) {
	prevHead, prevRebuild := driftHeadFn, driftRebuildFn
	t.Cleanup(func() { driftHeadFn, driftRebuildFn = prevHead, prevRebuild })

	targets := []RebuildTarget{{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent"}}

	t.Run("in sync: no rebuild regardless of flag", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "abc", nil }
		called := false
		driftRebuildFn = func(string, []RebuildTarget) error { called = true; return nil }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, Targets: targets})
		if err != nil || res.Stale || res.Rebuilt || called {
			t.Fatalf("in-sync: res=%+v err=%v called=%v, want no drift/rebuild", res, err, called)
		}
	})

	t.Run("stale + AutoRebuild off: WARN only, no rebuild", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "def", nil }
		called := false
		driftRebuildFn = func(string, []RebuildTarget) error { called = true; return nil }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: false, Targets: targets})
		if err != nil || !res.Stale || res.Rebuilt || called {
			t.Fatalf("stale/off: res=%+v err=%v called=%v, want stale && no rebuild", res, err, called)
		}
	})

	t.Run("stale + AutoRebuild on: rebuilds", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "def", nil }
		var gotTargets []RebuildTarget
		driftRebuildFn = func(_ string, ts []RebuildTarget) error { gotTargets = ts; return nil }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, Targets: targets})
		if err != nil || !res.Stale || !res.Rebuilt || len(gotTargets) != 1 {
			t.Fatalf("stale/on: res=%+v err=%v targets=%v, want stale && rebuilt", res, err, gotTargets)
		}
	})

	t.Run("stale + AutoRebuild on but rebuild fails: reports error, Rebuilt false", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "def", nil }
		driftRebuildFn = func(string, []RebuildTarget) error { return errors.New("build blew up") }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, Targets: targets})
		if err == nil || res.Rebuilt {
			t.Fatalf("rebuild failure: res=%+v err=%v, want error && Rebuilt=false", res, err)
		}
	})
}

// RebuildBackoff caps consecutive rebuild attempts against the same stale
// HEAD, with exponential backoff between attempts, and permanently blocks
// further attempts once MaxAttempts is reached — the guardrail (milestone 5,
// program 94b0b31) that stops a broken commit from retry-storming a
// `go build` every watcher interval. The guard resets the instant HEAD
// advances, since a new commit deserves a fresh chance.
func TestRebuildBackoff_BlocksAfterMaxAttempts(t *testing.T) {
	fakeNow := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	guard := &RebuildBackoff{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		MaxDelay:    time.Minute,
	}
	guard.nowFn = func() time.Time { return fakeNow }

	if !guard.Allow("head1") {
		t.Fatal("first attempt at a new head should be allowed")
	}
	guard.RecordFailure("head1")
	guard.RecordFailure("head1")
	guard.RecordFailure("head1")

	// Even after the backoff delay elapses, MaxAttempts consecutive failures
	// at the same head must permanently block further attempts.
	fakeNow = fakeNow.Add(time.Hour)
	if guard.Allow("head1") {
		t.Fatal("after MaxAttempts consecutive failures, Allow should stay false regardless of elapsed time")
	}

	// A new HEAD (e.g. a fix landed) must reset the guard immediately.
	if !guard.Allow("head2") {
		t.Fatal("a new HEAD should reset the guard and be allowed immediately")
	}
}

func TestRebuildBackoff_DelayGrowsBetweenAttempts(t *testing.T) {
	fakeNow := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	guard := &RebuildBackoff{
		MaxAttempts: 5,
		BaseDelay:   time.Minute,
		MaxDelay:    time.Hour,
	}
	guard.nowFn = func() time.Time { return fakeNow }

	if !guard.Allow("head1") {
		t.Fatal("first attempt should be allowed")
	}
	guard.RecordFailure("head1") // attempt 1: backoff ~= BaseDelay (1m)

	fakeNow = fakeNow.Add(30 * time.Second)
	if guard.Allow("head1") {
		t.Fatal("attempt should be blocked during the first backoff window")
	}
	fakeNow = fakeNow.Add(time.Minute) // 90s past attempt 1, > 1m base delay
	if !guard.Allow("head1") {
		t.Fatal("attempt should be allowed once the first backoff window elapses")
	}

	guard.RecordFailure("head1") // attempt 2: backoff should now be longer (exponential)
	fakeNow = fakeNow.Add(time.Minute + time.Second)
	if guard.Allow("head1") {
		t.Fatal("second-attempt backoff should be longer than the first — retry storm not throttled")
	}
}

func TestRebuildBackoff_SuccessResetsAttempts(t *testing.T) {
	fakeNow := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	guard := &RebuildBackoff{MaxAttempts: 2, BaseDelay: time.Minute, MaxDelay: time.Hour}
	guard.nowFn = func() time.Time { return fakeNow }

	guard.RecordFailure("head1")
	guard.RecordSuccess("head1")

	// A recorded success (a later, working rebuild at the same head) must
	// clear the failure count — the next drift at this head starts fresh.
	if !guard.Allow("head1") {
		t.Fatal("Allow should be true immediately after a recorded success")
	}
}

// DriftWatchOnce must consult the backoff guard before rebuilding, and must
// never invoke the rebuild function while attempts are throttled — a broken
// HEAD must not retry-storm `go build` every watcher tick.
func TestDriftWatchOnce_RespectsBackoffGuard(t *testing.T) {
	prevHead, prevRebuild := driftHeadFn, driftRebuildFn
	t.Cleanup(func() { driftHeadFn, driftRebuildFn = prevHead, prevRebuild })

	driftHeadFn = func(string) (string, error) { return "def", nil }
	targets := []RebuildTarget{{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent"}}

	guard := &RebuildBackoff{MaxAttempts: 1, BaseDelay: time.Hour, MaxDelay: time.Hour}
	fakeNow := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	guard.nowFn = func() time.Time { return fakeNow }

	callCount := 0
	driftRebuildFn = func(string, []RebuildTarget) error {
		callCount++
		return errors.New("broken HEAD: compile error")
	}

	cfg := DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, Targets: targets, Backoff: guard}

	// First tick: attempt allowed, fails, guard records the failure.
	if _, err := DriftWatchOnce(cfg); err == nil {
		t.Fatal("expected the rebuild failure to surface as an error")
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 rebuild attempt on the first tick, got %d", callCount)
	}

	// Second tick, same stale HEAD: MaxAttempts=1 already spent — must be
	// blocked without invoking the rebuild function again.
	if _, err := DriftWatchOnce(cfg); err != nil {
		t.Fatalf("a backoff-blocked tick should not surface a rebuild error, got %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected the second tick to be blocked by backoff (no new rebuild attempt), call count = %d", callCount)
	}
}
