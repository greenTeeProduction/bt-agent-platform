package research

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Programs are research-proposed multi-cycle changes: work too large for one
// scheduled run, split into file-scoped milestones that successive cycles
// execute one at a time. Persisted per ADR-003 (atomic JSON under
// ~/.go-bt-evolve).

type Milestone struct {
	Goal         string    `json:"goal"`
	Status       string    `json:"status"` // pending | done
	CompletedRun string    `json:"completed_run,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
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
func (p *Program) NextMilestone() (int, *Milestone) {
	for i := range p.Milestones {
		if p.Milestones[i].Status == "pending" {
			return i, &p.Milestones[i]
		}
	}
	return -1, nil
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

// Active returns the oldest program that still has a pending milestone.
func (ps *ProgramStore) Active() *Program {
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

// Save writes the store atomically (tmp+rename) per ADR-003.
func (ps *ProgramStore) Save() error {
	if err := os.MkdirAll(filepath.Dir(ps.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	tmp := ps.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ps.path)
}
