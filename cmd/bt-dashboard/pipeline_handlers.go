package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

type pipelineRunRecord struct {
	Status    string                    `json:"status"`
	RunID     string                    `json:"run_id"`
	Result    *dashboard.PipelineResult `json:"result,omitempty"`
	Error     string                    `json:"error,omitempty"`
	ErrorKind string                    `json:"error_kind,omitempty"`
	StartedAt time.Time                 `json:"started_at"`
}

var (
	pipelineRuns   = make(map[string]*pipelineRunRecord)
	pipelineRunsMu sync.RWMutex
)

func newRunID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func runPipelineAgentStep(ctx context.Context, runner *agent.RunDeps, agentName, task, sessionID string) (outcome, output string, err error) {
	opts := agent.RunOptions{
		InjectMemory:   true,
		EnforceQuality: true,
		RecordHistory:  true,
		DisplayName:    agentName,
		SessionID:      sessionID,
	}
	start := time.Now()
	outcome, output, _, err = agent.RunAgent(ctx, runner, agentName, task, "", opts)
	// Shared classifier: healthy non-"success" outcomes count as metric
	// successes, matching the executor's recordTaskMetric.
	dashboard.RecordTask(agentName, agent.IsBreakerSuccess(outcome, err), uint64(time.Since(start).Milliseconds()))
	return outcome, output, err
}

func newPipelineRunner(runID string, logAttrs ...any) (*dashboard.Runner, error) {
	deps := dashAgentRunner
	bbMgr, err := deps.BoardManager()
	if err != nil {
		return nil, err
	}
	return &dashboard.Runner{
		RunID:       runID,
		Blackboards: bbMgr,
		RunAgent: func(stepCtx context.Context, agentName, _, task string) (outcome, output string, err error) {
			slog.Info("pipeline: running agent step", append([]any{
				"run_id", runID, "agent", agentName, "task_len", len(task),
			}, logAttrs...)...)
			outcome, output, err = runPipelineAgentStep(stepCtx, deps, agentName, task, runID)
			slog.Info("pipeline: agent step complete",
				"run_id", runID, "agent", agentName, "outcome", outcome, "output_len", len(output))
			return outcome, output, err
		},
		WaitApproval: dashboard.WorkflowApprovalWait,
	}, nil
}

