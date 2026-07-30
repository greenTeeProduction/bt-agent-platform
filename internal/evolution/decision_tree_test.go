package evolution

import (
	"math"
	"path/filepath"
	"testing"
)

// pathStat returns the PathStats for pathName under selectorName, or nil.
func pathStat(d *DTAnalyzer, selectorName, pathName string) *PathStats {
	ss, ok := d.Stats[selectorName]
	if !ok || ss == nil {
		return nil
	}
	for i := range ss.Paths {
		if ss.Paths[i].PathName == pathName {
			return &ss.Paths[i]
		}
	}
	return nil
}

// TestDTAnalyzer_SaveLoadRoundTrip verifies that persisted DTSelectorStats
// survive a Save→Load into a fresh analyzer with identical counts.
func TestDTAnalyzer_SaveLoadRoundTrip(t *testing.T) {
	d := NewDTAnalyzer()
	for i := 0; i < 6; i++ {
		d.RecordHit("StrategyRouter", "PathA", "IsCodeReview", true)
	}
	for i := 0; i < 4; i++ {
		d.RecordHit("StrategyRouter", "PathB", "IsBuildTask", false)
	}

	path := filepath.Join(t.TempDir(), "dt_stats.json")
	if err := d.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := NewDTAnalyzer()
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ss, ok := loaded.Stats["StrategyRouter"]
	if !ok || ss == nil {
		t.Fatal("StrategyRouter stats missing after round-trip")
	}
	if ss.TotalTasks != 10 {
		t.Errorf("selector TotalTasks = %d, want 10", ss.TotalTasks)
	}
	pa := pathStat(loaded, "StrategyRouter", "PathA")
	if pa == nil {
		t.Fatal("PathA missing after round-trip")
	}
	if pa.HitCount != 6 || pa.SuccessCount != 6 {
		t.Errorf("PathA HitCount/SuccessCount = %d/%d, want 6/6", pa.HitCount, pa.SuccessCount)
	}
	if pa.Condition != "IsCodeReview" {
		t.Errorf("PathA Condition = %q, want IsCodeReview", pa.Condition)
	}
	pb := pathStat(loaded, "StrategyRouter", "PathB")
	if pb == nil {
		t.Fatal("PathB missing after round-trip")
	}
	if pb.HitCount != 4 || pb.SuccessCount != 0 {
		t.Errorf("PathB HitCount/SuccessCount = %d/%d, want 4/0", pb.HitCount, pb.SuccessCount)
	}
}

// TestDTAnalyzer_LoadMergeCounts verifies that Load sums HitCount,
// SuccessCount, and TotalTasks into the in-memory stats instead of clobbering
// them, so telemetry from independent runs accumulates.
func TestDTAnalyzer_LoadMergeCounts(t *testing.T) {
	first := NewDTAnalyzer()
	for i := 0; i < 6; i++ {
		first.RecordHit("SR", "PathA", "IsCodeReview", true)
	}
	path := filepath.Join(t.TempDir(), "dt_stats.json")
	if err := first.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A second analyzer with its own fresh telemetry for the same selector.
	second := NewDTAnalyzer()
	for i := 0; i < 3; i++ {
		second.RecordHit("SR", "PathA", "IsCodeReview", true)
	}
	if err := second.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ss := second.Stats["SR"]
	if ss == nil {
		t.Fatal("SR stats missing after merge")
	}
	if ss.TotalTasks != 9 {
		t.Errorf("merged selector TotalTasks = %d, want 9 (6+3)", ss.TotalTasks)
	}
	pa := pathStat(second, "SR", "PathA")
	if pa == nil {
		t.Fatal("PathA missing after merge")
	}
	if pa.HitCount != 9 {
		t.Errorf("merged PathA HitCount = %d, want 9 (6+3)", pa.HitCount)
	}
	if pa.SuccessCount != 9 {
		t.Errorf("merged PathA SuccessCount = %d, want 9 (6+3)", pa.SuccessCount)
	}
}

