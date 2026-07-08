package evolution

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewExperienceBank_Empty(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	if eb.Count() != 0 {
		t.Errorf("expected 0 entries, got %d", eb.Count())
	}
	if eb.PersistPath == "" {
		t.Error("PersistPath should be set")
	}
}

func TestAddFromMutation_NoLLM(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	tree := DefaultTree()
	op := MutationOp{Operation: "add_before", Target: "HasClearTask"}

	err = eb.AddFromMutation(tree, op, 0.35, 0.50, nil)
	if err != nil {
		t.Fatalf("AddFromMutation: %v", err)
	}

	if eb.Count() != 1 {
		t.Fatalf("expected 1 entry, got %d", eb.Count())
	}

	entry := eb.Entries[0]
	if entry.TreeType != "Default" {
		t.Errorf("expected TreeType=Default, got %s", entry.TreeType)
	}
	if entry.MutationOp != "add_before" {
		t.Errorf("expected MutationOp=add_before, got %s", entry.MutationOp)
	}
	if entry.FitnessDelta <= 0 {
		t.Error("expected positive FitnessDelta")
	}
	if entry.QualityScore <= 0 {
		t.Error("expected QualityScore > 0")
	}

	// Verify context was generated without LLM
	if entry.Context == "" {
		t.Error("Context should not be empty even without LLM")
	}
}

func TestAddFromMutation_RejectsRegression(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	tree := DefaultTree()
	op := MutationOp{Operation: "replace_node", Target: "SetupTools"}

	// Regression: fitness went down
	err = eb.AddFromMutation(tree, op, 0.50, 0.35, nil)
	if err != nil {
		t.Fatalf("AddFromMutation should not error on regression: %v", err)
	}

	if eb.Count() != 0 {
		t.Errorf("expected 0 entries after regression, got %d", eb.Count())
	}
}

func TestAddFromMutation_NoChange(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	tree := DefaultTree()
	op := MutationOp{Operation: "add_after", Target: "ValidateInput"}

	err = eb.AddFromMutation(tree, op, 0.50, 0.50, nil)
	if err != nil {
		t.Fatalf("AddFromMutation: %v", err)
	}

	if eb.Count() != 0 {
		t.Errorf("expected 0 entries when fitness unchanged, got %d", eb.Count())
	}
}

func TestRetrieve_Empty(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	results := eb.Retrieve("GoDev add_before", 3)
	if results != nil {
		t.Errorf("expected nil from empty bank, got %d results", len(results))
	}
}

func TestRetrieve_ReturnsTopK(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	tree := DefaultTree()

	// Add several entries
	for i, op := range []string{"add_before", "add_after", "wrap_retry", "add_fallback", "increase_retries"} {
		opObj := MutationOp{Operation: op, Target: "TestNode"}
		fitnessBefore := float64(30+i) / 100.0
		fitnessAfter := fitnessBefore + 0.15
		if err := eb.AddFromMutation(tree, opObj, fitnessBefore, fitnessAfter, nil); err != nil {
			t.Fatalf("AddFromMutation %d: %v", i, err)
		}
	}

	if eb.Count() != 5 {
		t.Fatalf("expected 5 entries, got %d", eb.Count())
	}

	// Retrieve top 3
	results := eb.Retrieve("Default add_before TestNode", 3)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Top result should be for "add_before" on "TestNode" in "Default"
	top := results[0]
	if top.MutationOp != "add_before" || top.TargetNode != "TestNode" {
		t.Errorf("expected top result: add_before on TestNode, got %s on %s", top.MutationOp, top.TargetNode)
	}
}

func TestRetrieveByTreeType(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	defaultTree := DefaultTree()
	goDevTree := GoDeveloperTree()

	// Add entries for both tree types
	_ = eb.AddFromMutation(defaultTree, MutationOp{Operation: "add_before", Target: "N1"}, 0.3, 0.5, nil)
	_ = eb.AddFromMutation(defaultTree, MutationOp{Operation: "add_after", Target: "N2"}, 0.3, 0.55, nil)
	_ = eb.AddFromMutation(goDevTree, MutationOp{Operation: "wrap_retry", Target: "N3"}, 0.3, 0.6, nil)

	// Retrieve by GoDev type
	results := eb.RetrieveByTreeType("GoDev", 5)
	if len(results) != 1 {
		t.Errorf("expected 1 GoDev result, got %d", len(results))
	}
	if results[0].MutationOp != "wrap_retry" {
		t.Errorf("expected wrap_retry, got %s", results[0].MutationOp)
	}

	// Retrieve by Default type
	results = eb.RetrieveByTreeType("Default", 5)
	if len(results) != 2 {
		t.Errorf("expected 2 Default results, got %d", len(results))
	}
}

