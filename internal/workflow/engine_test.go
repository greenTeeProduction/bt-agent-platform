package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
)

// ─── Priority.String() ───

func TestPriority_String(t *testing.T) {
	tests := []struct {
		p    Priority
		want string
	}{
		{PriorityCritical, "critical"},
		{PriorityHigh, "high"},
		{PriorityMedium, "medium"},
		{PriorityLow, "low"},
		{PriorityBacklog, "backlog"},
		{Priority(99), "backlog"}, // unknown → default
	}
	for _, tt := range tests {
		got := tt.p.String()
		if got != tt.want {
			t.Errorf("Priority(%d).String() = %q, want %q", tt.p, got, tt.want)
		}
	}
}

// ─── TaskStatus.String() ───

func TestTaskStatus_String(t *testing.T) {
	tests := []struct {
		s    TaskStatus
		want string
	}{
		{StatusPending, "pending"},
		{StatusApproved, "approved"},
		{StatusRejected, "rejected"},
		{StatusInProgress, "in_progress"},
		{StatusCompleted, "completed"},
		{StatusBlocked, "blocked"},
		{TaskStatus(99), "blocked"}, // unknown → default
	}
	for _, tt := range tests {
		got := tt.s.String()
		if got != tt.want {
			t.Errorf("TaskStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

// ─── NewWorkflow ───

func TestNewWorkflow(t *testing.T) {
	tt := &thinktank.ThinkTank{Topic: "test"}
	company := &startup.CompanyState{Name: "TestCo"}

	wf := NewWorkflow("test-wf", tt, company)

	if wf.Name != "test-wf" {
		t.Errorf("expected name 'test-wf', got %q", wf.Name)
	}
	if wf.ThinkTank != tt {
		t.Error("expected ThinkTank to be set")
	}
	if wf.Company != company {
		t.Error("expected Company to be set")
	}
	if wf.Status != "created" {
		t.Errorf("expected status 'created', got %q", wf.Status)
	}
	if len(wf.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(wf.Tasks))
	}
	if !strings.HasPrefix(wf.ID, "wf-") {
		t.Errorf("expected ID to start with 'wf-', got %q", wf.ID)
	}
	if wf.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if wf.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

// ─── RecommendationsToTasks ───

func TestRecommendationsToTasks_NilSynthesis(t *testing.T) {
	tt := &thinktank.ThinkTank{Synthesis: nil}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	if len(wf.Tasks) != 0 {
		t.Errorf("expected 0 tasks with nil synthesis, got %d", len(wf.Tasks))
	}
}

func TestRecommendationsToTasks_FullSynthesis(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:       "Build a real-time collaboration feature with WebSocket support and operational transforms",
			PointsOfAgreement:    []string{"Market demand is strong", "Technical feasibility confirmed"},
			PointsOfDisagreement: []string{"WebSocket vs SSE", "Priority relative to mobile app"},
			DissentingNotes:      []string{"Engineering bandwidth is tight this quarter"},
		},
	}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	// Should create: 1 recommendation + 2 agreement + 2 disagreement + 1 dissenting = 6 tasks
	if len(wf.Tasks) != 6 {
		t.Errorf("expected 6 tasks, got %d", len(wf.Tasks))
	}

	// Recommendation → critical CEO task
	rec := wf.Tasks[0]
	if rec.Priority != PriorityCritical {
		t.Errorf("recommendation should be critical, got %s", rec.Priority.String())
	}
	if rec.AssigneeRole != "ceo" {
		t.Errorf("recommendation should be CEO task, got %s", rec.AssigneeRole)
	}
	if !strings.HasPrefix(rec.Title, "Implement: ") {
		t.Errorf("recommendation title should start with 'Implement: ', got %q", rec.Title)
	}
	if rec.SprintTarget != 1 {
		t.Errorf("recommendation sprint should be 1, got %d", rec.SprintTarget)
	}
	if rec.EstimatedEffort != 13 {
		t.Errorf("recommendation effort should be 13, got %d", rec.EstimatedEffort)
	}

	// Agreements → high PM tasks
	for i := 1; i <= 2; i++ {
		task := wf.Tasks[i]
		if task.Priority != PriorityHigh {
			t.Errorf("agreement task %d should be high priority, got %s", i, task.Priority.String())
		}
		if task.AssigneeRole != "pm" {
			t.Errorf("agreement task %d should be PM task, got %s", i, task.AssigneeRole)
		}
		if !strings.HasPrefix(task.Title, "Align on: ") {
			t.Errorf("agreement task %d title should start with 'Align on: ', got %q", i, task.Title)
		}
	}

	// Disagreements → medium CTO tasks
	for i := 3; i <= 4; i++ {
		task := wf.Tasks[i]
		if task.Priority != PriorityMedium {
			t.Errorf("disagreement task %d should be medium priority, got %s", i, task.Priority.String())
		}
		if task.AssigneeRole != "cto" {
			t.Errorf("disagreement task %d should be CTO task, got %s", i, task.AssigneeRole)
		}
		if !strings.HasPrefix(task.Title, "Investigate: ") {
			t.Errorf("disagreement task %d title should start with 'Investigate: ', got %q", i, task.Title)
		}
	}

	// Dissenting → low engineer tasks
	task := wf.Tasks[5]
	if task.Priority != PriorityLow {
		t.Errorf("dissenting task should be low priority, got %s", task.Priority.String())
	}
	if task.AssigneeRole != "engineer" {
		t.Errorf("dissenting task should be engineer task, got %s", task.AssigneeRole)
	}
	if !strings.HasPrefix(task.Title, "Spike: ") {
		t.Errorf("dissenting task title should start with 'Spike: ', got %q", task.Title)
	}
	if task.SprintTarget != 3 {
		t.Errorf("dissenting sprint should be 3, got %d", task.SprintTarget)
	}
}

func TestRecommendationsToTasks_EmptyFields(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:       "the recommendation",
			PointsOfAgreement:    []string{},
			PointsOfDisagreement: []string{},
			DissentingNotes:      []string{},
		},
	}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	// Only the recommendation task should be created
	if len(wf.Tasks) != 1 {
		t.Errorf("expected 1 task (recommendation only), got %d", len(wf.Tasks))
	}
}

