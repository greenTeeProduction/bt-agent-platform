// Workflow orchestration for the Go BT framework.
// Supports sequential, parallel, conditional, loop, and human-in-loop patterns.
package dashboard

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

// StepKind defines the type of workflow step.
type StepKind string

const (
	StepAgent       StepKind = "agent"       // Run an agent
	StepCondition   StepKind = "condition"   // Evaluate a condition
	StepParallel    StepKind = "parallel"    // Run multiple agents in parallel
	StepLoop        StepKind = "loop"        // Loop until condition met
	StepApproval    StepKind = "approval"    // Wait for human approval
	StepSubworkflow StepKind = "subworkflow" // Run a sub-workflow
)

// Step is a single step in a workflow.
type Step struct {
	ID            string   `yaml:"id" json:"id"`
	Kind          StepKind `yaml:"kind" json:"kind"`
	Agent         string   `yaml:"agent,omitempty" json:"agent,omitempty"`                  // agent name for agent step
	Input         string   `yaml:"input,omitempty" json:"input,omitempty"`                  // task input (supports {{.prev.output}})
	Condition     string   `yaml:"condition,omitempty" json:"condition,omitempty"`          // Go template: "{{.prev.output.status}} == 'degraded'"
	MaxIterations int      `yaml:"max_iterations,omitempty" json:"max_iterations,omitzero"` // for loop steps
	Steps         []Step   `yaml:"steps,omitempty" json:"steps,omitempty"`                  // for parallel/subworkflow steps
	Timeout       string   `yaml:"timeout,omitempty" json:"timeout,omitempty"`              // "30s", "5m"
	OnFailure     string   `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`        // "skip", "abort", "retry"
}

// Workflow is a named sequence of steps.
type Pipeline struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Version     string `yaml:"version" json:"version"`
	Steps       []Step `yaml:"steps" json:"steps"`
}

// StepResult captures the output of a single workflow step.
type StepResult struct {
	StepID        string        `json:"step_id"`
	Agent         string        `json:"agent"`
	Outcome       string        `json:"outcome"` // success, failure, skipped, timeout, rejected
	Output        string        `json:"output"`
	Duration      time.Duration `json:"duration"`
	Error         string        `json:"error,omitempty"`
	HitlTaskID    string        `json:"hitl_task_id,omitempty"`
	HitlRequestID string        `json:"hitl_request_id,omitempty"`
	Steps         []StepResult  `json:"steps,omitempty"` // retained container child evidence
}

// WorkflowResult is the complete result of a workflow execution.
type PipelineResult struct {
	Workflow string        `json:"workflow"`
	RunID    string        `json:"run_id,omitempty"`
	Steps    []StepResult  `json:"steps"`
	Outcome  string        `json:"outcome"` // success, failure, partial
	Duration time.Duration `json:"duration"`
}

// Runner executes a workflow by delegating steps to the BT MCP agent.
// It uses a RunnerFunc to execute individual agent steps (injected for testability).
type Runner struct {
	RunID        string // optional external run id (dashboard API); generated if empty
	RunAgent     func(ctx context.Context, agentName, treeID, task string) (outcome, output string, err error)
	WaitApproval func(ctx context.Context, step Step, state *wfState) (ApprovalWaitResult, error)
	Blackboards  *blackboard.Manager // optional: promote step outputs to session scope
}

// ApprovalWaitResult carries HITL identifiers for workflow approval steps.
type ApprovalWaitResult struct {
	Approved  bool
	Escalated bool
	TaskID    string
	RequestID string
}

// Run executes the workflow and returns the result.
func (r *Runner) Run(ctx context.Context, wf Pipeline, initialInput string) (*PipelineResult, error) {
	start := time.Now()
	runID := r.RunID
	if runID == "" {
		runID = fmt.Sprintf("%d", start.UnixNano())
	}

	// Context carries state between steps
	state := &wfState{
		input:    initialInput,
		prev:     make(map[string]StepResult),
		workflow: wf.Name,
		runID:    runID,
	}

	if r.Blackboards != nil && runID != "" && strings.TrimSpace(initialInput) != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: runID}
		if err := r.Blackboards.SetWithContext(ctx, scope, "input", initialInput, "Initial workflow input", "text"); err != nil {
			return &PipelineResult{Workflow: wf.Name, RunID: runID, Steps: []StepResult{}, Outcome: "failure", Duration: time.Since(start)}, fmt.Errorf("persist workflow input before admission: %w", err)
		}
	}

	result, err := r.runSteps(ctx, wf.Steps, state)
	result.Duration = time.Since(start)
	return result, err
}

