package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"

	"github.com/tmc/langchaingo/llms"
)

// mockLangLLM implements llms.Model with a canned response. The langchaingo
// ReAct executor calls GenerateContent (not Call), so both must return
// content in the "Final Answer:" format the ReAct parser expects.
type mockLangLLM struct {
	content string
	err     error
}

func (m *mockLangLLM) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	return m.content, m.err
}

func (m *mockLangLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: m.content}},
	}, nil
}

func newTestServer(t *testing.T) *langAgentServer {
	t.Helper()
	s, err := newLangAgentServer(t.TempDir(), engine.NewMockLLM(), &mockLangLLM{content: "Final Answer: test passed"})
	if err != nil {
		t.Fatalf("newLangAgentServer: %v", err)
	}
	return s
}

func seedRecords(t *testing.T, s *langAgentServer, records []evolution.Record) {
	t.Helper()
	for _, r := range records {
		rec := r
		if err := s.refStore.Save(&rec); err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
}

func TestNewLangAgentServer_InitializesStoresAndDefaultTree(t *testing.T) {
	dir := t.TempDir()
	s, err := newLangAgentServer(dir, engine.NewMockLLM(), &mockLangLLM{})
	if err != nil {
		t.Fatalf("newLangAgentServer: %v", err)
	}
	if s.refStore == nil || s.treeStore == nil || s.bb == nil || s.bt == nil || s.evolved == nil {
		t.Fatalf("newLangAgentServer left a nil field: %+v", s)
	}

	wantTreePath := filepath.Join(dir, ".go-bt-reflections", "tree.json")
	if s.treeStore.Path() != wantTreePath {
		t.Fatalf("tree path = %q, want %q", s.treeStore.Path(), wantTreePath)
	}
	if _, err := os.Stat(wantTreePath); err != nil {
		t.Fatalf("expected newLangAgentServer to persist a default tree: %v", err)
	}

	tree, err := s.treeStore.Load()
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	if got, want := evolution.CountNodes(tree), evolution.CountNodes(evolution.DefaultTree()); got != want {
		t.Fatalf("default tree node count = %d, want %d", got, want)
	}

	// AgentFactory always constructs successfully against a writable temp
	// home (it only fails if its stores can't be created), so the
	// create-agent tool is registered alongside the 6 base tools.
	if got := len(s.evolved.Tools); got != 7 {
		t.Fatalf("evolved.Tools = %d, want 7 (6 base + create_agent via AgentFactory)", got)
	}
}

func TestNewLangAgentServer_LoadsExistingTreeInsteadOfOverwriting(t *testing.T) {
	dir := t.TempDir()
	refDir := filepath.Join(dir, ".go-bt-reflections")
	ts, err := evolution.NewTreeStore(refDir)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	custom := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "AnalyzeTask"},
		},
	}
	if err := ts.Save(custom); err != nil {
		t.Fatalf("save custom tree: %v", err)
	}

	s, err := newLangAgentServer(dir, engine.NewMockLLM(), &mockLangLLM{})
	if err != nil {
		t.Fatalf("newLangAgentServer: %v", err)
	}

	tree, err := s.treeStore.Load()
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	if got := evolution.CountNodes(tree); got != 2 {
		t.Fatalf("expected the pre-existing 2-node tree to survive server init, got %d nodes", got)
	}
}

func TestHandleFitness(t *testing.T) {
	tests := []struct {
		name           string
		records        []evolution.Record
		wantTotalTasks int
		wantSuccesses  int
		wantFailures   int
		wantRate       string
	}{
		{
			// refStore and treeStore share the same ~/.go-bt-reflections
			// directory; LoadAll must filter to the reflection-*.json files
			// Save writes so tree.json isn't decoded as a phantom record —
			// same fix pinned for cmd/bt-evaluator's ev_evaluate handler.
			name:           "no seeded reflections excludes the tree.json sibling file",
			records:        nil,
			wantTotalTasks: 0,
			wantSuccesses:  0,
			wantFailures:   0,
			wantRate:       "0.0%",
		},
		{
			name: "mixed outcomes",
			records: []evolution.Record{
				{TaskID: "t1", Task: "one", Outcome: evolution.Success},
				{TaskID: "t2", Task: "two", Outcome: evolution.Failure},
				{TaskID: "t3", Task: "three", Outcome: evolution.Failure},
			},
			wantTotalTasks: 3,
			wantSuccesses:  1,
			wantFailures:   2,
			wantRate:       "33.3%",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			seedRecords(t, s, tc.records)

			result := s.handleFitness(nil)
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
				t.Fatalf("unmarshal handleFitness output %q: %v", result.Content[0].Text, err)
			}
			if got := out["total_tasks"]; got != float64(tc.wantTotalTasks) {
				t.Errorf("total_tasks = %v, want %d", got, tc.wantTotalTasks)
			}
			if got := out["successes"]; got != float64(tc.wantSuccesses) {
				t.Errorf("successes = %v, want %d", got, tc.wantSuccesses)
			}
			if got := out["failures"]; got != float64(tc.wantFailures) {
				t.Errorf("failures = %v, want %d", got, tc.wantFailures)
			}
			if got := out["success_rate"]; got != tc.wantRate {
				t.Errorf("success_rate = %v, want %q", got, tc.wantRate)
			}
			if got := out["tools"]; got != float64(7) {
				t.Errorf("tools = %v, want 7", got)
			}
			if _, ok := out["node_count"]; !ok {
				t.Errorf("handleFitness output missing node_count field: %v", out)
			}
		})
	}
}

