package knowledge

import (
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// Feedback persistence — SaveFeedback / LoadFeedback round-trip
// =============================================================================

// TestSaveLoadFeedback_RoundTrip records runtime feedback on one graph, saves it
// to disk, then restores it into a fresh graph whose trees carry static metadata
// only. The restored graph must recover Fitness/RunCount/LastOutcome/LastDuration
// and the uses_tool edges, without clobbering the static metadata already present.
func TestSaveLoadFeedback_RoundTrip(t *testing.T) {
	src := NewKnowledgeGraph()
	src.Register(&TreeMeta{
		ID:          "tree:persist",
		Name:        "Persist Test",
		Category:    "test",
		Description: "static description",
		Fitness:     50.0,
	})

	src.RecordRun(RunRecord{
		TreeID:   "tree:persist",
		Task:     "do the thing",
		Outcome:  "success",
		Duration: 2 * time.Second,
		Tools:    []string{"web_search", "calculator"},
	})

	srcTree := src.Trees["tree:persist"]
	wantFitness := srcTree.Fitness       // EMA: 0.9*50 + 0.1*100 = 55
	wantRunCount := srcTree.RunCount     // 1
	wantOutcome := srcTree.LastOutcome   // "success"
	wantDuration := srcTree.LastDuration // 2s

	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := src.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	// Fresh graph with the SAME tree registered, but only static metadata —
	// no runtime feedback yet. Load must merge feedback in without clobbering
	// the static Description.
	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{
		ID:          "tree:persist",
		Name:        "Persist Test",
		Category:    "test",
		Description: "static description",
	})

	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	got := dst.Trees["tree:persist"]
	if got == nil {
		t.Fatal("tree:persist missing after LoadFeedback")
	}
	if got.Fitness != wantFitness {
		t.Errorf("Fitness = %.2f, want %.2f", got.Fitness, wantFitness)
	}
	if got.RunCount != wantRunCount {
		t.Errorf("RunCount = %d, want %d", got.RunCount, wantRunCount)
	}
	if got.LastOutcome != wantOutcome {
		t.Errorf("LastOutcome = %q, want %q", got.LastOutcome, wantOutcome)
	}
	if got.LastDuration != wantDuration {
		t.Errorf("LastDuration = %v, want %v", got.LastDuration, wantDuration)
	}
	// Static metadata must survive the merge.
	if got.Description != "static description" {
		t.Errorf("Description = %q, want static metadata preserved", got.Description)
	}

	// The uses_tool edge for web_search must be restored.
	if !hasToolEdge(dst, "tree:persist", "web_search") {
		t.Errorf("expected uses_tool edge tree:persist -> tool:web_search after LoadFeedback")
	}
}

// TestLoadFeedback_MissingFileNoError asserts that loading from a nonexistent
// path is a no-op: it returns nil and leaves the graph untouched.
func TestLoadFeedback_MissingFileNoError(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:solo", Name: "Solo", Category: "test"})

	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if err := kg.LoadFeedback(missing); err != nil {
		t.Fatalf("LoadFeedback on missing file should be nil, got %v", err)
	}

	tree := kg.Trees["tree:solo"]
	if tree.RunCount != 0 || tree.LastOutcome != "" {
		t.Errorf("graph should be untouched, got RunCount=%d LastOutcome=%q", tree.RunCount, tree.LastOutcome)
	}
}

// =============================================================================
// Debounced persistence — dirty flag + throttled write + force-on-shutdown
// =============================================================================

