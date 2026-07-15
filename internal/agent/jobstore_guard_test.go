package agent

import (
	"path/filepath"
	"testing"
)

// TestFileJobStore_RefusesEmptyOverwriteFromBlindProcess: on 2026-07-15 ~11:05
// the live scheduler-jobs.json was observed transiently overwritten with a
// literal [] by a sibling process whose Load had raced the daemon's atomic
// rename and seen nothing. A process that never saw the non-empty job table
// must not erase it: its Save([]) is a WARN-and-skip no-op.
func TestFileJobStore_RefusesEmptyOverwriteFromBlindProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduler-jobs.json")
	if err := NewFileJobStore(path).Save([]ScheduledJob{{ID: "job_a_1", AgentName: "a", Schedule: "0 6 * * *", RunCount: 3, Active: true}}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	// A separate store instance on the same path that never loaded the jobs —
	// the racing-sibling shape.
	blind := NewFileJobStore(path)
	if err := blind.Save([]ScheduledJob{}); err != nil {
		t.Fatalf("blind empty Save must not error (it is skipped, not fatal): %v", err)
	}

	jobs, err := NewFileJobStore(path).Load()
	if err != nil {
		t.Fatalf("Load after blind empty Save: %v", err)
	}
	if len(jobs) != 1 || jobs[0].RunCount != 3 {
		t.Fatalf("blind empty Save clobbered a non-empty job table: got %d jobs, want the 1 seeded job intact", len(jobs))
	}
}

// TestFileJobStore_AllowsLegitimateEmptyAfterOwnership: a process that loaded
// (or wrote) the non-empty table owns it and may legitimately empty it —
// RemoveJob of the last job and agent deletion must keep working.
func TestFileJobStore_AllowsLegitimateEmptyAfterOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduler-jobs.json")
	if err := NewFileJobStore(path).Save([]ScheduledJob{{ID: "job_a_1", AgentName: "a", Schedule: "0 6 * * *", Active: true}}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	owner := NewFileJobStore(path)
	if jobs, err := owner.Load(); err != nil || len(jobs) != 1 {
		t.Fatalf("owner Load: jobs=%d err=%v", len(jobs), err)
	}
	if err := owner.Save([]ScheduledJob{}); err != nil {
		t.Fatalf("owner empty Save: %v", err)
	}

	jobs, err := NewFileJobStore(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("an owning process's legitimate empty save was refused: got %d jobs, want 0", len(jobs))
	}
}

// TestFileJobStore_AllowsEmptySaveOnFreshStore: the guard must not break the
// legitimate first-boot case — an empty table saved where no meaningful state
// exists yet.
func TestFileJobStore_AllowsEmptySaveOnFreshStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduler-jobs.json")
	store := NewFileJobStore(path)

	if err := store.Save([]ScheduledJob{}); err != nil {
		t.Fatalf("fresh empty Save: %v", err)
	}
	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("fresh store should load empty, got %d jobs", len(jobs))
	}
}

// TestReadOnlyJobStore_LoadsButNeverSaves: MCP/CLI sibling bt-agent instances
// share the daemon's job file for visibility (bt_schedule_list) but must never
// write it — sibling saves are the attributed job-table wiper. The read-only
// wrapper delegates Load and silently drops Save.
func TestReadOnlyJobStore_LoadsButNeverSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduler-jobs.json")
	base := NewFileJobStore(path)
	if err := base.Save([]ScheduledJob{{ID: "job_a_1", AgentName: "a", Schedule: "0 6 * * *", RunCount: 9, Active: true}}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	ro := NewReadOnlyJobStore(base)

	jobs, err := ro.Load()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("read-only Load must delegate: jobs=%d err=%v, want 1/nil", len(jobs), err)
	}

	if err := ro.Save([]ScheduledJob{{ID: "job_b_1", AgentName: "b"}, {ID: "job_c_1", AgentName: "c"}}); err != nil {
		t.Fatalf("read-only Save must be a silent no-op, got err %v", err)
	}

	after, err := base.Load()
	if err != nil {
		t.Fatalf("base Load: %v", err)
	}
	if len(after) != 1 || after[0].AgentName != "a" || after[0].RunCount != 9 {
		t.Fatalf("read-only Save leaked through to the underlying file: got %d jobs (first agent %q)", len(after), after[0].AgentName)
	}
}
