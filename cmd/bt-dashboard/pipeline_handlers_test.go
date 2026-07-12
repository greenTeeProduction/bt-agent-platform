package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// pipelineRunPanicSubprocessEnv triggers the subprocess body of
// TestHandlePipelineRun_PanicRecoveredAsFailedStatus. An unrecovered panic
// inside the raw `go func(){}()` in handlePipelineRun crashes the entire
// process — it cannot be caught by the parent test's own recover() — so this
// test re-execs itself and asserts the child survives and resolves the run to
// "failed" instead of crashing or leaving pipelineRuns[runID] stuck at
// "running" forever. This mirrors the pattern in
// internal/llm/health_test.go and internal/engine/reactive_parallel_test.go.
const pipelineRunPanicSubprocessEnv = "BT_DASHBOARD_PIPELINE_RUN_PANIC_SUBPROCESS"

// TestHandlePipelineRun_PanicRecoveredAsFailedStatus is the regression test
// for the dashboard-triggered pipeline-run goroutine
// (cmd/bt-dashboard/pipeline_handlers.go:188) lacking panic recovery: a panic
// raised while executing a pipeline step (e.g. from a malformed agent tree
// resolver) must be recovered via reliability.SafeGo and recorded as a
// "failed" run with the panic value surfaced in the status response's error
// field, instead of crashing the dashboard process or leaving the run
// permanently stuck at "running".
func TestHandlePipelineRun_PanicRecoveredAsFailedStatus(t *testing.T) {
	if os.Getenv(pipelineRunPanicSubprocessEnv) == "1" {
		home := os.Getenv("BT_AGENT_HOME")
		workflowsDir := filepath.Join(home, "agents", "workflows")
		if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir workflows dir: %v\n", err)
			os.Exit(2)
		}
		pipelineYAML := []byte("name: panic-test\n" +
			"description: triggers a ResolveTree panic for SafeGo regression coverage\n" +
			"version: \"1\"\n" +
			"steps:\n" +
			"  - id: step1\n" +
			"    kind: agent\n" +
			"    agent: dummy-agent\n" +
			"    input: irrelevant\n")
		if err := os.WriteFile(filepath.Join(workflowsDir, "panic-test.yaml"), pipelineYAML, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write pipeline yaml: %v\n", err)
			os.Exit(2)
		}

		// A ResolveTree that always panics simulates a malformed/corrupt
		// stored tree — RunDeps.RunOnce calls it unconditionally once a
		// step's agent name fails to resolve to a registry Definition.
		dashAgentRunner = &agent.RunDeps{
			ResolveTree: func(treeID string) *evolution.SerializableNode {
				panic("pipeline_handlers_test: simulated ResolveTree panic for " + treeID)
			},
		}

		body, _ := json.Marshal(map[string]string{
			"pipeline_name": "panic-test",
			"input":         "go",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/pipelines/run", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		handlePipelineRun(rr, req)

		if rr.Code != http.StatusAccepted {
			fmt.Fprintf(os.Stderr, "handlePipelineRun status = %d, want 202; body=%s\n", rr.Code, rr.Body.String())
			os.Exit(2)
		}
		var startResp struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &startResp); err != nil || startResp.RunID == "" {
			fmt.Fprintf(os.Stderr, "decode run_id: err=%v body=%s\n", err, rr.Body.String())
			os.Exit(2)
		}

		// Poll status until the background goroutine's panic is recovered
		// and the run resolves — if the panic isn't recovered, this
		// process crashes before the deadline and never reaches os.Exit(0).
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			statusReq := httptest.NewRequest(http.MethodGet, "/api/pipelines/status?id="+startResp.RunID, nil)
			statusRR := httptest.NewRecorder()
			handlePipelineStatus(statusRR, statusReq)

			var statusResp struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal(statusRR.Body.Bytes(), &statusResp); err == nil {
				if statusResp.Status == "failed" {
					if statusResp.Error == "" {
						fmt.Fprintf(os.Stderr, "run resolved to failed but error field is empty\n")
						os.Exit(3)
					}
					os.Exit(0)
				}
				if statusResp.Status != "running" {
					fmt.Fprintf(os.Stderr, "unexpected status %q, want failed\n", statusResp.Status)
					os.Exit(3)
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		fmt.Fprintf(os.Stderr, "run %s never resolved (still running) — panic recovery did not record failure\n", startResp.RunID)
		os.Exit(4)
	}

	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandlePipelineRun_PanicRecoveredAsFailedStatus")
	cmd.Env = append(os.Environ(),
		pipelineRunPanicSubprocessEnv+"=1",
		"BT_AGENT_HOME="+home,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("handlePipelineRun: a panicking pipeline step crashed the process (or left the run "+
			"stuck at \"running\") instead of being recovered via reliability.SafeGo and recorded as "+
			"\"failed\"; exit error=%v output=%s", err, out)
	}
}