// TestDTAnalyzer_EmptyStatsNoOp verifies the empty-stats guards: Save/Load of
// an analyzer with no telemetry round-trips cleanly, Load of a missing file is
// a no-op, and BestSplitCondition/OptimizeSelectors degrade to no-ops rather
// than panicking when no stats have been recorded.
func TestDTAnalyzer_EmptyStatsNoOp(t *testing.T) {
	empty := NewDTAnalyzer()

	// Load of a non-existent file must be a silent no-op.
	missing := filepath.Join(t.TempDir(), "does_not_exist.json")
	if err := empty.Load(missing); err != nil {
		t.Fatalf("Load of missing file should be a no-op, got %v", err)
	}
	if len(empty.Stats) != 0 {
		t.Errorf("empty analyzer gained %d stats from a missing file", len(empty.Stats))
	}

	// Save of an empty analyzer must succeed and Load back to empty.
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := empty.Save(path); err != nil {
		t.Fatalf("Save of empty analyzer: %v", err)
	}
	reloaded := NewDTAnalyzer()
	if err := reloaded.Load(path); err != nil {
		t.Fatalf("Load of empty file: %v", err)
	}
	if len(reloaded.Stats) != 0 {
		t.Errorf("empty file yielded %d stats, want 0", len(reloaded.Stats))
	}

	// BestSplitCondition against empty stats is a no-op.
	if got := empty.BestSplitCondition("SR"); got != "" {
		t.Errorf("BestSplitCondition on empty stats = %q, want empty", got)
	}

	// OptimizeSelectors against empty stats makes no changes and does not panic.
	tree := &SerializableNode{
		Type: "Selector", Name: "SR",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "PathA", Children: []SerializableNode{
				{Type: "Condition", Name: "IsCodeReview"},
			}},
			{Type: "Sequence", Name: "PathB", Children: []SerializableNode{
				{Type: "Condition", Name: "IsBuildTask"},
			}},
		},
	}
	o := &BTOptimizer{Analyzer: empty}
	if changes := o.OptimizeSelectors(tree); changes != 0 {
		t.Errorf("OptimizeSelectors on empty stats made %d changes, want 0", changes)
	}
}

func TestDTAnalyzer_Entropy(t *testing.T) {
	d := NewDTAnalyzer()
	// Record 10 tasks: 6 hit PathA, 4 hit PathB
	for i := 0; i < 6; i++ {
		d.RecordHit("StrategyRouter", "PathA", "IsCodeReview", true)
	}
	for i := 0; i < 4; i++ {
		d.RecordHit("StrategyRouter", "PathB", "IsBuildTask", true)
	}

	entropy := d.Entropy("StrategyRouter")
	// Expected: -(0.6*log2(0.6) + 0.4*log2(0.4)) ≈ 0.971
	if entropy < 0.9 || entropy > 1.0 {
		t.Errorf("expected entropy ~0.97, got %.3f", entropy)
	}
}

func TestDTAnalyzer_Gini(t *testing.T) {
	d := NewDTAnalyzer()
	for i := 0; i < 10; i++ {
		d.RecordHit("SR", "PathA", "Check", true)
	}
	for i := 0; i < 0; i++ {
		d.RecordHit("SR", "PathB", "Other", true)
	}

	// Pure split: Gini = 1 - (1.0^2 + 0.0^2) = 0
	gini := d.GiniImpurity("SR")
	if gini > 0.01 {
		t.Errorf("expected gini ~0 for pure split, got %.3f", gini)
	}

	// 50/50 split
	d2 := NewDTAnalyzer()
	for i := 0; i < 5; i++ {
		d2.RecordHit("SR2", "A", "x", true)
	}
	for i := 0; i < 5; i++ {
		d2.RecordHit("SR2", "B", "y", true)
	}
	gini2 := d2.GiniImpurity("SR2")
	// Gini = 1 - (0.5^2 + 0.5^2) = 0.5
	if gini2 < 0.45 || gini2 > 0.55 {
		t.Errorf("expected gini ~0.5 for balanced split, got %.3f", gini2)
	}
}

func TestDTAnalyzer_BestSplit(t *testing.T) {
	d := NewDTAnalyzer()
	// Condition "IsCodeReview" perfectly splits: always hits PathA
	for i := 0; i < 8; i++ {
		d.RecordHit("SR", "PathA", "IsCodeReview", true)
	}
	for i := 0; i < 2; i++ {
		d.RecordHit("SR", "PathB", "IsBuildTask", true)
	}

	best := d.BestSplitCondition("SR")
	if best != "IsCodeReview" {
		t.Errorf("expected IsCodeReview as best split, got %q", best)
	}
}

