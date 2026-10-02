package agentexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/dashboard"
)

// LoadPipeline reads a workflow YAML from agent.WorkflowsDir().
func LoadPipeline(name string) (dashboard.Pipeline, error) {
	if !filepath.IsLocal(name) {
		return dashboard.Pipeline{}, fmt.Errorf("workflow name must stay within its configured root")
	}
	filename := name
	if !strings.HasSuffix(filename, ".yaml") {
		filename += ".yaml"
	}
	root, err := os.OpenRoot(agent.WorkflowsDir())
	if err != nil {
		return dashboard.Pipeline{}, err
	}
	defer root.Close()
	data, err := root.ReadFile(filename)
	if err != nil {
		return dashboard.Pipeline{}, err
	}
	var pipeline dashboard.Pipeline
	if err := yaml.Unmarshal(data, &pipeline); err != nil {
		return dashboard.Pipeline{}, err
	}
	return pipeline, nil
}

// RunPipeline executes a YAML workflow using in-process agent runs.
func RunPipeline(ctx context.Context, d *agent.RunDeps, pipeline dashboard.Pipeline, input string) (*dashboard.PipelineResult, error) {
	return RunPipelineWithID(ctx, d, pipeline, input, "")
}

// RunPipelineWithID executes a workflow and uses runID for HITL task correlation when set.
func RunPipelineWithID(ctx context.Context, d *agent.RunDeps, pipeline dashboard.Pipeline, input, runID string) (*dashboard.PipelineResult, error) {
	boards, err := d.BoardManager()
	if err != nil {
		return nil, fmt.Errorf("initialize pipeline blackboard: %w", err)
	}
	runner := &dashboard.Runner{
		RunID:       runID,
		Blackboards: boards,
		RunAgent: func(stepCtx context.Context, agentName, _, task string) (string, string, error) {
			opts := agent.RunOptions{
				InjectMemory:   true,
				EnforceQuality: true,
				RecordHistory:  true,
				DisplayName:    agentName,
				SessionID:      runID,
			}
			start := time.Now()
			outcome, output, _, err := agent.RunAgent(stepCtx, d, agentName, task, "", opts)
			// Shared classifier: healthy non-"success" outcomes count as
			// metric successes, matching the executor's recordTaskMetric.
			durationMs := max(time.Since(start).Milliseconds(), 0)
			dashboard.RecordTask(agentName, agent.IsBreakerSuccess(outcome, err), uint64(durationMs))
			return outcome, output, err
		},
		WaitApproval: dashboard.WorkflowApprovalWait,
	}
	return runner.Run(ctx, pipeline, input)
}