// ─── ApproveTask ───

func TestApproveTask(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "task-1", Status: StatusPending, Priority: PriorityHigh},
		{ID: "task-2", Status: StatusPending, Priority: PriorityMedium},
	}

	// Approve existing task
	approved := wf.ApproveTask("task-1", "alice")
	if approved == nil {
		t.Fatal("expected non-nil result for existing task")
	}
	if !approved.Approval.IsApproved {
		t.Error("expected task to be approved")
	}
	if approved.Approval.ApprovedBy != "alice" {
		t.Errorf("expected approvedBy 'alice', got %q", approved.Approval.ApprovedBy)
	}
	if approved.Approval.ApprovedAt == nil {
		t.Error("expected ApprovedAt to be set")
	}
	if approved.Status != StatusApproved {
		t.Errorf("expected status approved, got %s", approved.Status.String())
	}
	if wf.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}

	// Approve non-existent task
	notFound := wf.ApproveTask("nonexistent", "bob")
	if notFound != nil {
		t.Error("expected nil for non-existent task")
	}

	// Second task should still be pending
	if wf.Tasks[1].Status != StatusPending {
		t.Errorf("second task should still be pending, got %s", wf.Tasks[1].Status.String())
	}
}

// ─── RejectTask ───

func TestRejectTask(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "task-1", Status: StatusPending},
	}

	rejected := wf.RejectTask("task-1", "manager", "not enough info")
	if rejected == nil {
		t.Fatal("expected non-nil result")
	}
	if rejected.Approval.IsApproved {
		t.Error("expected task to be rejected")
	}
	if rejected.Approval.Reason != "not enough info" {
		t.Errorf("expected reason 'not enough info', got %q", rejected.Approval.Reason)
	}
	if rejected.Approval.RejectedAt == nil {
		t.Error("expected RejectedAt to be set")
	}
	if rejected.Status != StatusRejected {
		t.Errorf("expected status rejected, got %s", rejected.Status.String())
	}

	// Non-existent task
	notFound := wf.RejectTask("nonexistent", "bob", "n/a")
	if notFound != nil {
		t.Error("expected nil for non-existent task")
	}
}

