# Shared Claude Rate-Limit Backoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Backoff deadlines derive from the Claude CLI's reported reset time (fallback: the existing fixed windows), stored in one fleet-wide ADR-003 file instead of per-agent blackboard entries.

**Architecture:** New `internal/engine/goap_claude_backoff.go` absorbs the four existing backoff helpers from `actions_goap_fusion.go` and adds a mutex-guarded atomic-JSON store (`~/.go-bt-evolve/claude_backoff.json`, path seam `goapClaudeBackoffPath`) plus `parseClaudeRateLimitReset`/`claudeBackoffDeadline`. Load returns the latest of {shared file, legacy agent-scope key, ChainState}; clear/self-clear wipes all three (legacy stamps age out, no migration).

**Tech Stack:** Go 1.26 (`PATH=/usr/local/go/bin:$PATH`), stdlib only (regexp, sync, encoding/json).

**Spec:** `docs/superpowers/specs/2026-07-15-shared-claude-backoff-design.md`

## Global Constraints

- Go binary: prefix every command with `PATH=/usr/local/go/bin:$PATH`.
- All identifiers stay **unexported** (no API_REFERENCE / doc-drift impact).
- Every test touching backoff state MUST call `isolateClaudeBackoffStore(t)` — the default path is the operator's LIVE fleet-wide stamp.
- ADR-003 persistence: JSON under `~/.go-bt-evolve/`, tmp+rename atomic writes.
- Single TDD commit at the end (repo convention: the pre-commit gate is multi-minute; prior multi-part landings — 1861631, 3cd05fc, ffb1ab8 — are single commits).
- Test suites regenerate `graphify-out/` — discard before committing.

---

### Task 1: Reset parser + deadline helper

**Files:**
- Create: `internal/engine/goap_claude_backoff.go`
- Test: `internal/engine/goap_claude_backoff_test.go`

**Interfaces:**
- Produces: `parseClaudeRateLimitReset(text string, now time.Time) (time.Time, bool)`, `claudeBackoffDeadline(errText string, now time.Time, fallback time.Duration) time.Time`, `const claudeResetMargin = 2 * time.Minute`.

- [x] **Step 1: Write the failing tests**

```go
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
		{"resets am same day", "5-hour limit reached ∙ resets 3am", time.Date(2026, 7, 15, 3, 0, 0, 0, time.Local), true},
		{"resets pm", "usage limit reached — resets 11:30pm", time.Date(2026, 7, 15, 23, 30, 0, 0, time.Local), true},
		{"resets at 24h clock", "rate limit: resets at 15:00", time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local), true},
		{"resets rolls to tomorrow", "resets 12:30am", time.Date(2026, 7, 16, 0, 30, 0, 0, time.Local), true},
		{"bare hour rejected", "resets 3", time.Time{}, false},
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
	t.Run("hint wins over fallback", func(t *testing.T) {
		got := claudeBackoffDeadline(fmt.Sprintf("usage limit reached|%d", reset.Unix()), now, 6*time.Hour)
		if want := reset.Add(claudeResetMargin); !got.Equal(want) {
			t.Fatalf("deadline = %v, want reset+margin %v", got, want)
		}
	})
	t.Run("no hint falls back to window", func(t *testing.T) {
		got := claudeBackoffDeadline("exit status 1", now, 45*time.Minute)
		if want := now.Add(45 * time.Minute); !got.Equal(want) {
			t.Fatalf("deadline = %v, want now+fallback %v", got, want)
		}
	})
	t.Run("far reset capped at 24h", func(t *testing.T) {
		got := claudeBackoffDeadline(fmt.Sprintf("limit reached|%d", now.Add(48*time.Hour).Unix()), now, time.Hour)
		if want := now.Add(24 * time.Hour); !got.Equal(want) {
			t.Fatalf("deadline = %v, want 24h cap %v", got, want)
		}
	})
}
```

- [x] **Step 2: Run tests, verify they fail** — `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -short -count=1 -run 'ParseClaudeRateLimitReset|ClaudeBackoffDeadline'` → FAIL: undefined.

- [x] **Step 3: Implement** (in the new file):

```go
const claudeResetMargin = 2 * time.Minute

var (
	claudeResetEpochRe = regexp.MustCompile(`(?i)limit[^|\n]{0,60}\|\s*(\d{10,13})`)
	claudeResetClockRe = regexp.MustCompile(`(?i)\bresets\b(?:\s+at)?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
)

