package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// Claude rate-limit backoff state must survive across scheduled runs AND
// across agents: a rate-limited outcome in one cron tick should stop the next
// ticks — of every agent — from burning 15-minute Claude retry budgets
// against a quota that is known to be closed. The deadline lives in a single
// fleet-wide ADR-003 file (primary), with the run-local ChainState as the
// fallback when the file is unwritable. Per-agent stamps written by the
// previous implementation are still honored until they expire, then cleared.
//
// Why fleet-wide: on 2026-07-14/15 goap-fusion-runner's private stamp expired
// at 23:47 and its probe found Claude healthy, while goap-fusion-loop-runner
// kept fast-failing ~3h more on its own later-armed stamp. One agent's probe
// result is knowledge the whole fleet should share.

// sharedClaudeBackoff is the on-disk shape of the fleet-wide stamp.
// set_by/set_at exist for operator forensics only.
type sharedClaudeBackoff struct {
	Until string `json:"until"`
	SetBy string `json:"set_by,omitempty"`
	SetAt string `json:"set_at,omitempty"`
}

var (
	// goapClaudeBackoffPath is a test seam: tests MUST redirect it (see
	// isolateClaudeBackoffStore) — the default is the operator's LIVE
	// fleet-wide stamp.
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

// saveClaudeBackoffState records the fleet-wide backoff deadline (plus the
// run-local ChainState fallback). It intentionally no longer writes the
// per-agent blackboard key — that is what let sibling agents oversleep on
// private stamps.
func saveClaudeBackoffState(bb *Blackboard, until time.Time) {
	stamp := until.UTC().Format(time.RFC3339)
	setGoapState(bb, "claude_backoff_until", stamp)
	setBy := ""
	if bb.BB != nil {
		setBy = bb.BB.AgentName
	}
	writeSharedClaudeBackoff(until, setBy)
}

// loadClaudeBackoffState returns the latest valid deadline among the shared
// file, the legacy per-agent entry, and the ChainState fallback. Missing or
// malformed state reads as inactive — corrupt state must never wedge the
// loop into skipping Claude forever.
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

// claudeBackoffActive reports whether now is still inside the persisted
// backoff window. An elapsed window self-clears (half-open, mirroring the
// runaway-backstop lesson) so stale state cannot permanently block attempts.
func claudeBackoffActive(bb *Blackboard, now time.Time) bool {
	until, ok := loadClaudeBackoffState(bb)
	if !ok {
		return false
	}
	if now.Before(until) {
		return true
	}
	clearClaudeBackoffState(bb)
	return false
}

// clearClaudeBackoffState wipes the shared file, the legacy per-agent entry,
// and the ChainState fallback — a probe result reopens Claude fleet-wide.
func clearClaudeBackoffState(bb *Blackboard) {
	if bb.ChainState != nil {
		delete(bb.ChainState, "goap_fusion_claude_backoff_until")
	}
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Delete(scope, "goap_fusion_claude_backoff_until")
	}
	removeSharedClaudeBackoff()
}

// claudeBackoffWindow is the FALLBACK duration a rate-limited outcome closes
// Claude attempts for when the CLI output carries no parseable reset time:
// BT_GOAP_CLAUDE_BACKOFF when parsable, 6h otherwise. A reset hint in the
// output takes precedence — see claudeBackoffDeadline.
func claudeBackoffWindow() time.Duration {
	if d, err := time.ParseDuration(getenvDefault("BT_GOAP_CLAUDE_BACKOFF", "6h")); err == nil && d > 0 {
		return d
	}
	return 6 * time.Hour
}

// claudeResetMargin pads the CLI-reported reset so a probe never races the
// exact window boundary.
const claudeResetMargin = 2 * time.Minute

