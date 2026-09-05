package knowledge

import (
	"os"
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

// TestSaveLoadFeedback_RecentRunsRoundTrip asserts that TreeMeta.RecentRuns —
// the bounded run-history window a registered domain fitness function scores
// from (see RegisterDomainFitness) — survives a SaveFeedback/LoadFeedback
// round trip. Without this, a scheduler restart resets a domain-fitness
// tree's run history to whatever runs happen after the restart, even though
// Fitness itself (a plain field) is preserved.
func TestSaveLoadFeedback_RecentRunsRoundTrip(t *testing.T) {
	src := NewKnowledgeGraph()
	src.Register(&TreeMeta{
		ID:       "tree:history",
		Name:     "History Test",
		Category: "test",
	})

	for range 5 {
		src.RecordRun(RunRecord{
			TreeID:  "tree:history",
			Task:    "repeat",
			Outcome: "success",
			Quality: 80.0,
		})
	}

	srcRuns := src.Trees["tree:history"].RecentRuns
	if len(srcRuns) != 5 {
		t.Fatalf("setup: expected 5 recent runs recorded, got %d", len(srcRuns))
	}

	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := src.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{
		ID:       "tree:history",
		Name:     "History Test",
		Category: "test",
	})
	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	got := dst.Trees["tree:history"]
	if len(got.RecentRuns) != len(srcRuns) {
		t.Fatalf("RecentRuns length = %d, want %d (restart must not reset run history)", len(got.RecentRuns), len(srcRuns))
	}
	for i, rs := range srcRuns {
		if got.RecentRuns[i] != rs {
			t.Errorf("RecentRuns[%d] = %+v, want %+v", i, got.RecentRuns[i], rs)
		}
	}

	// A domain fitness function registered after restart must see the full
	// restored history immediately, not just runs recorded since the restart.
	scored := make(chan int, 1)
	dst.RegisterDomainFitness("tree:history", func(runs []RunSummary) float64 {
		scored <- len(runs)
		return 1.0
	})
	dst.RecordRun(RunRecord{TreeID: "tree:history", Task: "post-restart", Outcome: "success", Quality: 80.0})
	select {
	case n := <-scored:
		if n != len(srcRuns)+1 {
			t.Errorf("domain fitness fn saw %d runs, want %d (restored history + 1 new run)", n, len(srcRuns)+1)
		}
	default:
		t.Fatal("domain fitness function was never invoked")
	}
}

// TestSaveLoadFeedback_EvolvedMetadataRoundTrip asserts that StructuralFitness,
// NodeCount, and Category — populated on an evolved tree's metadata via
// RegisterEvolved, not through static registration — survive a
// SaveFeedback/LoadFeedback round trip. Without this, a daemon restart loses
// the evolved tree's structural-quality signal, node count, and inherited
// category even though the evolved tree file itself is still on disk.
func TestSaveLoadFeedback_EvolvedMetadataRoundTrip(t *testing.T) {
	src := NewKnowledgeGraph()
	src.Register(&TreeMeta{
		ID:       "tree:base",
		Name:     "Base",
		Category: "finance",
	})
	src.RegisterEvolved("tree:base", "tree:base-evolved", 42, 88.5)

	srcTree := src.Trees["tree:base-evolved"]
	wantStructural := srcTree.StructuralFitness // 88.5
	wantNodeCount := srcTree.NodeCount          // 42
	wantCategory := srcTree.Category            // "finance", inherited from base

	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := src.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	// Fresh graph where the evolved tree is re-registered (e.g. rebuilt from its
	// tree file at startup) but without the runtime structural metadata that
	// only RegisterEvolved populates — LoadFeedback must restore it.
	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{
		ID:       "tree:base-evolved",
		Name:     "Base Evolved",
		Category: "unknown",
	})

	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	got := dst.Trees["tree:base-evolved"]
	if got.StructuralFitness != wantStructural {
		t.Errorf("StructuralFitness = %.2f, want %.2f", got.StructuralFitness, wantStructural)
	}
	if got.NodeCount != wantNodeCount {
		t.Errorf("NodeCount = %d, want %d", got.NodeCount, wantNodeCount)
	}
	if got.Category != wantCategory {
		t.Errorf("Category = %q, want %q (must be restored, not left at the pre-registration placeholder)", got.Category, wantCategory)
	}
}

