package main

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"
)

// TestProgramStatusLines_MidProgress covers the normal case: a program with
// some milestones done and one still pending should point at the pending one
// by its 1-based index.
func TestProgramStatusLines_MidProgress(t *testing.T) {
	p := &research.Program{
		Title: "Deepen path detection",
		Milestones: []research.Milestone{
			{Goal: "m1", Status: "done"},
			{Goal: "m2", Status: "pending"},
			{Goal: "m3", Status: "pending"},
		},
	}

	got := programStatusLines(p)

	if !strings.Contains(got, "milestones 1/3 done") {
		t.Errorf("expected done count %q in output, got:\n%s", "milestones 1/3 done", got)
	}
	if !strings.Contains(got, "next 2/3") {
		t.Errorf("expected pending pointer %q in output, got:\n%s", "next 2/3", got)
	}
}

// TestProgramStatusLines_AllDone is the regression anchor. NextMilestone
// returns (-1, nil) when nothing is pending, so the old `idx, _ :=
// active.NextMilestone()` + `next idx+1/N` renders the bogus "next 0/3" for a
// program whose milestones are all complete. The helper must not emit a
// "next 0/" pointer in that state.
func TestProgramStatusLines_AllDone(t *testing.T) {
	p := &research.Program{
		Title: "Finished program",
		Milestones: []research.Milestone{
			{Goal: "m1", Status: "done"},
			{Goal: "m2", Status: "done"},
			{Goal: "m3", Status: "done"},
		},
	}

	got := programStatusLines(p)

	if strings.Contains(got, "next 0/") {
		t.Errorf("all-done program must not render a bogus next-milestone pointer, got:\n%s", got)
	}
	if !strings.Contains(got, "milestones 3/3 done") {
		t.Errorf("expected done count %q in output, got:\n%s", "milestones 3/3 done", got)
	}
}