// runSteps owns sequential policy for top-level, loop and nested workflow bodies.
// Containers must not bypass waits, replay terminal work or erase child evidence.
func (r *Runner) runSteps(ctx context.Context, steps []Step, state *wfState) (result *PipelineResult, err error) {
	start := time.Now()
	defer func() {
		if result != nil {
			result.Outcome, err = completedWorkflowStop(result.Outcome, result.Steps, err)
		}
	}()
	result = &PipelineResult{Workflow: state.workflow, RunID: state.runID, Steps: make([]StepResult, 0, len(steps))}
	for _, step := range steps {
		select {
		case <-ctx.Done():
			result.Outcome = "aborted"
			result.Duration = time.Since(start)
			return result, ctx.Err()
		default:
		}

		var retryState *wfState
		if step.OnFailure == "retry" {
			retryState = state.cloneForParallel()
		}
		sr, err := r.executeStep(ctx, step, state)
		if err != nil {
			if sr.Error == "" {
				sr.Error = err.Error()
			}
			if sr.Outcome == "" {
				sr.Outcome = "failure"
			}
		}
		result.Steps = append(result.Steps, sr)
		state.prev[step.ID] = sr

		// Update state for next step
		state.input = sr.Output

		// Completed-record failures and uncertain execution are terminal,
		// including when on_failure requests retry or skip.
		if reliability.IsExecutionTerminalError(err) {
			result.Outcome = "aborted"
			if reliability.IsExecutionStoppedError(err) {
				result.Outcome = reliability.ExecutionStopOutcome(err)
			}
			result.Duration = time.Since(start)
			return result, err
		}

		// Handle failure (including timeout, rejected, and escalated approval)
		if !reliability.IsHealthyOutcome(sr.Outcome) && sr.Outcome != "skipped" {
			switch step.OnFailure {
			case "skip":
				continue

			case "retry":
				// retry once — replace failed result with retry result
				sr2, err2 := r.executeStep(ctx, step, retryState)
				state.prev = retryState.prev
				if err2 != nil {
					sr2.Error = err2.Error()
					if !reliability.IsExecutionTerminalError(err2) {
						sr2.Outcome = "failure"
					}
				}
				// Replace the failed step in results array
				result.Steps[len(result.Steps)-1] = sr2
				state.prev[step.ID] = sr2
				state.input = sr2.Output
				if reliability.IsExecutionTerminalError(err2) {
					result.Outcome = "aborted"
					if reliability.IsExecutionStoppedError(err2) {
						result.Outcome = reliability.ExecutionStopOutcome(err2)
					}
					result.Duration = time.Since(start)
					return result, err2
				}
				if !reliability.IsHealthyOutcome(sr2.Outcome) && sr2.Outcome != "skipped" {
					result.Outcome = workflowFailureOutcome(sr2.Outcome)
					result.Duration = time.Since(start)
					return result, nil
				}
			default:
				result.Outcome = workflowFailureOutcome(sr.Outcome)
				result.Duration = time.Since(start)
				return result, nil
			}
		}
	}

	result.Outcome = "success"
	result.Duration = time.Since(start)
	return result, nil
}

type wfState struct {
	input    string
	prev     map[string]StepResult
	workflow string
	runID    string
}

func (s *wfState) cloneForParallel() *wfState {
	if s == nil {
		return &wfState{prev: make(map[string]StepResult)}
	}
	cp := &wfState{
		input:    s.input,
		workflow: s.workflow,
		runID:    s.runID,
		prev:     make(map[string]StepResult, len(s.prev)),
	}
	maps.Copy(cp.prev, s.prev)
	return cp
}

