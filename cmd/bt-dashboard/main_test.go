package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// TestDashboardDriftWatcherRebuildsItself pins — at the source level, the
// same audit style as cmd/bt-agent/main_test.go's requireBuildIdentityWiring
// — that bt-dashboard's deploy-drift watcher can actually rebuild its own
// binary, not just detect that it has drifted from repo HEAD.
//
// agent.DefaultRebuildTargets deliberately excludes bt-dashboard (its doc
// comment: "bt-dashboard and the MCP bin/bt-agent are intentionally excluded
// here — callers pass the set they own"), so passing it unmodified as
// Targets means an AutoRebuild-enabled bt-dashboard WARNs on its own drift
// but the rebuild it triggers only ever swaps bt-agent/bt-agent-cli/
// bt-gardener — never itself. main.go must instead pass a target list that
// includes bt-dashboard's own binary.
func TestDashboardDriftWatcherRebuildsItself(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "agent.DashboardRebuildTargets(repoDir)") {
		t.Errorf("main.go's deploy-drift watcher must pass agent.DashboardRebuildTargets(repoDir) as " +
			"Targets so bt-dashboard rebuilds its own binary on drift; found agent.DefaultRebuildTargets " +
			"(or equivalent) which intentionally excludes bt-dashboard")
	}
}

// TestDashboardDriftWatcherWiresRebuildBackoff pins — the same audit style as
// TestDashboardDriftWatcherRebuildsItself above — that bt-dashboard's
// deploy-drift watcher sets a RebuildBackoff guard so a broken HEAD cannot
// retry-storm `go build` every watcher tick (ADR-045 milestone 4, currently
// unwired per arc42 §Deploy Drift, 2026-07-12).
func TestDashboardDriftWatcherWiresRebuildBackoff(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "Backoff:") {
		t.Error("main.go's deploy-drift watcher must wire a RebuildBackoff (Backoff:); not found")
	}
}

