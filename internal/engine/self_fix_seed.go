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
// concurrent SELF-FIX seeds can't lose an update against EACH OTHER. That
// on-disk lock is scoped to ledger.json and to self-fix-vs-self-fix
// serialization only; the program-store add+save itself goes through
// research.UpdatePrograms(goapProgramsPath, ...), the SAME shared
// cross-process flock every other programs.json writer in the engine holds
// (persistGoapProgram, RefundAttempt, RecordRedPass, MarkDone, arc42_seeder,
// goap_seed_program) — so this write is now serialized against every other
// concurrent writer, not just other self-fix seeds, closing the engine-wide
// programs.json lost-update gap for this, the last write call site (the
// remaining research.OpenPrograms references in graphify_components.go and
// nlm_quota.go are read-only lookups with nothing to persist, so they were
// never part of the gap).
// It never errors upward: it returns (seeded, reason) for the caller to
// log/observe.

import (
	"errors"
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

// selfFixWriteLedger persists the ledger; writeErrorHandlerJSON in production.
// Tests override it to deterministically simulate a ledger-write failure
// (e.g. a full disk) to pin the ledger-before-store fail-safe ordering below —
// an OS-level directory permission can't isolate that failure, because
// store.lock (the on-disk cross-process lock acquired above) shares
// selfFixDir() with ledger.json, so a read-only ledger dir blocks the lock's
// own O_CREATE before the ledger write is ever reached.
var selfFixWriteLedger = writeErrorHandlerJSON

// selfFixStoreMu guards the in-process read-modify-write cycle of the self-fix
// ledger, so two goroutines seeding at once can't clobber each other's ledger
// entry. Cross-process cycles are guarded by the on-disk self_fix/store.lock;
// on lock contention the seed is skipped this run. Neither lock is what makes
// the program-store write itself safe against other engine writers — that's
// research.UpdatePrograms' own shared flock on programs.json (see the file
// doc comment above).
var selfFixStoreMu sync.Mutex

// errSelfFixAbort is the sentinel seedCodeFixProgram's research.UpdatePrograms
// callback returns to abort a write (cap reached / title collision / ledger
// write failure) without persisting anything — fn returning a non-nil error
// stops UpdatePrograms before it calls Save. The actual cause is carried out
// via the closure-captured reason string, not this error's text.
var errSelfFixAbort = errors.New("self-fix seed aborted")

// selfFixEnabled reports whether seeding is on; BT_SELF_FIX=off is the fleet
// kill switch both producers respect.
func selfFixEnabled() bool {
	return !strings.EqualFold(os.Getenv("BT_SELF_FIX"), "off")
}

// selfFixCooldown is the per-signature dedup window (env BT_SELF_FIX_COOLDOWN,
// a Go duration; default 24h) — a recurring defect seeds at most once per window.
// Note the asymmetry with selfFixMaxOpen: here a parseable-but-non-positive
// value (0s or negative) is treated as UNPARSEABLE and falls back to the 24h
// default, deliberately — a near-zero cooldown is the DANGEROUS direction
// (unbounded re-seeding), so refusing to honor it is the safe choice.
func selfFixCooldown() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("BT_SELF_FIX_COOLDOWN")); err == nil && d > 0 {
		return d
	}
	return 24 * time.Hour
}

// selfFixMaxOpen caps concurrently-open self-fix programs (env
// BT_SELF_FIX_MAX_OPEN, an int; default 3) so the backlog stays bounded. A
// parseable value <= 0 is honored as an explicit pause dial (0: block ALL new
// seeds) rather than falling back to the default — the safe/stricter
// direction, unlike selfFixCooldown's asymmetric handling of 0/negative
// (see its comment). Only an unparseable or absent value falls back to 3.
func selfFixMaxOpen() int {
	n, err := strconv.Atoi(os.Getenv("BT_SELF_FIX_MAX_OPEN"))
	if err != nil {
		return 3
	}
	if n <= 0 {
		return 0
	}
	return n
}

// selfFixGuardFileMarkers names the SEEDING-AUTONOMY guard files — the ones
// that decide whether/how much the loop is allowed to self-fix at all
// (selfFixEnabled/selfFixCooldown/selfFixMaxOpen live in self_fix_seed.go
// itself; validateCodeFix in error_handler_claude.go and
// validateSelfReviewFinding in actions_self_review.go gate what either
// producer may propose; error_handler_node.go/error_handler_store.go hold the
// escalation graft path and its ledger). mentionsSelfFixGuardFile denies BOTH
// producers from proposing a fix that edits any of these — the sharpest
// vector in the whole feature is the loop seeding a fix that quietly weakens
// its own guard (e.g. selfFixMaxOpen's `return 3` becoming `return 10`).
// Deliberately does NOT include the superpowers/goap fusion pipeline files
// (actions_superpowers*, actions_goap_fusion*) — the fleet does legitimate
// autonomous work there; only the guard files above are off-limits to
// self-seeded fixes.
var selfFixGuardFileMarkers = []string{
	"self_fix_seed",
	"error_handler_claude",
	"error_handler_node",
	"error_handler_store",
	"actions_self_review",
}

