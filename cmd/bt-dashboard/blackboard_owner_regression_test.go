package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/api"
)

func TestUnavailableBlackboardOwnerRejectsHTTPBeforePipelineReservation(t *testing.T) {
	for _, fixture := range []string{"missing-runner", "blocked-root"} {
		t.Run(fixture, func(t *testing.T) {
			root := isolatePipelinePaths(t)
			if err := os.WriteFile(filepath.Join(root, "owned.yaml"), []byte("name: owned fixture\nsteps:\n - id: never-admitted\n   kind: agent\n   agent: fixture\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if fixture == "missing-runner" {
				dashAgentRunner = nil
			} else if err := os.WriteFile(agent.BlackboardDir(), []byte("blocked fixture root"), 0600); err != nil {
				t.Fatal(err)
			}
			pipelineRunsMu.RLock()
			before := len(pipelineRuns)
			pipelineRunsMu.RUnlock()
			handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("board-owner-fixture-key"))
			for _, endpoint := range []string{"/api/blackboard?scope=agent&scope_id=fixture", "/api/blackboard/scopes?scope=agent", "/api/pipelines/run"} {
				for _, key := range []string{"", "board-owner-fixture-key"} {
					method := http.MethodGet
					if endpoint == "/api/pipelines/run" {
						method = http.MethodPost
					}
					req := httptest.NewRequest(method, endpoint, bytes.NewBufferString(`{"pipeline_name":"owned","input":"must not run"}`))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-API-Key", key)
					rr := httptest.NewRecorder()
					handler.ServeHTTP(rr, req)
					want := 503
					if key == "" {
						want = 401
					}
					if rr.Code != want {
						t.Fatalf("unavailable owner disposition lost: endpoint=%s status=%d want=%d body=%s", endpoint, rr.Code, want, rr.Body.String())
					}
					if key != "" && (strings.Contains(rr.Body.String(), agent.HomeDir()) || strings.Contains(rr.Body.String(), "run_id")) {
						t.Fatalf("failed admission leaked path/reserved a run: %s", rr.Body.String())
					}
				}
			}
			pipelineRunsMu.RLock()
			after := len(pipelineRuns)
			pipelineRunsMu.RUnlock()
			if before != after {
				t.Fatal("rejected pipeline published a run record")
			}
		})
	}
}
