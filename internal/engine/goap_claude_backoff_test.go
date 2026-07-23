package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// TestMain isolates operator-live state for the WHOLE engine test binary:
//
//   - the fleet-wide Claude backoff store: no test may arm or clear the live
//     ~/.go-bt-evolve/claude_backoff.json — the pollution class that silently
//     blocked live milestone 0977b1fa on 2026-07-10;
//   - the Superpowers runs directory: no test may scan or write the live
//     docs/superpowers/runs — tests running actions that embed the saturation
//     scan, pending-patch recovery, or orphaned-branch reap passes previously
//     walked hundreds of real run artifacts, inflating log-derived counts
//     4–1000× and misleading the 2026-07-23 fleet review three times.
//
// Tests whose assertions depend on store/dir contents additionally call
// isolateClaudeBackoffStore(t) / isolateSuperpowersRunsDir(t) for a private,
// deterministic path.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "engine-claude-backoff-*")
	if err == nil {
		goapClaudeBackoffPath = filepath.Join(dir, "claude_backoff.json")
	}
	runsDir, runsErr := os.MkdirTemp("", "engine-superpowers-runs-*")
	if runsErr == nil {
		superpowersRunsDir = runsDir
	}
	code := m.Run()
	if dir != "" {
		os.RemoveAll(dir)
	}
	if runsDir != "" {
		os.RemoveAll(runsDir)
	}
	os.Exit(code)
}

// isolateClaudeBackoffStore points goapClaudeBackoffPath at a private temp
// store. Tests that save, load, or clear Claude backoff state MUST call this
// (mirroring isolateGoapProgramStore): the store is FLEET-WIDE, so without
// isolation one test's stamp leaks into the next test's assertions.
func isolateClaudeBackoffStore(t *testing.T) {
	t.Helper()
	prev := goapClaudeBackoffPath
	goapClaudeBackoffPath = filepath.Join(t.TempDir(), "claude_backoff.json")
	t.Cleanup(func() { goapClaudeBackoffPath = prev })
}