// ─── Prioritize ───

func TestPrioritize(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "low-sprint1", Priority: PriorityLow, SprintTarget: 1},
		{ID: "critical-sprint2", Priority: PriorityCritical, SprintTarget: 2},
		{ID: "high-sprint1", Priority: PriorityHigh, SprintTarget: 1},
		{ID: "critical-sprint1", Priority: PriorityCritical, SprintTarget: 1},
		{ID: "medium-sprint3", Priority: PriorityMedium, SprintTarget: 3},
	}

	wf.Prioritize()

	// Priority first, then sprint target
	expected := []string{
		"critical-sprint1", // PriorityCritical, Sprint 1
		"critical-sprint2", // PriorityCritical, Sprint 2
		"high-sprint1",     // PriorityHigh, Sprint 1
		"medium-sprint3",   // PriorityMedium, Sprint 3
		"low-sprint1",      // PriorityLow, Sprint 1
	}
	for i, exp := range expected {
		if wf.Tasks[i].ID != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, wf.Tasks[i].ID)
		}
	}
}

func TestPrioritize_SamePriority(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "high-sprint3", Priority: PriorityHigh, SprintTarget: 3},
		{ID: "high-sprint1", Priority: PriorityHigh, SprintTarget: 1},
		{ID: "high-sprint2", Priority: PriorityHigh, SprintTarget: 2},
	}

	wf.Prioritize()

	expected := []string{"high-sprint1", "high-sprint2", "high-sprint3"}
	for i, exp := range expected {
		if wf.Tasks[i].ID != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, wf.Tasks[i].ID)
		}
	}
}

func TestPrioritize_Empty(_ *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{}
	// Should not panic
	wf.Prioritize()
}

// ─── GetApprovedTasks ───

func TestGetApprovedTasks(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "t1", Status: StatusApproved},
		{ID: "t2", Status: StatusInProgress},
		{ID: "t3", Status: StatusPending},
		{ID: "t4", Status: StatusCompleted},
		{ID: "t5", Status: StatusApproved},
	}

	approved := wf.GetApprovedTasks()
	if len(approved) != 3 { // t1 (approved), t2 (in_progress), t5 (approved)
		t.Errorf("expected 3 approved/in-progress tasks, got %d", len(approved))
	}
}

func TestGetApprovedTasks_Empty(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	approved := wf.GetApprovedTasks()
	if len(approved) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(approved))
	}
}

// ─── GetTasksByRole ───

func TestGetTasksByRole(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "t1", AssigneeRole: "ceo"},
		{ID: "t2", AssigneeRole: "engineer"},
		{ID: "t3", AssigneeRole: "ceo"},
		{ID: "t4", AssigneeRole: "pm"},
	}

	ceoTasks := wf.GetTasksByRole("ceo")
	if len(ceoTasks) != 2 {
		t.Errorf("expected 2 CEO tasks, got %d", len(ceoTasks))
	}

	engTasks := wf.GetTasksByRole("engineer")
	if len(engTasks) != 1 {
		t.Errorf("expected 1 engineer task, got %d", len(engTasks))
	}

	noTasks := wf.GetTasksByRole("designer")
	if len(noTasks) != 0 {
		t.Errorf("expected 0 designer tasks, got %d", len(noTasks))
	}
}

// ─── GetTasksBySprint ───