func TestRetrieve_RespectsTopK(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	tree := DefaultTree()
	for i := 0; i < 10; i++ {
		op := MutationOp{Operation: "add_before", Target: "N"}
		_ = eb.AddFromMutation(tree, op, 0.3, 0.3+float64(i)*0.02, nil)
	}

	results := eb.Retrieve("Default", 1)
	if len(results) != 1 {
		t.Errorf("topK=1 should return 1 result, got %d", len(results))
	}

	results = eb.Retrieve("Default", 100)
	if len(results) > 10 {
		t.Errorf("topK=100 should return at most 10 (bank size), got %d", len(results))
	}
}

func TestPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	tree := DefaultTree()
	op := MutationOp{Operation: "add_fallback", Target: "EscalateToDeepSeek"}
	if err := eb.AddFromMutation(tree, op, 0.4, 0.55, nil); err != nil {
		t.Fatalf("AddFromMutation: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(eb.PersistPath); os.IsNotExist(err) {
		t.Fatal("persist file was not created")
	}

	// Reload from same directory
	eb2, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (reload): %v", err)
	}

	if eb2.Count() != 1 {
		t.Errorf("reloaded bank: expected 1 entry, got %d", eb2.Count())
	}

	reloaded := eb2.Entries[0]
	if reloaded.MutationOp != "add_fallback" {
		t.Errorf("expected add_fallback, got %s", reloaded.MutationOp)
	}
	if reloaded.FitnessDelta <= 0 {
		t.Error("expected positive fitness delta")
	}
}

func TestStats(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	stats := eb.Stats()
	if stats["total_entries"].(int) != 0 {
		t.Error("empty bank should have 0 total_entries")
	}

	tree := DefaultTree()
	_ = eb.AddFromMutation(tree, MutationOp{Operation: "add_before", Target: "N1"}, 0.3, 0.5, nil)
	_ = eb.AddFromMutation(tree, MutationOp{Operation: "add_after", Target: "N2"}, 0.3, 0.6, nil)

	stats = eb.Stats()
	if stats["total_entries"].(int) != 2 {
		t.Errorf("expected 2 total_entries, got %v", stats["total_entries"])
	}
	avgDelta := stats["avg_fitness_delta"].(float64)
	if avgDelta <= 0 {
		t.Errorf("expected positive avg_fitness_delta, got %f", avgDelta)
	}
}

func TestExtractTreeType(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"GoDev_Main", "GoDev"},
		{"Merged_Main", "Merged"},
		{"MainSequence", "Default"},
		{"Default_Main", "Default"},
		{"Stockfish_Evolve", "Stockfish"},
		{"Kanban_Main", "Kanban"},
		{"GOAP_Planning", "GOAP"},
		{"UnknownTree_XYZ", "UnknownTree"},
	}

	for _, tt := range tests {
		tree := &SerializableNode{Name: tt.name}
		got := extractTreeType(tree)
		if got != tt.expected {
			t.Errorf("extractTreeType(%q) = %q, want %q", tt.name, got, tt.expected)
		}
	}

	// Nil tree
	if got := extractTreeType(nil); got != "Unknown" {
		t.Errorf("extractTreeType(nil) = %q, want Unknown", got)
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! How are you doing World?")
	if len(tokens) != 6 {
		t.Errorf("expected 6 unique tokens, got %d: %v", len(tokens), tokens)
	}
}

func TestJaccardSimilarity(t *testing.T) {
	a := []string{"add", "before", "node", "test"}
	b := []string{"add", "before", "node", "test"}
	if s := jaccardSimilarity(a, b); s != 1.0 {
		t.Errorf("identical sets should have similarity 1.0, got %f", s)
	}

	a = []string{"add", "before"}
	b = []string{"wrap", "retry"}
	if s := jaccardSimilarity(a, b); s != 0.0 {
		t.Errorf("disjoint sets should have similarity 0.0, got %f", s)
	}

	a = []string{"add", "before", "node"}
	b = []string{"add", "after", "node"}
	// intersection = {add, node} = 2, union = {add, before, node, after} = 4
	if s := jaccardSimilarity(a, b); s != 0.5 {
		t.Errorf("expected similarity 0.5, got %f", s)
	}
}