// TestHandleScalability_ReflectsInjectedQueueAndRouter pins milestone 3/5 of the
// horizontal-scaling adoption program: the /api/scalability endpoint must surface
// the injected TaskQueue depth and AgentRouter executor health instead of the
// hardcoded 0/nil placeholders that leave Queue and Router omitted from the JSON.
func TestHandleScalability_ReflectsInjectedQueueAndRouter(t *testing.T) {
	// Preserve and restore the package globals this test mutates.
	origQueue := dashTaskQueue
	origRouter := dashAgentRouter
	t.Cleanup(func() {
		dashTaskQueue = origQueue
		dashAgentRouter = origRouter
	})

	// Inject a task queue carrying two pending items.
	q := reliability.NewTaskQueue(filepath.Join(t.TempDir(), "queue.json"))
	q.Enqueue("task-a")
	q.Enqueue("task-b")
	dashTaskQueue = q

	// Inject an agent router holding a single healthy local executor.
	local := reliability.NewLocalExecutor("local-test", func(agent, task string) (*reliability.AgentResult, error) {
		return &reliability.AgentResult{Agent: agent, Success: true}, nil
	})
	dashAgentRouter = reliability.NewAgentRouter(local)

	req := httptest.NewRequest(http.MethodGet, "/api/scalability", nil)
	rr := httptest.NewRecorder()
	handleScalability(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var status reliability.ScalabilityStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	if status.Queue == nil {
		t.Fatalf("Queue is nil; endpoint still ignores the injected TaskQueue. body=%s", rr.Body.String())
	}
	if status.Queue.Pending != 2 {
		t.Errorf("Queue.Pending = %d, want 2 (injected queue depth)", status.Queue.Pending)
	}

	if status.Router == nil {
		t.Fatalf("Router is nil; endpoint still ignores the injected AgentRouter. body=%s", rr.Body.String())
	}
	if status.Router.Total != 1 {
		t.Errorf("Router.Total = %d, want 1 (injected executor count)", status.Router.Total)
	}
	if status.Router.Healthy != 1 {
		t.Errorf("Router.Healthy = %d, want 1 (single healthy local executor)", status.Router.Healthy)
	}
}

// TestHandleDLQReplay_RequeuesInsteadOfDropping pins milestone 4/5 of the
// drop-safe-DLQ program. The dashboard runs in a separate process from bt-agent
// and has no tree runner of its own, so it cannot actually execute a replayed
// task. The old handler called dlq.Replay(id), which REMOVES the entry from the
// queue and persists the removal — a cross-process silent drop: bt-agent's
// executor never sees the entry again and the task is lost.
//
// The fixed handler must instead reload the DLQ from disk and mark the entry for
// retry (RequeuedAt) without removing it, so the entry survives on disk and
// bt-agent's executor picks it up on its next scan.
func TestHandleDLQReplay_RequeuesInsteadOfDropping(t *testing.T) {
	origDLQ := dlq
	t.Cleanup(func() { dlq = origDLQ })

	dlqPath := filepath.Join(t.TempDir(), "dead_letter_queue.json")
	dlq = reliability.NewDeadLetterQueue(dlqPath)
	dlq.Push(reliability.DeadLetterEntry{
		ID:    "dead-1",
		Task:  "rebuild-index",
		Agent: "indexer",
		Error: "boom",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/dlq/replay?id=dead-1", nil)
	rr := httptest.NewRecorder()
	handleDLQReplay(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Reload from disk to assert the cross-process contract: the entry must
	// survive the replay rather than being silently dropped.
	entries := reliability.NewDeadLetterQueue(dlqPath).List()
	if len(entries) != 1 {
		t.Fatalf("entries on disk after replay = %d, want 1 (dashboard silently dropped the entry)", len(entries))
	}
	if entries[0].ID != "dead-1" {
		t.Fatalf("surviving entry ID = %q, want dead-1", entries[0].ID)
	}

	// The surviving entry must be flagged for retry so bt-agent's executor
	// requeues it. A zero RequeuedAt means it was left untouched (never picked up).
	if entries[0].RequeuedAt.IsZero() {
		t.Errorf("RequeuedAt is zero; entry was not flagged for retry")
	}
}

// TestHandleTaskApproveReject_EscalatedVsPending pins milestone 4/4 of the
// stop-HITL-escalation-from-silently-auto-approving program. handleTaskApprove
// and handleTaskReject call hitl.DefaultStore.ApproveByTaskID/RejectByTaskID,
// which already resolve both StatusPending and StatusEscalated requests (see
// internal/hitl/store_extensions.go), but the HTTP response never tells the
// caller which case it was: a request that had been escalated to a human
// operator resolves silently the same way a routine pending approval does,
// and a task with no matching HITL request at all gets no signal either — the
// hitl.DefaultStore error is swallowed outright. The response must surface
// "hitl_resolved_from" (escalated vs pending) on success and "hitl_note" (the
// underlying "no pending request for task" message) when no request is found.
func TestHandleTaskApproveReject_EscalatedVsPending(t *testing.T) {
	origTaskStore := taskStore
	origHitl := hitl.DefaultStore
	t.Cleanup(func() {
		taskStore = origTaskStore
		hitl.DefaultStore = origHitl
	})

	tests := []struct {
		name             string
		action           string // "approve" or "reject"
		startEscalated   bool
		noHitlRequest    bool
		wantTaskStatus   string
		wantResolvedFrom string
		wantNoteContains string
	}{
		{name: "approve pending request", action: "approve", startEscalated: false, wantTaskStatus: "approved", wantResolvedFrom: "pending"},
		{name: "approve escalated request", action: "approve", startEscalated: true, wantTaskStatus: "approved", wantResolvedFrom: "escalated"},
		{name: "reject pending request", action: "reject", startEscalated: false, wantTaskStatus: "rejected", wantResolvedFrom: "pending"},
		{name: "reject escalated request", action: "reject", startEscalated: true, wantTaskStatus: "rejected", wantResolvedFrom: "escalated"},
		{name: "approve task with no hitl request", action: "approve", noHitlRequest: true, wantTaskStatus: "approved", wantNoteContains: "no pending request for task"},
		{name: "reject task with no hitl request", action: "reject", noHitlRequest: true, wantTaskStatus: "rejected", wantNoteContains: "no pending request for task"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))
			store, err := hitl.InitStore(filepath.Join(dir, "hitl"))
			if err != nil {
				t.Fatalf("InitStore: %v", err)
			}

			taskID := "task-" + strings.ReplaceAll(tc.name, " ", "-")
			if err := taskStore.Create(dashboard.Task{ID: taskID, Title: "t"}); err != nil {
				t.Fatalf("taskStore.Create: %v", err)
			}

			var wantReqID string
			if !tc.noHitlRequest {
				req := hitl.NewRequest("N", "HumanApprovalGate", "body", "", "", "p", map[string]any{"task_id": taskID})
				if err := store.Create(req); err != nil {
					t.Fatalf("hitl store.Create: %v", err)
				}
				wantReqID = req.ID
				if tc.startEscalated {
					if _, err := store.Escalate(req.ID, "ops", "needs review"); err != nil {
						t.Fatalf("Escalate: %v", err)
					}
				}
			}

			var rr *httptest.ResponseRecorder
			switch tc.action {
			case "approve":
				httpReq := httptest.NewRequest(http.MethodPost, "/api/task/approve?id="+taskID, nil)
				rr = httptest.NewRecorder()
				handleTaskApprove(rr, httpReq)
			case "reject":
				httpReq := httptest.NewRequest(http.MethodPost, "/api/task/reject?id="+taskID, nil)
				rr = httptest.NewRecorder()
				handleTaskReject(rr, httpReq)
			default:
				t.Fatalf("unknown action %q", tc.action)
			}

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
			}

			var resp map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
			}

			if resp["status"] != tc.wantTaskStatus {
				t.Errorf("status = %q, want %q; full resp=%v", resp["status"], tc.wantTaskStatus, resp)
			}

			if tc.noHitlRequest {
				if !strings.Contains(resp["hitl_note"], tc.wantNoteContains) {
					t.Errorf("hitl_note = %q, want it to contain %q; full resp=%v", resp["hitl_note"], tc.wantNoteContains, resp)
				}
				if resp["hitl_request_id"] != "" {
					t.Errorf("hitl_request_id = %q, want empty since no request existed", resp["hitl_request_id"])
				}
				return
			}

			if resp["hitl_request_id"] != wantReqID {
				t.Errorf("hitl_request_id = %q, want %q", resp["hitl_request_id"], wantReqID)
			}
			if resp["hitl_resolved_from"] != tc.wantResolvedFrom {
				t.Errorf("hitl_resolved_from = %q, want %q (must distinguish an escalated request from a routine pending one); full resp=%v", resp["hitl_resolved_from"], tc.wantResolvedFrom, resp)
			}
		})
	}
}