func TestParseClaudeRateLimitReset(t *testing.T) {
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.Local)
	epoch := now.Add(3 * time.Hour).Unix()
	cases := []struct {
		name string
		text string
		want time.Time
		ok   bool
	}{
		{"pipe epoch seconds", fmt.Sprintf("Claude AI usage limit reached|%d", epoch), time.Unix(epoch, 0), true},
		{"pipe epoch millis", fmt.Sprintf("usage limit reached|%d", epoch*1000), time.Unix(epoch, 0), true},
		{"observed CLI 2.1.x pipe+clock", "Claude AI usage limit reached|resets 3pm", time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local), true},
		{"executor-wrapped multiline", "red-phase claude failed: session limit reached; resets at 3pm\nClaude Code session limit reached.", time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local), true},
		{"resets am same day", "5-hour limit reached ∙ resets 3am", time.Date(2026, 7, 15, 3, 0, 0, 0, time.Local), true},
		{"resets pm with minutes", "usage limit reached — resets 11:30pm", time.Date(2026, 7, 15, 23, 30, 0, 0, time.Local), true},
		{"resets at 24h clock", "rate limit: resets at 15:00", time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local), true},
		{"resets rolls to tomorrow", "resets 12:30am", time.Date(2026, 7, 16, 0, 30, 0, 0, time.Local), true},
		{"resets 12pm is noon", "resets 12pm", time.Date(2026, 7, 15, 12, 0, 0, 0, time.Local), true},
		{"resets 12am is midnight", "resets 12am", time.Date(2026, 7, 16, 0, 0, 0, 0, time.Local), true},
		{"bare hour rejected", "resets 3", time.Time{}, false},
		{"weekly date form falls back (real DLQ shape)", "You've hit your weekly limit · resets Jul 7", time.Time{}, false},
		{"past epoch rejected", fmt.Sprintf("limit reached|%d", now.Add(-time.Hour).Unix()), time.Time{}, false},
		{"absurd epoch rejected", fmt.Sprintf("limit reached|%d", now.Add(30*24*time.Hour).Unix()), time.Time{}, false},
		{"no hint", "green-phase claude failed: exit status 1", time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseClaudeRateLimitReset(tc.text, now)
			if ok != tc.ok || (ok && !got.Equal(tc.want)) {
				t.Fatalf("parseClaudeRateLimitReset(%q) = (%v, %v), want (%v, %v)", tc.text, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestClaudeBackoffDeadline(t *testing.T) {
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.Local)
	reset := now.Add(3 * time.Hour)

	t.Run("hint wins over fallback window", func(t *testing.T) {
		got := claudeBackoffDeadline(fmt.Sprintf("usage limit reached|%d", reset.Unix()), now, 6*time.Hour)
		if want := reset.Add(claudeResetMargin); !got.Equal(want) {
			t.Fatalf("deadline = %v, want reset+margin %v: a parseable reset must beat the fixed window", got, want)
		}
	})
	t.Run("no hint falls back to window", func(t *testing.T) {
		got := claudeBackoffDeadline("exit status 1", now, 45*time.Minute)
		if want := now.Add(45 * time.Minute); !got.Equal(want) {
			t.Fatalf("deadline = %v, want now+fallback %v: hint-less output must keep the pre-change behavior", got, want)
		}
	})
	t.Run("far reset capped at 24h", func(t *testing.T) {
		got := claudeBackoffDeadline(fmt.Sprintf("limit reached|%d", now.Add(48*time.Hour).Unix()), now, time.Hour)
		if want := now.Add(24 * time.Hour); !got.Equal(want) {
			t.Fatalf("deadline = %v, want the 24h cap %v: a mis-parse must not idle the fleet for days", got, want)
		}
	})
}

// TestClaudeBackoffState_SharedAcrossAgents proves the fleet-wide contract
// that motivated the store: on 2026-07-14/15 goap-fusion-runner's probe
// succeeded at 23:47 while goap-fusion-loop-runner slept ~3h more on its own
// private stamp. One agent's detection must close Claude for every agent, and
// one agent's clear must reopen it for every agent.
func TestClaudeBackoffState_SharedAcrossAgents(t *testing.T) {
	isolateClaudeBackoffStore(t)
	loop := &Blackboard{BB: blackboard.NewHandle(blackboard.NewManager(nil), "run-1", "", "goap-fusion-loop-runner")}
	sibling := &Blackboard{BB: blackboard.NewHandle(blackboard.NewManager(nil), "run-2", "", "goap-fusion-runner")}

	until := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	saveClaudeBackoffState(loop, until)

	got, ok := loadClaudeBackoffState(sibling)
	if !ok || !got.Equal(until) {
		t.Fatalf("sibling agent loadClaudeBackoffState = (%v, %v), want (%v, true): one agent's rate-limit detection must close Claude fleet-wide", got, ok, until)
	}
	if !claudeBackoffActive(sibling, until.Add(-time.Minute)) {
		t.Fatal("claudeBackoffActive on a sibling agent inside the window = false, want true")
	}

	clearClaudeBackoffState(sibling)
	fresh := &Blackboard{BB: blackboard.NewHandle(blackboard.NewManager(nil), "run-3", "", "goap-fusion-loop-runner")}
	if _, ok := loadClaudeBackoffState(fresh); ok {
		t.Fatal("clear by one agent must reopen the window fleet-wide: a fresh run of another agent still sees a stamp")
	}
}

// TestClaudeBackoffState_LegacyAgentScopeStampStillHonored covers the deploy
// transition: stamps written by the previous per-agent implementation must
// stay honored until they expire (no double-probe mid-window) and then
// self-clear like everything else — no migration step.
func TestClaudeBackoffState_LegacyAgentScopeStampStillHonored(t *testing.T) {
	isolateClaudeBackoffStore(t)
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "goap-loop"}
	until := time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC)
	if err := bb.BB.Mgr.Set(scope, "goap_fusion_claude_backoff_until", until.Format(time.RFC3339), "legacy per-agent stamp", "text"); err != nil {
		t.Fatalf("seeding legacy agent-scope stamp: %v", err)
	}

	if got, ok := loadClaudeBackoffState(bb); !ok || !got.Equal(until) {
		t.Fatalf("legacy stamp load = (%v, %v), want (%v, true): pre-deploy stamps must stay honored until expiry", got, ok, until)
	}
	if claudeBackoffActive(bb, until.Add(time.Minute)) {
		t.Fatal("expired legacy stamp must read inactive")
	}
	if _, ok := loadClaudeBackoffState(bb); ok {
		t.Fatal("expired legacy stamp must self-clear from the agent scope")
	}
}
