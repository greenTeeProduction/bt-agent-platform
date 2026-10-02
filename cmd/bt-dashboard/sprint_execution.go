package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// sprintTaskDiagnostic retains observed work separately from its task commit.
// This is process-local evidence, not a durable resume queue.
type sprintTaskDiagnostic struct {
	TaskID        string `json:"task_id"`
	Agent         string `json:"agent"`
	TreeID        string `json:"tree_id"`
	RunID         string `json:"run_id,omitempty"`
	TaskStatus    string `json:"task_status"`
	Output        string `json:"output"`
	Outcome       string `json:"outcome"`
	Error         string `json:"error"`
	ErrorKind     string `json:"error_kind"`
	TaskCommitted bool   `json:"task_committed"`
	CommitError   string `json:"commit_error,omitempty"`
	store         *dashboard.TaskStore
	result        dashboard.TaskExecutionResult
}

// reconcileSprintCommitsLocked only retries retained record writes. The caller
// holds sprintState; admission stays closed if the owner or disposition changed.
func reconcileSprintCommitsLockedWithContext(ctx context.Context, store *dashboard.TaskStore) error {
	if sprintState.Uncertain {
		return fmt.Errorf("uncertain sprint execution requires operator reconciliation")
	}
	for i := range sprintState.Diagnostics {
		diagnostic := &sprintState.Diagnostics[i]
		if diagnostic.TaskCommitted {
			continue
		}
		if store != diagnostic.store {
			return fmt.Errorf("sprint task owner changed")
		}
		if err := store.CommitExecutionWithContext(ctx, diagnostic.TaskID, diagnostic.result); err != nil {
			diagnostic.CommitError = err.Error()
			return err
		}
		diagnostic.TaskCommitted = true
		diagnostic.CommitError = ""
		syncWorkflowTaskStatus(diagnostic.TaskID, sprintWorkflowStatus(diagnostic.TaskStatus))
	}
	return nil
}

func sprintWorkflowStatus(status string) dashboard.WorkflowTaskStatus {
	switch status {
	case "approved":
		return dashboard.StatusPending
	case "completed":
		return dashboard.StatusCompleted
	default:
		return dashboard.StatusBlocked
	}
}