func parseClaudeRateLimitReset(text string, now time.Time) (time.Time, bool) {
	if m := claudeResetEpochRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			if len(m[1]) == 13 {
				n /= 1000
			}
			t := time.Unix(n, 0)
			if t.After(now) && t.Before(now.Add(7*24*time.Hour)) {
				return t, true
			}
		}
	}
	if m := claudeResetClockRe.FindStringSubmatch(text); m != nil {
		hour, _ := strconv.Atoi(m[1])
		mins := 0
		if m[2] != "" {
			mins, _ = strconv.Atoi(m[2])
		}
		ampm := strings.ToLower(m[3])
		if ampm == "" && m[2] == "" {
			return time.Time{}, false // bare "resets 3": too ambiguous
		}
		if ampm == "pm" && hour < 12 {
			hour += 12
		}
		if ampm == "am" && hour == 12 {
			hour = 0
		}
		if hour > 23 || mins > 59 {
			return time.Time{}, false
		}
		local := now.Local()
		t := time.Date(local.Year(), local.Month(), local.Day(), hour, mins, 0, 0, time.Local)
		if !t.After(now) {
			t = t.Add(24 * time.Hour)
		}
		return t, true
	}
	return time.Time{}, false
}

func claudeBackoffDeadline(errText string, now time.Time, fallback time.Duration) time.Time {
	if reset, ok := parseClaudeRateLimitReset(errText, now); ok {
		deadline := reset.Add(claudeResetMargin)
		if maxDeadline := now.Add(24 * time.Hour); deadline.After(maxDeadline) {
			deadline = maxDeadline
		}
		return deadline
	}
	return now.Add(fallback)
}
```

- [x] **Step 4: Run tests, verify pass.**

### Task 2: Fleet-wide store + helper relocation

**Files:**
- Modify: `internal/engine/goap_claude_backoff.go` (add store; receive the five helpers moved from `actions_goap_fusion.go:768-840`)
- Modify: `internal/engine/actions_goap_fusion.go` (delete the moved block)
- Test: `internal/engine/goap_claude_backoff_test.go` (new tests + `isolateClaudeBackoffStore`)
- Modify: `internal/engine/actions_goap_fusion_test.go:138-271` (add `isolateClaudeBackoffStore(t)` to the five existing backoff tests; update stale "agent-scope is primary" comments)

**Interfaces:**
- Consumes: Task 1's helpers.
- Produces: `goapClaudeBackoffPath` (test seam), `isolateClaudeBackoffStore(t *testing.T)`; `saveClaudeBackoffState`/`loadClaudeBackoffState`/`claudeBackoffActive`/`clearClaudeBackoffState` keep their signatures.

- [x] **Step 1: Write the failing tests**

```go
func isolateClaudeBackoffStore(t *testing.T) {
	t.Helper()
	prev := goapClaudeBackoffPath
	goapClaudeBackoffPath = filepath.Join(t.TempDir(), "claude_backoff.json")
	t.Cleanup(func() { goapClaudeBackoffPath = prev })
}

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
	clearClaudeBackoffState(sibling)
	fresh := &Blackboard{BB: blackboard.NewHandle(blackboard.NewManager(nil), "run-3", "", "goap-fusion-loop-runner")}
	if _, ok := loadClaudeBackoffState(fresh); ok {
		t.Fatal("clear by one agent must reopen the window fleet-wide")
	}
}

func TestClaudeBackoffState_LegacyAgentScopeStampStillHonored(t *testing.T) {
	isolateClaudeBackoffStore(t)
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "goap-loop"}
	until := time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC)
	if err := bb.BB.Mgr.Set(scope, "goap_fusion_claude_backoff_until", until.Format(time.RFC3339), "legacy per-agent stamp", "text"); err != nil {
		t.Fatal(err)
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
```

Also add `isolateClaudeBackoffStore(t)` as the first line of the five existing tests in `actions_goap_fusion_test.go` (`TestClaudeBackoffState_PersistsAcrossRuns`, `_ChainStateFallback`, `TestClaudeBackoffActive_WindowExpiry`, `_ClearCounterpart`, `_MissingOrMalformedIsInactive`) — without it they arm/clear the operator's live stamp.

- [x] **Step 2: Run, verify the two new tests fail** (shared visibility does not exist yet).

- [x] **Step 3: Implement the store and rewire the helpers**

```go
type sharedClaudeBackoff struct {
	Until string `json:"until"`
	SetBy string `json:"set_by,omitempty"`
	SetAt string `json:"set_at,omitempty"`
}

var (
	goapClaudeBackoffPath = defaultClaudeBackoffPath()
	goapClaudeBackoffMu   sync.Mutex
)

func defaultClaudeBackoffPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/nico"
	}
	return filepath.Join(home, ".go-bt-evolve", "claude_backoff.json")
}

