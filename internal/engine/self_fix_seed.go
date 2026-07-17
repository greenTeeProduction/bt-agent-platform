package engine

// self_fix_seed.go — the shared primitive that seeds a code-fix PROGRAM into
// the goap-fusion loop's program store so the loop implements the fix via
// Claude Code TDD → verify → auto-apply (self-fixing fleet, spec
// docs/superpowers/specs/2026-07-17-self-fixing-fleet-design.md §1). Both
// producers — the reactive error-handler escalation (Part A) and the proactive
// self-review agent (Part B) — route through here, so the guards live in ONE
// place:
//
//   - a per-signature cooldown ledger (~/.go-bt-evolve/self_fix/ledger.json) so
//     a recurring defect is seeded ONCE per cooldown, not every failure;
//   - a cap on OPEN self-fix programs so the backlog stays bounded and code-fix
//     programs don't starve the single active-program slot indefinitely;
//   - the BT_SELF_FIX kill switch that halts all seeding instantly.
//
// The whole read-modify-write (ledger read → cap count → program-store add →
// ledger record) is serialized by an in-process mutex AND an on-disk lock file
// (fleet siblings share these files — see the job-table-wiper incident), so
// concurrent seeds can't lose an update. It never errors upward: it returns
// (seeded, reason) for the caller to log/observe.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

// selfFixLedgerEntry is one signature's dedup record: when it was last seeded,
// the program it produced, and how many times this defect has recurred.
type selfFixLedgerEntry struct {
	LastSeeded time.Time `json:"last_seeded"`
	Title      string    `json:"title"`
	ProgramID  string    `json:"program_id"`
	Count      int       `json:"count"`
}

// selfFixDirOverride redirects the ledger dir in tests (same var-override
// pattern as errorHandlerDirOverride).
var selfFixDirOverride string

// selfFixStoreMu guards the in-process read-modify-write cycle of the self-fix
// ledger AND the program-store seed it wraps, so two goroutines seeding at once
// can't clobber each other's Add. Cross-process cycles are guarded by the
// on-disk self_fix/store.lock; on lock contention the seed is skipped this run.
var selfFixStoreMu sync.Mutex

// selfFixEnabled reports whether seeding is on; BT_SELF_FIX=off is the fleet
// kill switch both producers respect.
func selfFixEnabled() bool {
	return !strings.EqualFold(os.Getenv("BT_SELF_FIX"), "off")
}

// selfFixCooldown is the per-signature dedup window (env BT_SELF_FIX_COOLDOWN,
// a Go duration; default 24h) — a recurring defect seeds at most once per window.
func selfFixCooldown() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("BT_SELF_FIX_COOLDOWN")); err == nil && d > 0 {
		return d
	}
	return 24 * time.Hour
}

// selfFixMaxOpen caps concurrently-open self-fix programs (env
// BT_SELF_FIX_MAX_OPEN, an int; default 3) so the backlog stays bounded.
func selfFixMaxOpen() int {
	if n, err := strconv.Atoi(os.Getenv("BT_SELF_FIX_MAX_OPEN")); err == nil && n > 0 {
		return n
	}
	return 3
}