func TestBTOptimizer_ReorderSelectors(t *testing.T) {
	o := NewBTOptimizer()
	// Record usage: PathB hit more often than PathA
	for i := 0; i < 8; i++ {
		o.Analyzer.RecordHit("StrategyRouter", "BuildPath", "IsBuildTask", true)
	}
	for i := 0; i < 2; i++ {
		o.Analyzer.RecordHit("StrategyRouter", "ReviewPath", "IsCodeReview", true)
	}

	tree := &SerializableNode{
		Type: "Selector", Name: "StrategyRouter",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "ReviewPath", Children: []SerializableNode{
				{Type: "Condition", Name: "IsCodeReview"},
			}},
			{Type: "Sequence", Name: "BuildPath", Children: []SerializableNode{
				{Type: "Condition", Name: "IsBuildTask"},
			}},
			{Type: "Sequence", Name: "ExecutionPath", Children: []SerializableNode{
				{Type: "Condition", Name: "AlwaysSucceed"},
			}},
		},
	}

	changes := o.OptimizeSelectors(tree)
	if changes == 0 {
		t.Log("no reordering needed or no stats available")
	}
	// ExecutionPath should be last (isDefaultPath)
	if tree.Children[len(tree.Children)-1].Name != "ExecutionPath" {
		t.Error("ExecutionPath should be last (default path)")
	}

	report := o.AnalyzeTree(tree, "test")
	if report.OverallScore < 0 || report.OverallScore > 10 {
		t.Errorf("invalid overall score: %.2f", report.OverallScore)
	}
	t.Logf("DT Report: Entropy=%.3f, Gini=%.3f, BestSplit=%q, Score=%.1f",
		report.Entropy, report.Gini, report.BestSplit, report.OverallScore)
}

func TestConditionOverlap(t *testing.T) {
	// "IsCodeReview" and "IsCodeStyle" overlap (both contain "code")
	overlap := conditionOverlap("IsCodeReview", "IsCodeStyle")
	if overlap < 0.3 {
		t.Errorf("expected overlap for code-related conditions, got %.2f", overlap)
	}

	// "IsCodeReview" and "IsBuildTask" have no overlap
	overlap2 := conditionOverlap("IsCodeReview", "IsBuildTask")
	if overlap2 > 0.3 {
		t.Errorf("expected no overlap for different conditions, got %.2f", overlap2)
	}
}

// TestGiniImpurityFromProbs_Canonical exercises the canonical, generic
// Gini-impurity helper in selector_optimizer.go that decision_tree.go's
// DTAnalyzer.GiniImpurity must be consolidated onto, instead of carrying its
// own private copy of the 1-Σp² formula.
func TestGiniImpurityFromProbs_Canonical(t *testing.T) {
	if g := GiniImpurityFromProbs(1.0, 0.0); g > 0.0001 {
		t.Errorf("pure distribution gini = %.4f, want ~0", g)
	}
	if g := GiniImpurityFromProbs(0.5, 0.5); g < 0.49 || g > 0.51 {
		t.Errorf("balanced binary gini = %.4f, want ~0.5", g)
	}
	want := 1.0 - (0.3*0.3 + 0.7*0.7)
	if g := GiniImpurityFromProbs(0.3, 0.7); math.Abs(g-want) > 1e-9 {
		t.Errorf("GiniImpurityFromProbs(0.3, 0.7) = %.6f, want %.6f", g, want)
	}
}

// TestDTAnalyzer_GiniImpurity_MatchesCanonicalGiniImpurityFromProbs verifies
// DTAnalyzer.GiniImpurity is computed via the canonical GiniImpurityFromProbs
// helper (same formula, single source) rather than a duplicated private
// implementation, so the two never drift.
func TestDTAnalyzer_GiniImpurity_MatchesCanonicalGiniImpurityFromProbs(t *testing.T) {
	d := NewDTAnalyzer()
	for i := 0; i < 3; i++ {
		d.RecordHit("SR", "A", "x", true)
	}
	for i := 0; i < 7; i++ {
		d.RecordHit("SR", "B", "y", true)
	}
	got := d.GiniImpurity("SR")
	want := GiniImpurityFromProbs(0.3, 0.7)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("DTAnalyzer.GiniImpurity = %.6f, want %.6f (canonical GiniImpurityFromProbs)", got, want)
	}
}

func TestDTAnalyzer_InformationGain(t *testing.T) {
	d := NewDTAnalyzer()
	// Simulate: CodeReview paths succeed 90%, Build path succeeds 50%
	for i := 0; i < 9; i++ {
		d.RecordHit("SR", "Review", "IsCodeReview", true)
	}
	d.RecordHit("SR", "Review", "IsCodeReview", false)
	for i := 0; i < 5; i++ {
		d.RecordHit("SR", "Build", "IsBuildTask", true)
	}
	for i := 0; i < 5; i++ {
		d.RecordHit("SR", "Build", "IsBuildTask", false)
	}

	gain := d.InformationGain("SR", "IsCodeReview")
	t.Logf("Information gain for IsCodeReview: %.4f", gain)
	if gain < 0 {
		t.Error("information gain should be non-negative")
	}
}