// TestHandleAnalyze_TaskIDsKeyedOnInsightIndex pins milestone 1/3 of the
// dashboard Workflow/Approval wiring program. handleAnalyze mints each
// auto-generated task's ID as
//
//	fmt.Sprintf("tt-%d-%d", time.Now().UnixNano(), len(f.KeyInsights))
//
// via `for _, insight := range f.KeyInsights[:min(2, len(f.KeyInsights))]`.
// The second Sprintf component, len(f.KeyInsights), is CONSTANT across every
// insight minted from the same ResearchFinding — the ID varies only by
// nanosecond timestamp. Two insights minted for the same fellow within the
// same nanosecond tick therefore collide on an identical ID, and
// dashboard.TaskStore's Get/UpdateStatus/SetOutput (internal/dashboard/tasks.go)
// linear-scan and act on the first ID match only — permanently orphaning the
// second task. A real nanosecond collision cannot be reliably forced from a
// black-box test (verified: 0 collisions across 100k back-to-back
// time.Now().UnixNano() calls on this host), so this pins the fix at the
// source level like TestDashboardDriftWatcherRebuildsItself above: the loop
// must capture its own index and use it instead of the constant slice
// length.
func TestHandleAnalyze_TaskIDsKeyedOnInsightIndex(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(src)

	start := strings.Index(source, "func handleAnalyze(")
	if start < 0 {
		t.Fatal("handleAnalyze not found in main.go")
	}
	rest := source[start+len("func handleAnalyze("):]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	body := rest[:end]

	if strings.Contains(body, "for _, insight := range f.KeyInsights") {
		t.Error("handleAnalyze's insight loop discards its index (`for _, insight " +
			":= range f.KeyInsights`); it must capture the index so it can be folded " +
			"into the task ID, e.g. `for idx, insight := range f.KeyInsights[...]`")
	}

	if strings.Contains(body, `fmt.Sprintf("tt-%d-%d", time.Now().UnixNano(), len(f.KeyInsights))`) {
		t.Error("handleAnalyze still mints task IDs from time.Now().UnixNano() + " +
			"len(f.KeyInsights), which is constant across every insight minted from " +
			"the same finding; key the ID on the insight's own loop index instead so " +
			"two insights minted for the same fellow in the same nanosecond tick " +
			"don't collide and permanently orphan one another in TaskStore")
	}
}

