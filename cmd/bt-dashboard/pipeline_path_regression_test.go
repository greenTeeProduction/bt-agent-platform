package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/security"
)

func isolatePipelinePaths(t *testing.T) string {
	t.Helper()
	isolateExecutionAdmission(t)
	oldQueue, oldSessions := dashTaskQueue, sessionStore
	dashTaskQueue = nil
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{})
	t.Cleanup(func() { sessionStore.Stop(); dashTaskQueue, sessionStore = oldQueue, oldSessions })
	root := agent.WorkflowsDir()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func waitAndRemoveFixturePipeline(t *testing.T, runID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		pipelineRunsMu.Lock()
		record := pipelineRuns[runID]
		settled := record != nil && record.Status != "running"
		if settled {
			delete(pipelineRuns, runID)
		}
		pipelineRunsMu.Unlock()
		if settled {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("empty fixture pipeline did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPipelineRunConfinesRequestsAndSymlinksToWorkflowRoot(t *testing.T) {
	root := isolatePipelinePaths(t)
	fixture := []byte("name: owned-empty-fixture\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(root, "owned.yaml"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.yaml")
	if err := os.WriteFile(outside, []byte("name: outside-owner-secret\nsteps: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"escape.yaml": "../outside.yaml", "absolute.yaml": outside, "alias.yaml": "owned.yaml"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	mux := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("path-fixture-key"))
	for _, tc := range []struct {
		name string
		code int
	}{
		{"../outside", 400}, {"sub/../owned", 400}, {"sub/owned", 400},
		{outside, 400}, {"..\\outside", 400}, {"owned\x00", 400}, {".", 400}, {"..", 400},
		{"escape", 404}, {"absolute.yaml", 404}, {"missing", 404},
		{"owned", 202}, {"owned.yaml", 202}, {"alias.yaml", 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"pipeline_name": tc.name})
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/pipelines/run", bytes.NewReader(body))
			req.Header.Set("X-API-Key", "path-fixture-key")
			mux.ServeHTTP(rr, req)
			var response struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.RunID != "" {
				waitAndRemoveFixturePipeline(t, response.RunID)
			}
			if rr.Code != tc.code || strings.Contains(rr.Body.String(), "outside-owner-secret") || (tc.code != 202 && response.RunID != "") {
				t.Fatalf("name=%q status=%d want=%d body=%s", tc.name, rr.Code, tc.code, rr.Body.String())
			}
		})
	}
}

func TestPipelineInventoryOmitsEscapesAndReportsDirectoryFailure(t *testing.T) {
	root := isolatePipelinePaths(t)
	if err := os.WriteFile(filepath.Join(root, "owned.yaml"), []byte("name: owned\nsteps: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.yaml")
	if err := os.WriteFile(outside, []byte("name: outside-owner-secret\nsteps: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside.yaml", filepath.Join(root, "escape.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("owned.yaml", filepath.Join(root, "alias.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid.yaml"), []byte("steps: ["), 0600); err != nil {
		t.Fatal(err)
	}
	mux := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("path-fixture-key"))
	list := func(key string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/pipelines", nil)
		req.Header.Set("X-API-Key", key)
		mux.ServeHTTP(rr, req)
		return rr
	}
	for _, key := range []string{"", "wrong"} {
		if rr := list(key); rr.Code != 401 {
			t.Fatalf("unauthenticated inventory: status=%d body=%s", rr.Code, rr.Body.String())
		}
	}
	rr := list("path-fixture-key")
	var entries []struct{ Filename string }
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || len(entries) != 2 || strings.Contains(rr.Body.String(), "outside-owner-secret") {
		t.Fatalf("escaping/invalid pipeline listed: status=%d body=%s", rr.Code, rr.Body.String())
	}
	// An operator may relocate the configured directory through a symlink.
	relocated := t.TempDir()
	if err := os.Rename(root, filepath.Join(relocated, "workflows")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(relocated, "workflows"), root); err != nil {
		t.Fatal(err)
	}
	if rr := list("path-fixture-key"); rr.Code != 200 || !strings.Contains(rr.Body.String(), "owned.yaml") {
		t.Fatalf("configured root relocation failed: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if rr := list("path-fixture-key"); rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("missing inventory contract: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := os.WriteFile(root, []byte("blocked directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if rr := list("path-fixture-key"); rr.Code != 503 {
		t.Fatalf("directory failure acknowledged as empty inventory: status=%d body=%s", rr.Code, rr.Body.String())
	}
}