func writeSharedClaudeBackoff(until time.Time, setBy string) {
	goapClaudeBackoffMu.Lock()
	defer goapClaudeBackoffMu.Unlock()
	b, err := json.Marshal(sharedClaudeBackoff{
		Until: until.UTC().Format(time.RFC3339),
		SetBy: setBy,
		SetAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	path := goapClaudeBackoffPath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func readSharedClaudeBackoff() (time.Time, bool) {
	goapClaudeBackoffMu.Lock()
	defer goapClaudeBackoffMu.Unlock()
	b, err := os.ReadFile(goapClaudeBackoffPath)
	if err != nil {
		return time.Time{}, false
	}
	var s sharedClaudeBackoff
	if json.Unmarshal(b, &s) != nil {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(s.Until))
	if err != nil {
		return time.Time{}, false
	}
	return until, true
}

func removeSharedClaudeBackoff() {
	goapClaudeBackoffMu.Lock()
	defer goapClaudeBackoffMu.Unlock()
	_ = os.Remove(goapClaudeBackoffPath)
}
```

Rewired helpers (moved into the new file; ChainState fallback key unchanged):

```go
func saveClaudeBackoffState(bb *Blackboard, until time.Time) {
	stamp := until.UTC().Format(time.RFC3339)
	setGoapState(bb, "claude_backoff_until", stamp)
	setBy := ""
	if bb.BB != nil {
		setBy = bb.BB.AgentName
	}
	writeSharedClaudeBackoff(until, setBy)
}

func loadClaudeBackoffState(bb *Blackboard) (time.Time, bool) {
	best, ok := readSharedClaudeBackoff()
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_claude_backoff_until"); err == nil {
			if until, perr := time.Parse(time.RFC3339, strings.TrimSpace(e.Value)); perr == nil && (!ok || until.After(best)) {
				best, ok = until, true
			}
		}
	}
	if s, sok := bb.ChainState["goap_fusion_claude_backoff_until"].(string); sok {
		if until, perr := time.Parse(time.RFC3339, strings.TrimSpace(s)); perr == nil && (!ok || until.After(best)) {
			best, ok = until, true
		}
	}
	return best, ok
}
```

`claudeBackoffActive` unchanged; `clearClaudeBackoffState` additionally calls `removeSharedClaudeBackoff()`. `claudeBackoffWindow` moves verbatim.

- [x] **Step 4: Run the full backoff test set, verify pass** — `-run 'ClaudeBackoff'`.

### Task 3: Call sites use the parsed deadline

**Files:**
- Modify: `internal/engine/actions_superpowers_prod.go:934`
- Modify: `internal/engine/actions_goap_fusion_claude_review.go:318-325,371`
- Test: `internal/engine/actions_goap_fusion_claude_review_test.go` (reuse the file's existing fake runner/deps pattern)

**Interfaces:** consumes `claudeBackoffDeadline` from Task 1.

- [x] **Step 1: Failing test — review path honors the reset hint** (exact fake names per the existing rate-limit test in that file; fixed `deps.now`; runner errors with output `"5-hour limit reached ∙ resets 3am"`): assert `loadClaudeBackoffState` returns next-3am+2m in `time.Local`, not `now+1h`.

- [x] **Step 2: Run, verify it fails** (stamp is `now+1h`).

- [x] **Step 3: Implement** — replace the two save lines:

```go
// actions_superpowers_prod.go
saveClaudeBackoffState(bb, claudeBackoffDeadline(errStr, time.Now(), claudeBackoffWindow()))

// actions_goap_fusion_claude_review.go
saveClaudeBackoffState(bb, claudeBackoffDeadline(combined, now(), goapClaudeBackoffWindow))
```

Update the `goapClaudeBackoffWindow` doc comment: the `resets <time>` hint IS machine-parsed now; the hour is the hint-less fallback.

- [x] **Step 4: Run, verify pass.**

### Task 4: Gate and landing

- [x] Full engine tests: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -short -count=1 -timeout 900s` (Jetson may be running a claude cycle — retry transient 5s-timeout config flakes once).
- [x] Verify no live-state pollution: `~/.go-bt-evolve/claude_backoff.json` must NOT exist after the test run (unless armed by the real fleet meanwhile — check `set_at`).
- [x] Discard regenerated `graphify-out/` churn.
- [x] Single commit (docs + code + tests); pre-commit hook is the full gate.
- [x] Reconcile with master (expect autonomous commits landed meanwhile): `git fetch`/rebase — if rebase refuses in the worktree ("Could not detach HEAD"), `git reset --hard master` + `git cherry-pick` instead.
- [x] ff the bare repo's master; push needs fresh user authorization (permission classifier). The live daemon self-redeploys via the drift watcher (~20min cadence).
