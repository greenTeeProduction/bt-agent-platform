package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
	"github.com/nico/go-bt-evolve/internal/util"
)

// Workflow connects thinktank analysis to company execution.
// Thinktank produces recommendations → Workflow creates tasks → Company executes sprints.
type Workflow struct {
	// mu guards Tasks (and the other fields below it mutated alongside Tasks)
	// against concurrent HTTP handlers acting on the same *Workflow — see
	// cmd/bt-dashboard's package-level currentWorkflow.
	mu        sync.Mutex
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	ThinkTank *thinktank.ThinkTank  `json:"thinktank"`
	Company   *startup.CompanyState `json:"company"`
	Tasks     []WorkflowTask        `json:"tasks"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
	Status    string                `json:"status"` // created, analyzing, executing, completed, archived
}

// WorkflowTask is a unit of work derived from thinktank recommendations.
type WorkflowTask struct {
	ID              string             `json:"id"`
	Title           string             `json:"title"`
	Description     string             `json:"description"`
	Source          string             `json:"source"` // which thinktank recommendation spawned this
	Priority        WorkflowPriority   `json:"priority"`
	Status          WorkflowTaskStatus `json:"status"`
	Approval        Approval           `json:"approval"`
	AssigneeRole    string             `json:"assignee_role"`    // ceo, cto, pm, engineer, marketing, sales
	SprintTarget    int                `json:"sprint_target"`    // which sprint to execute in
	EstimatedEffort int                `json:"estimated_effort"` // story points
	CreatedAt       time.Time          `json:"created_at"`
	CompletedAt     *time.Time         `json:"completed_at,omitempty"`
}

// WorkflowPriority levels for task ordering.
type WorkflowPriority int

const (
	PriorityCritical WorkflowPriority = iota
	PriorityHigh
	PriorityMedium
	PriorityLow
	PriorityBacklog
)

func (p WorkflowPriority) String() string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	default:
		return "backlog"
	}
}

// WorkflowTaskStatus tracks execution state.
type WorkflowTaskStatus int

const (
	StatusPending WorkflowTaskStatus = iota
	StatusApproved
	StatusRejected
	StatusInProgress
	StatusCompleted
	StatusBlocked
)

func (s WorkflowTaskStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusApproved:
		return "approved"
	case StatusRejected:
		return "rejected"
	case StatusInProgress:
		return "in_progress"
	case StatusCompleted:
		return "completed"
	default:
		return "blocked"
	}
}

// Approval tracks the review state of a task.
type Approval struct {
	ApprovedBy string     `json:"approved_by"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	RejectedAt *time.Time `json:"rejected_at,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	IsApproved bool       `json:"is_approved"`
}

// ─── Conversion: ThinkTank → Tasks ───

// RecommendationsToTasks converts thinktank recommendations into company tasks.
func (w *Workflow) RecommendationsToTasks() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ThinkTank.Synthesis == nil {
		return
	}

	s := w.ThinkTank.Synthesis
	// nonce carries real per-call entropy so WorkflowTask IDs stay unique
	// across separate Workflow instances even when their positional index
	// matches and their parent Workflow.ID collides (NewWorkflow mints IDs
	// at second granularity, so two analyses in the same wall-clock second
	// would otherwise produce identical composed task IDs downstream).
	nonce := newTaskNonce()

	// Main recommendation becomes a critical CEO task
	w.Tasks = append(w.Tasks, WorkflowTask{
		ID:              "rec-001-" + nonce,
		Title:           "Implement: " + util.Truncate(s.Recommendation, 80),
		Description:     s.Recommendation,
		Source:          "thinktank:synthesis:recommendation",
		Priority:        PriorityCritical,
		Status:          StatusPending,
		AssigneeRole:    "ceo",
		SprintTarget:    1,
		EstimatedEffort: 13,
		CreatedAt:       time.Now(),
	})

	// Points of agreement → PM tasks
	for i, point := range s.PointsOfAgreement {
		w.Tasks = append(w.Tasks, WorkflowTask{
			ID:              fmt.Sprintf("agree-%03d-%s", i+1, nonce),
			Title:           "Align on: " + util.Truncate(point, 70),
			Description:     point,
			Source:          "thinktank:synthesis:agreement",
			Priority:        PriorityHigh,
			Status:          StatusPending,
			AssigneeRole:    "pm",
			SprintTarget:    1,
			EstimatedEffort: 5,
			CreatedAt:       time.Now(),
		})
	}

	// Points of disagreement → CTO investigation tasks
	for i, point := range s.PointsOfDisagreement {
		w.Tasks = append(w.Tasks, WorkflowTask{
			ID:              fmt.Sprintf("disagree-%03d-%s", i+1, nonce),
			Title:           "Investigate: " + util.Truncate(point, 70),
			Description:     point,
			Source:          "thinktank:synthesis:disagreement",
			Priority:        PriorityMedium,
			Status:          StatusPending,
			AssigneeRole:    "cto",
			SprintTarget:    2,
			EstimatedEffort: 8,
			CreatedAt:       time.Now(),
		})
	}

	// Dissenting notes → engineer spike tasks
	for i, note := range s.DissentingNotes {
		w.Tasks = append(w.Tasks, WorkflowTask{
			ID:              fmt.Sprintf("dissent-%03d-%s", i+1, nonce),
			Title:           "Spike: " + util.Truncate(note, 70),
			Description:     note,
			Source:          "thinktank:synthesis:dissenting",
			Priority:        PriorityLow,
			Status:          StatusPending,
			AssigneeRole:    "engineer",
			SprintTarget:    3,
			EstimatedEffort: 3,
			CreatedAt:       time.Now(),
		})
	}
}

// newTaskNonce returns a short random hex string used to disambiguate
// WorkflowTask IDs minted by separate RecommendationsToTasks calls.
func newTaskNonce() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ─── WorkflowTask Management ───

// ApproveTask marks a task as approved and ready for execution.
func (w *Workflow) ApproveTask(taskID, approver string) *WorkflowTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.Tasks {
		if w.Tasks[i].ID == taskID {
			now := time.Now()
			w.Tasks[i].Approval = Approval{
				ApprovedBy: approver,
				ApprovedAt: &now,
				IsApproved: true,
			}
			w.Tasks[i].Status = StatusApproved
			w.UpdatedAt = now
			resolveHITLAudit(taskID, approver, "", true)
			return &w.Tasks[i]
		}
	}
	return nil
}

// RejectTask marks a task as rejected with a reason.
func (w *Workflow) RejectTask(taskID, rejector, reason string) *WorkflowTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.Tasks {
		if w.Tasks[i].ID == taskID {
			now := time.Now()
			w.Tasks[i].Approval = Approval{
				ApprovedBy: rejector,
				RejectedAt: &now,
				Reason:     reason,
				IsApproved: false,
			}
			w.Tasks[i].Status = StatusRejected
			w.UpdatedAt = now
			resolveHITLAudit(taskID, rejector, reason, false)
			return &w.Tasks[i]
		}
	}
	return nil
}

// SetTaskStatus updates the status of the WorkflowTask matching taskID and,
// when the task is being marked completed, advances Company.CurrentSprint to
// keep pace with the task's SprintTarget — mirroring ExecuteSprint's own
// convention. This lets callers that dispatch tasks individually (e.g.
// handleSprintExecute's per-task loop, rather than a batched ExecuteSprint
// call) reconcile currentWorkflow with taskStore as each task finishes.
func (w *Workflow) SetTaskStatus(taskID string, status WorkflowTaskStatus) *WorkflowTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.Tasks {
		if w.Tasks[i].ID != taskID {
			continue
		}
		w.Tasks[i].Status = status
		if status == StatusCompleted {
			now := time.Now()
			w.Tasks[i].CompletedAt = &now
			w.Company.Lock()
			if w.Tasks[i].SprintTarget > w.Company.CurrentSprint {
				w.Company.CurrentSprint = w.Tasks[i].SprintTarget
			}
			w.Company.Unlock()
		}
		w.UpdatedAt = time.Now()
		return &w.Tasks[i]
	}
	return nil
}

// Prioritize reorders tasks by priority and sprint target.
func (w *Workflow) Prioritize() {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Stable sort by priority (critical first) then sprint
	sortTasks(w.Tasks)
}

// GetApprovedTasks returns tasks ready for execution.
func (w *Workflow) GetApprovedTasks() []WorkflowTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	var approved []WorkflowTask
	for _, t := range w.Tasks {
		if t.Status == StatusApproved || t.Status == StatusInProgress {
			approved = append(approved, t)
		}
	}
	return approved
}

// GetTasksByRole returns tasks assigned to a specific role.
func (w *Workflow) GetTasksByRole(role string) []WorkflowTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	var result []WorkflowTask
	for _, t := range w.Tasks {
		if t.AssigneeRole == role {
			result = append(result, t)
		}
	}
	return result
}

// GetTasksBySprint returns tasks for a specific sprint.
func (w *Workflow) GetTasksBySprint(sprint int) []WorkflowTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.getTasksBySprintLocked(sprint)
}

// getTasksBySprintLocked is GetTasksBySprint's body, callable by methods that
// already hold w.mu (e.g. ExecuteSprint) without deadlocking on a
// non-reentrant sync.Mutex.
func (w *Workflow) getTasksBySprintLocked(sprint int) []WorkflowTask {
	var result []WorkflowTask
	for _, t := range w.Tasks {
		if t.SprintTarget == sprint {
			result = append(result, t)
		}
	}
	return result
}

// PendingApprovals returns tasks awaiting approval.
func (w *Workflow) PendingApprovals() []WorkflowTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	var pending []WorkflowTask
	for _, t := range w.Tasks {
		if t.Status == StatusPending {
			pending = append(pending, t)
		}
	}
	return pending
}

// ─── Company Integration ───

// ExecuteSprint runs approved tasks through the company simulation.
//
// w.mu only ever protects this *Workflow instance's own fields (Tasks,
// UpdatedAt, ...); it does nothing to serialize concurrent writes to
// w.Company from a *different* *Workflow instance wrapping the same
// *startup.CompanyState pointer (see cmd/bt-dashboard, which mints a
// fresh, uncontended *Workflow per request against one shared
// package-level companyState). Company field reads/writes are therefore
// guarded by w.Company's own mutex instead, held only around those
// specific accesses — and never while calling orch.RunSprint(), since the
// real CompanyOrchestrator.RunSprint locks the same CompanyState
// internally and the mutex is not reentrant.
func (w *Workflow) ExecuteSprint(sprintNum int, orch interface {
	RunSprint() *startup.SprintResult
}) *startup.SprintResult {
	w.mu.Lock()

	// Set sprint goal from approved tasks
	tasks := w.getTasksBySprintLocked(sprintNum)
	approved := 0
	for _, t := range tasks {
		if t.Status == StatusApproved {
			approved++
		}
	}

	if approved > 0 {
		w.Company.Lock()
		w.Company.CurrentSprint = sprintNum
		w.Company.SprintGoal = fmt.Sprintf("Execute %d approved tasks", approved)
		w.Company.Unlock()
		for i := range w.Tasks {
			if w.Tasks[i].SprintTarget == sprintNum && w.Tasks[i].Status == StatusApproved {
				w.Tasks[i].Status = StatusInProgress
			}
		}
	}
	w.mu.Unlock()

	result := orch.RunSprint()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Mark completed tasks
	for i := range w.Tasks {
		if w.Tasks[i].SprintTarget == sprintNum && w.Tasks[i].Status == StatusInProgress {
			now := time.Now()
			w.Tasks[i].Status = StatusCompleted
			w.Tasks[i].CompletedAt = &now
		}
	}

	w.UpdatedAt = time.Now()
	return result
}

// ─── New Workflow ───

// NewWorkflow creates a connected thinktank + company workflow.
func NewWorkflow(name string, tt *thinktank.ThinkTank, company *startup.CompanyState) *Workflow {
	return &Workflow{
		ID:        fmt.Sprintf("wf-%d", time.Now().Unix()),
		Name:      name,
		ThinkTank: tt,
		Company:   company,
		Tasks:     []WorkflowTask{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    "created",
	}
}

// distinctSprintTargets returns every distinct SprintTarget present in
// w.Tasks, sorted ascending, so RunFullPipeline can execute exactly the
// sprints that have tasks instead of a hardcoded sprint count.
func (w *Workflow) distinctSprintTargets() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	seen := make(map[int]bool)
	var sprints []int
	for _, t := range w.Tasks {
		if !seen[t.SprintTarget] {
			seen[t.SprintTarget] = true
			sprints = append(sprints, t.SprintTarget)
		}
	}
	slices.Sort(sprints)
	return sprints
}

// RunFullPipeline executes: thinktank analysis → task creation → approval → company execution.
func (w *Workflow) RunFullPipeline(ttOrch interface {
	RunResearchRound() error
	RunDebate() error
	RunSynthesis() error
	RunPeerReview() error
	RunReportGeneration() error
}, compOrch interface {
	RunSprint() *startup.SprintResult
}) {
	// Phase 1: Thinktank analysis
	w.Status = "analyzing"
	if err := ttOrch.RunResearchRound(); err != nil {
		w.Status = "failed"
		return
	}
	if err := ttOrch.RunDebate(); err != nil {
		w.Status = "failed"
		return
	}
	if err := ttOrch.RunSynthesis(); err != nil {
		w.Status = "failed"
		return
	}
	if err := ttOrch.RunPeerReview(); err != nil {
		w.Status = "failed"
		return
	}
	if err := ttOrch.RunReportGeneration(); err != nil {
		w.Status = "failed"
		return
	}

	// Phase 2: Convert recommendations to tasks
	w.RecommendationsToTasks()
	w.Prioritize()

	// Phase 3: Execute sprints. No task is auto-approved here — every
	// WorkflowTask requires an explicit human/HITL decision via
	// ApproveTask/RejectTask before ExecuteSprint will move it forward.
	w.Status = "executing"
	w.Company.Lock()
	w.Company.CurrentSprint = 1
	if len(w.Tasks) > 0 {
		w.Company.SprintGoal = w.Tasks[0].Title
	}
	w.Company.Unlock()
	for _, sprintNum := range w.distinctSprintTargets() {
		w.ExecuteSprint(sprintNum, compOrch)
	}

	w.Status = "completed"
}

// ─── Helpers ───

func sortTasks(tasks []WorkflowTask) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].SprintTarget < tasks[j].SprintTarget
	})
}