var (
	claudeResetEpochRe = regexp.MustCompile(`(?i)limit[^|\n]{0,60}\|\s*(\d{10,13})`)
	claudeResetClockRe = regexp.MustCompile(`(?i)\bresets\b(?:\s+at)?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)

	// claudeResetWeeklyRe matches the weekly-quota shape, which — unlike the
	// plain wall-clock form — carries an explicit IANA zone (and optionally a
	// month/day), so the reset can be resolved without assuming time.Local
	// and can legitimately land more than 24h out.
	//
	//	"You've hit your weekly limit · resets Jul 7, 11pm (Europe/Berlin)"
	//	"resets 11pm (Europe/Berlin)"
	claudeResetWeeklyRe = regexp.MustCompile(`(?i)\bresets\b(?:\s+at)?\s+(?:([A-Za-z]{3})\s+(\d{1,2}),?\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s*\(([^)]+)\)`)
)

// parseClaudeRateLimitReset extracts the quota-reset instant from Claude CLI
// rate-limit output. Three observed shapes:
//
//	"Claude AI usage limit reached|resets 3pm"                    → wall-clock (CLI 2.1.x here)
//	"…limit reached|1752537600"                                    → unix epoch (s or ms)
//	"weekly limit · resets Jul 7, 11pm (Europe/Berlin)"             → weekly quota, explicit zone
//
// The plain wall-clock form carries no date or zone — the CLI prints the
// user's local time, so the next occurrence after now in time.Local is used.
// A bare hour with neither am/pm nor minutes is too ambiguous and is
// rejected, as is an epoch in the past or further than 7 days out. The
// weekly-quota form carries its own IANA zone (and optionally a month/day),
// so it is resolved in that zone rather than time.Local and — being the
// weekly quota, not the rolling window — is allowed to land more than 24h
// out (see claudeBackoffDeadline).
func parseClaudeRateLimitReset(text string, now time.Time) (time.Time, bool) {
	if t, ok := parseClaudeWeeklyRateLimitReset(text, now); ok {
		return t, true
	}
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
			return time.Time{}, false
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

// parseClaudeWeeklyRateLimitReset resolves the weekly-quota shape (see
// claudeResetWeeklyRe) in its own explicit IANA zone rather than time.Local.
// When no month/day is present, the reset falls on "now" as observed in that
// zone; when a month/day is present, it is taken in now's year in that zone.
func parseClaudeWeeklyRateLimitReset(text string, now time.Time) (time.Time, bool) {
	m := claudeResetWeeklyRe.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(m[6])
	if err != nil {
		return time.Time{}, false
	}
	hour, err := strconv.Atoi(m[3])
	if err != nil || hour > 12 {
		return time.Time{}, false
	}
	mins := 0
	if m[4] != "" {
		mins, _ = strconv.Atoi(m[4])
	}
	if mins > 59 {
		return time.Time{}, false
	}
	switch strings.ToLower(m[5]) {
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "am":
		if hour == 12 {
			hour = 0
		}
	}

	nowInLoc := now.In(loc)
	year, month, day := nowInLoc.Date()
	if m[1] != "" && m[2] != "" {
		parsedMonth, merr := time.Parse("Jan", m[1])
		if merr != nil {
			return time.Time{}, false
		}
		d, derr := strconv.Atoi(m[2])
		if derr != nil {
			return time.Time{}, false
		}
		month, day = parsedMonth.Month(), d
	}
	return time.Date(year, month, day, hour, mins, 0, 0, loc), true
}

// claudeBackoffDeadline converts rate-limited CLI output into the backoff
// deadline: the CLI-reported reset plus claudeResetMargin when parseable,
// otherwise now+fallback (the fixed-window behavior this replaces). The
// reset is capped at now+24h so a mis-parse cannot idle the fleet for days —
// except for the weekly-quota shape (claudeResetWeeklyRe), whose explicit
// date/zone is trusted verbatim since a multi-day gap until the weekly quota
// reopens is the expected, not mis-parsed, case. The fixed 6h window slept
// ~3h past the real reset on 2026-07-14/15; the parsed deadline wakes the
// fleet when the quota actually reopens.
func claudeBackoffDeadline(errText string, now time.Time, fallback time.Duration) time.Time {
	if reset, ok := parseClaudeRateLimitReset(errText, now); ok {
		deadline := reset.Add(claudeResetMargin)
		if !claudeResetWeeklyRe.MatchString(errText) {
			if maxDeadline := now.Add(24 * time.Hour); deadline.After(maxDeadline) {
				deadline = maxDeadline
			}
		}
		return deadline
	}
	return now.Add(fallback)
}