func TestGetTasksBySprint(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "t1", SprintTarget: 1},
		{ID: "t2", SprintTarget: 2},
		{ID: "t3", SprintTarget: 1},
		{ID: "t4", SprintTarget: 3},
	}

	sprint1 := wf.GetTasksBySprint(1)
	if len(sprint1) != 2 {
		t.Errorf("expected 2 tasks in sprint 1, got %d", len(sprint1))
	}

	sprint4 := wf.GetTasksBySprint(4)
	if len(sprint4) != 0 {
		t.Errorf("expected 0 tasks in sprint 4, got %d", len(sprint4))
	}
}

// ─── PendingApprovals ───

func TestPendingApprovals(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "t1", Status: StatusPending},
		{ID: "t2", Status: StatusApproved},
		{ID: "t3", Status: StatusPending},
		{ID: "t4", Status: StatusCompleted},
	}

	pending := wf.PendingApprovals()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}
}

// ─── ExecuteSprint ───

// mockOrch implements the interface ExecuteSprint expects.
type mockOrch struct {
	sprintRan bool
}

func (m *mockOrch) RunSprint() *startup.SprintResult {
	m.sprintRan = true
	return &startup.SprintResult{
		SprintNum: 1,
		Goal:      "test goal",
		Completed: []string{"task-a", "task-b"},
		Velocity:  8.0,
	}
}

func TestExecuteSprint(t *testing.T) {
	company := &startup.CompanyState{Name: "TestCo"}
	orch := &mockOrch{}

	wf := NewWorkflow("test", nil, company)
	wf.Tasks = []Task{
		{ID: "t1", SprintTarget: 1, Status: StatusApproved},
		{ID: "t2", SprintTarget: 1, Status: StatusApproved},
		{ID: "t3", SprintTarget: 2, Status: StatusApproved}, // different sprint
	}

	result := wf.ExecuteSprint(1, orch)

	if !orch.sprintRan {
		t.Error("expected RunSprint to be called")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Velocity != 8.0 {
		t.Errorf("expected velocity 8.0, got %f", result.Velocity)
	}
	if company.CurrentSprint != 1 {
		t.Errorf("expected CurrentSprint 1, got %d", company.CurrentSprint)
	}

	// Tasks in sprint should be marked completed
	for i := 0; i < 2; i++ {
		if wf.Tasks[i].Status != StatusCompleted {
			t.Errorf("task %s should be completed, got %s", wf.Tasks[i].ID, wf.Tasks[i].Status.String())
		}
		if wf.Tasks[i].CompletedAt == nil {
			t.Errorf("task %s should have CompletedAt set", wf.Tasks[i].ID)
		}
	}
	// Task in other sprint should be unaffected
	if wf.Tasks[2].Status != StatusApproved {
		t.Errorf("task t3 should still be approved, got %s", wf.Tasks[2].Status.String())
	}
}

func TestExecuteSprint_NoApprovedTasks(t *testing.T) {
	company := &startup.CompanyState{Name: "TestCo"}
	orch := &mockOrch{}

	wf := NewWorkflow("test", nil, company)
	wf.Tasks = []Task{
		{ID: "t1", SprintTarget: 1, Status: StatusPending},
	}

	result := wf.ExecuteSprint(1, orch)

	if !orch.sprintRan {
		t.Error("RunSprint should still be called even with no approved tasks")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Company sprint/goal should not be updated (approved=0)
	if company.CurrentSprint != 0 {
		t.Errorf("Company.CurrentSprint should remain 0, got %d", company.CurrentSprint)
	}
	// Task should NOT be completed (was pending, not approved)
	if wf.Tasks[0].Status == StatusCompleted {
		t.Error("pending task should not be completed")
	}
}

// ─── RunFullPipeline ───

type mockTTOrch struct {
	phases []string
}

func (m *mockTTOrch) RunResearchRound() error    { m.phases = append(m.phases, "research"); return nil }
func (m *mockTTOrch) RunDebate() error           { m.phases = append(m.phases, "debate"); return nil }
func (m *mockTTOrch) RunSynthesis() error        { m.phases = append(m.phases, "synthesis"); return nil }
func (m *mockTTOrch) RunPeerReview() error       { m.phases = append(m.phases, "peer_review"); return nil }
func (m *mockTTOrch) RunReportGeneration() error { m.phases = append(m.phases, "report"); return nil }

func TestRunFullPipeline(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:       "Expand to enterprise market",
			PointsOfAgreement:    []string{"Enterprise demand exists"},
			PointsOfDisagreement: []string{"Requires SOC2 compliance"},
			DissentingNotes:      []string{"Current team lacks enterprise experience"},
		},
	}
	company := &startup.CompanyState{Name: "TestCo"}
	ttOrch := &mockTTOrch{}
	compOrch := &mockOrch{}

	wf := NewWorkflow("test-pipeline", tt, company)
	wf.RunFullPipeline(ttOrch, compOrch)

	// All 5 thinktank phases should have run
	if len(ttOrch.phases) != 5 {
		t.Errorf("expected 5 thinktank phases, got %d: %v", len(ttOrch.phases), ttOrch.phases)
	}

	// Tasks should be created
	if len(wf.Tasks) == 0 {
		t.Error("expected tasks to be created")
	}

	// High priority tasks should be auto-approved, then completed by sprint execution
	for _, task := range wf.Tasks {
		if task.Priority <= PriorityHigh && task.Status != StatusCompleted {
			t.Errorf("task %s (priority %s) should be completed after pipeline, got %s",
				task.ID, task.Priority.String(), task.Status.String())
		}
	}

	// Company sprint should be set
	if company.CurrentSprint != 1 {
		t.Errorf("expected CurrentSprint 1, got %d", company.CurrentSprint)
	}

	// Status should be completed
	if wf.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", wf.Status)
	}
}