// TestHandleAnalyze_UsesWorkflowForTaskDerivation pins milestone 2/4 of the
// dashboard Workflow/Approval wiring program. handleAnalyze's hand-rolled
// loop over tt.ResearchFindings assigns every generated task straight to a
// fellow's name (e.g. "Victoria Bull") and stamps every task with
// companyState.CurrentSprint (12 in startup.NewDefaultCompany's defaults),
// with no synthesis step and no approval workflow.
//
// dashboard.NewWorkflow + RecommendationsToTasks derives tasks from the
// ThinkTank's Synthesis instead: the main recommendation becomes a
// critical, "ceo"-assigned, sprint-1 task (WorkflowTask.AssigneeRole="ceo",
// SprintTarget=1) — independent of any fellow name and of the company's
// current sprint. That requires handleAnalyze to run the synthesis phase,
// build a *dashboard.Workflow, call RecommendationsToTasks + Prioritize,
// and persist the resulting WorkflowTasks (carrying
// AssigneeRole/SprintTarget/Approval) into taskStore instead of building
// dashboard.Task values directly from tt.ResearchFindings.
func TestHandleAnalyze_UsesWorkflowForTaskDerivation(t *testing.T) {
	origTaskStore := taskStore
	origLLM := sharedLLM
	t.Cleanup(func() {
		taskStore = origTaskStore
		sharedLLM = origLLM
	})

	dir := t.TempDir()
	taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))
	sharedLLM = engine.NewMockLLM()

	httpReq := httptest.NewRequest(http.MethodGet, "/api/thinktank/analyze?topic=Should+we+ship+feature+X", nil)
	rr := httptest.NewRecorder()
	handleAnalyze(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	tasks := taskStore.List()

	var recTask *dashboard.Task
	for i := range tasks {
		if tasks[i].Assignee == "ceo" {
			recTask = &tasks[i]
			break
		}
	}
	if recTask == nil {
		assignees := make([]string, 0, len(tasks))
		for _, tk := range tasks {
			assignees = append(assignees, tk.Assignee)
		}
		t.Fatalf("no task assigned to workflow role %q found among %d tasks (assignees: %v); "+
			"handleAnalyze must derive tasks via dashboard.NewWorkflow + RecommendationsToTasks, "+
			"whose main-recommendation task carries AssigneeRole=%q — not a fellow name",
			"ceo", len(tasks), assignees, "ceo")
		return
	}

	if recTask.Sprint != 1 {
		t.Errorf("recommendation task Sprint = %d, want 1 (from WorkflowTask.SprintTarget); "+
			"got companyState.CurrentSprint (%d) instead, meaning tasks are still being stamped "+
			"with the company's current sprint rather than the workflow-derived sprint target",
			recTask.Sprint, companyState.CurrentSprint)
	}

	if recTask.Priority != "critical" {
		t.Errorf("recommendation task Priority = %q, want %q (from WorkflowPriority.String() "+
			"via RecommendationsToTasks)", recTask.Priority, "critical")
	}
}

