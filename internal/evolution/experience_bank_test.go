package evolution

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
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