func (r *Runner) executeStep(ctx context.Context, step Step, state *wfState) (StepResult, error) {
	sr := StepResult{StepID: step.ID, Agent: step.Agent}
	start := time.Now()
	stepCtx, cancel := stepContext(ctx, step.Timeout)
	defer cancel()
	ctx = stepCtx

	switch step.Kind {
	case StepAgent:
		task := expandTemplate(step.Input, state)
		outcome, output, err := r.RunAgent(stepCtx, step.Agent, "", task)
		sr.Outcome = outcome
		sr.Output = output
		sr.Duration = time.Since(start)
		if err == nil && reliability.IsPausedOutcome(outcome) {
			err = &reliability.ExecutionStoppedError{Outcome: outcome, Err: fmt.Errorf("agent %s is waiting: %s", step.Agent, output)}
		}
		if reliability.IsExecutionTerminalError(err) {
			sr.Error = err.Error()
			if reliability.IsExecutionUncertainError(err) {
				sr.Outcome = "uncertain"
			} else if reliability.IsExecutionStoppedError(err) {
				sr.Outcome = reliability.ExecutionStopOutcome(err)
			}
			return sr, err
		}
		if errors.Is(stepCtx.Err(), context.DeadlineExceeded) && (err != nil || !agent.IsHealthyOutcome(outcome)) {
			sr.Outcome = "timeout"
			if sr.Error == "" {
				sr.Error = "step timeout exceeded"
			}
			return sr, stepCtx.Err()
		}
		if err != nil {
			sr.Error = err.Error()
			if sr.Outcome == "" || reliability.IsHealthyOutcome(sr.Outcome) || reliability.IsPausedOutcome(sr.Outcome) {
				sr.Outcome = "failure"
			}
		}
		if persistErr := r.promoteStepToSession(ctx, state, step.ID, output); persistErr != nil {
			persistErr = fmt.Errorf("persist workflow step %s output: %w", step.ID, persistErr)
			if err == nil && reliability.IsHealthyOutcome(sr.Outcome) {
				err = &reliability.ExecutionPersistenceError{Err: persistErr}
			} else {
				err = &reliability.ExecutionStoppedError{Outcome: "failure", Err: errors.Join(err, persistErr)}
			}
			sr.Error = err.Error()
		}
		return sr, err

	case StepCondition:
		result := evaluateCondition(step.Condition, state)
		if result {
			sr.Outcome = "success"
			sr.Output = "condition_met"
		} else {
			sr.Outcome = "skipped"
			sr.Output = "condition_not_met"
		}
		sr.Duration = time.Since(start)
		return sr, nil

	case StepParallel:
		return r.executeParallel(ctx, step, state)

	case StepLoop:
		return r.executeLoop(ctx, step, state)

	case StepSubworkflow:
		body, err := r.runSteps(ctx, step.Steps, state)
		sr.Steps = body.Steps
		sr.Outcome = body.Outcome
		sr.Duration = time.Since(start)
		if len(body.Steps) > 0 {
			sr.Output = body.Steps[len(body.Steps)-1].Output
		}
		if err != nil {
			sr.Error = err.Error()
		}
		return protectCompletedWorkflowContainer(sr, err)

	case StepApproval:
		taskID := WorkflowApprovalTaskID(state.workflow, step.ID, state.runID)
		sr.HitlTaskID = taskID
		if r.WaitApproval != nil {
			res, err := r.WaitApproval(ctx, step, state)
			if res.TaskID != "" {
				sr.HitlTaskID = res.TaskID
			}
			sr.HitlRequestID = res.RequestID
			sr.Duration = time.Since(start)
			if res.Escalated {
				sr.Outcome = "escalated"
				sr.Output = "approval escalated"
				sr.Error = "approval escalated for human review"
				return stoppedWorkflowStep(sr, errors.Join(fmt.Errorf("approval escalated"), err))
			}
			if err != nil {
				sr.Error = err.Error()
				if errors.Is(err, context.DeadlineExceeded) {
					sr.Outcome = "timeout"
				} else if errors.Is(err, context.Canceled) {
					sr.Outcome = "cancelled"
				} else {
					sr.Outcome = "failure"
				}
				return stoppedWorkflowStep(sr, err)
			}
			if !res.Approved {
				sr.Outcome = "rejected"
				sr.Output = "approval rejected"
				return stoppedWorkflowStep(sr, fmt.Errorf("approval rejected"))
			}
			sr.Outcome = "success"
			sr.Output = "approved"
			return sr, nil
		}
		sr.Outcome = "pending_approval"
		sr.Output = fmt.Sprintf("Waiting for approval (hitl_task_id=%s): %s", taskID, expandTemplate(step.Input, state))
		sr.Duration = time.Since(start)
		return stoppedWorkflowStep(sr, fmt.Errorf("approval waiter not configured; explicit decision required"))

	default:
		return sr, fmt.Errorf("unknown step kind: %s", step.Kind)
	}
}

