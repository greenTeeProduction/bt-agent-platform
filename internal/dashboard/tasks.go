package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/hitl"
)

// Task represents a workflow task in the pipeline.
type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"` // critical, high, medium, low
	Status      string `json:"status"`   // pending, approved, rejected, in_progress, completed, failed
	Assignee    string `json:"assignee"` // agent name or role
	Sprint      int    `json:"sprint"`
	StoryPoints int    `json:"sp"`
	Source      string `json:"source"`    // thinktank, manual
	SourceID    string `json:"source_id"` // thinktank finding ID
	TreeID      string `json:"tree_id"`   // which BT tree to run
	CreatedAt   string `json:"created_at"`
	CompletedAt string `json:"completed_at,omitempty"`
	Output      string `json:"output,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	// Approval is the audit trail for approve/reject decisions, mirroring
	// the Approval struct on WorkflowTask in workflow_engine.go.
	Approval Approval `json:"approval,omitzero"`
}

// TaskStore persists tasks to a JSON file.
type TaskStore struct {
	mu    sync.Mutex
	path  string
	Tasks []Task `json:"tasks"`
}

// NewTaskStore loads the store at path, creating a fresh one if the file
// does not exist yet. It panics if the file exists but fails to parse: a
// corrupted tasks.json must never be silently mistaken for an empty store,
// since that would look identical to "no tasks yet" and hide real data loss.
func NewTaskStore(path string) *TaskStore {
	ts := &TaskStore{path: path, Tasks: []Task{}}
	if err := ts.Load(); err != nil {
		panic(fmt.Sprintf("dashboard: loading task store %s: %v", path, err))
	}
	return ts
}

// Load reads and parses the store's JSON file. A missing file is a fresh
// store, not an error. A file that exists but fails to parse returns an
// error instead of silently discarding it and leaving Tasks unchanged.
func (s *TaskStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh store, no tasks yet
		}
		return fmt.Errorf("dashboard: read task store: %w", err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return fmt.Errorf("dashboard: parse task store: %w", err)
	}
	return nil
}

func (s *TaskStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked writes the store atomically: it marshals to a sibling temp
// file and renames it into place, so a failure (or a crash mid-write)
// leaves the existing tasks.json untouched rather than truncated.
func (s *TaskStore) saveLocked() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("dashboard: write task store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("dashboard: rename task store: %w", err)
	}
	return nil
}

func (s *TaskStore) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.Tasks))
	copy(out, s.Tasks)
	return out
}

func (s *TaskStore) Get(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

func (s *TaskStore) Create(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task.CreatedAt = time.Now().Format(time.RFC3339)
	if task.Status == "" {
		task.Status = "pending"
	}
	if task.Sprint == 0 {
		task.Sprint = 1
	}
	s.Tasks = append(s.Tasks, task)
	return s.saveLocked()
}

func (s *TaskStore) UpdateStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			s.Tasks[i].Status = status
			if status == "completed" || status == "failed" {
				s.Tasks[i].CompletedAt = time.Now().Format(time.RFC3339)
			}
			return s.saveLocked()
		}
	}
	return fmt.Errorf("task %s not found", id)
}

// Approve marks a task as approved and records who approved it and when.
func (s *TaskStore) Approve(id, approver string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			now := time.Now()
			s.Tasks[i].Status = "approved"
			s.Tasks[i].Approval = Approval{
				ApprovedBy: approver,
				ApprovedAt: &now,
				IsApproved: true,
			}
			resolveHITLAudit(id, approver, "", true)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("task %s not found", id)
}

// Reject marks a task as rejected and records who rejected it, when, and why.
func (s *TaskStore) Reject(id, rejector, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			now := time.Now()
			s.Tasks[i].Status = "rejected"
			s.Tasks[i].Approval = Approval{
				ApprovedBy: rejector,
				RejectedAt: &now,
				Reason:     reason,
				IsApproved: false,
			}
			resolveHITLAudit(id, rejector, reason, false)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("task %s not found", id)
}

// resolveHITLAudit resolves the pending (or escalated) hitl.Request recorded
// for taskID, if any, so an approve/reject decision made through TaskStore or
// Workflow always lands on the same audit-trail record a dashboard operator
// or MCP tool would see via hitl.DefaultStore — instead of each model keeping
// its own Approval field in sync with a separately-driven HITL resolution.
// Most tasks are not HITL-gated, so a missing request is not an error.
func resolveHITLAudit(taskID, reviewer, reason string, approved bool) {
	store := hitl.DefaultStore
	if store == nil {
		return
	}
	if approved {
		_, _ = store.ApproveByTaskID(taskID, reviewer, reason)
	} else {
		_, _ = store.RejectByTaskID(taskID, reviewer, reason)
	}
}

func (s *TaskStore) SetOutput(id, output, outcome string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			s.Tasks[i].Output = output
			s.Tasks[i].Outcome = outcome
			return s.saveLocked()
		}
	}
	return fmt.Errorf("task %s not found", id)
}

// priorityRank maps a Task's string Priority to the same ordinal used by
// WorkflowPriority in workflow_engine.go (critical first), so Approved()
// dispatches in the same order as Workflow.Prioritize's sortTasks.
func priorityRank(priority string) int {
	switch priority {
	case "critical":
		return int(PriorityCritical)
	case "high":
		return int(PriorityHigh)
	case "medium":
		return int(PriorityMedium)
	case "low":
		return int(PriorityLow)
	default:
		return int(PriorityBacklog)
	}
}

// Approved returns tasks with status "approved", ordered by priority
// (critical first) then sprint — mirroring workflow_engine.go's
// sortTasks/Prioritize — so callers like handleSprintExecute dispatch
// high-urgency work first regardless of task creation order.
func (s *TaskStore) Approved() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.Tasks {
		if t.Status == "approved" {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := priorityRank(out[i].Priority), priorityRank(out[j].Priority)
		if ri != rj {
			return ri < rj
		}
		return out[i].Sprint < out[j].Sprint
	})
	return out
}
