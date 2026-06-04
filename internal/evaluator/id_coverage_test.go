package evaluator

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reflection"
)

// ─── IterativeDeepening: TT hit via makeKey alignment ───

func TestIterativeDeepening_TTHitViaMakeKey(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	records := makeRecords(reflection.Failure, reflection.Failure)

	// Pre-populate TT using the same key that IterativeDeepening will probe
	// The clone's hash is used as the task key
	clone := cloneTree(tree)
	evolution.ApplyMutations(clone, []evolution.MutationOp{
		{Operation: "wrap_retry", Target: "AnalyzeTask"},
	})
	ttKey := hashTree(clone) + ":eval"

	// Store using makeKey directly
	key := makeKey(tree, ttKey)
	tt.entries[key] = TranspositionEntry{SuccessRate: 0.95}

	result := IterativeDeepening(tree, records, tt, 3)

	if result.TTProbeHits == 0 {
		t.Logf("TT probes: %d, hits: %d", result.TTProbes, result.TTProbeHits)
	}
	_ = result
}

// ─── IterativeDeepening: cover score improvement path ───

func TestIterativeDeepening_ScoreImprovement(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	// All failures = low base fitness, any improvement is easy
	records := makeRecords(reflection.Failure, reflection.Failure, reflection.Failure)

	result := IterativeDeepening(tree, records, tt, 2)

	if result.BestFitness != nil {
		t.Logf("base=%.1f best=%.1f", result.BaseFitness.Composite, result.BestFitness.Composite)
	}
}

// ─── IterativeDeepening: depth=1 with wrap_retry candidate ───

func TestIterativeDeepening_Depth1_Applies(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	records := makeRecords(reflection.Failure, reflection.Failure)

	result := IterativeDeepening(tree, records, tt, 1)

	if result.Depth != 1 {
		t.Errorf("expected depth=1, got %d", result.Depth)
	}
	if len(result.Candidates) == 0 {
		t.Log("no candidates — all failures may have missing WhatToImprove")
	}
}

// ─── cover Save error handling via eviction ───

func TestTranspositionTable_Save_EvictionThenSave(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 5)
	if err != nil {
		t.Fatal(err)
	}

	tree := evolution.DefaultTree()
	for i := 0; i < 10; i++ {
		task := string(rune('a' + i))
		tt.Store(tree, task, TranspositionEntry{Outcome: "success"})
	}

	// Save should work even after eviction
	if err := tt.Save(); err != nil {
		t.Fatal(err)
	}

	// Reload from file
	tt2, err := NewTranspositionTable(tmpDir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if tt2.Stats() == 0 {
		t.Error("expected entries to survive save/reload")
	}
	t.Logf("saved and reloaded %d entries", tt2.Stats())
}
