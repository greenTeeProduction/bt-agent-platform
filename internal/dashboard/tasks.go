package dashboard

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

var (
	ErrTaskNotFound      = errors.New("task not found")
	ErrTaskInvalidStatus = errors.New("invalid task status")
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
	Error       string `json:"error,omitempty"`
	ErrorKind   string `json:"error_kind,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	// Approval is the audit trail for approve/reject decisions, mirroring
	// the Approval struct on WorkflowTask in workflow_engine.go.
	Approval Approval `json:"approval,omitzero"`
	// AuditPending blocks admission until a committed decision reaches its HITL audit.
	AuditPending bool `json:"approval_audit_pending,omitempty"`
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
	data, err := util.ReadPersistenceFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh store, no tasks yet
		}
		return fmt.Errorf("dashboard: read task store: %w", err)
	}
	var loaded struct {
		Tasks []Task `json:"tasks"`
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("dashboard: parse task store: %w", err)
	}
	s.Tasks = loaded.Tasks
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
	return s.commitTasksLocked(s.Tasks)
}

// commitTasksLocked persists a complete snapshot before publishing it in memory.
// Like JobStore, this serializes replacement writes, not stale-snapshot merging.
func (s *TaskStore) commitTasksLocked(tasks []Task) error {
	return s.commitTasksLockedWithContext(context.Background(), tasks)
}

func (s *TaskStore) commitTasksLockedWithContext(ctx context.Context, tasks []Task) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := util.EnsurePersistenceParent(s.path); err != nil {
		return fmt.Errorf("dashboard: create task directory: %w", err)
	}
	release, err := reliability.AcquireFileLockWithContext(ctx, s.path)
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := util.SaveJSONAtomic(s.path, struct {
		Tasks []Task `json:"tasks"`
	}{Tasks: tasks}); err != nil {
		return fmt.Errorf("dashboard: save tasks: %w", err)
	}
	s.Tasks = tasks
	return nil
}

// lockWithContext bounds contention with legacy mutations using the same mutex.
func (s *TaskStore) lockWithContext(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.mu.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *TaskStore) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.Tasks))
	for i, task := range s.Tasks {
		out[i] = cloneTask(task)
	}
	return out
}

func (s *TaskStore) Get(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.Tasks {
		if t.ID == id {
			return cloneTask(t), true
		}
	}
	return Task{}, false
}

func (s *TaskStore) Create(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.Tasks {
		if existing.ID == task.ID {
			return fmt.Errorf("task %s already exists", task.ID)
		}
	}
	task.CreatedAt = time.Now().Format(time.RFC3339)
	if task.Status == "" {
		task.Status = "pending"
	}
	if task.Sprint == 0 {
		task.Sprint = 1
	}
	return s.commitTasksLocked(append(slices.Clone(s.Tasks), cloneTask(task)))
}

func (s *TaskStore) UpdateStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			tasks := slices.Clone(s.Tasks)
			tasks[i].Status = status
			if status == "completed" || status == "failed" {
				tasks[i].CompletedAt = time.Now().Format(time.RFC3339)
			}
			return s.commitTasksLocked(tasks)
		}
	}
	return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
}

// Approve marks a task as approved and records who approved it and when.
func (s *TaskStore) Approve(id, approver string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			switch s.Tasks[i].Status {
			case "approved":
				if s.Tasks[i].AuditPending {
					return s.finishDecisionAuditLocked(i)
				}
				return nil
			case "in_progress", "completed":
				return fmt.Errorf("%w: task %s is already %s", ErrTaskInvalidStatus, id, s.Tasks[i].Status)
			}
			tasks := slices.Clone(s.Tasks)
			now := time.Now()
			tasks[i].Status = "approved"
			tasks[i].AuditPending = true
			tasks[i].Approval = Approval{
				ApprovedBy: approver,
				ApprovedAt: &now,
				IsApproved: true,
			}
			if err := s.commitTasksLocked(tasks); err != nil {
				return err
			}
			return s.finishDecisionAuditLocked(i)
		}
	}
	return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
}

// Reject marks a task as rejected and records who rejected it, when, and why.
func (s *TaskStore) Reject(id, rejector, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			if s.Tasks[i].Status == "rejected" {
				if s.Tasks[i].AuditPending {
					return s.finishDecisionAuditLocked(i)
				}
				return nil
			}

			tasks := slices.Clone(s.Tasks)
			now := time.Now()
			tasks[i].Status = "rejected"
			tasks[i].AuditPending = true
			tasks[i].Approval = Approval{
				ApprovedBy: rejector,
				RejectedAt: &now,
				Reason:     reason,
				IsApproved: false,
			}
			if err := s.commitTasksLocked(tasks); err != nil {
				return err
			}
			return s.finishDecisionAuditLocked(i)
		}
	}
	return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
}

// TaskDecisionPersistenceError reports a committed task decision awaiting audit
// reconciliation. Admission stays blocked; retry the decision to finish it.
type TaskDecisionPersistenceError struct {
	TaskID string
	Err    error
}

func (e *TaskDecisionPersistenceError) Error() string {
	return fmt.Sprintf("task %s decision committed; audit synchronization pending: %v", e.TaskID, e.Err)
}
func (e *TaskDecisionPersistenceError) Unwrap() error { return e.Err }

func (s *TaskStore) finishDecisionAuditLocked(index int) error {
	task := s.Tasks[index]
	if err := resolveHITLAudit(task.ID, task.Approval.ApprovedBy, task.Approval.Reason, task.Approval.IsApproved); err != nil {
		return &TaskDecisionPersistenceError{TaskID: task.ID, Err: err}
	}
	tasks := slices.Clone(s.Tasks)
	tasks[index].AuditPending = false
	if err := s.commitTasksLocked(tasks); err != nil {
		return &TaskDecisionPersistenceError{TaskID: task.ID, Err: err}
	}
	return nil
}

// resolveHITLAudit synchronizes an existing audit; optional non-HITL tasks have
// no request. Storage failure must not be confused with a missing request.
func resolveHITLAudit(taskID, reviewer, reason string, approved bool) error {
	store := hitl.DefaultStore
	if store == nil {
		return nil
	}
	var err error
	if approved {
		_, err = store.ApproveByTaskID(taskID, reviewer, reason)
	} else {
		_, err = store.RejectByTaskID(taskID, reviewer, reason)
	}
	if errors.Is(err, hitl.ErrRequestNotFound) {
		return nil
	}
	return err
}

func cloneTask(task Task) Task {
	if task.Approval.ApprovedAt != nil {
		value := *task.Approval.ApprovedAt
		task.Approval.ApprovedAt = &value
	}
	if task.Approval.RejectedAt != nil {
		value := *task.Approval.RejectedAt
		task.Approval.RejectedAt = &value
	}
	return task
}

func (s *TaskStore) SetOutput(id, output, outcome string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			tasks := slices.Clone(s.Tasks)
			tasks[i].Output = output
			tasks[i].Outcome = outcome
			return s.commitTasksLocked(tasks)
		}
	}
	return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
}

// TaskExecutionResult is an observed run's task metadata, separate from the
// work itself. Retain it when CommitExecution fails; repairing that commit must
// not execute the task again.
type TaskExecutionResult struct {
	Status    string
	Output    string
	Outcome   string
	Error     string
	ErrorKind string
	RunID     string
}

// CommitExecution atomically records disposition, output and diagnostics for a
// durably claimed task. A concurrent operator decision cannot be overwritten.
func (s *TaskStore) CommitExecution(id string, result TaskExecutionResult) error {
	return s.CommitExecutionWithContext(context.Background(), id, result)
}

func (s *TaskStore) CommitExecutionWithContext(ctx context.Context, id string, result TaskExecutionResult) error {
	return s.CommitExecutionBatchWithContext(ctx, map[string]TaskExecutionResult{id: result})
}

// CommitExecutionBatchWithContext applies related results together. Sprint
// cleanup uses this to return only proven unstarted claims within one budget.
func (s *TaskStore) CommitExecutionBatchWithContext(ctx context.Context, results map[string]TaskExecutionResult) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, result := range results {
		if result.Status != "approved" && result.Status != "failed" && result.Status != "completed" {
			return fmt.Errorf("%w: execution disposition %s", ErrTaskInvalidStatus, result.Status)
		}
		if result.Status == "completed" && !reliability.IsHealthyOutcome(result.Outcome) {
			return fmt.Errorf("%w: completed execution outcome %s", ErrTaskInvalidStatus, result.Outcome)
		}
	}
	if err := s.lockWithContext(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	tasks := slices.Clone(s.Tasks)
	matched := 0
	for i, task := range tasks {
		result, ok := results[task.ID]
		if !ok {
			continue
		}
		if task.Status != "in_progress" {
			return fmt.Errorf("%w: task %s is %s, not claimed", ErrTaskInvalidStatus, task.ID, task.Status)
		}
		matched++
		tasks[i].Status = result.Status
		tasks[i].Output, tasks[i].Outcome = result.Output, result.Outcome
		tasks[i].Error, tasks[i].ErrorKind, tasks[i].RunID = result.Error, result.ErrorKind, result.RunID
		tasks[i].CompletedAt = ""
		if result.Status != "approved" {
			tasks[i].CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}
	if matched != len(results) {
		return fmt.Errorf("%w: execution batch contains missing tasks", ErrTaskNotFound)
	}
	if matched == 0 {
		return ctx.Err()
	}
	return s.commitTasksLockedWithContext(ctx, tasks)
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
		if t.Status == "approved" && !t.AuditPending {
			out = append(out, cloneTask(t))
		}
	}
	slices.SortStableFunc(out, func(a, b Task) int {
		return cmp.Or(
			cmp.Compare(priorityRank(a.Priority), priorityRank(b.Priority)),
			cmp.Compare(a.Sprint, b.Sprint),
		)
	})
	return out
}

// ClaimApproved atomically persists admission before any task can execute.
// Failed persistence restores the prior state and returns no claimed tasks.
func (s *TaskStore) ClaimApproved() ([]Task, error) {
	return s.ClaimApprovedWithContext(context.Background())
}

func (s *TaskStore) ClaimApprovedWithContext(ctx context.Context) ([]Task, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.lockWithContext(ctx); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	tasks := slices.Clone(s.Tasks)
	var out []Task
	for i := range s.Tasks {
		if s.Tasks[i].Status == "approved" && !s.Tasks[i].AuditPending {
			out = append(out, cloneTask(s.Tasks[i]))
			tasks[i].Status = "in_progress"
		}
	}
	if len(out) > 0 {
		if err := s.commitTasksLockedWithContext(ctx, tasks); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(out, func(a, b Task) int {
		return cmp.Or(cmp.Compare(priorityRank(a.Priority), priorityRank(b.Priority)), cmp.Compare(a.Sprint, b.Sprint))
	})
	return out, nil
}