func TestTransferExperiences(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	goDev := GoDeveloperTree()
	def := DefaultTree()

	// Add GoDev experiences
	_ = eb.AddFromMutation(goDev, MutationOp{Operation: "add_before", Target: "N1"}, 0.3, 0.6, nil)
	_ = eb.AddFromMutation(goDev, MutationOp{Operation: "wrap_retry", Target: "N2"}, 0.3, 0.7, nil)
	// Add Default experiences
	_ = eb.AddFromMutation(def, MutationOp{Operation: "add_fallback", Target: "N3"}, 0.3, 0.5, nil)

	// Transfer GoDev → Default
	results := eb.TransferExperiences("GoDev", "Default")
	if len(results) == 0 {
		t.Error("transfer should return some results")
	}
}

func TestParseFloat(t *testing.T) {
	if f, err := parseFloat("0.85"); err != nil || f != 0.85 {
		t.Errorf("parseFloat(0.85) = %f, %v", f, err)
	}
	if f, err := parseFloat("1.0"); err != nil || f != 1.0 {
		t.Errorf("parseFloat(1.0) = %f, %v", f, err)
	}
	if _, err := parseFloat(""); err == nil {
		t.Error("parseFloat('') should error")
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	done := make(chan bool, 10)
	tree := DefaultTree()

	// Concurrent writers
	for i := 0; i < 5; i++ {
		go func(id int) {
			op := MutationOp{Operation: "add_before", Target: "N"}
			_ = eb.AddFromMutation(tree, op, 0.3, 0.3+float64(id)*0.05, nil)
			done <- true
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			eb.Retrieve("Default", 3)
			eb.Stats()
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic, should have entries
	if eb.Count() == 0 {
		t.Error("should have entries after concurrent writes")
	}
}

func TestAddFromMutation_MultipleTreeTypes(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	trees := []*SerializableNode{
		DefaultTree(),
		GoDeveloperTree(),
	}

	for _, tree := range trees {
		ops := []string{"add_before", "add_after", "wrap_retry"}
		for _, op := range ops {
			opObj := MutationOp{Operation: op, Target: "N"}
			_ = eb.AddFromMutation(tree, opObj, 0.3, 0.3+rand.Float64()*0.2, nil)
		}
	}

	stats := eb.Stats()
	byType := stats["by_tree_type"].(map[string]int)
	if len(byType) < 2 {
		t.Errorf("expected at least 2 tree types, got %d: %v", len(byType), byType)
	}

	// Each type should have 3 entries
	for _, count := range byType {
		if count != 3 {
			t.Errorf("expected 3 entries per type, got %d", count)
		}
	}
}

// Integration-style test with real file persistence
func TestPersistDirCreated(t *testing.T) {
	baseDir := t.TempDir()
	nestedDir := filepath.Join(baseDir, "deep", "nested", "experience")

	eb, err := NewExperienceBank(nestedDir)
	if err != nil {
		t.Fatalf("NewExperienceBank with nested dir: %v", err)
	}

	tree := DefaultTree()
	op := MutationOp{Operation: "add_before", Target: "N"}
	if err := eb.AddFromMutation(tree, op, 0.3, 0.5, nil); err != nil {
		t.Fatalf("AddFromMutation: %v", err)
	}

	// Verify file exists at the nested path
	if _, err := os.Stat(eb.PersistPath); os.IsNotExist(err) {
		t.Fatal("persist file not found in nested dir")
	}
}

// ─── EvolveWithExperience warm-start (Q2 Evolvability) ─────────────────────
//
// The package header (experience_bank.go) promises Population.EvolveWithExperience
// in learning.go: evolution that (a) warm-starts operator selection from
// ExperienceBank.RetrieveByTreeType hints and (b) records fitness-improving
// mutations back into the bank via AddFromMutation, closing the
// learn→retrieve→mutate feedback loop.

// growthFitness is monotone in node count and bounded in (0,1), so any mutation
// that adds nodes strictly improves fitness — a seeded run is guaranteed to
// encounter improving mutations for the bank to record.
func growthFitness(tr *SerializableNode) float64 {
	n := float64(CountNodes(tr))
	return n / (n + 40.0)
}

func TestEvolveWithExperience_RecordsImprovingMutations(t *testing.T) {
	rand.Seed(42) //nolint:staticcheck // deterministic evolution run for reproducibility
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	pop := NewPopulation(8, DefaultTree())
	best := pop.EvolveWithExperience(3, growthFitness, eb)
	if best == nil {
		t.Fatal("EvolveWithExperience returned nil best tree")
	}

	if eb.Count() == 0 {
		t.Fatal("expected fitness-improving mutations to be recorded via AddFromMutation; bank is empty")
	}
	for i, e := range eb.Entries {
		if e.FitnessDelta <= 0 {
			t.Errorf("entry %d: recorded non-improving mutation (delta=%.4f)", i, e.FitnessDelta)
		}
	}
	// AddFromMutation persists on every add — the bank must survive a restart.
	if _, err := os.Stat(eb.PersistPath); err != nil {
		t.Errorf("expected persisted experience file at %s: %v", eb.PersistPath, err)
	}
}

func TestEvolveWithExperience_WarmStartConsultsBankHints(t *testing.T) {
	rand.Seed(42) //nolint:staticcheck // deterministic evolution run for reproducibility
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	// Pre-seed high-quality experiences for the Default tree type so the
	// warm-start path has hints to retrieve.
	seedTree := DefaultTree()
	seedOps := []MutationOp{
		{Operation: "add_fallback", Target: "SetupTools"},
		{Operation: "add_before", Target: "HasClearTask"},
	}
	for i, op := range seedOps {
		if err := eb.AddFromMutation(seedTree, op, 0.30, 0.55+float64(i)*0.05, nil); err != nil {
			t.Fatalf("seed AddFromMutation: %v", err)
		}
	}

	pop := NewPopulation(8, DefaultTree())
	pop.EvolveWithExperience(2, growthFitness, eb)

	// Warm-start must consult RetrieveByTreeType for the population's tree type
	// and mark the retrieved hints as reused.
	reused := 0
	for _, e := range eb.Entries {
		if e.TreeType == "Default" && e.TimesReused > 0 {
			reused++
		}
	}
	if reused == 0 {
		t.Fatal("expected warm-start to retrieve Default hints via RetrieveByTreeType and increment TimesReused; no entry was consulted")
	}
}

func TestEvolveWithExperience_NilBankStillEvolves(t *testing.T) {
	rand.Seed(42) //nolint:staticcheck // deterministic evolution run for reproducibility
	pop := NewPopulation(6, DefaultTree())
	best := pop.EvolveWithExperience(2, growthFitness, nil)
	if best == nil {
		t.Fatal("EvolveWithExperience with nil bank should degrade to plain evolution and return a best tree")
	}
	if pop.Generation != 2 {
		t.Errorf("expected 2 generations to run, got %d", pop.Generation)
	}
}

// ─── Capacity cap + quality-aware eviction ──────────────────────────────────
//
// The bank must be bounded: a capacity cap in the 200–500 range, enforced on
// the Add path and when loading from disk, with quality-aware eviction —
// lowest QualityScore evicted first, oldest first among equal quality, and
// entries with high TimesReused protected from eviction. The persisted
// {"entries": [...]} format (ADR-003 atomic write) must stay unchanged.

const (
	// The cap itself is an implementation choice; the goal only promises it
	// lands in this range. Overflow safely past the ceiling to force eviction.
	experienceCapFloor   = 200
	experienceCapCeiling = 500
	experienceOverflow   = experienceCapCeiling + 20
)

// mkExperienceEntry builds a minimal valid entry with controlled quality,
// age, and reuse count for eviction-order tests.
func mkExperienceEntry(id string, quality float64, created time.Time, reused int) ExperienceEntry {
	return ExperienceEntry{
		ID:           id,
		TreeType:     "Default",
		MutationOp:   "add_before",
		TargetNode:   "N",
		Context:      "cap test",
		Strategy:     "cap test",
		Trajectory:   "cap test",
		Summary:      "cap test",
		Reflection:   "cap test",
		FitnessDelta: 0.1,
		QualityScore: quality,
		CreatedAt:    created,
		TimesReused:  reused,
	}
}

func bankHasEntry(eb *ExperienceBank, id string) bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, e := range eb.Entries {
		if e.ID == id {
			return true
		}
	}
	return false
}

func TestExperienceBank_CapEnforcedOnAdd(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < experienceOverflow; i++ {
		e := mkExperienceEntry(fmt.Sprintf("cap_%04d", i), 0.5, base.Add(time.Duration(i)*time.Minute), 0)
		if err := eb.addEntry(e); err != nil {
			t.Fatalf("addEntry %d: %v", i, err)
		}
	}

	got := eb.Count()
	if got > experienceCapCeiling {
		t.Fatalf("bank is unbounded: %d entries after %d adds, want <= %d (capacity cap)", got, experienceOverflow, experienceCapCeiling)
	}
	if got < experienceCapFloor {
		t.Errorf("cap too aggressive: %d entries retained, want >= %d", got, experienceCapFloor)
	}
}

func TestExperienceBank_EvictsLowestQualityFirst(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	// 10 low-quality entries (also the oldest, so quality-first and
	// oldest-first eviction agree), then high-quality entries to overflow.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eb.mu.Lock()
	for i := 0; i < 10; i++ {
		eb.Entries = append(eb.Entries, mkExperienceEntry(fmt.Sprintf("low_%04d", i), 0.05, base.Add(time.Duration(i)*time.Minute), 0))
	}
	for i := 10; i < experienceOverflow-1; i++ {
		eb.Entries = append(eb.Entries, mkExperienceEntry(fmt.Sprintf("high_%04d", i), 0.9, base.Add(time.Duration(i)*time.Minute), 0))
	}
	eb.mu.Unlock()

	// The overflowing Add must trigger eviction down to the cap.
	last := mkExperienceEntry("high_last", 0.9, base.Add(time.Duration(experienceOverflow)*time.Minute), 0)
	if err := eb.addEntry(last); err != nil {
		t.Fatalf("addEntry (overflow): %v", err)
	}

	if got := eb.Count(); got > experienceCapCeiling {
		t.Fatalf("bank is unbounded: %d entries, want <= %d", got, experienceCapCeiling)
	}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("low_%04d", i)
		if bankHasEntry(eb, id) {
			t.Errorf("low-quality entry %s survived eviction; lowest QualityScore must be evicted first", id)
		}
	}
	if !bankHasEntry(eb, "high_last") {
		t.Error("newest high-quality entry was evicted; eviction must prefer low-quality/old entries")
	}
}

func TestExperienceBank_EvictsOldestAmongEqualQuality(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eb.mu.Lock()
	for i := 0; i < experienceOverflow-1; i++ {
		eb.Entries = append(eb.Entries, mkExperienceEntry(fmt.Sprintf("eq_%04d", i), 0.5, base.Add(time.Duration(i)*time.Minute), 0))
	}
	eb.mu.Unlock()

	newest := mkExperienceEntry("eq_newest", 0.5, base.Add(time.Duration(experienceOverflow)*time.Minute), 0)
	if err := eb.addEntry(newest); err != nil {
		t.Fatalf("addEntry (overflow): %v", err)
	}

	if got := eb.Count(); got > experienceCapCeiling {
		t.Fatalf("bank is unbounded: %d entries, want <= %d", got, experienceCapCeiling)
	}
	if bankHasEntry(eb, "eq_0000") {
		t.Error("oldest equal-quality entry survived; eviction must be oldest-first among equal QualityScore")
	}
	if !bankHasEntry(eb, "eq_newest") {
		t.Error("newest equal-quality entry was evicted; eviction must be oldest-first among equal QualityScore")
	}
}

func TestExperienceBank_ProtectsHighReuseEntries(t *testing.T) {
	dir := t.TempDir()
	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	// A veteran entry that is the oldest AND lowest quality — prime eviction
	// candidate on every axis except reuse — must be protected by its high
	// TimesReused count.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	veteran := mkExperienceEntry("veteran", 0.05, base, 50)
	eb.mu.Lock()
	eb.Entries = append(eb.Entries, veteran)
	for i := 1; i < experienceOverflow-1; i++ {
		eb.Entries = append(eb.Entries, mkExperienceEntry(fmt.Sprintf("fresh_%04d", i), 0.6, base.Add(time.Duration(i)*time.Minute), 0))
	}
	eb.mu.Unlock()

	last := mkExperienceEntry("fresh_last", 0.6, base.Add(time.Duration(experienceOverflow)*time.Minute), 0)
	if err := eb.addEntry(last); err != nil {
		t.Fatalf("addEntry (overflow): %v", err)
	}

	if got := eb.Count(); got > experienceCapCeiling {
		t.Fatalf("bank is unbounded: %d entries, want <= %d", got, experienceCapCeiling)
	}
	if !bankHasEntry(eb, "veteran") {
		t.Error("high-TimesReused entry was evicted; frequently reused experiences must be protected from eviction")
	}
}

func TestExperienceBank_CapEnforcedOnLoad(t *testing.T) {
	dir := t.TempDir()

	// Write an oversized bank directly in the persisted ADR-003 wrapper format
	// — as if produced by an older, unbounded build.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := make([]ExperienceEntry, 0, experienceOverflow)
	for i := 0; i < 10; i++ {
		entries = append(entries, mkExperienceEntry(fmt.Sprintf("low_%04d", i), 0.05, base.Add(time.Duration(i)*time.Minute), 0))
	}
	for i := 10; i < experienceOverflow; i++ {
		entries = append(entries, mkExperienceEntry(fmt.Sprintf("high_%04d", i), 0.9, base.Add(time.Duration(i)*time.Minute), 0))
	}
	data, err := json.MarshalIndent(struct {
		Entries []ExperienceEntry `json:"entries"`
	}{Entries: entries}, "", "  ")
	if err != nil {
		t.Fatalf("marshal oversized bank: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "experience.json"), data, 0644); err != nil {
		t.Fatalf("write oversized bank: %v", err)
	}

	eb, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank must load an oversized legacy file without error: %v", err)
	}

	got := eb.Count()
	if got > experienceCapCeiling {
		t.Fatalf("Load did not enforce the cap: %d entries, want <= %d", got, experienceCapCeiling)
	}
	if got < experienceCapFloor {
		t.Errorf("Load cap too aggressive: %d entries retained, want >= %d", got, experienceCapFloor)
	}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("low_%04d", i)
		if bankHasEntry(eb, id) {
			t.Errorf("low-quality entry %s survived Load-time eviction", id)
		}
	}

	// Format compatibility: a capped bank must persist and reload in the same
	// wrapper format with a stable count.
	if err := eb.Persist(); err != nil {
		t.Fatalf("Persist after capped load: %v", err)
	}
	eb2, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (reload of capped bank): %v", err)
	}
	if eb2.Count() != got {
		t.Errorf("capped bank did not round-trip: persisted %d entries, reloaded %d", got, eb2.Count())
	}
}