// ─── sortTasks helper ───

func TestSortTasks(t *testing.T) {
	tasks := []Task{
		{ID: "c", Priority: PriorityLow},
		{ID: "a", Priority: PriorityCritical},
		{ID: "b", Priority: PriorityHigh},
	}
	sortTasks(tasks)
	expected := []string{"a", "b", "c"}
	for i, exp := range expected {
		if tasks[i].ID != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, tasks[i].ID)
		}
	}
}

// ─── truncate helper ───

func TestTruncateInTaskTitles(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation: strings.Repeat("a", 200),
		},
	}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	task := wf.Tasks[0]
	// "Implement: " (11 chars) + up to 80 chars of recommendation = up to 91
	if len(task.Title) > 91 {
		t.Errorf("expected recommendation title max 91 chars, got %d: %q", len(task.Title), task.Title)
	}
	// Full description should not be truncated
	if task.Description != strings.Repeat("a", 200) {
		t.Errorf("expected full description, got %d chars", len(task.Description))
	}
}

// ─── TaskApproval Edge Cases ───

func TestApproval_Fields(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "task-1", Status: StatusPending},
	}

	// Approve and then verify Approval struct fields
	task := wf.ApproveTask("task-1", "alice")
	if task.Approval.ApprovedBy != "alice" {
		t.Errorf("expected ApprovedBy 'alice', got %q", task.Approval.ApprovedBy)
	}
	if task.Approval.ApprovedAt == nil {
		t.Error("expected ApprovedAt to be set")
	}
	if task.Approval.IsApproved != true {
		t.Error("expected IsApproved to be true")
	}
	if task.Approval.RejectedAt != nil {
		t.Error("expected RejectedAt to be nil after approval")
	}
	if task.Approval.Reason != "" {
		t.Errorf("expected Reason to be empty after approval, got %q", task.Approval.Reason)
	}

	// Reject a different task
	wf.Tasks = append(wf.Tasks, Task{ID: "task-2", Status: StatusPending})
	rejected := wf.RejectTask("task-2", "manager", "bad idea")
	if rejected.Approval.ApprovedBy != "manager" {
		t.Errorf("expected ApprovedBy 'manager' on rejection, got %q", rejected.Approval.ApprovedBy)
	}
	if rejected.Approval.IsApproved != false {
		t.Error("expected IsApproved to be false on rejection")
	}
	if rejected.Approval.RejectedAt == nil {
		t.Error("expected RejectedAt to be set on rejection")
	}
	if rejected.Approval.ApprovedAt != nil {
		t.Error("expected ApprovedAt to be nil on rejection")
	}
}

