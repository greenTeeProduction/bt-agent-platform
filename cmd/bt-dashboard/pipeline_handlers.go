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
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/dashboard"
)

type pipelineRunRecord struct {
	Status    string                    `json:"status"`
	RunID     string                    `json:"run_id"`
	Result    *dashboard.PipelineResult `json:"result,omitempty"`
	Error     string                    `json:"error,omitempty"`
	StartedAt time.Time                 `json:"started_at"`
}

var (
	pipelineRuns   = make(map[string]*pipelineRunRecord)
	pipelineRunsMu sync.RWMutex
)

func newRunID() string {
	b := make([]byte, 8)
	rand.Read(b)
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
	outcome, output, _, err = agent.RunAgent(ctx, runner, agentName, task, "", opts)
	return outcome, output, err
}

func newPipelineRunner(runID string, logAttrs ...any) *dashboard.Runner {
	var bbMgr *blackboard.Manager
	if dashAgentRunner != nil {
		bbMgr = dashAgentRunner.BoardManager()
	}
	return &dashboard.Runner{
		RunID:       runID,
		Blackboards: bbMgr,
		RunAgent: func(stepCtx context.Context, agentName, _, task string) (outcome, output string, err error) {
			slog.Info("pipeline: running agent step", append([]any{
				"run_id", runID, "agent", agentName, "task_len", len(task),
			}, logAttrs...)...)
			outcome, output, err = runPipelineAgentStep(stepCtx, dashAgentRunner, agentName, task, runID)
			slog.Info("pipeline: agent step complete",
				"run_id", runID, "agent", agentName, "outcome", outcome, "output_len", len(output))
			return outcome, output, err
		},
		WaitApproval: dashboard.WorkflowApprovalWait,
	}
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
		json.NewEncoder(w).Encode([]map[string]string{})
		return
	}

	type pipelineInfo struct {
		Name        string `json:"name"`
		Filename    string `json:"filename"`
		Description string `json:"description"`
		Version     string `json:"version"`
		StepCount   int    `json:"step_count"`
	}

	var pipelines []pipelineInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		filePath := filepath.Join(workflowsDir, entry.Name())
		data, err := os.ReadFile(filePath)
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
	json.NewEncoder(w).Encode(pipelines)
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
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if req.PipelineName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing required field: pipeline_name"})
		return
	}

	filename := req.PipelineName
	if !strings.HasSuffix(filename, ".yaml") {
		filename += ".yaml"
	}
	data, err := os.ReadFile(filepath.Join(agent.WorkflowsDir(), filename))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("pipeline not found: %s (%v)", req.PipelineName, err),
		})
		return
	}

	var pipeline dashboard.Pipeline
	if err := yaml.Unmarshal(data, &pipeline); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid pipeline YAML: " + err.Error()})
		return
	}

	runID := newRunID()
	rec := &pipelineRunRecord{Status: "running", RunID: runID, StartedAt: time.Now()}
	pipelineRunsMu.Lock()
	pipelineRuns[runID] = rec
	pipelineRunsMu.Unlock()

	slog.Info("pipeline: starting execution", "run_id", runID, "pipeline", pipeline.Name)

	go func() {
		runner := newPipelineRunner(runID, "pipeline", pipeline.Name)
		result, runErr := runner.Run(context.Background(), pipeline, req.Input)

		pipelineRunsMu.Lock()
		defer pipelineRunsMu.Unlock()
		if runErr != nil {
			rec.Status = "failed"
			rec.Error = runErr.Error()
			slog.Warn("pipeline: execution completed with error", "run_id", runID, "error", runErr)
		} else {
			rec.Status = "complete"
			slog.Info("pipeline: execution complete", "run_id", runID, "outcome", result.Outcome)
		}
		rec.Result = result
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
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
		json.NewEncoder(w).Encode(map[string]string{"error": "missing required parameter: id"})
		return
	}

	pipelineRunsMu.RLock()
	rec, ok := pipelineRuns[runID]
	pipelineRunsMu.RUnlock()
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "pipeline run not found: " + runID})
		return
	}

	resp := map[string]interface{}{
		"run_id":     rec.RunID,
		"status":     rec.Status,
		"started_at": rec.StartedAt.Format(time.RFC3339),
	}
	if rec.Error != "" {
		resp["error"] = rec.Error
	}
	if rec.Result != nil {
		resp["workflow"] = rec.Result.Workflow
		resp["outcome"] = rec.Result.Outcome
		resp["duration"] = rec.Result.Duration.String()
		resp["steps"] = rec.Result.Steps
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