// ─── Two-writer persistence safety (Q3 Reliability) ─────────────────────────
//
// The daemon and the gardener each hold their own ExperienceBank over the SAME
// experience.json. Add's full-file rewrite must first reload and merge on-disk
// entries by ID (preserving the higher TimesReused), so one writer's adds are
// never silently dropped by the other's rewrite.

func TestExperienceBank_TwoWriterInterleavedWritesPreserveAllEntries(t *testing.T) {
	dir := t.TempDir()
	daemon, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (daemon): %v", err)
	}
	gardener, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (gardener): %v", err)
	}

	// Interleave adds: neither bank ever reloads the other's in-memory state,
	// exactly like two long-lived processes sharing the file.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const perWriter = 5
	for i := 0; i < perWriter; i++ {
		de := mkExperienceEntry(fmt.Sprintf("daemon_%02d", i), 0.6, base.Add(time.Duration(2*i)*time.Minute), 0)
		if err := daemon.addEntry(de); err != nil {
			t.Fatalf("daemon addEntry %d: %v", i, err)
		}
		ge := mkExperienceEntry(fmt.Sprintf("gardener_%02d", i), 0.6, base.Add(time.Duration(2*i+1)*time.Minute), 0)
		if err := gardener.addEntry(ge); err != nil {
			t.Fatalf("gardener addEntry %d: %v", i, err)
		}
	}

	// A fresh load sees only what survived on disk.
	reloaded, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (reload): %v", err)
	}
	if got, want := reloaded.Count(), 2*perWriter; got != want {
		t.Errorf("interleaved two-writer adds dropped entries: %d on disk, want %d", got, want)
	}
	for i := 0; i < perWriter; i++ {
		for _, id := range []string{fmt.Sprintf("daemon_%02d", i), fmt.Sprintf("gardener_%02d", i)} {
			if !bankHasEntry(reloaded, id) {
				t.Errorf("entry %s was silently dropped by the other writer's rewrite", id)
			}
		}
	}
}