// TestSaveLoadFeedback_EvolvedFromEdgeRoundTrip asserts that the "evolved_from"
// edge RegisterEvolved writes between a base tree and its evolved descendant
// survives a SaveFeedback/LoadFeedback round trip, just like uses_tool edges
// already do. Without this, a daemon restart loses the evolution lineage
// (EvolutionLineage) even though both tree files and their bookkeeping
// metadata (StructuralFitness, NodeCount, Category) are otherwise restored.
func TestSaveLoadFeedback_EvolvedFromEdgeRoundTrip(t *testing.T) {
	src := NewKnowledgeGraph()
	src.Register(&TreeMeta{
		ID:       "tree:lineage-base",
		Name:     "Base",
		Category: "finance",
	})
	src.RegisterEvolved("tree:lineage-base", "tree:lineage-base-evolved", 10, 75.0)

	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := src.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	// Fresh graph: both trees are re-registered (e.g. rebuilt from tree files
	// at startup), but the evolved_from edge connecting them only exists in
	// the persisted feedback state — LoadFeedback must restore it.
	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{
		ID:       "tree:lineage-base",
		Name:     "Base",
		Category: "finance",
	})
	dst.Register(&TreeMeta{
		ID:       "tree:lineage-base-evolved",
		Name:     "Base Evolved",
		Category: "finance",
	})

	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	baseID, evolvedIDs, ok := dst.EvolutionLineage("tree:lineage-base-evolved")
	if !ok {
		t.Fatal("expected evolution lineage to be restored after LoadFeedback, got ok=false")
	}
	if baseID != "tree:lineage-base" {
		t.Errorf("baseID = %q, want tree:lineage-base", baseID)
	}
	if len(evolvedIDs) != 1 || evolvedIDs[0] != "tree:lineage-base-evolved" {
		t.Errorf("evolvedIDs = %v, want [tree:lineage-base-evolved]", evolvedIDs)
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

// TestLoadFeedback_ResurrectsUnregisteredEvolvedTree asserts that an evolved
// tree's ID found in the feedback snapshot, but not yet present in kg.Trees,
// is resurrected as a new TreeMeta instead of being silently skipped.
//
// Evolved trees are only ever added to kg.Trees at runtime via RegisterEvolved
// (called from the evolution MCP tool) — unlike static domain trees, nothing
// rebuilds them from disk at daemon startup. So after a restart, LoadFeedback
// runs before any evolution pass has re-registered the evolved tree, and its
// ID is genuinely absent from kg.Trees even though its tree file and feedback
// metadata both still exist. Skipping it here would permanently lose the
// evolved tree's StructuralFitness/NodeCount/Category/EvolvedCount bookkeeping
// even though the tree file itself survived the restart on disk.
func TestLoadFeedback_ResurrectsUnregisteredEvolvedTree(t *testing.T) {
	src := NewKnowledgeGraph()
	src.Register(&TreeMeta{
		ID:       "tree:resurrect-base",
		Name:     "Base",
		Category: "finance",
	})
	src.RegisterEvolved("tree:resurrect-base", "tree:resurrect-base-evolved", 17, 91.0)

	srcTree := src.Trees["tree:resurrect-base-evolved"]
	wantStructural := srcTree.StructuralFitness
	wantNodeCount := srcTree.NodeCount
	wantCategory := srcTree.Category
	wantEvolvedCount := srcTree.EvolvedCount

	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := src.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	// Fresh graph simulating a daemon restart: only the base tree has been
	// re-registered (as static domain trees always are at startup) — the
	// evolved tree has NOT, since no evolution pass has run yet since restart.
	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{
		ID:       "tree:resurrect-base",
		Name:     "Base",
		Category: "finance",
	})

	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	got, ok := dst.Trees["tree:resurrect-base-evolved"]
	if !ok {
		t.Fatal("expected tree:resurrect-base-evolved to be resurrected into kg.Trees, but it is missing")
	}
	if got.StructuralFitness != wantStructural {
		t.Errorf("StructuralFitness = %.2f, want %.2f", got.StructuralFitness, wantStructural)
	}
	if got.NodeCount != wantNodeCount {
		t.Errorf("NodeCount = %d, want %d", got.NodeCount, wantNodeCount)
	}
	if got.Category != wantCategory {
		t.Errorf("Category = %q, want %q", got.Category, wantCategory)
	}
	if got.EvolvedCount != wantEvolvedCount {
		t.Errorf("EvolvedCount = %d, want %d", got.EvolvedCount, wantEvolvedCount)
	}

	// The evolution lineage should also resolve now that the evolved tree is
	// itself a real node in the graph.
	baseID, evolvedIDs, ok := dst.EvolutionLineage("tree:resurrect-base-evolved")
	if !ok {
		t.Fatal("expected evolution lineage to resolve after resurrection")
	}
	if baseID != "tree:resurrect-base" {
		t.Errorf("baseID = %q, want tree:resurrect-base", baseID)
	}
	if len(evolvedIDs) != 1 || evolvedIDs[0] != "tree:resurrect-base-evolved" {
		t.Errorf("evolvedIDs = %v, want [tree:resurrect-base-evolved]", evolvedIDs)
	}
}

