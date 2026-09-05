package goap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// GoalStore persists a user's goal set as JSON under a directory (typically
// users/<user>/goals/). It activates the previously dormant GoalQueue for
// per-user use: goals survive process restarts and are re-queued on load.
type GoalStore struct {
	mu  sync.Mutex
	dir string
}

// NewGoalStore creates a store rooted at dir, creating the directory if needed.
func NewGoalStore(dir string) (*GoalStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("goalstore: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("goalstore: create dir: %w", err)
	}
	return &GoalStore{dir: dir}, nil
}

func (s *GoalStore) file() string {
	return filepath.Join(s.dir, "goals.json")
}

// Load returns all persisted goals. A missing file is an empty goal set.
func (s *GoalStore) Load() ([]*Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *GoalStore) loadLocked() ([]*Goal, error) {
	data, err := os.ReadFile(s.file())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("goalstore: read: %w", err)
	}
	var goals []*Goal
	if err := json.Unmarshal(data, &goals); err != nil {
		return nil, fmt.Errorf("goalstore: parse: %w", err)
	}
	return goals, nil
}

// Save replaces the persisted goal set atomically (temp file + rename).
func (s *GoalStore) Save(goals []*Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(goals)
}

func (s *GoalStore) saveLocked(goals []*Goal) error {
	if goals == nil {
		goals = []*Goal{}
	}
	data, err := json.MarshalIndent(goals, "", "  ")
	if err != nil {
		return fmt.Errorf("goalstore: marshal: %w", err)
	}
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("goalstore: write: %w", err)
	}
	if err := os.Rename(tmp, s.file()); err != nil {
		return fmt.Errorf("goalstore: rename: %w", err)
	}
	return nil
}

// Add upserts a goal by name and persists the result.
func (s *GoalStore) Add(goal *Goal) error {
	if goal == nil || goal.Name == "" {
		return fmt.Errorf("goalstore: goal must have a name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	goals, err := s.loadLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i, g := range goals {
		if g.Name == goal.Name {
			goals[i] = goal
			replaced = true
			break
		}
	}
	if !replaced {
		goals = append(goals, goal)
	}
	return s.saveLocked(goals)
}

// Remove deletes a goal by name. Returns false when the goal did not exist.
func (s *GoalStore) Remove(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	goals, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	kept := goals[:0]
	removed := false
	for _, g := range goals {
		if g.Name == name {
			removed = true
			continue
		}
		kept = append(kept, g)
	}
	if !removed {
		return false, nil
	}
	return true, s.saveLocked(kept)
}

// Queue loads the persisted goals into a fresh GoalQueue ready for
// SelectGoal / InterleaveCheck.
func (s *GoalStore) Queue() (*GoalQueue, error) {
	goals, err := s.Load()
	if err != nil {
		return nil, err
	}
	return NewGoalQueueFrom(goals...), nil
}