func TestExperienceBank_TwoWriterMergePreservesHigherTimesReused(t *testing.T) {
	dir := t.TempDir()
	daemon, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (daemon): %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	shared := mkExperienceEntry("shared", 0.7, base, 0)
	if err := daemon.addEntry(shared); err != nil {
		t.Fatalf("daemon addEntry (shared): %v", err)
	}

	// The gardener loads the shared entry and records reuse on disk.
	gardener, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (gardener): %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := gardener.MarkReused([]string{"shared"}); err != nil {
			t.Fatalf("gardener MarkReused %d: %v", i, err)
		}
	}

	// The daemon still holds the stale in-memory copy (TimesReused=0). Its next
	// Add rewrites the file and must merge by ID, keeping the higher on-disk
	// TimesReused instead of clobbering it.
	fresh := mkExperienceEntry("daemon_fresh", 0.6, base.Add(time.Minute), 0)
	if err := daemon.addEntry(fresh); err != nil {
		t.Fatalf("daemon addEntry (fresh): %v", err)
	}

	reloaded, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (reload): %v", err)
	}
	if !bankHasEntry(reloaded, "daemon_fresh") {
		t.Error("daemon's new entry missing after merge-on-Add rewrite")
	}
	found := false
	reloaded.mu.RLock()
	for _, e := range reloaded.Entries {
		if e.ID == "shared" {
			found = true
			if e.TimesReused != 3 {
				t.Errorf("shared entry TimesReused = %d after daemon rewrite, want 3 (higher on-disk count must win the merge)", e.TimesReused)
			}
		}
	}
	reloaded.mu.RUnlock()
	if !found {
		t.Error("shared entry was silently dropped by daemon's rewrite")
	}
}