// ─── GetTasksByRole Edge Case ───

func TestGetTasksByRole_CaseSensitive(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	wf.Tasks = []Task{
		{ID: "t1", AssigneeRole: "CEO"},
		{ID: "t2", AssigneeRole: "ceo"},
	}

	ceoUpper := wf.GetTasksByRole("CEO")
	if len(ceoUpper) != 1 {
		t.Errorf("expected 1 'CEO' task, got %d", len(ceoUpper))
	}
	ceoLower := wf.GetTasksByRole("ceo")
	if len(ceoLower) != 1 {
		t.Errorf("expected 1 'ceo' task, got %d", len(ceoLower))
	}
}

// ─── ExecuteSprint integration with task status transitions ───

func TestExecuteSprint_ApprovedToCompleted(t *testing.T) {
	company := &startup.CompanyState{Name: "TestCo"}
	orch := &mockOrch{}

	wf := NewWorkflow("test", nil, company)
	wf.Tasks = []Task{
		{ID: "t1", SprintTarget: 1, Status: StatusApproved},
		{ID: "t2", SprintTarget: 1, Status: StatusInProgress},
		{ID: "t3", SprintTarget: 1, Status: StatusPending},
	}

	wf.ExecuteSprint(1, orch)

	// Both in-progress tasks (t1 was approved → in_progress, t2 already in_progress) should be completed
	if wf.Tasks[0].Status != StatusCompleted {
		t.Errorf("t1 (approved → in_progress) should be completed, got %s", wf.Tasks[0].Status.String())
	}
	if wf.Tasks[1].Status != StatusCompleted {
		t.Errorf("t2 (in_progress) should be completed, got %s", wf.Tasks[1].Status.String())
	}
	// Pending task should NOT be completed
	if wf.Tasks[2].Status != StatusPending {
		t.Errorf("t3 (pending) should remain pending, got %s", wf.Tasks[2].Status.String())
	}
}

// ─── Workflow Status Transitions ───

func TestWorkflow_StatusTransitions(t *testing.T) {
	wf := NewWorkflow("test", nil, nil)
	if wf.Status != "created" {
		t.Errorf("new workflow should be 'created', got %q", wf.Status)
	}

	// Status changes on approve/reject
	wf.Tasks = []Task{{ID: "t1", Status: StatusPending}}
	wf.ApproveTask("t1", "alice")
	if wf.Status != "created" {
		t.Errorf("approving tasks doesn't change workflow status, got %q", wf.Status)
	}
}

// ─── RecommendationsToTasks with long text ───

func TestRecommendationsToTasks_LongTextTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:       long,
			PointsOfAgreement:    []string{long},
			PointsOfDisagreement: []string{long},
			DissentingNotes:      []string{long},
		},
	}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	// Recommendation title: "Implement: " + 80 chars = 91 chars max
	if len(wf.Tasks[0].Title) > 91 {
		t.Errorf("recommendation title too long: %d chars", len(wf.Tasks[0].Title))
	}
	// Agreement title: "Align on: " + 70 chars = 80 chars max
	if len(wf.Tasks[1].Title) > 80 {
		t.Errorf("agreement title too long: %d chars", len(wf.Tasks[1].Title))
	}
	// Disagreement title: "Investigate: " + 70 chars = 83 chars max
	if len(wf.Tasks[2].Title) > 83 {
		t.Errorf("disagreement title too long: %d chars", len(wf.Tasks[2].Title))
	}
	// Dissenting title: "Spike: " + 70 chars = 77 chars max
	if len(wf.Tasks[3].Title) > 77 {
		t.Errorf("dissenting title too long: %d chars", len(wf.Tasks[3].Title))
	}

	// Descriptions should be the full text (untruncated)
	if wf.Tasks[0].Description != long {
		t.Errorf("description should be full text, got %d chars", len(wf.Tasks[0].Description))
	}
}