func executeSprintTasksWithContext(ctx context.Context, store *dashboard.TaskStore, executor *dashboard.AgentExecutor, tasks []dashboard.Task) {
	defer func() {
		panicValue := recover()
		sprintState.Lock()
		defer sprintState.Unlock()
		if panicValue != nil {
			slog.Error("sprint panic", "error", panicValue)
			sprintState.Error = "Sprint execution interrupted; inspect claimed tasks before retrying"
			sprintState.ErrorKind = reliability.ExecutionUncertainKind
			sprintState.Uncertain = true
		}
		sprintState.Running = false
		sprintState.Progress = "done"
		if sprintState.Error != "" {
			sprintState.Progress = "failed"
		}
	}()
	for index, task := range tasks {
		if err := ctx.Err(); err != nil {
			returnUnstartedSprintTasks(store, tasks[index:], err)
			return
		}
		sprintState.Lock()
		sprintState.CurrentTask = task.Title
		sprintState.Progress = "running"
		sprintState.Unlock()
		syncWorkflowTaskStatus(task.ID, dashboard.StatusInProgress)

		treeID := task.TreeID
		if treeID == "" {
			treeID = dashboard.PickTreeForTask(task)
		}
		agentName := dashboard.ResolveAgentName(task.Assignee)
		taskDesc := task.Title
		if task.Description != "" {
			taskDesc = task.Description
		}
		var output, outcome, runID string
		var runErr error
		breakerSkipped := executor.CBStore != nil && !executor.CBStore.Allowed(agentName)
		if breakerSkipped {
			output, outcome = "skipped: circuit breaker open for agent "+agentName, "deferred"
		} else {
			slog.Info("sprint: running task", "task", task.ID, "agent", agentName, "tree", treeID)
			run, err := executor.RunTaskResultWithContext(ctx, agentName, taskDesc, treeID)
			runErr = err
			outcome = "failure"
			if run != nil {
				output, outcome, runID = run.Output, run.Outcome, run.RunID
			}
		}
		disposition := sprintTaskDisposition(outcome, runErr)
		if breakerSkipped {
			disposition = sprintDeferred // explicit pre-execution breaker skip
		}
		if disposition == sprintFailed && runErr == nil {
			if reliability.IsStoppedOutcome(outcome) {
				runErr = &reliability.ExecutionStoppedError{Outcome: outcome, Err: fmt.Errorf("task stopped without completion")}
			} else {
				runErr = &reliability.ExecutionUncertainError{Err: fmt.Errorf("unrecognized task outcome %q", outcome)}
			}
		}
		result := dashboard.TaskExecutionResult{Status: "completed", Output: output, Outcome: outcome, RunID: runID}
		switch disposition {
		case sprintDeferred:
			result.Status = "approved"
		case sprintFailed:
			result.Status = "failed"
		}
		if runErr != nil {
			result.Error, result.ErrorKind = runErr.Error(), reliability.ExecutionErrorKind(runErr)
		}
		commitErr := store.CommitExecution(task.ID, result)
		if commitErr == nil {
			syncWorkflowTaskStatus(task.ID, sprintWorkflowStatus(result.Status))
		}
		if runErr != nil || commitErr != nil {
			diagnostic := sprintTaskDiagnostic{
				TaskID: task.ID, Agent: agentName, TreeID: treeID, RunID: runID, TaskStatus: result.Status,
				Output: output, Outcome: outcome, Error: result.Error, ErrorKind: result.ErrorKind,
				TaskCommitted: commitErr == nil, store: store, result: result,
			}
			if commitErr != nil {
				diagnostic.CommitError = commitErr.Error()
				if diagnostic.ErrorKind == "" {
					diagnostic.ErrorKind = reliability.ExecutionPersistenceKind
				}
				diagnostic.Error = fmt.Sprintf("Task result commit failed: %v", commitErr)
				if result.Error != "" {
					diagnostic.Error = result.Error + "; " + diagnostic.Error
				}
			}
			sprintState.Lock()
			sprintState.Diagnostics = append(sprintState.Diagnostics, diagnostic)
			if commitErr != nil || disposition != sprintDeferred {
				sprintState.Error = "Sprint has execution or task-result errors; inspect diagnostics"
				// Keep the most conservative observed diagnostic across tasks.
				kind := diagnostic.ErrorKind
				if kind == "" {
					kind = "execution"
				}
				if sprintDiagnosticPriority(kind) > sprintDiagnosticPriority(sprintState.ErrorKind) {
					sprintState.ErrorKind = kind
				}
			}
			if reliability.IsExecutionUncertainError(runErr) {
				sprintState.Uncertain = true
			}
			sprintState.Unlock()
		}
	}
}

func sprintDiagnosticPriority(kind string) int {
	switch kind {
	case reliability.ExecutionUncertainKind:
		return 4
	case reliability.ExecutionStoppedKind:
		return 3
	case reliability.ExecutionPersistenceKind:
		return 2
	case "execution":
		return 1
	default:
		return 0
	}
}

// Only tasks not dispatched by this batch can return to approved work. Cleanup
// has its own bounded record budget after the execution context has expired.
func returnUnstartedSprintTasks(store *dashboard.TaskStore, tasks []dashboard.Task, cause error) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results := make(map[string]dashboard.TaskExecutionResult, len(tasks))
	for _, task := range tasks {
		results[task.ID] = dashboard.TaskExecutionResult{Status: "approved", Outcome: "not_started", Output: "Not started: sprint execution budget ended", Error: cause.Error(), ErrorKind: reliability.ExecutionStoppedKind}
	}
	commitErr := store.CommitExecutionBatchWithContext(cleanupCtx, results)
	for _, task := range tasks {
		result := results[task.ID]
		diagnostic := sprintTaskDiagnostic{TaskID: task.ID, Agent: dashboard.ResolveAgentName(task.Assignee), TreeID: task.TreeID, TaskStatus: result.Status, Output: result.Output, Outcome: result.Outcome, Error: result.Error, ErrorKind: result.ErrorKind, TaskCommitted: commitErr == nil, store: store, result: result}
		if commitErr != nil {
			diagnostic.CommitError = commitErr.Error()
		} else {
			syncWorkflowTaskStatus(task.ID, dashboard.StatusPending)
		}
		sprintState.Lock()
		sprintState.Diagnostics = append(sprintState.Diagnostics, diagnostic)
		sprintState.Error = "Sprint budget ended; remaining tasks were not started, inspect diagnostics"
		if sprintDiagnosticPriority(sprintState.ErrorKind) < sprintDiagnosticPriority(reliability.ExecutionStoppedKind) {
			sprintState.ErrorKind = reliability.ExecutionStoppedKind
		}
		sprintState.Unlock()
	}
}