// TestMarkReusedDoesNotDropConcurrentWriterEntries: MarkReused is a full-file
// rewrite like addEntry, so it must run the same lock→merge→write sequence. A
// gardener bank loaded at startup that later marks reuse would otherwise erase
// every entry the daemon added since load AND clobber reuse counts the daemon
// recorded on shared entries.
func TestMarkReusedDoesNotDropConcurrentWriterEntries(t *testing.T) {
	dir := t.TempDir()
	daemon, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (daemon): %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []ExperienceEntry{
		mkExperienceEntry("gardener_own", 0.6, base, 0),
		mkExperienceEntry("shared", 0.7, base.Add(time.Minute), 0),
	} {
		if err := daemon.addEntry(e); err != nil {
			t.Fatalf("daemon addEntry (%s): %v", e.ID, err)
		}
	}

	// The gardener loads now — its memory holds gardener_own + shared only.
	gardener, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (gardener): %v", err)
	}

	// After the gardener's load, the daemon adds a fresh entry and records
	// reuse on the shared entry. Both land on disk but not in the gardener's
	// memory. (The daemon's memory is complete, so its own rewrites are safe.)
	if err := daemon.addEntry(mkExperienceEntry("daemon_since_load", 0.6, base.Add(2*time.Minute), 0)); err != nil {
		t.Fatalf("daemon addEntry (daemon_since_load): %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := daemon.MarkReused([]string{"shared"}); err != nil {
			t.Fatalf("daemon MarkReused %d: %v", i, err)
		}
	}

	// The gardener marks reuse on its own entry. Its rewrite must merge from
	// disk first, not blast the gardener's stale memory over the file.
	if err := gardener.MarkReused([]string{"gardener_own"}); err != nil {
		t.Fatalf("gardener MarkReused: %v", err)
	}

	reloaded, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (reload): %v", err)
	}
	if !bankHasEntry(reloaded, "daemon_since_load") {
		t.Error("daemon_since_load was silently dropped by the gardener's MarkReused rewrite")
	}
	reused := map[string]int{}
	reloaded.mu.RLock()
	for _, e := range reloaded.Entries {
		reused[e.ID] = e.TimesReused
	}
	reloaded.mu.RUnlock()
	if got, ok := reused["gardener_own"]; !ok {
		t.Error("gardener_own missing after MarkReused rewrite")
	} else if got != 1 {
		t.Errorf("gardener_own TimesReused = %d, want 1 (the reuse increment must be recorded)", got)
	}
	if got, ok := reused["shared"]; !ok {
		t.Error("shared entry was silently dropped by the gardener's MarkReused rewrite")
	} else if got != 5 {
		t.Errorf("shared TimesReused = %d after gardener rewrite, want 5 (higher on-disk count must win the merge)", got)
	}
}

