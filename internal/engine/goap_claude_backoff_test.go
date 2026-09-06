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
//   - the fleet-wide Codex backoff store: same isolation for the
//     provider-namespaced ~/.go-bt-evolve/codex_backoff.json, so Codex backoff
//     tests never arm or clear the live stamp;
//   - the Superpowers runs directory: no test may scan or write the live
//     docs/superpowers/runs — tests running actions that embed the saturation
//     scan, pending-patch recovery, or orphaned-branch reap passes previously
//     walked hundreds of real run artifacts, inflating log-derived counts
//     4–1000× and misleading the 2026-07-23 fleet review three times.
//
// Tests whose assertions depend on store/dir contents additionally call
// isolateClaudeBackoffStore(t) / isolateCodexBackoffStore(t) /
// isolateSuperpowersRunsDir(t) for a private, deterministic path.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "engine-claude-backoff-*")
	if err == nil {
		goapClaudeBackoffPath = filepath.Join(dir, "claude_backoff.json")
		goapCodexBackoffPath = filepath.Join(dir, "codex_backoff.json")
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

// isolateCodexBackoffStore points goapCodexBackoffPath at a private temp store
// (the Codex-namespaced sibling of isolateClaudeBackoffStore). Codex backoff
// tests MUST call this so a Codex stamp never leaks into another test's
// assertions or the live ~/.go-bt-evolve/codex_backoff.json.
func isolateCodexBackoffStore(t *testing.T) {
	t.Helper()
	prev := goapCodexBackoffPath
	goapCodexBackoffPath = filepath.Join(t.TempDir(), "codex_backoff.json")
	t.Cleanup(func() { goapCodexBackoffPath = prev })
}

// isolateBackoffStores isolates BOTH provider backoff stores in one call — for
// tests that assert the cross-provider non-interference contract (a Codex stamp
// must never appear in the Claude store and vice versa).
func isolateBackoffStores(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	prevClaude := goapClaudeBackoffPath
	prevCodex := goapCodexBackoffPath
	goapClaudeBackoffPath = filepath.Join(dir, "claude_backoff.json")
	goapCodexBackoffPath = filepath.Join(dir, "codex_backoff.json")
	t.Cleanup(func() {
		goapClaudeBackoffPath = prevClaude
		goapCodexBackoffPath = prevCodex
	})
}

func TestParseClaudeRateLimitReset(t *testing.T) {
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.Local)
	epoch := now.Add(3 * time.Hour).Unix()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("loading Europe/Berlin location: %v", err)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("loading America/New_York location: %v", err)
	}
	weeklyNow := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		text string
		now  time.Time
		want time.Time
		ok   bool
	}{
		{"pipe epoch seconds", fmt.Sprintf("Claude AI usage limit reached|%d", epoch), time.Time{}, time.Unix(epoch, 0), true},
		{"pipe epoch millis", fmt.Sprintf("usage limit reached|%d", epoch*1000), time.Time{}, time.Unix(epoch, 0), true},
		{"observed CLI 2.1.x pipe+clock", "Claude AI usage limit reached|resets 3pm", time.Time{}, time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local), true},
		{"executor-wrapped multiline", "red-phase claude failed: session limit reached; resets at 3pm\nClaude Code session limit reached.", time.Time{}, time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local), true},
		{"resets am same day", "5-hour limit reached ∙ resets 3am", time.Time{}, time.Date(2026, 7, 15, 3, 0, 0, 0, time.Local), true},
		{"resets pm with minutes", "usage limit reached — resets 11:30pm", time.Time{}, time.Date(2026, 7, 15, 23, 30, 0, 0, time.Local), true},
		{"resets at 24h clock", "rate limit: resets at 15:00", time.Time{}, time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local), true},
		{"resets rolls to tomorrow", "resets 12:30am", time.Time{}, time.Date(2026, 7, 16, 0, 30, 0, 0, time.Local), true},
		{"resets 12pm is noon", "resets 12pm", time.Time{}, time.Date(2026, 7, 15, 12, 0, 0, 0, time.Local), true},
		{"resets 12am is midnight", "resets 12am", time.Time{}, time.Date(2026, 7, 16, 0, 0, 0, 0, time.Local), true},
		{"bare hour rejected", "resets 3", time.Time{}, time.Time{}, false},
		{"weekly date form falls back without time/zone", "You've hit your weekly limit · resets Jul 7", time.Time{}, time.Time{}, false},
		{
			"weekly quota with explicit date and zone",
			"You've hit your weekly limit · resets Jul 7, 11pm (Europe/Berlin)",
			weeklyNow,
			time.Date(2026, 7, 7, 23, 0, 0, 0, berlin),
			true,
		},
		{
			"weekly quota time-only with zone",
			"resets 11pm (America/New_York)",
			weeklyNow,
			time.Date(2026, 7, 3, 23, 0, 0, 0, newYork),
			true,
		},
		{"past epoch rejected", fmt.Sprintf("limit reached|%d", now.Add(-time.Hour).Unix()), time.Time{}, time.Time{}, false},
		{"absurd epoch rejected", fmt.Sprintf("limit reached|%d", now.Add(30*24*time.Hour).Unix()), time.Time{}, time.Time{}, false},
		{"no hint", "green-phase claude failed: exit status 1", time.Time{}, time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useNow := tc.now
			if useNow.IsZero() {
				useNow = now
			}
			got, ok := parseClaudeRateLimitReset(tc.text, useNow)
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
	t.Run("weekly quota multi-day deadline is not clipped to 24h", func(t *testing.T) {
		berlin, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			t.Fatalf("loading Europe/Berlin location: %v", err)
		}
		weeklyNow := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
		got := claudeBackoffDeadline("You've hit your weekly limit · resets Jul 7, 11pm (Europe/Berlin)", weeklyNow, time.Hour)
		want := time.Date(2026, 7, 7, 23, 0, 0, 0, berlin).Add(claudeResetMargin)
		if !got.Equal(want) {
			t.Fatalf("deadline = %v, want reset+margin %v: a weekly-quota reset more than 24h out must not be clipped to now+24h", got, want)
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

// TestDelegationBackoffState_ProviderNamespaced proves a Codex rate limit
// writes ONLY the Codex store (codex_backoff.json + codex ChainState key) and
// never touches the Claude store. A Codex failure must not close Claude and
// vice versa — this is the isolation contract the provider selector depends on.
func TestDelegationBackoffState_ProviderNamespaced(t *testing.T) {
	isolateBackoffStores(t)
	bb := &Blackboard{BB: blackboard.NewHandle(blackboard.NewManager(nil), "run-1", "", "goap-loop")}
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	saveDelegationBackoffState(bb, DelegationProviderCodex, until)

	if _, ok := readSharedBackoff(backoffPathFor(DelegationProviderCodex)); !ok {
		t.Fatal("codex shared store = inactive after a Codex save, want a recorded deadline")
	}
	if _, ok := readSharedBackoff(backoffPathFor(DelegationProviderClaude)); ok {
		t.Fatal("claude shared store = active after a Codex save: a Codex rate limit must never write the Claude cooldown")
	}
	if _, ok := bb.ChainState[backoffChainKey(DelegationProviderCodex)]; !ok {
		t.Fatalf("Codex ChainState key %q not written: saveDelegationBackoffState must stamp the provider's own key", backoffChainKey(DelegationProviderCodex))
	}
	if _, ok := bb.ChainState[backoffChainKey(DelegationProviderClaude)]; ok {
		t.Fatalf("Claude ChainState key %q written by a Codex save: the keys must be namespaced by provider", backoffChainKey(DelegationProviderClaude))
	}

	if _, ok := loadDelegationBackoffState(bb, DelegationProviderClaude); ok {
		t.Fatal("loadDelegationBackoffState(claude) = active after a Codex save: a Codex stamp must be invisible to Claude's backoff guard")
	}
	if got, ok := loadDelegationBackoffState(bb, DelegationProviderCodex); !ok || !got.Equal(until) {
		t.Fatalf("loadDelegationBackoffState(codex) = (%v, %v), want (%v, true)", got, ok, until)
	}
}

// TestDelegationBackoffState_ClearCodexLeavesClaudeIntact proves that clearing
// one provider's cooldown never clears the other's.
func TestDelegationBackoffState_ClearCodexLeavesClaudeIntact(t *testing.T) {
	isolateBackoffStores(t)
	bb := &Blackboard{BB: blackboard.NewHandle(blackboard.NewManager(nil), "run-1", "", "goap-loop")}
	claudeUntil := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	codexUntil := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	saveDelegationBackoffState(bb, DelegationProviderClaude, claudeUntil)
	saveDelegationBackoffState(bb, DelegationProviderCodex, codexUntil)

	clearDelegationBackoffState(bb, DelegationProviderCodex)

	if _, ok := loadDelegationBackoffState(bb, DelegationProviderCodex); ok {
		t.Fatal("loadDelegationBackoffState(codex) = active after clearDelegationBackoffState(codex), want inactive")
	}
	if got, ok := loadDelegationBackoffState(bb, DelegationProviderClaude); !ok || !got.Equal(claudeUntil) {
		t.Fatalf("loadDelegationBackoffState(claude) = (%v, %v) after clearing Codex, want (%v, true): clearing Codex must never clear the Claude cooldown", got, ok, claudeUntil)
	}
}

// TestIsDelegationRateLimit_ProviderDispatch proves the rate-limit matcher is
// provider-specific: a Claude-only signal must not trip the Codex matcher and a
// Codex-only signal must not trip the Claude matcher.
func TestIsDelegationRateLimit_ProviderDispatch(t *testing.T) {
	// "session limit" is a Claude CLI phrase (isClaudeRateLimit), not in the
	// conservative Codex matcher.
	if !isDelegationRateLimit(DelegationProviderClaude, "Claude Code session limit reached") {
		t.Fatal("claude session-limit output must read as a Claude rate limit")
	}
	if isDelegationRateLimit(DelegationProviderCodex, "Claude Code session limit reached") {
		t.Fatal("claude session-limit output must NOT read as a Codex rate limit")
	}
	// "429" is an OpenAI/Codex HTTP signal (isCodexRateLimit); the Claude
	// matcher does not include it.
	if !isDelegationRateLimit(DelegationProviderCodex, "429 too many requests") {
		t.Fatal("429 output must read as a Codex rate limit")
	}
	if isDelegationRateLimit(DelegationProviderClaude, "429 too many requests") {
		t.Fatal("429 output must NOT read as a Claude rate limit")
	}
}

// TestDelegationBackoffDeadline_CodexUsesFixedFallback proves Codex does not
// inherit Claude's CLI-reset parsing: Codex's reset shape is uncharacterized,
// so a Codex rate limit uses the fixed fallback window, while Claude still
// honors a parseable "resets <time>" hint.
func TestDelegationBackoffDeadline_CodexUsesFixedFallback(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local)

	codexGot := delegationBackoffDeadline(DelegationProviderCodex, "usage limit reached|resets 3pm", now, time.Hour)
	if want := now.Add(time.Hour); !codexGot.Equal(want) {
		t.Fatalf("codex deadline = %v, want fixed now+fallback %v: Codex must not parse Claude's reset hint", codexGot, want)
	}
	claudeGot := delegationBackoffDeadline(DelegationProviderClaude, "usage limit reached|resets 3pm", now, time.Hour)
	if want := time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local).Add(claudeResetMargin); !claudeGot.Equal(want) {
		t.Fatalf("claude deadline = %v, want CLI-reported reset+margin %v", claudeGot, want)
	}
}

// TestDelegationBackoffWindow_Provider proves each provider reads its OWN
// fallback-window env var (BT_GOAP_CLAUDE_BACKOFF vs BT_GOAP_CODEX_BACKOFF).
func TestDelegationBackoffWindow_Provider(t *testing.T) {
	t.Setenv("BT_GOAP_CLAUDE_BACKOFF", "3h")
	t.Setenv("BT_GOAP_CODEX_BACKOFF", "90m")

	if got, want := delegationBackoffWindow(DelegationProviderClaude), 3*time.Hour; got != want {
		t.Fatalf("delegationBackoffWindow(claude) = %v, want %v (BT_GOAP_CLAUDE_BACKOFF)", got, want)
	}
	if got, want := delegationBackoffWindow(DelegationProviderCodex), 90*time.Minute; got != want {
		t.Fatalf("delegationBackoffWindow(codex) = %v, want %v (BT_GOAP_CODEX_BACKOFF)", got, want)
	}
}

// TestDelegationBinary_Provider proves the preflight resolves each provider's
// own binary env var (BT_SUPERPOWERS_CLAUDE_BIN vs BT_SUPERPOWERS_CODEX_BIN)
// rather than hardcoding Claude.
func TestDelegationBinary_Provider(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CLAUDE_BIN", "/opt/claude")
	t.Setenv("BT_SUPERPOWERS_CODEX_BIN", "/opt/codex")

	if got := delegationBinary(DelegationProviderClaude); got != "/opt/claude" {
		t.Fatalf("delegationBinary(claude) = %q, want /opt/claude", got)
	}
	if got := delegationBinary(DelegationProviderCodex); got != "/opt/codex" {
		t.Fatalf("delegationBinary(codex) = %q, want /opt/codex", got)
	}
}