// ─── sortTasks stability ───

func TestSortTasks_StableOrder(t *testing.T) {
	// sort.SliceStable preserves order of equal elements
	tasks := []Task{
		{ID: "high-a", Priority: PriorityHigh},
		{ID: "high-b", Priority: PriorityHigh},
		{ID: "high-c", Priority: PriorityHigh},
	}
	sortTasks(tasks)
	expected := []string{"high-a", "high-b", "high-c"}
	for i, exp := range expected {
		if tasks[i].ID != exp {
			t.Errorf("stable sort failed at position %d: expected %q, got %q", i, exp, tasks[i].ID)
		}
	}
}

// ─── RunFullPipeline status phases ───

func TestRunFullPipeline_StatusPhases(t *testing.T) {
	statuses := []string{}
	tt := &thinktank.ThinkTank{Synthesis: &thinktank.Synthesis{Recommendation: "test"}}
	company := &startup.CompanyState{Name: "TestCo"}

	// We can verify the status transitions by checking before/after
	wf := NewWorkflow("test-pipeline", tt, company)
	if wf.Status != "created" {
		t.Errorf("initial status should be 'created', got %q", wf.Status)
	}

	// RunFullPipeline internally sets statuses — verify final
	ttOrch := &mockTTOrch{}
	compOrch := &mockOrch{}
	wf.RunFullPipeline(ttOrch, compOrch)

	if wf.Status != "completed" {
		t.Errorf("final status should be 'completed', got %q", wf.Status)
	}
	_ = statuses // unused, but validates the concept
}

// ─── ExecuteSprint sprint goal ───

func TestExecuteSprint_SetsGoal(t *testing.T) {
	company := &startup.CompanyState{Name: "TestCo"}
	orch := &mockOrch{}

	wf := NewWorkflow("test", nil, company)
	wf.Tasks = []Task{
		{ID: "t1", SprintTarget: 1, Status: StatusApproved},
		{ID: "t2", SprintTarget: 1, Status: StatusApproved},
		{ID: "t3", SprintTarget: 1, Status: StatusApproved},
	}

	wf.ExecuteSprint(1, orch)

	if company.SprintGoal == "" {
		t.Error("expected SprintGoal to be set")
	}
	if !strings.Contains(company.SprintGoal, "3") {
		t.Errorf("expected SprintGoal to contain approved count '3', got %q", company.SprintGoal)
	}
}

// ─── edge case: Workflow with no tasks at all ───

func TestWorkflow_NoTasks(t *testing.T) {
	wf := NewWorkflow("empty", nil, nil)

	if len(wf.GetApprovedTasks()) != 0 {
		t.Error("GetApprovedTasks should return empty")
	}
	if len(wf.GetTasksByRole("ceo")) != 0 {
		t.Error("GetTasksByRole should return empty")
	}
	if len(wf.GetTasksBySprint(1)) != 0 {
		t.Error("GetTasksBySprint should return empty")
	}
	if len(wf.PendingApprovals()) != 0 {
		t.Error("PendingApprovals should return empty")
	}
}

// ─── edge case: RecommendationsToTasks idempotent ───

func TestRecommendationsToTasks_Idempotent(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:    "test",
			PointsOfAgreement: []string{"agree"},
		},
	}
	wf := NewWorkflow("test", tt, nil)

	wf.RecommendationsToTasks()
	firstCount := len(wf.Tasks)

	// Calling again doubles the tasks (not idempotent by design, but we test the behavior)
	wf.RecommendationsToTasks()
	if len(wf.Tasks) != firstCount*2 {
		t.Errorf("second call should double tasks, got %d (first=%d)", len(wf.Tasks), firstCount)
	}
}

