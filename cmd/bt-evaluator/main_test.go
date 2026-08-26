package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// sampleTree returns a minimal but non-trivial tree usable across handler tests.
func sampleTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "DoThing"},
			{Type: "Action", Name: "DoOtherThing"},
		},
	}
}

func seedRecords(t *testing.T, s *evaluatorServer) {
	t.Helper()
	records := []evolution.Record{
		{TaskID: "t1", Task: "task one", Outcome: evolution.Success, DurationMs: 100},
		{TaskID: "t2", Task: "task two", Outcome: evolution.Failure, DurationMs: 200},
	}
	for _, r := range records {
		rec := r
		if err := s.refStore.Save(&rec); err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
}

func newTestEvaluatorServer(t *testing.T) *evaluatorServer {
	t.Helper()
	s, err := newEvaluatorServer(t.TempDir())
	if err != nil {
		t.Fatalf("newEvaluatorServer: %v", err)
	}
	return s
}

func TestNewEvaluatorServer_InitializesStoresUnderRefDir(t *testing.T) {
	dir := t.TempDir()
	s, err := newEvaluatorServer(dir)
	if err != nil {
		t.Fatalf("newEvaluatorServer: %v", err)
	}
	if s.refStore == nil || s.treeStore == nil || s.tt == nil {
		t.Fatalf("newEvaluatorServer left a nil store: %+v", s)
	}
	wantTTPath := filepath.Join(dir, "transposition.json")
	if s.tt.Path() != wantTTPath {
		t.Fatalf("tt path = %q, want %q", s.tt.Path(), wantTTPath)
	}
}

func TestHandleEvaluate_NoTreeLoadedReturnsError(t *testing.T) {
	s := newTestEvaluatorServer(t)

	result := s.handleEvaluate(nil)
	if len(result.Content) != 1 {
		t.Fatalf("handleEvaluate content = %d items, want 1", len(result.Content))
	}
	if got := result.Content[0].Text; got != `{"error": "no tree loaded"}` {
		t.Fatalf("handleEvaluate() with no tree = %q, want the no-tree-loaded stub", got)
	}
}

func TestHandleEvaluate_WithTreeAndRecordsReportsFitnessFields(t *testing.T) {
	s := newTestEvaluatorServer(t)
	if err := s.treeStore.Save(sampleTree()); err != nil {
		t.Fatalf("save tree: %v", err)
	}
	seedRecords(t, s)

	result := s.handleEvaluate(nil)
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleEvaluate output %q: %v", result.Content[0].Text, err)
	}

	for _, field := range []string{"success_rate", "avg_duration_ms", "node_count", "stability", "path_coverage", "composite", "total_tasks"} {
		if _, ok := out[field]; !ok {
			t.Fatalf("handleEvaluate output missing field %q: %v", field, out)
		}
	}
	// refStore and treeStore share the same directory (mirroring production's
	// shared ~/.go-bt-reflections layout); LoadAll must filter to the
	// reflection-*.json files Save writes so tree.json isn't decoded as an
	// extra phantom record alongside the 2 seeded ones.
	if got := out["total_tasks"]; got != float64(2) {
		t.Fatalf("total_tasks = %v, want 2", got)
	}
	if got := out["success_rate"]; got != "50.0%" {
		t.Fatalf("success_rate = %v, want \"50.0%%\"", got)
	}
}

func TestHandleOrderMutations_ReturnsCandidatesAndTotal(t *testing.T) {
	s := newTestEvaluatorServer(t)
	if err := s.treeStore.Save(sampleTree()); err != nil {
		t.Fatalf("save tree: %v", err)
	}
	seedRecords(t, s)

	result := s.handleOrderMutations(nil)
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleOrderMutations output %q: %v", result.Content[0].Text, err)
	}

	candidates, ok := out["candidates"].([]any)
	if !ok {
		t.Fatalf("candidates field missing or wrong type: %v", out)
	}
	total, ok := out["total"].(float64)
	if !ok {
		t.Fatalf("total field missing or wrong type: %v", out)
	}
	if int(total) != len(candidates) {
		t.Fatalf("total = %v, want len(candidates) = %d", total, len(candidates))
	}
}

func TestHandleDeepen_DefaultsMaxDepthAndAutoSavesTT(t *testing.T) {
	s := newTestEvaluatorServer(t)
	if err := s.treeStore.Save(sampleTree()); err != nil {
		t.Fatalf("save tree: %v", err)
	}
	seedRecords(t, s)

	result := s.handleDeepen(json.RawMessage(`{}`))
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleDeepen output %q: %v", result.Content[0].Text, err)
	}
	if got := out["depth"]; got != float64(2) {
		t.Fatalf("depth = %v, want default 2", got)
	}
	if _, err := os.Stat(s.tt.Path()); err != nil {
		t.Fatalf("expected handleDeepen to auto-save the transposition table: %v", err)
	}
}

func TestHandleDeepen_HonorsExplicitMaxDepth(t *testing.T) {
	s := newTestEvaluatorServer(t)
	if err := s.treeStore.Save(sampleTree()); err != nil {
		t.Fatalf("save tree: %v", err)
	}
	seedRecords(t, s)

	result := s.handleDeepen(json.RawMessage(`{"max_depth": 1}`))
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleDeepen output %q: %v", result.Content[0].Text, err)
	}
	if got := out["depth"]; got != float64(1) {
		t.Fatalf("depth = %v, want explicit 1", got)
	}
}

func TestHandleTTStats_ReportsEntriesMaxSizeAndPath(t *testing.T) {
	dir := t.TempDir()
	s, err := newEvaluatorServer(dir)
	if err != nil {
		t.Fatalf("newEvaluatorServer: %v", err)
	}

	result := s.handleTTStats(nil)
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleTTStats output %q: %v", result.Content[0].Text, err)
	}
	if got := out["entries"]; got != float64(0) {
		t.Fatalf("entries = %v, want 0 for a fresh table", got)
	}
	if got := out["max_size"]; got != float64(1000) {
		t.Fatalf("max_size = %v, want 1000", got)
	}
	wantPath := filepath.Join(dir, "transposition.json")
	if got := out["path"]; got != wantPath {
		t.Fatalf("path = %v, want %q", got, wantPath)
	}
}

func TestHandleTTSave_PersistsAndReportsEntryCount(t *testing.T) {
	s := newTestEvaluatorServer(t)
	if err := s.treeStore.Save(sampleTree()); err != nil {
		t.Fatalf("save tree: %v", err)
	}
	seedRecords(t, s)
	// Populate the TT via a deepen call so there is something to persist.
	s.handleDeepen(json.RawMessage(`{}`))

	result := s.handleTTSave(nil)
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal handleTTSave output %q: %v", result.Content[0].Text, err)
	}
	if got := out["saved"]; got != true {
		t.Fatalf("saved = %v, want true", got)
	}
	if _, err := os.Stat(s.tt.Path()); err != nil {
		t.Fatalf("expected handleTTSave to write the TT file: %v", err)
	}
}