// TestLoadFeedback_DoesNotClobberRegisteredStaticMetadata pins the contract
// the feedbackSnapshot doc states ("static tree metadata is deliberately
// excluded so a Load merges into already-registered trees without clobbering
// it"): loading a feedback file written by a PRE-upgrade build — whose entries
// carry no category/structural_fitness/node_count keys — must not wipe the
// Category (or evolved bookkeeping) a fresh Register/RegisterEvolved just set.
// The pre-fix code assigned all three unconditionally, so the zero values from
// the old file blanked the live tree's Category; the next Save then persisted
// "" and the corruption survived every later restart.
func TestLoadFeedback_DoesNotClobberRegisteredStaticMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.json")
	// Pre-upgrade file shape: runtime feedback only, no metadata keys.
	blob := `{"trees":{"tree:static":{"fitness":61.5,"run_count":7,"last_outcome":"success"}},"tool_edges":[]}`
	if err := os.WriteFile(path, []byte(blob), 0644); err != nil {
		t.Fatal(err)
	}

	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:static", Name: "Static", Category: "domain"})
	kg.Trees["tree:static"].StructuralFitness = 42.0
	kg.Trees["tree:static"].NodeCount = 9

	if err := kg.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	tree := kg.Trees["tree:static"]
	if tree.Category != "domain" {
		t.Errorf("Category = %q, want %q preserved (a pre-upgrade feedback file must not blank registered static metadata)", tree.Category, "domain")
	}
	if tree.StructuralFitness != 42.0 {
		t.Errorf("StructuralFitness = %.1f, want 42.0 preserved", tree.StructuralFitness)
	}
	if tree.NodeCount != 9 {
		t.Errorf("NodeCount = %d, want 9 preserved", tree.NodeCount)
	}
	// Runtime feedback must still restore.
	if tree.Fitness != 61.5 || tree.RunCount != 7 {
		t.Errorf("runtime feedback not restored: fitness=%.1f runs=%d, want 61.5/7", tree.Fitness, tree.RunCount)
	}
}

