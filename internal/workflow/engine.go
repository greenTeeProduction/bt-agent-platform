package workflow

import (
	"fmt"
	"sort"
	"time"

	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
	"github.com/nico/go-bt-evolve/internal/util"
)

// Workflow connects thinktank analysis to company execution.
// Thinktank produces recommendations → Workflow creates tasks → Company executes sprints.
type Workflow struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	ThinkTank   *thinktank.ThinkTank `json:"thinktank"`
	Company     *startup.CompanyState `json:"company"`
	Tasks       []Task              `json:"tasks"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	Status      string              `json:"status"` // created, analyzing, executing, completed, archived
}

// Task is a unit of work derived from thinktank recommendations.
type Task struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Source         string    `json:"source"`          // which thinktank recommendation spawned this
	Priority       Priority  `json:"priority"`
	Status         TaskStatus `json:"status"`
	Approval       Approval  `json:"approval"`
	AssigneeRole   string    `json:"assignee_role"`   // ceo, cto, pm, engineer, marketing, sales
	SprintTarget   int       `json:"sprint_target"`   // which sprint to execute in
	EstimatedEffort int     `json:"estimated_effort"` // story points
	CreatedAt      time.Time `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// Priority levels for task ordering.
type Priority int

const (
	PriorityCritical Priority = iota
	PriorityHigh
	PriorityMedium
	PriorityLow
	PriorityBacklog
)

func (p Priority) String() string {
	switch p {
	case PriorityCritical: return "critical"
	case PriorityHigh: return "high"
	case PriorityMedium: return "medium"
	case PriorityLow: return "low"
	default: return "backlog"
	}
}

// TaskStatus tracks execution state.
type TaskStatus int

const (
	StatusPending TaskStatus = iota
	StatusApproved
	StatusRejected
	StatusInProgress
	StatusCompleted
	StatusBlocked
)

func (s TaskStatus) String() string {
	switch s {
	case StatusPending: return "pending"
	case StatusApproved: return "approved"
	case StatusRejected: return "rejected"
	case StatusInProgress: return "in_progress"
	case StatusCompleted: return "completed"
	default: return "blocked"
	}
}

