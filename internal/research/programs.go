package research

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// Programs are research-proposed multi-cycle changes: work too large for one
// scheduled run, split into file-scoped milestones that successive cycles
// execute one at a time. Persisted per ADR-003 (atomic JSON under
// ~/.go-bt-evolve).

type Milestone struct {
	Goal         string    `json:"goal"`
	Status       string    `json:"status"` // pending | done | blocked
	Attempts     int       `json:"attempts,omitempty"`
	CompletedRun string    `json:"completed_run,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	BlockedAt    time.Time `json:"blocked_at,omitempty"`
	// RedPassStreak counts consecutive cycles whose RED command unexpectedly
	// passed for this milestone — evidence the work already exists at HEAD
	// rather than an unbuildable goal. Reset on any genuine failure.
	RedPassStreak int `json:"red_pass_streak,omitempty"`
}

type Program struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Source     string      `json:"source"`
	Created    time.Time   `json:"created"`
	Updated    time.Time   `json:"updated"`
	Milestones []Milestone `json:"milestones"`
}

// NextMilestone returns the first pending milestone and its index, or (-1, nil).
// Blocked milestones (abandoned after too many attempts) are skipped, so a
// program with one unbuildable milestone advances past it instead of freezing.
func (p *Program) NextMilestone() (int, *Milestone) {
	for i := range p.Milestones {
		if p.Milestones[i].Status == "pending" {
			return i, &p.Milestones[i]
		}
	}
	return -1, nil
}

// RecordAttemptAndMaybeBlock increments the attempt count of the milestone at
// idx and, when it reaches maxAttempts without landing, marks it "blocked" so
// NextMilestone skips it — the loop moves on to the next milestone (or, if none
// remain pending, the program is no longer Active and the self-seeder proposes
// a fresh one). It reports whether the milestone is now blocked. This is how a
// fabricated/unbuildable milestone (which the implementation agent correctly
// declines every cycle) stops freezing the program (2026-07-05: a "TDAD
// decorator node" research-echo milestone was declined 10 times).
func (ps *ProgramStore) RecordAttemptAndMaybeBlock(programID string, idx, maxAttempts int) bool {
	for _, p := range ps.Programs {
		if p.ID != programID || idx < 0 || idx >= len(p.Milestones) {
			continue
		}
		m := &p.Milestones[idx]
		if m.Status != "pending" {
			return m.Status == "blocked"
		}
		m.Attempts++
		if m.Attempts >= maxAttempts {
			m.Status = "blocked"
			m.BlockedAt = time.Now().UTC()
			p.Updated = time.Now().UTC()
			return true
		}
		p.Updated = time.Now().UTC()
		return false
	}
	return false
}

// RefundAttempt un-records one attempt charged against the milestone at idx —
// the complement of RecordAttemptAndMaybeBlock for cycles that died for
// *infrastructure* reasons (Claude rate limit, commit gate wedged by an
// external landing, apply/sync refusal) rather than an implementation failure.
// Only genuine agent declines may consume the milestone-abandon budget; without
// the refund, three cycles of external outage wrongly block a milestone
// (2026-07-09 doc-drift wedge, 2026-07-08 rate-limit window).
//
// The refund decrements Attempts (never below zero — a refund without a
// remaining charge is a no-op) and, when the milestone is blocked but the
// refunded charge is what pushed it to maxAttempts, restores it to pending and
// clears BlockedAt. A block accrued from more genuine attempts in earlier
// cycles stays blocked. Done milestones are immutable. Reports whether
// anything changed.
func (ps *ProgramStore) RefundAttempt(programID string, idx, maxAttempts int) bool {
	for _, p := range ps.Programs {
		if p.ID != programID {
			continue
		}
		if idx < 0 || idx >= len(p.Milestones) {
			return false
		}
		m := &p.Milestones[idx]
		if m.Status == "done" || m.Attempts <= 0 {
			return false
		}
		m.Attempts--
		if m.Status == "blocked" && m.Attempts < maxAttempts {
			m.Status = "pending"
			m.BlockedAt = time.Time{}
		}
		p.Updated = time.Now().UTC()
		return true
	}
	return false
}

type ProgramStore struct {
	path     string
	Programs []*Program `json:"programs"`
}

// DefaultProgramsPath is the ADR-003 location of the program backlog.
func DefaultProgramsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/nico"
	}
	return filepath.Join(home, ".go-bt-evolve", "research", "programs.json")
}

// OpenPrograms loads the store; a missing file yields an empty store.
func OpenPrograms(path string) (*ProgramStore, error) {
	ps := &ProgramStore{path: path}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ps, nil
	}
	if err != nil {
		return nil, fmt.Errorf("program store %s: %w", path, err)
	}
	if err := json.Unmarshal(b, ps); err != nil {
		return nil, fmt.Errorf("program store %s is corrupt: %w", path, err)
	}
	return ps, nil
}

// UpdatePrograms performs one read-modify-write cycle against the store at
// path under an exclusive cross-process flock: open, run fn against the
// loaded store, then save — all while holding the lock. Plain OpenPrograms +
// Save (as most callers still do) never merges against what is currently on
// disk, so two concurrent read-modify-write callers can race: the second
// Save silently clobbers the first's already-persisted change with a stale
// in-memory copy. Holding one lock across the whole open→fn→save sequence
// closes that gap by serializing writers, the same idiom
// reliability.DeadLetterQueue.save uses for its own sidecar file.
func UpdatePrograms(path string, fn func(*ProgramStore) error) error {
	release, err := reliability.AcquireFileLock(path)
	if err != nil {
		return fmt.Errorf("update programs %s: %w", path, err)
	}
	defer release()

	ps, err := OpenPrograms(path)
	if err != nil {
		return err
	}
	if err := fn(ps); err != nil {
		return err
	}
	return ps.Save()
}

// SelfFixSourcePrefix tags programs seeded to repair the platform's own
// defects (error-handler escalations, self-review findings, operator review
// seeds). It is the canonical spelling — the engine's seeding cap and source
// normalization key on it too — and Active() gives programs carrying it
// absolute scheduling priority.
const SelfFixSourcePrefix = "self-fix:"

// Active returns the program the next cycle should work: self-fix programs
// FIRST (fixes-first — the platform repairs itself before building more;
// before 2026-07-23 self-fix seeds competed in plain array order and could
// starve behind a continuously refilling feature backlog, ADR-196), then the
// oldest general program that still has a pending milestone. Within each
// class, array order (seed order) decides.
func (ps *ProgramStore) Active() *Program {
	for _, p := range ps.Programs {
		if !strings.HasPrefix(p.Source, SelfFixSourcePrefix) {
			continue
		}
		if _, m := p.NextMilestone(); m != nil {
			return p
		}
	}
	for _, p := range ps.Programs {
		if _, m := p.NextMilestone(); m != nil {
			return p
		}
	}
	return nil
}

// Add registers a new program unless one with the same title-key already
// exists (research may re-propose the same program across cycles).
func (ps *ProgramStore) Add(title, source string, milestones []string) *Program {
	key := Key(title)
	for _, p := range ps.Programs {
		if Key(p.Title) == key {
			return p
		}
	}
	now := time.Now().UTC()
	p := &Program{
		ID:      key,
		Title:   title,
		Source:  source,
		Created: now,
		Updated: now,
	}
	for _, m := range milestones {
		p.Milestones = append(p.Milestones, Milestone{Goal: m, Status: "pending"})
	}
	ps.Programs = append(ps.Programs, p)
	return p
}

// MarkDone completes one milestone; reports whether anything changed.
func (ps *ProgramStore) MarkDone(programID string, milestoneIdx int, runID string) bool {
	for _, p := range ps.Programs {
		if p.ID != programID {
			continue
		}
		if milestoneIdx < 0 || milestoneIdx >= len(p.Milestones) {
			return false
		}
		m := &p.Milestones[milestoneIdx]
		if m.Status == "done" {
			return false
		}
		m.Status = "done"
		m.CompletedRun = runID
		m.CompletedAt = time.Now().UTC()
		p.Updated = time.Now().UTC()
		return true
	}
	return false
}

// Save writes the store atomically (tmp+rename) per ADR-003. The tmp file
// uses a RANDOMIZED name (os.CreateTemp) rather than a fixed ps.path+".tmp" —
// this branch adds a second scheduled writer (the self-review agent) to the
// SAME programs.json alongside the existing arc42/goap seeders, and a fixed
// tmp name lets two concurrent writers interleave: whichever writer's
// os.Rename runs first moves the SHARED tmp file away, so the other writer's
// own os.Rename of that now-nonexistent path fails outright (losing that
// writer's update) or, with different timing, one writer's WriteFile can
// truncate/overwrite the other's in-flight bytes before either renames.
// Creating the tmp file in the SAME directory as ps.path keeps the rename
// atomic on one filesystem while giving each concurrent Save its own,
// unrelated tmp file.
func (ps *ProgramStore) Save() error {
	dir := filepath.Dir(ps.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(ps.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// os.CreateTemp creates the file 0600; restore the documented 0644 perms
	// before the rename makes it visible as ps.path.
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, ps.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// RecordRedPass increments the milestone's consecutive red-pass counter and
// returns the new streak. A red-pass (the plan's RED command passing before
// GREEN) is evidence the milestone's work already exists at HEAD.
func (ps *ProgramStore) RecordRedPass(programID string, milestoneIdx int) int {
	for _, p := range ps.Programs {
		if p.ID != programID {
			continue
		}
		if milestoneIdx < 0 || milestoneIdx >= len(p.Milestones) {
			return 0
		}
		m := &p.Milestones[milestoneIdx]
		if m.Status == "done" {
			return m.RedPassStreak
		}
		m.RedPassStreak++
		p.Updated = time.Now().UTC()
		return m.RedPassStreak
	}
	return 0
}

// ResetRedPassStreak clears the milestone's red-pass streak — called when a
// genuine implementation failure proves the milestone's tests can still fail.
func (ps *ProgramStore) ResetRedPassStreak(programID string, milestoneIdx int) {
	for _, p := range ps.Programs {
		if p.ID != programID {
			continue
		}
		if milestoneIdx < 0 || milestoneIdx >= len(p.Milestones) {
			return
		}
		m := &p.Milestones[milestoneIdx]
		if m.RedPassStreak == 0 {
			return
		}
		m.RedPassStreak = 0
		p.Updated = time.Now().UTC()
		return
	}
}
