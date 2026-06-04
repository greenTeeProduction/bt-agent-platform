// Package workflow provides multi-agent workflow orchestration for the Go BT framework.
// Supports sequential, parallel, conditional, loop, and human-in-loop patterns.
package dashboard

import (
	"context"
	"fmt"
	"sync"
	"time"
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
	StepID   string        `json:"step_id"`
	Agent    string        `json:"agent"`
	Outcome  string        `json:"outcome"` // success, failure, skipped
	Output   string        `json:"output"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// WorkflowResult is the complete result of a workflow execution.
type PipelineResult struct {
	Workflow string        `json:"workflow"`
	Steps    []StepResult  `json:"steps"`
	Outcome  string        `json:"outcome"` // success, failure, partial
	Duration time.Duration `json:"duration"`
}

// Runner executes a workflow by delegating steps to the BT MCP agent.
// It uses a RunnerFunc to execute individual agent steps (injected for testability).
type Runner struct {
	RunAgent func(agentName, treeID, task string) (outcome, output string, err error)
}

// Run executes the workflow and returns the result.
func (r *Runner) Run(ctx context.Context, wf Pipeline, initialInput string) (*PipelineResult, error) {
	start := time.Now()
	result := &PipelineResult{Workflow: wf.Name}

	// Context carries state between steps
	state := &wfState{
		input: initialInput,
		prev:  make(map[string]StepResult),
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
			sr.Error = err.Error()
			sr.Outcome = "failure"
		}
		result.Steps = append(result.Steps, sr)
		state.prev[step.ID] = sr

		// Update state for next step
		state.input = sr.Output

		// Handle failure
		if sr.Outcome == "failure" {
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
	input string
	prev  map[string]StepResult
}

func (r *Runner) executeStep(ctx context.Context, step Step, state *wfState) (StepResult, error) {
	sr := StepResult{StepID: step.ID, Agent: step.Agent}
	start := time.Now()

	switch step.Kind {
	case StepAgent:
		task := expandTemplate(step.Input, state)
		outcome, output, err := r.RunAgent(step.Agent, "", task)
		sr.Outcome = outcome
		sr.Output = output
		sr.Duration = time.Since(start)
		if err != nil {
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

	case StepApproval:
		sr.Outcome = "pending_approval"
		sr.Output = fmt.Sprintf("Waiting for approval: %s", step.Input)
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
		go func(idx int, s Step) {
			defer wg.Done()
			sr, err := r.executeStep(ctx, s, state)
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
		}(i, sub)
	}
	wg.Wait()

	// Aggregate: success if all succeeded
	allSuccess := true
	var outputs []string
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

func replaceAll(s, old, new string) string {
	result := s
	for {
		next := result
		for i := 0; i <= len(result)-len(old); i++ {
			if result[i:i+len(old)] == old {
				result = result[:i] + new + result[i+len(old):]
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