// TestHandleAnalyze_SurfacesOrchestratorError pins milestone 3/4 of the Q1
// Correctness program: handleAnalyze discards every phase orchestrator error
// (`_ = orch.RunResearchRound()`, `_ = orch.RunDebate()`,
// `_ = orch.RunSynthesis()`) and always proceeds to derive tasks and respond
// as if the analysis succeeded — even when a phase genuinely failed. None of
// orch.Tank/orch.LLM can be forced nil through handleAnalyze's own guards
// (topic sourced from *thinktank.ThinkTank, which thinktank.NewThinkTank
// always populates with fellows, and c is nil-checked before the
// orchestrator is even constructed), so the only way to pin this at HEAD is
// a source-level audit — the same style already used by
// TestDashboardDriftWatcherRebuildsItself and
// TestHandleAnalyze_TaskIDsKeyedOnInsightIndex in this file. handleAnalyze
// must check each phase's returned error and, on failure, respond with an
// explicit non-empty "error" field instead of continuing on to
// dashboard.NewWorkflow/RecommendationsToTasks with empty findings.
func TestHandleAnalyze_SurfacesOrchestratorError(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(src)

	start := strings.Index(source, "func handleAnalyze(")
	if start < 0 {
		t.Fatal("handleAnalyze not found in main.go")
	}
	rest := source[start+len("func handleAnalyze("):]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	body := rest[:end]

	for _, discard := range []string{
		"_ = orch.RunResearchRound()",
		"_ = orch.RunDebate()",
		"_ = orch.RunSynthesis()",
	} {
		if strings.Contains(body, discard) {
			t.Errorf("handleAnalyze still discards the orchestrator's returned error via %q; "+
				"it must check the error and, on failure, respond with an explicit \"error\" field "+
				"instead of proceeding to derive tasks from empty findings", discard)
		}
	}

	if !strings.Contains(body, "err != nil") {
		t.Error("handleAnalyze must check the orchestrator phase calls' returned error (err != nil) " +
			"and surface a failure response instead of silently treating a failed analysis as a success")
	}
}