// TestFeedbackFlush_ThrottlesWrites asserts that bursty flushes do NOT rewrite
// the whole graph on every call. With a large min-interval, only the first flush
// lands on disk (the throttle window opens at construction time); subsequent
// dirty flushes within the window are suppressed, and the pending change stays
// marked dirty so a later forced/expired flush can still capture it.
func TestFeedbackFlush_ThrottlesWrites(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:throttle", Name: "Throttle", Category: "test", Fitness: 50.0})

	path := filepath.Join(t.TempDir(), "feedback.json")
	// A huge interval means every flush after the first is inside the throttle
	// window for the whole (millisecond-scale) duration of this test.
	kg.ConfigureFeedbackPersistence(path, time.Hour)

	const flushes = 5
	for i := 0; i < flushes; i++ {
		// Each iteration mutates feedback and marks the graph dirty, then flushes.
		kg.RecordRun(RunRecord{TreeID: "tree:throttle", Task: "burst", Outcome: "success", Duration: time.Second})
		kg.MarkFeedbackDirty()
		if err := kg.FlushFeedback(false); err != nil {
			t.Fatalf("FlushFeedback(false) #%d: %v", i, err)
		}
	}

	kg.mu.RLock()
	gotWrites := kg.feedbackPersist.writeCount
	gotDirty := kg.feedbackPersist.dirty
	kg.mu.RUnlock()

	if gotWrites != 1 {
		t.Errorf("write count = %d, want 1 (throttled to a single write over %d flushes)", gotWrites, flushes)
	}
	// The last N-1 mutations were never persisted, so the graph must still be
	// dirty — nothing has flushed the pending state to disk.
	if !gotDirty {
		t.Error("dirty flag should remain set: pending mutations were throttled, not written")
	}
}

// TestFeedbackFlush_ForceOnShutdown asserts that a throttled window suppresses
// writes, but a forced flush (shutdown) lands the pending state and clears the
// dirty flag — and that a fresh graph then recovers the LATEST Fitness/RunCount.
func TestFeedbackFlush_ForceOnShutdown(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:shutdown", Name: "Shutdown", Category: "test", Fitness: 50.0})

	path := filepath.Join(t.TempDir(), "feedback.json")
	kg.ConfigureFeedbackPersistence(path, time.Hour)

	// First run + flush: the throttle window is open at construction, so this one
	// lands on disk (RunCount == 1).
	kg.RecordRun(RunRecord{TreeID: "tree:shutdown", Task: "first", Outcome: "success", Duration: time.Second})
	kg.MarkFeedbackDirty()
	if err := kg.FlushFeedback(false); err != nil {
		t.Fatalf("FlushFeedback(false) initial: %v", err)
	}

	// Second run: mutates feedback but is inside the throttle window, so a
	// non-forced flush must NOT touch disk.
	kg.RecordRun(RunRecord{TreeID: "tree:shutdown", Task: "second", Outcome: "success", Duration: time.Second})
	kg.MarkFeedbackDirty()
	if err := kg.FlushFeedback(false); err != nil {
		t.Fatalf("FlushFeedback(false) throttled: %v", err)
	}

	// Confirm the throttled write was suppressed: disk still holds RunCount == 1.
	mid := NewKnowledgeGraph()
	mid.Register(&TreeMeta{ID: "tree:shutdown", Name: "Shutdown", Category: "test"})
	if err := mid.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback (mid): %v", err)
	}
	if got := mid.Trees["tree:shutdown"].RunCount; got != 1 {
		t.Errorf("mid RunCount = %d, want 1 (second flush must be throttled)", got)
	}

	wantFitness := kg.Trees["tree:shutdown"].Fitness
	wantRunCount := kg.Trees["tree:shutdown"].RunCount // 2

	// Forced flush (shutdown) must land the pending state regardless of throttle.
	if err := kg.FlushFeedback(true); err != nil {
		t.Fatalf("FlushFeedback(true) force: %v", err)
	}

	kg.mu.RLock()
	stillDirty := kg.feedbackPersist.dirty
	kg.mu.RUnlock()
	if stillDirty {
		t.Error("dirty flag should be cleared after a successful forced flush")
	}

	// A fresh graph must recover the LATEST feedback written on shutdown.
	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{ID: "tree:shutdown", Name: "Shutdown", Category: "test"})
	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback (final): %v", err)
	}
	got := dst.Trees["tree:shutdown"]
	if got.Fitness != wantFitness {
		t.Errorf("recovered Fitness = %.2f, want %.2f", got.Fitness, wantFitness)
	}
	if got.RunCount != wantRunCount {
		t.Errorf("recovered RunCount = %d, want %d", got.RunCount, wantRunCount)
	}
}

// hasToolEdge reports whether kg holds a uses_tool edge from treeID to tool:<tool>.
func hasToolEdge(kg *KnowledgeGraph, treeID, tool string) bool {
	for _, e := range kg.Edges {
		if e.From == treeID && e.To == "tool:"+tool && e.Type == "uses_tool" {
			return true
		}
	}
	return false
}