// mentionsSelfFixGuardFile reports whether any of texts names one of the
// self-fix guard files above (substring match, so it catches both the .go
// source and its _test.go, and is deliberately robust to "./", "//", and
// absolute-path tricks — no path normalization is attempted). Used by
// validateCodeFix and validateSelfReviewFinding to deny an autonomous fix
// that targets its own guards — those changes require a human. Callers pass
// BOTH the structured Files entries and the free-text Milestone: the
// Milestone is the actual instruction the downstream TDD implementer
// executes with unrestricted Read/Write/Edit tools, so an innocuous Files
// list paired with a Milestone that names a guard file must be caught too
// (a files-only scan is bypassable by never listing the guard file, only
// instructing the implementer to touch it).
func mentionsSelfFixGuardFile(texts ...string) bool {
	for _, t := range texts {
		for _, marker := range selfFixGuardFileMarkers {
			if strings.Contains(t, marker) {
				return true
			}
		}
	}
	return false
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
		source = research.SelfFixSourcePrefix + sig
	} else if !strings.HasPrefix(source, research.SelfFixSourcePrefix) {
		// A non-empty but MIS-tagged source (missing the prefix) would
		// otherwise escape the cap count entirely, since the cap keys on the
		// "self-fix:" prefix (step below). Normalize rather than trust callers.
		source = research.SelfFixSourcePrefix + source
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

	// 4-7. Cap check, seed, ledger stamp, and program-store persistence all run
	// inside ONE research.UpdatePrograms cycle, so the open-count read, the
	// Add, and the eventual Save are atomic under its shared programs.json
	// flock — the same idiom persistGoapProgram/RefundAttempt/RecordRedPass use
	// (see the file doc comment). fn returning a non-nil error aborts BEFORE
	// UpdatePrograms calls Save, so a cap/collision/ledger-write abort leaves
	// the on-disk store untouched, matching the previous bare-OpenPrograms
	// fail-safe semantics exactly.
	var programID, reason string
	err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
		// Cap: count OPEN (has a pending milestone) self-fix programs.
		open := 0
		for _, p := range ps.Programs {
			if !strings.HasPrefix(p.Source, research.SelfFixSourcePrefix) {
				continue
			}
			if _, m := p.NextMilestone(); m != nil {
				open++
			}
		}
		if open >= selfFixMaxOpen() {
			reason = "self-fix backlog cap reached"
			return errSelfFixAbort
		}

		// Seed in-memory only (Add dedupes by title-key: on a title collision
		// it returns the PRE-EXISTING program unchanged and mutates nothing, so
		// ps.Programs' length is unchanged).
		preCount := len(ps.Programs)
		p := ps.Add(title, source, []string{milestoneGoal})
		if len(ps.Programs) == preCount {
			// Add returned a pre-existing program (title-key collision), not a
			// new one. The caller's defect was never queued — record NOTHING
			// (no ledger entry, no Save) rather than reporting success for work
			// that silently didn't happen.
			reason = "title collides with existing program " + p.ID
			return errSelfFixAbort
		}
		programID = p.ID

		// Record the ledger entry in memory, then persist it BEFORE returning
		// nil (which is what lets UpdatePrograms persist the program store) —
		// deliberately. Cooldown recording must never be weaker than program
		// persistence: if a durable program existed without a durable cooldown
		// stamp, the very next identical failure would re-seed it (the exact
		// runaway the ledger exists to prevent). Ledger-first means any
		// failure here aborts with NOTHING persisted (fn's error stops
		// UpdatePrograms before its Save) — a clean, correctly-retryable
		// fail-safe.
		entry := ledger[sig]
		entry.LastSeeded = time.Now()
		entry.Title = title
		entry.ProgramID = p.ID
		entry.Count++
		ledger[sig] = entry
		if err := selfFixWriteLedger(selfFixLedgerPath(), ledger); err != nil {
			reason = "self-fix ledger write failed: " + err.Error()
			return errSelfFixAbort
		}
		return nil
	})
	if err != nil {
		if reason != "" {
			return false, reason
		}
		// The ledger stamp (if fn reached it) is ALREADY durable at this
		// point, so a failure here — lock acquisition, an unreadable store, or
		// the final Save — errs toward under-seeding (the fix is deferred up
		// to one cooldown window) rather than over-seeding: safe, but worth
		// logging loudly since it means a queued fix silently didn't land.
		Error("self-fix: program store update failed after ledger stamp; fix deferred to next cooldown", "sig", sig, "title", title, "program", programID, "err", err)
		return false, "program store update failed: " + err.Error()
	}

	// 8. Log.
	Info("self-fix: seeded code-fix program", "sig", sig, "title", title, "source", source, "program", programID)

	// 9. Done.
	return true, "seeded program " + programID
}
