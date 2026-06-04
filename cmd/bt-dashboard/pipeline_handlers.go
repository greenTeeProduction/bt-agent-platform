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

	"gopkg.in/yaml.v3"

	"github.com/nico/go-bt-evolve/internal/dashboard"
)

// pipelineResults stores in-memory results of pipeline executions.
var (
	pipelineResults   = make(map[string]*dashboard.PipelineResult)
	pipelineResultsMu sync.RWMutex
)

// agentToTree maps pipeline agent names to BT tree IDs.
// This allows YAML pipeline definitions to use friendly agent names.
var agentToTree = map[string]string{
	// Research agents
	"hermes-researcher": "research:deep_research",
	"daily-researcher":  "research:deep_research",
	"research-agent":    "research:deep_research",
	"quick-researcher":  "research:quick_research",

	// Code / dev agents
	"code-reviewer":        "domain:code_review",
	"hermes-code-reviewer": "domain:code_review",
	"bt-implementer":       "godev",
	"refactoring-agent":    "domain:refactoring",

	// DevOps agents
	"system-monitor": "domain:agent_monitor",
	"devops-agent":   "domain:devops_ci",
	"ci-agent":       "domain:devops_ci",

	// Security agents
	"security-auditor": "domain:security_audit",

	// Data / notebook agents
	"notebooklm":          "research:deep_research",
	"data-pipeline-agent": "domain:data_pipeline",

	// Notification / routing agents
	"notification-router": "godev",

	// Vault / storage agents
	"vault": "godev",

	// Thinktank / analysis agents
	"thinktank": "thinktank:synthesis",

	// Default fallback
	"default": "godev",
}

// resolveTree maps an agent name to a tree ID.
func resolveTree(agentName string) string {
	if tid, ok := agentToTree[agentName]; ok {
		return tid
	}
	// Try lowercased variant
	if tid, ok := agentToTree[strings.ToLower(agentName)]; ok {
		return tid
	}
	return "godev"
}

// newRunID generates a unique execution ID for pipeline runs.
func newRunID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// handlePipelines lists all pipeline YAML files from agents/workflows/.
// GET /api/pipelines
func handlePipelines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	home := os.Getenv("HOME")
	workflowsDir := filepath.Join(home, "go-bt-evolve", "agents", "workflows")

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
			Name:        wf.Name,
			Filename:    entry.Name(),
			Description: wf.Description,
			Version:     wf.Version,
			StepCount:   len(wf.Steps),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pipelines)
}

// handlePipelineRun executes a pipeline from agents/workflows/.
// POST /api/pipelines/run
// Body: {"pipeline_name": "daily-research", "input": "Research topic X"}
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

	// Resolve the YAML file
	home := os.Getenv("HOME")
	filename := req.PipelineName
	if !strings.HasSuffix(filename, ".yaml") {
		filename += ".yaml"
	}
	filePath := filepath.Join(home, "go-bt-evolve", "agents", "workflows", filename)

	data, err := os.ReadFile(filePath)
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
	slog.Info("pipeline: starting execution", "run_id", runID, "pipeline", pipeline.Name)

	// Build the runner with real agent execution via dashboard executor.
	runner := &dashboard.Runner{
		RunAgent: func(agentName, treeID, task string) (outcome, output string, err error) {
			// Resolve tree ID from agent name if not explicitly provided.
			if treeID == "" {
				treeID = resolveTree(agentName)
			}
			slog.Info("pipeline: running agent step",
				"run_id", runID,
				"agent", agentName,
				"tree", treeID,
				"task_len", len(task),
			)

			executor := dashboard.NewAgentExecutor()
			stepOutput, stepOutcome, stepErr := executor.RunTask(agentName, task, treeID)

			slog.Info("pipeline: agent step complete",
				"run_id", runID,
				"agent", agentName,
				"outcome", stepOutcome,
				"output_len", len(stepOutput),
			)
			return stepOutcome, stepOutput, stepErr
		},
	}

	ctx := context.Background()
	result, runErr := runner.Run(ctx, pipeline, req.Input)

	// Store result
	pipelineResultsMu.Lock()
	pipelineResults[runID] = result
	pipelineResultsMu.Unlock()

	if runErr != nil {
		slog.Warn("pipeline: execution completed with error", "run_id", runID, "error", runErr)
	} else {
		slog.Info("pipeline: execution complete", "run_id", runID, "outcome", result.Outcome)
	}

	// Return result with the run_id
	response := map[string]interface{}{
		"run_id":   runID,
		"workflow": result.Workflow,
		"outcome":  result.Outcome,
		"duration": result.Duration.String(),
		"steps":    result.Steps,
	}
	if runErr != nil {
		response["error"] = runErr.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handlePipelineStatus returns the result of a pipeline execution.
// GET /api/pipelines/status?id=<run_id>
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

	pipelineResultsMu.RLock()
	result, ok := pipelineResults[runID]
	pipelineResultsMu.RUnlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "pipeline run not found: " + runID})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run_id":   runID,
		"workflow": result.Workflow,
		"outcome":  result.Outcome,
		"duration": result.Duration.String(),
		"steps":    result.Steps,
	})
}