// handlePipelines lists all pipeline YAML files from agents/workflows/.
func handlePipelines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	workflowsDir := agent.WorkflowsDir()
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		slog.Warn("pipelines: cannot read workflows dir", "path", workflowsDir, "error", err)
		w.Header().Set("Content-Type", "application/json")
		if os.IsNotExist(err) {
			_ = encodeJSON(w, []map[string]string{})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = encodeJSON(w, map[string]string{"error": "pipeline inventory unavailable"})
		}
		return
	}

	type pipelineInfo struct {
		Name        string `json:"name"`
		Filename    string `json:"filename"`
		Description string `json:"description"`
		Version     string `json:"version"`
		StepCount   int    `json:"step_count"`
	}

	var pipelines = make([]pipelineInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		filePath := filepath.Join(workflowsDir, entry.Name())
		data, err := util.ReadPersistenceFile(filePath)
		if err != nil {
			slog.Warn("pipelines: cannot read file", "path", filePath, "error", err)
			continue
		}
		var wf dashboard.Pipeline
		if err := yaml.Unmarshal(data, &wf); err != nil {
			slog.Warn("pipelines: cannot parse YAML", "path", filePath, "error", err)
			continue
		}
		pipelines = append(pipelines, pipelineInfo{
			Name: wf.Name, Filename: entry.Name(), Description: wf.Description,
			Version: wf.Version, StepCount: len(wf.Steps),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, pipelines)
}

// handlePipelineRun starts pipeline execution asynchronously.
// POST /api/pipelines/run — returns run_id immediately; poll /api/pipelines/status?id=
func handlePipelineRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PipelineName string `json:"pipeline_name"`
		Input        string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if req.PipelineName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "missing required field: pipeline_name"})
		return
	}
	// Validate the request before joining/cleaning: only catalog basenames are
	// selectable. Rooted reads below also prevent symlink escapes at open time.
	if !filepath.IsLocal(req.PipelineName) || filepath.Base(req.PipelineName) != req.PipelineName || strings.ContainsAny(req.PipelineName, "\\\x00") || req.PipelineName == "." || req.PipelineName == ".." {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "pipeline_name must be a workflow filename without directories"})
		return
	}

	filename := req.PipelineName
	if !strings.HasSuffix(filename, ".yaml") {
		filename += ".yaml"
	}
	data, err := util.ReadPersistenceFile(filepath.Join(agent.WorkflowsDir(), filename))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = encodeJSON(w, map[string]string{
			"error": "pipeline not found or unavailable: " + req.PipelineName,
		})
		return
	}

	var pipeline dashboard.Pipeline
	if err := yaml.Unmarshal(data, &pipeline); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "invalid pipeline YAML: " + err.Error()})
		return
	}

	endActivity, admitErr := dashActivity.acquire()
	if admitErr != nil {
		writeDashboardRestarting(w)
		return
	}
	transferred := false
	defer func() {
		if !transferred {
			endActivity()
		}
	}()
	runID := newRunID()
	runner, initErr := newPipelineRunner(runID, "pipeline", pipeline.Name)
	if initErr != nil {
		slog.Warn("pipeline: blackboard owner unavailable", "error", initErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = encodeJSON(w, map[string]string{"error": "pipeline blackboard unavailable"})
		return
	}
	rec := &pipelineRunRecord{Status: "running", RunID: runID, StartedAt: time.Now()}
	pipelineRunsMu.Lock()
	pipelineRuns[runID] = rec
	pipelineRunsMu.Unlock()

	// Enqueue the run onto the horizontal-scaling task queue so /api/scalability
	// reports real pending depth; the run drains it on completion below.
	if dashTaskQueue != nil {
		dashTaskQueue.Enqueue(runID)
	}

	slog.Info("pipeline: starting execution", "run_id", runID, "pipeline", pipeline.Name)

	reliability.SafeGoWithCleanup(fmt.Sprintf("pipeline-run[%s]", runID), func() {
		if dashTaskQueue != nil {
			defer dashTaskQueue.Dequeue()
		}
		result, runErr := runner.Run(context.Background(), pipeline, req.Input)

		pipelineRunsMu.Lock()
		defer pipelineRunsMu.Unlock()
		rec.ErrorKind = reliability.ExecutionErrorKind(runErr)
		if result != nil && reliability.IsExecutionPause(result.Outcome, runErr) {
			rec.Status = "waiting"
			if runErr != nil {
				rec.Error = runErr.Error()
			}
			slog.Info("pipeline: waiting", "run_id", runID, "status", rec.Status)
		} else if runErr != nil || result == nil || !agent.IsHealthyOutcome(result.Outcome) {
			rec.Status = "failed"
			if runErr != nil {
				rec.Error = runErr.Error()
			} else if result != nil {
				rec.Error = "pipeline outcome: " + result.Outcome
			} else {
				rec.Error = "pipeline returned no result"
			}
			slog.Warn("pipeline: execution stopped", "run_id", runID, "status", rec.Status)
		} else {
			rec.Status = "complete"
			slog.Info("pipeline: execution complete", "run_id", runID, "status", rec.Status)
		}
		rec.Result = result
	}, func(panicVal any, panicCtx string) {
		reliability.DefaultPanicHandler(panicVal, panicCtx)
		pipelineRunsMu.Lock()
		defer pipelineRunsMu.Unlock()
		rec.Status = "failed"
		rec.Error = fmt.Sprintf("panic: %v", panicVal)
	}, endActivity)
	transferred = true

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = encodeJSON(w, map[string]any{
		"run_id":   runID,
		"status":   "running",
		"pipeline": pipeline.Name,
		"message":  "Poll GET /api/pipelines/status?id=" + runID,
	})
}

// handlePipelineStatus returns pipeline run status and result when complete.
func handlePipelineStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	runID := r.URL.Query().Get("id")
	if runID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "missing required parameter: id"})
		return
	}

	pipelineRunsMu.RLock()
	rec, ok := pipelineRuns[runID]
	if !ok {
		pipelineRunsMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = encodeJSON(w, map[string]string{"error": "pipeline run not found: " + runID})
		return
	}
	// Snapshot fields under the read lock — returning the pointer then
	// unlocking would race with the SafeGo completion/panic writers that
	// mutate Status/Error/Result under pipelineRunsMu.
	runIDOut := rec.RunID
	status := rec.Status
	startedAt := rec.StartedAt
	errMsg := rec.Error
	errorKind := rec.ErrorKind
	result := rec.Result
	pipelineRunsMu.RUnlock()

	resp := map[string]any{
		"run_id":     runIDOut,
		"status":     status,
		"started_at": startedAt.Format(time.RFC3339),
	}
	if errMsg != "" {
		resp["error"] = errMsg
	}
	if errorKind != "" {
		resp["error_kind"] = errorKind
	}
	if result != nil {
		resp["workflow"] = result.Workflow
		resp["outcome"] = result.Outcome
		resp["duration"] = result.Duration.String()
		resp["steps"] = result.Steps
	}

	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, resp)
}