// Approval tracks the review state of a task.
type Approval struct {
	ApprovedBy  string     `json:"approved_by"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty"`
	RejectedAt  *time.Time `json:"rejected_at,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	IsApproved  bool       `json:"is_approved"`
}

// ─── Conversion: ThinkTank → Tasks ───

// RecommendationsToTasks converts thinktank recommendations into company tasks.
func (w *Workflow) RecommendationsToTasks() {
	if w.ThinkTank.Synthesis == nil {
		return
	}

	s := w.ThinkTank.Synthesis

	// Main recommendation becomes a critical CEO task
	w.Tasks = append(w.Tasks, Task{
		ID:             "rec-001",
		Title:          "Implement: " + truncate(s.Recommendation, 80),
		Description:    s.Recommendation,
		Source:         "thinktank:synthesis:recommendation",
		Priority:       PriorityCritical,
		Status:         StatusPending,
		AssigneeRole:   "ceo",
		SprintTarget:   1,
		EstimatedEffort: 13,
		CreatedAt:      time.Now(),
	})

	// Points of agreement → PM tasks
	for i, point := range s.PointsOfAgreement {
		w.Tasks = append(w.Tasks, Task{
			ID:             fmt.Sprintf("agree-%03d", i+1),
			Title:          "Align on: " + truncate(point, 70),
			Description:    point,
			Source:         "thinktank:synthesis:agreement",
			Priority:       PriorityHigh,
			Status:         StatusPending,
			AssigneeRole:   "pm",
			SprintTarget:   1,
			EstimatedEffort: 5,
			CreatedAt:      time.Now(),
		})
	}

	// Points of disagreement → CTO investigation tasks
	for i, point := range s.PointsOfDisagreement {
		w.Tasks = append(w.Tasks, Task{
			ID:             fmt.Sprintf("disagree-%03d", i+1),
			Title:          "Investigate: " + truncate(point, 70),
			Description:    point,
			Source:         "thinktank:synthesis:disagreement",
			Priority:       PriorityMedium,
			Status:         StatusPending,
			AssigneeRole:   "cto",
			SprintTarget:   2,
			EstimatedEffort: 8,
			CreatedAt:      time.Now(),
		})
	}

	// Dissenting notes → engineer spike tasks
	for i, note := range s.DissentingNotes {
		w.Tasks = append(w.Tasks, Task{
			ID:             fmt.Sprintf("dissent-%03d", i+1),
			Title:          "Spike: " + truncate(note, 70),
			Description:    note,
			Source:         "thinktank:synthesis:dissenting",
			Priority:       PriorityLow,
			Status:         StatusPending,
			AssigneeRole:   "engineer",
			SprintTarget:   3,
			EstimatedEffort: 3,
			CreatedAt:      time.Now(),
		})
	}
}

// ─── Task Management ───

// ApproveTask marks a task as approved and ready for execution.
func (w *Workflow) ApproveTask(taskID, approver string) *Task {
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
			return &w.Tasks[i]
		}
	}
	return nil
}

// RejectTask marks a task as rejected with a reason.
func (w *Workflow) RejectTask(taskID, rejector, reason string) *Task {
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
			return &w.Tasks[i]
		}
	}
	return nil
}

// Prioritize reorders tasks by priority and sprint target.
func (w *Workflow) Prioritize() {
	// Stable sort by priority (critical first) then sprint
	sortTasks(w.Tasks)
}

// GetApprovedTasks returns tasks ready for execution.
func (w *Workflow) GetApprovedTasks() []Task {
	var approved []Task
	for _, t := range w.Tasks {
		if t.Status == StatusApproved || t.Status == StatusInProgress {
			approved = append(approved, t)
		}
	}
	return approved
}

// GetTasksByRole returns tasks assigned to a specific role.
func (w *Workflow) GetTasksByRole(role string) []Task {
	var result []Task
	for _, t := range w.Tasks {
		if t.AssigneeRole == role {
			result = append(result, t)
		}
	}
	return result
}

// GetTasksBySprint returns tasks for a specific sprint.
func (w *Workflow) GetTasksBySprint(sprint int) []Task {
	var result []Task
	for _, t := range w.Tasks {
		if t.SprintTarget == sprint {
			result = append(result, t)
		}
	}
	return result
}

// PendingApprovals returns tasks awaiting approval.
func (w *Workflow) PendingApprovals() []Task {
	var pending []Task
	for _, t := range w.Tasks {
		if t.Status == StatusPending {
			pending = append(pending, t)
		}
	}
	return pending
}

// ─── Company Integration ───

// ExecuteSprint runs approved tasks through the company simulation.
func (w *Workflow) ExecuteSprint(sprintNum int, orch interface {
	RunSprint() *startup.SprintResult
}) *startup.SprintResult {
	// Set sprint goal from approved tasks
	tasks := w.GetTasksBySprint(sprintNum)
	approved := 0
	for _, t := range tasks {
		if t.Status == StatusApproved {
			approved++
		}
	}

	if approved > 0 {
		w.Company.CurrentSprint = sprintNum
		w.Company.SprintGoal = fmt.Sprintf("Execute %d approved tasks", approved)
		for _, t := range tasks {
			if t.Status == StatusApproved {
				t.Status = StatusInProgress
			}
		}
	}

	result := orch.RunSprint()

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
		Tasks:     []Task{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    "created",
	}
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
	ttOrch.RunResearchRound()
	ttOrch.RunDebate()
	ttOrch.RunSynthesis()
	ttOrch.RunPeerReview()
	ttOrch.RunReportGeneration()

	// Phase 2: Convert recommendations to tasks
	w.RecommendationsToTasks()
	w.Prioritize()

	// Phase 3: Auto-approve high-priority tasks
	for i := range w.Tasks {
		if w.Tasks[i].Priority <= PriorityHigh {
			w.ApproveTask(w.Tasks[i].ID, "system")
		}
	}

	// Phase 4: Execute sprints
	w.Status = "executing"
	w.Company.CurrentSprint = 1
	w.Company.SprintGoal = w.Tasks[0].Title
	w.ExecuteSprint(1, compOrch)
	w.ExecuteSprint(2, compOrch)

	w.Status = "completed"
}

// ─── Helpers ───

func sortTasks(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].SprintTarget < tasks[j].SprintTarget
	})
}

func truncate(s string, n int) string { return util.Truncate(s, n) }