func TestHandleEvolve_DefaultTreeHasNoMatchingTargetsSoNothingApplies(t *testing.T) {
	s := newTestServer(t)

	result := s.handleEvolve(nil)
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleEvolve output %q: %v", result.Content[0].Text, err)
	}
	if got := out["evolved"]; got != false {
		t.Fatalf("evolved = %v, want false", got)
	}
	if got := out["mutations"]; got != float64(0) {
		t.Fatalf("mutations = %v, want 0", got)
	}
	if out["nodes_before"] != out["nodes_after"] {
		t.Fatalf("nodes_before/after should be unchanged when no mutation applies: %v vs %v", out["nodes_before"], out["nodes_after"])
	}
}

func TestHandleEvolve_TreeWithAnalyzeTaskGetsWrapRetryApplied(t *testing.T) {
	dir := t.TempDir()
	refDir := filepath.Join(dir, ".go-bt-reflections")
	ts, err := evolution.NewTreeStore(refDir)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	custom := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "AnalyzeTask", Description: "analyze"},
		},
	}
	if err := ts.Save(custom); err != nil {
		t.Fatalf("save custom tree: %v", err)
	}

	s, err := newLangAgentServer(dir, engine.NewMockLLM(), &mockLangLLM{})
	if err != nil {
		t.Fatalf("newLangAgentServer: %v", err)
	}

	result := s.handleEvolve(nil)
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleEvolve output %q: %v", result.Content[0].Text, err)
	}
	if got := out["evolved"]; got != true {
		t.Fatalf("evolved = %v, want true", got)
	}
	mutations, _ := out["mutations"].(float64)
	if mutations < 1 {
		t.Fatalf("mutations = %v, want >= 1", mutations)
	}
	before, _ := out["nodes_before"].(float64)
	after, _ := out["nodes_after"].(float64)
	if after <= before {
		t.Fatalf("nodes_after (%v) should exceed nodes_before (%v) after wrap_retry", after, before)
	}

	reloaded, err := s.treeStore.Load()
	if err != nil {
		t.Fatalf("reload tree: %v", err)
	}
	if got := evolution.CountNodes(reloaded); got != int(after) {
		t.Fatalf("persisted tree node count = %d, want %d (handleEvolve should save applied mutations)", got, int(after))
	}
}

func TestHandleRun_BadJSONReturnsError(t *testing.T) {
	s := newTestServer(t)

	result := s.handleRun(json.RawMessage(`not json`))
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleRun output %q: %v", result.Content[0].Text, err)
	}
	if _, ok := out["error"]; !ok {
		t.Fatalf("handleRun with malformed JSON = %q, want an error field", result.Content[0].Text)
	}
}

func TestHandleRun_SuccessReturnsResultAndOutcome(t *testing.T) {
	dir := t.TempDir()
	s, err := newLangAgentServer(dir, engine.NewMockLLM(), &mockLangLLM{content: "Final Answer: test passed"})
	if err != nil {
		t.Fatalf("newLangAgentServer: %v", err)
	}

	result := s.handleRun(json.RawMessage(`{"task": "do the thing"}`))
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleRun output %q: %v", result.Content[0].Text, err)
	}
	if got := out["result"]; got != "test passed" {
		t.Fatalf("result = %v, want %q", got, "test passed")
	}
	if _, ok := out["outcome"]; !ok {
		t.Fatalf("handleRun output missing outcome field: %v", out)
	}
}

func TestHandleRun_LLMErrorReturnsError(t *testing.T) {
	dir := t.TempDir()
	s, err := newLangAgentServer(dir, engine.NewMockLLM(), &mockLangLLM{err: context.DeadlineExceeded})
	if err != nil {
		t.Fatalf("newLangAgentServer: %v", err)
	}

	result := s.handleRun(json.RawMessage(`{"task": "do the thing"}`))
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleRun output %q: %v", result.Content[0].Text, err)
	}
	if _, ok := out["error"]; !ok {
		t.Fatalf("handleRun with failing LLM = %q, want an error field", result.Content[0].Text)
	}
}