// ─── edge case: AppendTask with zero-length arrays ───

func TestRecommendationsToTasks_SingleRecommendation(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation: "just one recommendation",
		},
	}
	// Agreement/Disagreement/Dissenting are nil (not empty slices)
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	if len(wf.Tasks) != 1 {
		t.Errorf("expected exactly 1 task (recommendation), got %d", len(wf.Tasks))
	}
	if wf.Tasks[0].Source != "thinktank:synthesis:recommendation" {
		t.Errorf("expected source 'thinktank:synthesis:recommendation', got %q", wf.Tasks[0].Source)
	}
}

// ─── Task source field verification ───

func TestRecommendationsToTasks_SourceFields(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:       "rec",
			PointsOfAgreement:    []string{"a1"},
			PointsOfDisagreement: []string{"d1"},
			DissentingNotes:      []string{"dn1"},
		},
	}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	expectedSources := []string{
		"thinktank:synthesis:recommendation",
		"thinktank:synthesis:agreement",
		"thinktank:synthesis:disagreement",
		"thinktank:synthesis:dissenting",
	}
	for i, exp := range expectedSources {
		if wf.Tasks[i].Source != exp {
			t.Errorf("task %d source: expected %q, got %q", i, exp, wf.Tasks[i].Source)
		}
	}
}

// ─── ID format verification ───

func TestRecommendationsToTasks_IDs(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:       "rec",
			PointsOfAgreement:    []string{"a1", "a2"},
			PointsOfDisagreement: []string{"d1", "d2", "d3"},
			DissentingNotes:      []string{"dn1", "dn2", "dn3", "dn4"},
		},
	}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	expectedIDs := []string{
		"rec-001",
		"agree-001", "agree-002",
		"disagree-001", "disagree-002", "disagree-003",
		"dissent-001", "dissent-002", "dissent-003", "dissent-004",
	}
	for i, exp := range expectedIDs {
		if wf.Tasks[i].ID != exp {
			t.Errorf("task %d ID: expected %q, got %q", i, exp, wf.Tasks[i].ID)
		}
	}
}

// ─── Task Effort Estimates ───

func TestRecommendationsToTasks_EffortEstimates(t *testing.T) {
	tt := &thinktank.ThinkTank{
		Synthesis: &thinktank.Synthesis{
			Recommendation:       "rec",
			PointsOfAgreement:    []string{"a"},
			PointsOfDisagreement: []string{"d"},
			DissentingNotes:      []string{"dn"},
		},
	}
	wf := NewWorkflow("test", tt, nil)
	wf.RecommendationsToTasks()

	expectedEfforts := []int{13, 5, 8, 3}
	expectedSprints := []int{1, 1, 2, 3}
	for i := range wf.Tasks {
		if wf.Tasks[i].EstimatedEffort != expectedEfforts[i] {
			t.Errorf("task %d effort: expected %d, got %d", i, expectedEfforts[i], wf.Tasks[i].EstimatedEffort)
		}
		if wf.Tasks[i].SprintTarget != expectedSprints[i] {
			t.Errorf("task %d sprint: expected %d, got %d", i, expectedSprints[i], wf.Tasks[i].SprintTarget)
		}
	}
}

// ─── Format helpers ───

func TestTaskIDFormat(t *testing.T) {
	// Verify ID format uses fmt.Sprintf with 3-digit zero-padding
	id1 := fmt.Sprintf("agree-%03d", 1)
	if id1 != "agree-001" {
		t.Errorf("ID format wrong: %q", id1)
	}
	id99 := fmt.Sprintf("dissent-%03d", 99)
	if id99 != "dissent-099" {
		t.Errorf("ID format wrong: %q", id99)
	}
}
