// Package workflow provides multi-agent workflow orchestration for the Go BT framework.
// Supports sequential, parallel, conditional, loop, and human-in-loop patterns.
package dashboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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
	Agent         string   `yaml:"agent,omitempty" json:"agent,omitempty"`                   // agent name for agent step
	Input         string   `yaml:"input,omitempty" json:"input,omitempty"`                   // task input (supports {{.prev.output}})
	Condition     string   `yaml:"condition,omitempty" json:"condition,omitempty"`           // Go template: "{{.prev.output.status}} == 'degraded'"
	MaxIterations int      `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty"` // for loop steps
	Steps         []Step   `yaml:"steps,omitempty" json:"steps,omitempty"`                   // for parallel/subworkflow steps
	Timeout       string   `yaml:"timeout,omitempty" json:"timeout,omitempty"`               // "30s", "5m"
	OnFailure     string   `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`         // "skip", "abort", "retry"
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
	result := &PipelineResult{Workflow: wf.Name, RunID: runID}

	// Context carries state between steps
	state := &wfState{
		input:    initialInput,
		prev:     make(map[string]StepResult),
		workflow: wf.Name,
		runID:    runID,
	}

	if r.Blackboards != nil && runID != "" && strings.TrimSpace(initialInput) != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: runID}
		_ = r.Blackboards.Set(scope, "input", initialInput, "Initial workflow input", "text")
	}

	for _, step := range wf.Steps {
		select {
		case <-ctx.Done():
			result.Outcome = "aborted"
			result.Duration = time.Since(start)
			return result, ctx.Err()
		default:
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

		// Handle failure (including timeout, rejected, and escalated approval)
		if sr.Outcome == "failure" || sr.Outcome == "timeout" || sr.Outcome == "rejected" || sr.Outcome == "escalated" {
			switch step.OnFailure {
			case "skip":
				continue
			case "abort", "":
				result.Outcome = "failure"
				result.Duration = time.Since(start)
				return result, nil
			case "retry":
				// retry once — replace failed result with retry result
				sr2, err2 := r.executeStep(ctx, step, state)
				if err2 != nil {
					sr2.Error = err2.Error()
					sr2.Outcome = "failure"
				}
				// Replace the failed step in results array
				result.Steps[len(result.Steps)-1] = sr2
				state.prev[step.ID] = sr2
				state.input = sr2.Output
				if sr2.Outcome == "failure" {
					result.Outcome = "failure"
					result.Duration = time.Since(start)
					return result, nil
				}
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
	for k, v := range s.prev {
		cp.prev[k] = v
	}
	return cp
}

func (r *Runner) executeStep(ctx context.Context, step Step, state *wfState) (StepResult, error) {
	sr := StepResult{StepID: step.ID, Agent: step.Agent}
	start := time.Now()

	switch step.Kind {
	case StepAgent:
		task := expandTemplate(step.Input, state)
		stepCtx, cancel := stepContext(ctx, step.Timeout)
		defer cancel()
		outcome, output, err := r.RunAgent(stepCtx, step.Agent, "", task)
		sr.Outcome = outcome
		sr.Output = output
		sr.Duration = time.Since(start)
		if errors.Is(stepCtx.Err(), context.DeadlineExceeded) {
			sr.Outcome = "timeout"
			if sr.Error == "" {
				sr.Error = "step timeout exceeded"
			}
			return sr, stepCtx.Err()
		}
		if err != nil {
			sr.Error = err.Error()
			if sr.Outcome == "" || sr.Outcome == "success" {
				sr.Outcome = "failure"
			}
		}
		r.promoteStepToSession(state, step.ID, output)
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

	case StepApproval:
		taskID := WorkflowApprovalTaskID(state.workflow, step.ID, state.runID)
		sr.HitlTaskID = taskID
		if r.WaitApproval != nil {
			res, err := r.WaitApproval(ctx, step, state)
			sr.HitlTaskID = res.TaskID
			sr.HitlRequestID = res.RequestID
			sr.Duration = time.Since(start)
			if res.Escalated {
				sr.Outcome = "escalated"
				sr.Output = "approval escalated"
				sr.Error = "approval escalated for human review"
				return sr, fmt.Errorf("approval escalated")
			}
			if err != nil {
				sr.Error = err.Error()
				if errors.Is(err, context.DeadlineExceeded) {
					sr.Outcome = "timeout"
				} else if !res.Approved {
					sr.Outcome = "rejected"
					sr.Output = "approval rejected"
				} else {
					sr.Outcome = "failure"
				}
				return sr, err
			}
			if !res.Approved {
				sr.Outcome = "rejected"
				sr.Output = "approval rejected"
				return sr, fmt.Errorf("approval rejected")
			}
			sr.Outcome = "success"
			sr.Output = "approved"
			return sr, nil
		}
		sr.Outcome = "pending_approval"
		sr.Output = fmt.Sprintf("Waiting for approval (hitl_task_id=%s): %s", taskID, expandTemplate(step.Input, state))
		sr.Duration = time.Since(start)
		return sr, nil

	default:
		return sr, fmt.Errorf("unknown step kind: %s", step.Kind)
	}
}

func (r *Runner) executeParallel(ctx context.Context, step Step, state *wfState) (StepResult, error) {
	start := time.Now()
	var wg sync.WaitGroup
	results := make([]StepResult, len(step.Steps))
	mu := sync.Mutex{}

	for i, sub := range step.Steps {
		wg.Add(1)
		idx, s := i, sub
		reliability.SafeGo(
			fmt.Sprintf("workflow-parallel-step[%s]", s.ID),
			func() {
				defer wg.Done()
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
				mu.Unlock()
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
			},
		)
	}
	wg.Wait()

	// Aggregate: success if all succeeded
	allSuccess := true
	outputs := make([]string, 0, 8)
	for _, sr := range results {
		if sr.Outcome != "success" {
			allSuccess = false
		}
		outputs = append(outputs, sr.Output)
	}

	sr := StepResult{
		StepID:   step.ID,
		Agent:    "parallel(" + fmt.Sprintf("%d", len(step.Steps)) + " agents)",
		Duration: time.Since(start),
		Output:   fmt.Sprintf("%v", outputs),
	}
	if allSuccess {
		sr.Outcome = "success"
	} else {
		sr.Outcome = "partial"
	}
	return sr, nil
}

func (r *Runner) executeLoop(ctx context.Context, step Step, state *wfState) (StepResult, error) {
	start := time.Now()
	maxIter := step.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	for i := 0; i < maxIter; i++ {
		select {
		case <-ctx.Done():
			return StepResult{StepID: step.ID, Outcome: "aborted", Duration: time.Since(start)}, ctx.Err()
		default:
		}

		// Run the loop body (first sub-step)
		if len(step.Steps) == 0 {
			return StepResult{StepID: step.ID, Outcome: "failure", Error: "loop has no body steps", Duration: time.Since(start)}, nil
		}

		sr, err := r.executeStep(ctx, step.Steps[0], state)
		if err != nil {
			return sr, err
		}
		state.prev[step.Steps[0].ID] = sr
		state.input = sr.Output

		// Check exit condition
		if step.Condition != "" && evaluateCondition(step.Condition, state) {
			return StepResult{
				StepID:   step.ID,
				Outcome:  "success",
				Output:   fmt.Sprintf("loop completed after %d iterations", i+1),
				Duration: time.Since(start),
			}, nil
		}

		// If step failed, break
		if sr.Outcome == "failure" {
			return StepResult{
				StepID:   step.ID,
				Outcome:  "failure",
				Output:   fmt.Sprintf("loop failed at iteration %d", i+1),
				Error:    sr.Error,
				Duration: time.Since(start),
			}, nil
		}
	}

	return StepResult{
		StepID:   step.ID,
		Outcome:  "success",
		Output:   fmt.Sprintf("loop completed (max %d iterations)", maxIter),
		Duration: time.Since(start),
	}, nil
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
	if len(expanded) > 3 && expanded[:4] == "true" {
		return true
	}
	if expanded == "condition_met" || expanded == "success" {
		return true
	}
	// Check for "X == 'Y'" pattern
	for i := 0; i < len(expanded)-4; i++ {
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

func (r *Runner) promoteStepToSession(state *wfState, stepID, output string) {
	if r == nil || r.Blackboards == nil || state == nil || state.runID == "" || output == "" {
		return
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: state.runID}
	summary := util.Truncate(output, 200)
	_ = r.Blackboards.Set(scope, "steps/"+stepID+"/output", output, summary, "text")
	_ = r.Blackboards.Set(scope, "prev/output", output, summary, "text")
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
