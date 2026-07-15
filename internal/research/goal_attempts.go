package research

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Research-goal failure budgets: program milestones already block after
// repeated failed attempts (RecordAttemptAndMaybeBlock), but notebooklm-lane
// P0 goals had no budget at all — on 2026-07-10 one goal burned 11 blind
// implementation attempts on the same lint failure. This store persists per-
// goal attempts plus the last failure tail so (a) the queue can abandon a goal
// the agent cannot land and (b) the next attempt is steered by what actually
// failed instead of retrying blind. Keys are normalized goal digests
// (engine-side goapResearchGoalKey). Persisted per ADR-003.

// goalFailureTailLimit bounds the stored failure output. The TAIL is kept —
// the actionable lint/test lines of a commit-gate transcript come last.
const goalFailureTailLimit = 1200

// GoalAttempt tracks the implementation-failure budget of one research goal.
type GoalAttempt struct {
	Attempts    int       `json:"attempts"`
	LastFailure string    `json:"last_failure,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	// RedPassStreak counts consecutive cycles whose RED command unexpectedly
	// passed for this goal — evidence the work already exists at HEAD rather
	// than an unlandable goal. RecordFailure resets it: a genuine failure
	// proves the goal's tests can still fail.
	RedPassStreak int `json:"red_pass_streak,omitempty"`
}

// GoalAttemptStore persists per-goal failure budgets.
type GoalAttemptStore struct {
	path     string
	Attempts map[string]*GoalAttempt `json:"attempts"`
}

// DefaultGoalAttemptsPath is the ADR-003 location of the goal-attempt budgets.
func DefaultGoalAttemptsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/nico"
	}
	return filepath.Join(home, ".go-bt-evolve", "research", "goal-attempts.json")
}

// OpenGoalAttempts loads the store; a missing file yields an empty store.
func OpenGoalAttempts(path string) (*GoalAttemptStore, error) {
	s := &GoalAttemptStore{path: path, Attempts: map[string]*GoalAttempt{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("goal attempt store %s: %w", path, err)
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("goal attempt store %s is corrupt: %w", path, err)
	}
	if s.Attempts == nil {
		s.Attempts = map[string]*GoalAttempt{}
	}
	return s, nil
}

// RecordFailure charges one failed implementation attempt against key, keeping
// the bounded tail of the failure output, and returns the new attempt count.
func (s *GoalAttemptStore) RecordFailure(key, failureTail string) int {
	a, ok := s.Attempts[key]
	if !ok {
		a = &GoalAttempt{}
		s.Attempts[key] = a
	}
	a.Attempts++
	a.RedPassStreak = 0
	if len(failureTail) > goalFailureTailLimit {
		failureTail = failureTail[len(failureTail)-goalFailureTailLimit:]
	}
	a.LastFailure = failureTail
	a.UpdatedAt = time.Now().UTC()
	return a.Attempts
}

// Count returns the recorded attempts for key (0 when unknown).
func (s *GoalAttemptStore) Count(key string) int {
	if a, ok := s.Attempts[key]; ok {
		return a.Attempts
	}
	return 0
}

// LastFailure returns the most recent failure tail recorded for key.
func (s *GoalAttemptStore) LastFailure(key string) string {
	if a, ok := s.Attempts[key]; ok {
		return a.LastFailure
	}
	return ""
}

// Clear removes key's budget record — the goal landed, so a later re-proposal
// starts fresh. Reports whether anything changed.
func (s *GoalAttemptStore) Clear(key string) bool {
	if _, ok := s.Attempts[key]; !ok {
		return false
	}
	delete(s.Attempts, key)
	return true
}

// Save writes the store atomically (tmp+rename) per ADR-003.
func (s *GoalAttemptStore) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// RecordRedPass increments the goal's consecutive red-pass counter and
// returns the new streak. A red-pass (the plan's RED command passing before
// GREEN) is evidence the goal's work already exists at HEAD.
func (s *GoalAttemptStore) RecordRedPass(key string) int {
	a, ok := s.Attempts[key]
	if !ok {
		a = &GoalAttempt{}
		s.Attempts[key] = a
	}
	a.RedPassStreak++
	a.UpdatedAt = time.Now().UTC()
	return a.RedPassStreak
}

// RedPassStreak returns the recorded consecutive red-pass count for key.
func (s *GoalAttemptStore) RedPassStreak(key string) int {
	if a, ok := s.Attempts[key]; ok {
		return a.RedPassStreak
	}
	return 0
}
