package agent

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

func TestExportPreviousRuns(t *testing.T) {
	histDir := t.TempDir()
	hist, err := NewHistory(histDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = hist.Record(RunRecord{
		AgentName: "demo",
		Task:      "prior task",
		Outcome:   "success",
		Output:    strings.Repeat("Y", 500),
	})

	block := exportPreviousRuns(hist, "demo", 1)
	if !strings.Contains(block, "prior task") || !strings.Contains(block, "YYY") {
		t.Fatalf("unexpected export: %q", block[:min(80, len(block))])
	}
}

func TestSeedMemoryToBlackboard_HistoryOffloaded(t *testing.T) {
	histDir := t.TempDir()
	hist, err := NewHistory(histDir)
	if err != nil {
		t.Fatal(err)
	}
	longOut := strings.Repeat("Z", 4000)
	_ = hist.Record(RunRecord{
		AgentName: "demo",
		Task:      "big output run",
		Outcome:   "success",
		Output:    longOut,
	})

	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_seed", "", "demo")
	d := &RunDeps{History: hist}

	task := d.seedMemoryToBlackboard("demo", "do work", 1, h)
	if strings.Contains(task, longOut) {
		t.Fatal("full history output should not be in prompt")
	}
	if !strings.Contains(task, "bb_read") {
		t.Fatal("expected blackboard hint")
	}
	e, err := h.Get("history/runs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Value, longOut) {
		t.Fatal("full output should be on blackboard")
	}
}

func TestSeedMemoryToBlackboard_NilHandle(t *testing.T) {
	d := &RunDeps{}
	got := d.seedMemoryToBlackboard("a", "task", 2, nil)
	if got != "task" {
		t.Fatalf("expected unchanged task, got %q", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