// selfFixDir is the ledger directory (~/.go-bt-evolve/self_fix), overridable in
// tests. Empty only when the home dir is unresolvable.
func selfFixDir() string {
	if selfFixDirOverride != "" {
		return selfFixDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-bt-evolve", "self_fix")
}

func selfFixLedgerPath() string { return filepath.Join(selfFixDir(), "ledger.json") }

// selfFixStoreLockStale bounds a crashed store-lock holder: a seed is
// milliseconds, so an older lock belongs to a dead process and is swept.
const selfFixStoreLockStale = 10 * time.Second

// acquireSelfFixStoreLock takes an O_CREATE|O_EXCL advisory lock in the
// self_fix dir, sweeping a stale lock from a crashed holder. On contention the
// caller skips this run (no waiting, no retries). This mirrors
// acquireErrorHandlerFileLock, scoped to selfFixDir() instead of
// errorHandlerDir() (that helper hard-codes the error-handler dir).
func acquireSelfFixStoreLock() (func(), bool) {
	dir := selfFixDir()
	if dir == "" {
		return nil, false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false
	}
	lockPath := filepath.Join(dir, "store.lock")
	if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > selfFixStoreLockStale {
		_ = os.Remove(lockPath)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	_ = f.Close()
	return func() { _ = os.Remove(lockPath) }, true
}

// seedCodeFixProgram appends a self-fix code-fix program to the goap program
// store so the goap-fusion loop picks it up and implements it. Guarded by a
// per-signature cooldown ledger (a recurring defect seeds once, not every
// failure), a cap on open self-fix programs (bounded backlog / don't starve the
// single active-program slot), and the BT_SELF_FIX kill switch. source MUST be
// the caller's tagged source (e.g. "self-fix:error-handler:<sig>") so seeded
// programs stay distinguishable from research/arc42 work in the store and logs.
// Never errors upward; returns (seeded, reason) for the caller to log.
func seedCodeFixProgram(sig, title, milestoneGoal, source string) (bool, string) {
	// 1. Kill switch.
	if !selfFixEnabled() {
		return false, "self-fix disabled"
	}

	// 2. Input guard.
	sig = strings.TrimSpace(sig)
	title = strings.TrimSpace(title)
	milestoneGoal = strings.TrimSpace(milestoneGoal)
	source = strings.TrimSpace(source)
	if sig == "" || title == "" || milestoneGoal == "" {
		return false, "incomplete seed request"
	}
	if source == "" {
		// Preserve the "self-fix:" prefix the cap counter keys on even if a
		// caller forgets to tag the source.
		source = "self-fix:" + sig
	}

	// Serialize the whole ledger+store read-modify-write (in-process mutex +
	// cross-process lock file) so concurrent seeds can't lose an update.
	selfFixStoreMu.Lock()
	defer selfFixStoreMu.Unlock()
	release, ok := acquireSelfFixStoreLock()
	if !ok {
		return false, "self-fix store busy"
	}
	defer release()

	// 3. Dedup / cooldown.
	ledger := map[string]selfFixLedgerEntry{}
	if err := readErrorHandlerJSONStrict(selfFixLedgerPath(), &ledger); err != nil {
		// An existing-but-unreadable ledger must not be overwritten (that would
		// erase every cooldown); skip rather than seed blind.
		return false, "self-fix ledger unreadable: " + err.Error()
	}
	if entry, exists := ledger[sig]; exists && time.Since(entry.LastSeeded) < selfFixCooldown() {
		return false, "within cooldown"
	}

	// 4. Cap: count OPEN (has a pending milestone) self-fix programs.
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		return false, "program store unreadable: " + err.Error()
	}
	open := 0
	for _, p := range ps.Programs {
		if !strings.HasPrefix(p.Source, "self-fix:") {
			continue
		}
		if _, m := p.NextMilestone(); m != nil {
			open++
		}
	}
	if open >= selfFixMaxOpen() {
		return false, "self-fix backlog cap reached"
	}

	// 5. Seed (Add dedupes by title-key; Save is atomic tmp+rename per ADR-003).
	p := ps.Add(title, source, []string{milestoneGoal})
	if err := ps.Save(); err != nil {
		return false, "program store write failed: " + err.Error()
	}

	// 6. Record the ledger (atomic write, same helper as the error-handler store).
	entry := ledger[sig]
	entry.LastSeeded = time.Now()
	entry.Title = title
	entry.ProgramID = p.ID
	entry.Count++
	ledger[sig] = entry
	if err := writeErrorHandlerJSON(selfFixLedgerPath(), ledger); err != nil {
		return false, "self-fix ledger write failed: " + err.Error()
	}

	// 7. Log.
	Info("self-fix: seeded code-fix program", "sig", sig, "title", title, "source", source, "program", p.ID)

	// 8. Done.
	return true, "seeded program " + p.ID
}