// TestHandleWorkflowApprovalEndpoints pins milestone 4/4 of the Q1 Correctness
// dashboard Workflow/Approval wiring program. internal/dashboard/workflow_engine.go's
// Workflow.PendingApprovals/ApproveTask/RejectTask are fully tested in
// workflow_engine_test.go but have zero callers outside that file (confirmed via
// git grep for ".ApproveTask(" / ".RejectTask(" / ".PendingApprovals("): handleAnalyze
// builds a *dashboard.Workflow locally, copies its derived WorkflowTasks into
// taskStore as plain dashboard.Task values, and lets the Workflow itself fall out of
// scope, so nothing in cmd/bt-dashboard/main.go can ever call these three methods.
//
// This pins the dashboard-level contract the fix must satisfy: handleAnalyze retains
// the *dashboard.Workflow it builds in the package-level `currentWorkflow` var (mirroring
// how taskStore/companyState are already held as package vars), and three new handlers —
// handleWorkflowPending/handleWorkflowApprove/handleWorkflowReject, registered alongside
// the existing /api/tasks/approve and /api/tasks/reject routes per main.go's mux setup —
// operate on it directly, proving the Workflow-level approval gate is reachable over HTTP
// instead of existing only in unit tests.
func TestHandleWorkflowApprovalEndpoints(t *testing.T) {
	origWorkflow := currentWorkflow
	t.Cleanup(func() { currentWorkflow = origWorkflow })

	wf := dashboard.NewWorkflow("test-wf", nil, nil)
	wf.Tasks = []dashboard.WorkflowTask{
		{ID: "task-a", Status: dashboard.StatusPending, Priority: dashboard.PriorityHigh},
		{ID: "task-b", Status: dashboard.StatusPending, Priority: dashboard.PriorityMedium},
	}
	currentWorkflow = wf

	// /api/workflow/pending must surface every WorkflowTask still awaiting a
	// decision, via Workflow.PendingApprovals — not an empty/hardcoded list.
	pendingReq := httptest.NewRequest(http.MethodGet, "/api/workflow/pending", nil)
	pendingRR := httptest.NewRecorder()
	handleWorkflowPending(pendingRR, pendingReq)
	if pendingRR.Code != http.StatusOK {
		t.Fatalf("pending status = %d, want 200; body=%s", pendingRR.Code, pendingRR.Body.String())
	}
	var pending []dashboard.WorkflowTask
	if err := json.Unmarshal(pendingRR.Body.Bytes(), &pending); err != nil {
		t.Fatalf("decode pending response: %v; body=%s", err, pendingRR.Body.String())
	}
	if len(pending) != 2 {
		t.Fatalf("pending count = %d, want 2 (both seeded tasks, since neither has been decided yet)", len(pending))
	}

	// /api/workflow/approve must call Workflow.ApproveTask on the retained Workflow.
	approveReq := httptest.NewRequest(http.MethodPost, "/api/workflow/approve?id=task-a", nil)
	approveRR := httptest.NewRecorder()
	handleWorkflowApprove(approveRR, approveReq)
	if approveRR.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body=%s", approveRR.Code, approveRR.Body.String())
	}
	if !currentWorkflow.Tasks[0].Approval.IsApproved {
		t.Errorf("task-a Approval.IsApproved = false after /api/workflow/approve; handler must call Workflow.ApproveTask")
	}
	if currentWorkflow.Tasks[0].Status != dashboard.StatusApproved {
		t.Errorf("task-a Status = %s after /api/workflow/approve, want %s",
			currentWorkflow.Tasks[0].Status.String(), dashboard.StatusApproved.String())
	}

	// /api/workflow/reject must call Workflow.RejectTask on the retained Workflow.
	rejectReq := httptest.NewRequest(http.MethodPost, "/api/workflow/reject?id=task-b&reason=not+now", nil)
	rejectRR := httptest.NewRecorder()
	handleWorkflowReject(rejectRR, rejectReq)
	if rejectRR.Code != http.StatusOK {
		t.Fatalf("reject status = %d, want 200; body=%s", rejectRR.Code, rejectRR.Body.String())
	}
	if currentWorkflow.Tasks[1].Approval.IsApproved {
		t.Errorf("task-b Approval.IsApproved = true after /api/workflow/reject; handler must call Workflow.RejectTask")
	}
	if currentWorkflow.Tasks[1].Status != dashboard.StatusRejected {
		t.Errorf("task-b Status = %s after /api/workflow/reject, want %s",
			currentWorkflow.Tasks[1].Status.String(), dashboard.StatusRejected.String())
	}
	if currentWorkflow.Tasks[1].Approval.Reason != "not now" {
		t.Errorf("task-b Approval.Reason = %q, want %q", currentWorkflow.Tasks[1].Approval.Reason, "not now")
	}

	// Both tasks now decided — pending must reflect that instead of staying stale.
	pendingReq2 := httptest.NewRequest(http.MethodGet, "/api/workflow/pending", nil)
	pendingRR2 := httptest.NewRecorder()
	handleWorkflowPending(pendingRR2, pendingReq2)
	var pending2 []dashboard.WorkflowTask
	if err := json.Unmarshal(pendingRR2.Body.Bytes(), &pending2); err != nil {
		t.Fatalf("decode second pending response: %v; body=%s", err, pendingRR2.Body.String())
	}
	if len(pending2) != 0 {
		t.Errorf("pending count after approve+reject = %d, want 0", len(pending2))
	}
}