func (r *Runner) executeParallel(ctx context.Context, step Step, state *wfState) (StepResult, error) {
	start := time.Now()
	var wg sync.WaitGroup
	results := make([]StepResult, len(step.Steps))
	terminalErrors := make([]error, len(step.Steps))
	mu := sync.Mutex{}

	for i, sub := range step.Steps {
		wg.Add(1)
		idx, s := i, sub
		reliability.SafeGo(
			fmt.Sprintf("workflow-parallel-step[%s]", s.ID),
			func() {
				childState := state.cloneForParallel()
				sr, err := r.executeStep(ctx, s, childState)
				mu.Lock()
				if err != nil {
					if sr.Error == "" {
						sr.Error = err.Error()
					}
					if sr.Outcome == "" {
						sr.Outcome = "error"
					}
				}
				results[idx] = sr
				if reliability.IsExecutionTerminalError(err) {
					terminalErrors[idx] = err
				}
				mu.Unlock()
				wg.Done()
			},
			func(panicVal any, _ string) {
				mu.Lock()
				results[idx] = StepResult{
					StepID:  s.ID,
					Agent:   s.Agent,
					Outcome: "error",
					Error:   fmt.Sprintf("panic: %v", panicVal),
				}
				mu.Unlock()
				wg.Done()
			},
		)
	}
	wg.Wait()

	// Ordinary failures keep the historical parallel policy when no terminal
	// branch exists. Once a branch stops execution, include every admitted
	// sibling's failed disposition so a wait cannot hide failed or panicked work.
	if errors.Join(terminalErrors...) != nil {
		for i, child := range results {
			if terminalErrors[i] != nil || (child.Error == "" && (reliability.IsHealthyOutcome(child.Outcome) || child.Outcome == "skipped")) {
				continue
			}
			outcome := child.Outcome
			if !reliability.IsStoppedOutcome(outcome) {
				outcome = "failure"
			}
			terminalErrors[i] = &reliability.ExecutionStoppedError{Outcome: outcome, Err: fmt.Errorf("parallel step %s: %s", child.StepID, child.Error)}
		}
	}

	// Aggregate: success if all succeeded
	allSuccess := true
	outputs := make([]string, 0, 8)
	for _, sr := range results {
		if !reliability.IsHealthyOutcome(sr.Outcome) && sr.Outcome != "skipped" {
			allSuccess = false
		}
		outputs = append(outputs, sr.Output)
	}

	sr := StepResult{
		StepID:   step.ID,
		Steps:    results,
		Agent:    "parallel(" + fmt.Sprintf("%d", len(step.Steps)) + " agents)",
		Duration: time.Since(start),
		Output:   fmt.Sprintf("%v", outputs),
	}
	if allSuccess {
		sr.Outcome = "success"
	} else {
		sr.Outcome = "partial"
	}
	return protectCompletedWorkflowContainer(sr, errors.Join(terminalErrors...))
}

func (r *Runner) executeLoop(ctx context.Context, step Step, state *wfState) (StepResult, error) {
	start := time.Now()
	maxIter := step.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}
	sr := StepResult{StepID: step.ID}
	if len(step.Steps) == 0 {
		sr.Outcome = "failure"
		sr.Error = "loop has no body steps"
		return sr, nil
	}
	for i := range maxIter {
		body, err := r.runSteps(ctx, step.Steps, state)
		sr.Steps = append(sr.Steps, body.Steps...)
		sr.Duration = time.Since(start)
		if err != nil || body.Outcome != "success" {
			sr.Outcome = body.Outcome
			if len(body.Steps) > 0 {
				sr.Output = body.Steps[len(body.Steps)-1].Output
			}
			if err != nil {
				sr.Error = err.Error()
			}
			return protectCompletedWorkflowContainer(sr, err)
		}
		if step.Condition != "" && evaluateCondition(step.Condition, state) {
			sr.Outcome = "success"
			sr.Output = fmt.Sprintf("loop completed after %d iterations", i+1)
			return sr, nil
		}
	}
	sr.Outcome = "success"
	sr.Output = fmt.Sprintf("loop completed (max %d iterations)", maxIter)
	return sr, nil
}

