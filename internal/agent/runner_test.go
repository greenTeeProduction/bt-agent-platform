package agent

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestRunOnce_RequiresDeps(t *testing.T) {
	var d *RunDeps
	_, err := d.RunOnce(context.Background(), "x", "task", RunOptions{})
	if err == nil {
		t.Fatal("expected error for nil deps")
	}
}

func TestRunOnce_EmptyAgentName(t *testing.T) {
	d := &RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode { return nil },
	}
	_, err := d.RunOnce(context.Background(), "", "task", RunOptions{})
	if err == nil {
		t.Fatal("expected error for empty agent name")
	}
}

func TestRunOnce_NoTreeFound(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := &RunDeps{
		Registry: reg,
		ResolveTree: func(_ string) *evolution.SerializableNode {
			return nil
		},
	}
	res, err := d.RunOnce(context.Background(), "missing-agent", "do something", RunOptions{})
	if err == nil {
		t.Fatal("expected error when tree not found")
	}
	if res == nil || res.Outcome != "failure" {
		t.Fatalf("expected failure outcome, got %+v", res)
	}
}

func TestHistoryQualityScore_UsesSpecWhenHigher(t *testing.T) {
	inst := &Instance{
		Definition: Definition{
			Quality: &QualitySpec{MinLength: 100},
		},
	}
	longOut := string(make([]byte, 150))
	for i := range longOut {
		longOut = longOut[:i] + "x" + longOut[i+1:]
	}
	// simpler: just repeat
	longOut = repeatChar('x', 150)
	score := historyQualityScore(inst, "success", longOut)
	if score < 0.5 {
		t.Fatalf("expected quality score >= 0.5, got %f", score)
	}
}

func repeatChar(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