// TestLoadFeedback_ResurrectedTreeIsDiscoverableWithoutReEvolution pins that
// resurrection ALONE restores discoverability. Waiting for the next
// RegisterEvolved call is not enough in production: persistEvolvedWinner
// (cmd/bt-agent/tools.go) peeks EvolvedFitnessImproves and returns before
// RegisterEvolved unless a strictly BETTER winner appears — and the restored
// StructuralFitness is exactly the strong stored value, so a resurrected
// strong tree would stay permanently undiscoverable. LoadFeedback must
// inherit the base's Capabilities/Keywords (via the restored evolved_from
// edge, whose From side is the registered base) at resurrection time.
func TestLoadFeedback_ResurrectedTreeIsDiscoverableWithoutReEvolution(t *testing.T) {
	src := NewKnowledgeGraph()
	src.Register(&TreeMeta{
		ID:       "tree:disco-base",
		Name:     "Base",
		Category: "finance",
		Keywords: []string{"ledger"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance"},
		},
	})
	src.RegisterEvolved("tree:disco-base", "tree:disco-base-evolved", 17, 91.0)

	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := src.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{
		ID:       "tree:disco-base",
		Name:     "Base",
		Category: "finance",
		Keywords: []string{"ledger"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance"},
		},
	})
	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	// No RegisterEvolved call — LoadFeedback alone must have repaired the
	// resurrected tree's metadata.
	evolved := dst.Trees["tree:disco-base-evolved"]
	if evolved == nil {
		t.Fatal("evolved tree not resurrected")
	}
	if len(evolved.Capabilities) == 0 || len(evolved.Keywords) == 0 {
		t.Fatalf("resurrected tree has Capabilities=%v Keywords=%v; LoadFeedback must inherit them from the evolved_from base so the tree is discoverable without waiting for a strictly better evolution winner", evolved.Capabilities, evolved.Keywords)
	}
	found := false
	for _, id := range dst.Synonyms {
		if id == "tree:disco-base-evolved" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no synonym entry points at the resurrected tree after LoadFeedback alone")
	}
}

// TestRegisterEvolved_FillsMetadataForResurrectedTree closes the discovery
// regression resurrection introduced: LoadFeedback pre-creates unregistered
// evolved trees as bare ID/Name shells, so the next evolution pass's
// RegisterEvolved found the tree "existing" and skipped the base-metadata
// inheritance (Capabilities/Keywords/Synonyms) it performs on first creation —
// leaving the resurrected tree permanently undiscoverable by keyword or
// capability routing. The fill must happen even when the pass's fitness does
// not beat the restored StructuralFitness.
func TestRegisterEvolved_FillsMetadataForResurrectedTree(t *testing.T) {
	src := NewKnowledgeGraph()
	src.Register(&TreeMeta{
		ID:       "tree:fill-base",
		Name:     "Base",
		Category: "finance",
		Keywords: []string{"ledger"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance"},
		},
	})
	src.RegisterEvolved("tree:fill-base", "tree:fill-base-evolved", 17, 91.0)

	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := src.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	// Restart: base re-registered, evolved tree resurrected by LoadFeedback.
	dst := NewKnowledgeGraph()
	dst.Register(&TreeMeta{
		ID:       "tree:fill-base",
		Name:     "Base",
		Category: "finance",
		Keywords: []string{"ledger"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance"},
		},
	})
	if err := dst.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback: %v", err)
	}

	// Next evolution pass: a WEAKER winner (fitness below the restored 91.0
	// structural fitness) must still fill the resurrected shell's metadata,
	// even though the bookkeeping write-back is correctly skipped.
	if updated := dst.RegisterEvolved("tree:fill-base", "tree:fill-base-evolved", 17, 50.0); updated {
		t.Fatal("a weaker winner must not update the stored bookkeeping")
	}

	evolved := dst.Trees["tree:fill-base-evolved"]
	if len(evolved.Capabilities) == 0 || evolved.Capabilities[0].Action != "analyze_financials" {
		t.Errorf("Capabilities = %v, want inherited from base — the resurrected tree stays undiscoverable without them", evolved.Capabilities)
	}
	if len(evolved.Keywords) == 0 {
		t.Errorf("Keywords = %v, want inherited from base", evolved.Keywords)
	}
	if got := dst.Synonyms["analyze_financials"]; got != "tree:fill-base-evolved" && got != "tree:fill-base" {
		t.Errorf("Synonyms[analyze_financials] = %q, want indexed", got)
	}
	// The synonym index must be able to reach the evolved tree via at least
	// one of its inherited terms.
	found := false
	for _, id := range dst.Synonyms {
		if id == "tree:fill-base-evolved" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no synonym entry points at the resurrected evolved tree; it is unreachable via keyword/capability discovery")
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
	for i := range flushes {
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