// A container with completed work cannot be replayed as an ordinary failure.
// A skipped condition is not evidence of completed execution.
func hasCompletedWorkflowStep(steps []StepResult) bool {
	for _, step := range steps {
		if reliability.IsHealthyOutcome(step.Outcome) || hasCompletedWorkflowStep(step.Steps) {
			return true
		}
	}
	return false
}

func completedWorkflowStop(outcome string, steps []StepResult, err error) (string, error) {
	if !reliability.IsHealthyOutcome(outcome) && !reliability.IsExecutionTerminalError(err) && hasCompletedWorkflowStep(steps) {
		return "partial", &reliability.ExecutionStoppedError{Outcome: "partial", Err: errors.Join(err, fmt.Errorf("workflow stopped after completed work; explicit continuation required"))}
	}
	return outcome, err
}

func protectCompletedWorkflowContainer(sr StepResult, err error) (StepResult, error) {
	sr.Outcome, err = completedWorkflowStop(sr.Outcome, sr.Steps, err)
	if err != nil {
		sr.Error = err.Error()
	}
	return sr, err
}

func workflowFailureOutcome(outcome string) string {
	if outcome == "partial" {
		return outcome
	}
	return "failure"
}

// Approval errors cannot be skipped or used to request a fresh decision. Keep
// known owner identifiers and preserve stronger terminal diagnostics from hooks.
func stoppedWorkflowStep(sr StepResult, err error) (StepResult, error) {
	if reliability.IsExecutionUncertainError(err) {
		sr.Outcome = "uncertain"
	} else if reliability.IsExecutionStoppedError(err) {
		sr.Outcome = reliability.ExecutionStopOutcome(err)
	} else {
		if reliability.IsExecutionPersistenceError(err) {
			sr.Outcome = "aborted"
		}
		err = &reliability.ExecutionStoppedError{Outcome: sr.Outcome, Err: err}
	}
	sr.Error = err.Error()
	return sr, err
}

// expandTemplate replaces {{.prev.stepID.output}} and {{.input}} with actual values.
func expandTemplate(input string, state *wfState) string {
	// Simple replacement: {{.prev.STEPID.output}} → state.prev[STEPID].Output
	result := input
	// Expand {{.input}} first — the top-level input to the workflow
	result = replaceAll(result, "{{.input}}", state.input)
	for id, sr := range state.prev {
		result = replaceAll(result, "{{.prev."+id+".output}}", sr.Output)
		result = replaceAll(result, "{{.prev."+id+".outcome}}", sr.Outcome)
	}
	return result
}

func replaceAll(s, old, newStr string) string {
	result := s
	for {
		next := result
		for i := 0; i <= len(result)-len(old); i++ {
			if result[i:i+len(old)] == old {
				result = result[:i] + newStr + result[i+len(old):]
				break
			}
		}
		if next == result {
			break
		}
	}
	return result
}

// evaluateCondition checks a simple condition against the workflow state.
// Supports: "{{.prev.X.output}} == 'value'" and "{{.prev.X.outcome}} == 'success'"
func evaluateCondition(cond string, state *wfState) bool {
	expanded := expandTemplate(cond, state)
	// Very simple: string contains check
	// Full expression evaluation would need a proper expression engine
	if expanded == "true" {
		return true
	}
	if expanded == "condition_met" || expanded == "success" {
		return true
	}
	// Check for "X == 'Y'" pattern
	for i := range len(expanded) - 4 {
		if expanded[i:i+4] == " == " {
			left := expanded[:i]
			right := expanded[i+4:]
			// strip quotes
			right = trimQuotes(right)
			return left == right
		}
	}
	return false
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

func (r *Runner) promoteStepToSession(ctx context.Context, state *wfState, stepID, output string) error {
	if r == nil || r.Blackboards == nil || state == nil || state.runID == "" || output == "" {
		return nil
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: state.runID}
	summary := util.Truncate(output, 200)
	if err := r.Blackboards.SetWithContext(ctx, scope, "steps/"+stepID+"/output", output, summary, "text"); err != nil {
		return err
	}
	return r.Blackboards.SetWithContext(ctx, scope, "prev/output", output, summary, "text")
}

// stepContext returns a child context with step timeout when timeoutStr is valid (e.g. "30s", "5m").
func stepContext(ctx context.Context, timeoutStr string) (context.Context, context.CancelFunc) {
	if timeoutStr == "" {
		return ctx, func() {}
	}
	d, err := time.ParseDuration(timeoutStr)
	if err != nil || d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
