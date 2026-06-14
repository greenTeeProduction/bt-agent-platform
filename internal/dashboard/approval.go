package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/hitl"
)

// WorkflowApprovalWait blocks until a workflow approval step is approved via HITL.
func WorkflowApprovalWait(ctx context.Context, step Step, state *wfState) (ApprovalWaitResult, error) {
	store := hitl.DefaultStore
	if store == nil {
		return ApprovalWaitResult{}, fmt.Errorf("HITL store not initialized")
	}

	prompt := expandTemplate(step.Input, state)
	proposed := state.input
	taskID := WorkflowApprovalTaskID(state.workflow, step.ID, state.runID)

	meta := map[string]any{
		"task_id":  taskID,
		"workflow": state.workflow,
		"step_id":  step.ID,
	}
	req := hitl.NewRequest(step.ID, "WorkflowApproval", prompt, "", proposed, prompt, meta)
	req = hitl.ApplyAutoApproveIfPolicy(req)
	result := ApprovalWaitResult{TaskID: taskID, RequestID: req.ID}
	if req.Status == hitl.StatusSkipped || req.Status == hitl.StatusApproved {
		result.Approved = true
		return result, nil
	}
	if err := store.Create(req); err != nil {
		return result, err
	}

	waitCtx, cancel := stepContext(ctx, step.Timeout)
	defer cancel()
	if _, hasDeadline := waitCtx.Deadline(); !hasDeadline {
		pol := hitl.GetPolicy()
		if pol.Timeout > 0 {
			var c context.CancelFunc
			waitCtx, c = context.WithTimeout(waitCtx, pol.Timeout)
			defer c()
		}
	}

	_, err := store.WaitForRequest(waitCtx, req.ID, 500*time.Millisecond)
	if err != nil {
		if err == context.DeadlineExceeded || waitCtx.Err() == context.DeadlineExceeded {
			return result, context.DeadlineExceeded
		}
		return result, err
	}
	result.Approved = true
	return result, nil
}

// WorkflowApprovalTaskID builds the stable HITL task id for a workflow approval step.
func WorkflowApprovalTaskID(workflow, stepID, runID string) string {
	if workflow == "" {
		workflow = "workflow"
	}
	if runID == "" {
		runID = "run"
	}
	return fmt.Sprintf("wf:%s:%s:%s", workflow, stepID, runID)
}