// TestExperienceBank_ConcurrentWritersLoseNoEntries drives two banks over the
// same experience.json from concurrent goroutines, like the daemon and the
// gardener adding at the same moment. Each bank's in-process mutex does not
// serialize the OTHER writer's merge→rename window: writer A can rename its
// snapshot into place after writer B ran mergeFromDiskLocked but before B's
// os.Rename, so B's rewrite silently erases A's entry. Closing the race
// requires holding the sidecar file lock from mergeFromDiskLocked through the
// rename. All IDs stay far under the cap, so eviction cannot explain absence.
func TestExperienceBank_ConcurrentWritersLoseNoEntries(t *testing.T) {
	dir := t.TempDir()
	daemon, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (daemon): %v", err)
	}
	gardener, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (gardener): %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const perWriter = 50 // 100 total, well under experienceBankCap

	writers := map[string]*ExperienceBank{"daemon": daemon, "gardener": gardener}
	start := make(chan struct{})
	errs := make(chan error, len(writers)*perWriter)
	var wg sync.WaitGroup
	for name, bank := range writers {
		wg.Add(1)
		go func(name string, bank *ExperienceBank) {
			defer wg.Done()
			<-start
			for i := 0; i < perWriter; i++ {
				e := mkExperienceEntry(fmt.Sprintf("%s_%02d", name, i), 0.6, base.Add(time.Duration(i)*time.Minute), 0)
				// A racing writer can also make the persist step itself fail
				// (shared tmp file renamed away underneath us). Keep going —
				// the reload below judges what actually survived on disk.
				if err := bank.addEntry(e); err != nil {
					errs <- fmt.Errorf("%s addEntry %d: %w", name, i, err)
				}
			}
		}(name, bank)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent add failed (writers must not disturb each other's persist): %v", err)
	}

	// A fresh load sees only what survived on disk.
	reloaded, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatalf("NewExperienceBank (reload): %v", err)
	}
	var missing []string
	for name := range writers {
		for i := 0; i < perWriter; i++ {
			id := fmt.Sprintf("%s_%02d", name, i)
			if !bankHasEntry(reloaded, id) {
				missing = append(missing, id)
			}
		}
	}
	if len(missing) > 0 {
		sample := missing
		if len(sample) > 5 {
			sample = sample[:5]
		}
		t.Errorf("concurrent two-writer adds lost %d of %d entries in the merge→rename window (e.g. %v); the sidecar lock must be held from merge through rename", len(missing), 2*perWriter, sample)
	}
	if got, want := reloaded.Count(), 2*perWriter; got != want {
		t.Errorf("reloaded bank has %d entries, want %d", got, want)
	}
}

// Benchmarks
func BenchmarkAddFromMutation(b *testing.B) {
	dir := b.TempDir()
	eb, _ := NewExperienceBank(dir)
	tree := DefaultTree()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		op := MutationOp{Operation: "add_before", Target: "N"}
		_ = eb.AddFromMutation(tree, op, 0.3, 0.5, nil)
	}
}

func BenchmarkRetrieve(b *testing.B) {
	dir := b.TempDir()
	eb, _ := NewExperienceBank(dir)
	tree := DefaultTree()

	// Populate with 100 entries
	for i := 0; i < 100; i++ {
		op := MutationOp{Operation: "add_before", Target: "N"}
		_ = eb.AddFromMutation(tree, op, 0.3, 0.3+float64(i)*0.005, nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eb.Retrieve("Default add_before", 5)
	}
}
