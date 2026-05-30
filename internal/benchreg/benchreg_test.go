package benchreg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBenchOutput_Standard(t *testing.T) {
	input := `goos: linux
goarch: arm64
pkg: github.com/nico/go-bt-evolve/internal/benchmark
cpu: ARMv8 Processor rev 0 (v8l)
BenchmarkBTPG_Quality-4    100000    1234 ns/op    512 B/op    5 allocs/op
BenchmarkToolBench_API-4    50000    2400 ns/op    1024 B/op    10 allocs/op
PASS
ok  	github.com/nico/go-bt-evolve/internal/benchmark	2.456s
`

	results := ParseBenchOutput(input)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result
	if results[0].Name != "BenchmarkBTPG_Quality" {
		t.Errorf("expected name 'BenchmarkBTPG_Quality', got %q", results[0].Name)
	}
	if results[0].NsPerOp != 1234 {
		t.Errorf("expected 1234 ns/op, got %f", results[0].NsPerOp)
	}
	if results[0].BPerOp != 512 {
		t.Errorf("expected 512 B/op, got %f", results[0].BPerOp)
	}
	if results[0].Allocs != 5 {
		t.Errorf("expected 5 allocs, got %d", results[0].Allocs)
	}

	// Second result
	if results[1].Name != "BenchmarkToolBench_API" {
		t.Errorf("expected name 'BenchmarkToolBench_API', got %q", results[1].Name)
	}
}

func TestParseBenchOutput_NoAllocs(t *testing.T) {
	input := `BenchmarkSimple-4    100000    500 ns/op`
	results := ParseBenchOutput(input)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "BenchmarkSimple" {
		t.Errorf("expected 'BenchmarkSimple', got %q", results[0].Name)
	}
	if results[0].BPerOp != 0 {
		t.Errorf("expected 0 B/op, got %f", results[0].BPerOp)
	}
	if results[0].Allocs != 0 {
		t.Errorf("expected 0 allocs, got %d", results[0].Allocs)
	}
}

func TestParseBenchOutput_NoBPerOp(t *testing.T) {
	input := `BenchmarkJustNs-4    75000    1800 ns/op    3 allocs/op`
	results := ParseBenchOutput(input)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].NsPerOp != 1800 {
		t.Errorf("expected 1800 ns/op, got %f", results[0].NsPerOp)
	}
	if results[0].BPerOp != 0 {
		t.Errorf("expected 0 B/op, got %f", results[0].BPerOp)
	}
	if results[0].Allocs != 3 {
		t.Errorf("expected 3 allocs, got %d", results[0].Allocs)
	}
}

func TestParseBenchOutput_Empty(t *testing.T) {
	results := ParseBenchOutput("")
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestParseBenchOutput_MetadataSkipped(t *testing.T) {
	input := `goos: linux
goarch: arm64
pkg: example
cpu: ARM
PASS
ok  	example	1.234s
`
	results := ParseBenchOutput(input)
	if len(results) != 0 {
		t.Errorf("expected 0 results for metadata-only input, got %d", len(results))
	}
}

func TestParseBenchOutput_MultipleLines(t *testing.T) {
	input := `BenchmarkA-4    1000    100 ns/op
BenchmarkB-4    2000    200 ns/op    50 B/op
BenchmarkC-4    3000    300 ns/op    100 B/op    2 allocs/op
BenchmarkWithSlash/Small-4    4000    400 ns/op
BenchmarkWithSlash/Large/text-4    5000    500 ns/op    200 B/op    5 allocs/op
ok  	pkg	3s
`
	results := ParseBenchOutput(input)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	if results[3].Name != "BenchmarkWithSlash/Small" {
		t.Errorf("expected 'BenchmarkWithSlash/Small', got %q", results[3].Name)
	}
	if results[4].Name != "BenchmarkWithSlash/Large/text" {
		t.Errorf("expected 'BenchmarkWithSlash/Large/text', got %q", results[4].Name)
	}
}

func TestParseBenchOutput_FailLine(t *testing.T) {
	input := `BenchmarkA-4    1000    100 ns/op
FAIL
BenchmarkB-4    2000    200 ns/op
`
	results := ParseBenchOutput(input)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (FAIL line skipped), got %d", len(results))
	}
}

func TestPctChange(t *testing.T) {
	tests := []struct {
		old, new float64
		want     float64
	}{
		{100, 110, 10.0},   // 10% slower
		{100, 125, 25.0},   // 25% slower
		{100, 90, -10.0},   // 10% faster
		{100, 100, 0.0},    // no change
		{0, 100, 100.0},    // new benchmark
		{0, 0, 0.0},        // both zero
		{50, 100, 100.0},   // 2x slower
		{200, 100, -50.0},  // 2x faster
	}
	for _, tt := range tests {
		got := pctChange(tt.old, tt.new)
		if got != tt.want {
			t.Errorf("pctChange(%f, %f) = %f, want %f", tt.old, tt.new, got, tt.want)
		}
	}
}

func TestBaselineStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")

	store := NewBaselineStore(path)
	err := store.Load()
	if err != nil {
		t.Fatalf("Load() on non-existent file: %v", err)
	}
	if len(store.Baseline) != 0 {
		t.Errorf("expected empty baseline after Load() on missing file, got %d entries", len(store.Baseline))
	}

	// Add some data and save
	store.Baseline["BenchA"] = BenchmarkResult{Name: "BenchA", NsPerOp: 100, BPerOp: 50, Allocs: 2}
	store.Baseline["BenchB"] = BenchmarkResult{Name: "BenchB", NsPerOp: 200, BPerOp: 100, Allocs: 4}
	err = store.Save()
	if err != nil {
		t.Fatalf("Save(): %v", err)
	}

	// Load into a new store
	store2 := NewBaselineStore(path)
	err = store2.Load()
	if err != nil {
		t.Fatalf("Load() on existing file: %v", err)
	}
	if len(store2.Baseline) != 2 {
		t.Fatalf("expected 2 entries after Load(), got %d", len(store2.Baseline))
	}
	if store2.Baseline["BenchA"].NsPerOp != 100 {
		t.Errorf("expected 100 ns/op for BenchA, got %f", store2.Baseline["BenchA"].NsPerOp)
	}
}

func TestBaselineStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")

	// Write an empty file
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewBaselineStore(path)
	err := store.Load()
	if err != nil {
		t.Fatalf("Load() on empty file: %v", err)
	}
	if len(store.Baseline) != 0 {
		t.Errorf("expected 0 entries from empty file, got %d", len(store.Baseline))
	}
}

func TestBaselineStore_UpdateBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	store := NewBaselineStore(path)

	results := []BenchmarkResult{
		{Name: "BenchA", NsPerOp: 100},
		{Name: "BenchB", NsPerOp: 200},
	}
	if err := store.UpdateBaseline(results); err != nil {
		t.Fatalf("UpdateBaseline: %v", err)
	}

	// Verify on disk
	store2 := NewBaselineStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() after UpdateBaseline: %v", err)
	}
	if len(store2.Baseline) != 2 {
		t.Errorf("expected 2 entries, got %d", len(store2.Baseline))
	}
}

func TestComparator_NoRegression(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchFast": {Name: "BenchFast", NsPerOp: 1000},
	}}
	config := DefaultRegressionConfig()
	comp := NewComparator(store, config)

	current := []BenchmarkResult{
		{Name: "BenchFast", NsPerOp: 1050}, // 5% slower — below warning threshold
	}

	results := comp.Compare(current)
	if HasRegressions(results) {
		t.Errorf("expected no regressions at 5%% slowdown")
	}
	if HasWarnings(results) {
		t.Errorf("expected no warnings at 5%% slowdown")
	}
}

func TestComparator_WarningRegression(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchA": {Name: "BenchA", NsPerOp: 1000},
	}}
	config := DefaultRegressionConfig()
	comp := NewComparator(store, config)

	current := []BenchmarkResult{
		{Name: "BenchA", NsPerOp: 1150}, // 15% slower — warning
	}

	results := comp.Compare(current)
	if !HasWarnings(results) {
		t.Errorf("expected warning at 15%% slowdown")
	}
	if HasRegressions(results) {
		t.Errorf("expected no critical regression at 15%% slowdown")
	}
}

func TestComparator_CriticalRegression(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchA": {Name: "BenchA", NsPerOp: 1000},
	}}
	config := DefaultRegressionConfig()
	comp := NewComparator(store, config)

	current := []BenchmarkResult{
		{Name: "BenchA", NsPerOp: 1300}, // 30% slower — critical
	}

	results := comp.Compare(current)
	if !HasRegressions(results) {
		t.Errorf("expected critical regression at 30%% slowdown")
	}
	if !HasWarnings(results) {
		t.Errorf("expected warning flag at 30%% slowdown")
	}
}

func TestComparator_MinNsFilter(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchTiny": {Name: "BenchTiny", NsPerOp: 10},
	}}
	config := DefaultRegressionConfig()
	config.MinNsPerOp = 100.0
	comp := NewComparator(store, config)

	// 100% slowdown but below MinNsPerOp — shouldn't trigger
	current := []BenchmarkResult{
		{Name: "BenchTiny", NsPerOp: 20},
	}

	results := comp.Compare(current)
	if HasWarnings(results) {
		t.Errorf("expected no warning for tiny benchmark below MinNsPerOp (10→20 ns)")
	}
}

func TestComparator_NewBenchmark(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchOld": {Name: "BenchOld", NsPerOp: 1000},
	}}
	config := DefaultRegressionConfig()
	comp := NewComparator(store, config)

	current := []BenchmarkResult{
		{Name: "BenchOld", NsPerOp: 1000},
		{Name: "BenchNew", NsPerOp: 500},
	}

	results := comp.Compare(current)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// New benchmarks should be "ok"
	for _, r := range results {
		if r.Name == "BenchNew" && r.Severity != "ok" {
			t.Errorf("new benchmark should have severity 'ok', got %q", r.Severity)
		}
	}
}

func TestComparator_MissingBaseline(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchOld": {Name: "BenchOld", NsPerOp: 1000},
	}}
	config := DefaultRegressionConfig()
	comp := NewComparator(store, config)

	// BenchOld not in current run
	current := []BenchmarkResult{
		{Name: "BenchOther", NsPerOp: 500},
	}

	results := comp.Compare(current)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (old missing + new), got %d", len(results))
	}
}

func TestComparator_Improvement(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchFast": {Name: "BenchFast", NsPerOp: 1000},
	}}
	config := DefaultRegressionConfig()
	comp := NewComparator(store, config)

	// 50% faster — no regression
	current := []BenchmarkResult{
		{Name: "BenchFast", NsPerOp: 500},
	}

	results := comp.Compare(current)
	if HasRegressions(results) || HasWarnings(results) {
		t.Errorf("expected no warnings/regressions for improvement (50%% faster)")
	}
}

func TestComparator_SortOrder(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{
		"BenchA": {Name: "BenchA", NsPerOp: 1000},
		"BenchB": {Name: "BenchB", NsPerOp: 1000},
		"BenchC": {Name: "BenchC", NsPerOp: 1000},
	}}
	config := DefaultRegressionConfig()
	comp := NewComparator(store, config)

	current := []BenchmarkResult{
		{Name: "BenchA", NsPerOp: 1000}, // ok
		{Name: "BenchB", NsPerOp: 1300}, // critical
		{Name: "BenchC", NsPerOp: 1150}, // warning
	}

	results := comp.Compare(current)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Should be: critical first, then warning, then ok
	expectedOrder := []string{"BenchB", "BenchC", "BenchA"}
	for i, exp := range expectedOrder {
		if results[i].Name != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, results[i].Name)
		}
	}
}

func TestFormatReport(t *testing.T) {
	results := []ComparisonResult{
		{Name: "BenchCritical", Baseline: 1000, Current: 1300, DeltaPct: 30.0, Regression: true, Severity: "critical"},
		{Name: "BenchWarning", Baseline: 1000, Current: 1150, DeltaPct: 15.0, Severity: "warning"},
		{Name: "BenchOK", Baseline: 1000, Current: 1010, DeltaPct: 1.0, Severity: "ok"},
	}

	report := FormatReport(results)

	if !strings.Contains(report, "🔴 BenchCritical") {
		t.Error("report should contain critical icon for BenchCritical")
	}
	if !strings.Contains(report, "🟡 BenchWarning") {
		t.Error("report should contain warning icon for BenchWarning")
	}
	if !strings.Contains(report, "✅ BenchOK") {
		t.Error("report should contain ok icon for BenchOK")
	}
	if !strings.Contains(report, "1 critical") {
		t.Error("report should mention 1 critical")
	}
	if !strings.Contains(report, "1 warning") {
		t.Error("report should mention 1 warning")
	}
	if !strings.Contains(report, "ACTION REQUIRED") {
		t.Error("report should contain ACTION REQUIRED for critical regressions")
	}
}

func TestFormatReport_NoRegressions(t *testing.T) {
	results := []ComparisonResult{
		{Name: "BenchOK", Baseline: 1000, Current: 1010, DeltaPct: 1.0, Severity: "ok"},
		{Name: "BenchFast", Baseline: 1000, Current: 500, DeltaPct: -50.0, Severity: "ok"},
	}

	report := FormatReport(results)
	if strings.Contains(report, "ACTION REQUIRED") {
		t.Error("report should NOT contain ACTION REQUIRED when no regressions")
	}
	if !strings.Contains(report, "acceptable thresholds") {
		t.Error("report should mention acceptable thresholds")
	}
}

func TestFormatReport_WarningsOnly(t *testing.T) {
	results := []ComparisonResult{
		{Name: "BenchWarn", Baseline: 1000, Current: 1150, DeltaPct: 15.0, Severity: "warning"},
	}

	report := FormatReport(results)
	if strings.Contains(report, "ACTION REQUIRED") {
		t.Error("report should NOT contain ACTION REQUIRED for warnings only")
	}
	if !strings.Contains(report, "Review warnings") {
		t.Error("report should suggest reviewing warnings")
	}
}

func TestFormatReport_Empty(t *testing.T) {
	report := FormatReport(nil)
	if !strings.Contains(report, "No benchmark results") {
		t.Errorf("empty report should say 'No benchmark results', got: %s", report)
	}
}

func TestHasRegressions_Empty(t *testing.T) {
	if HasRegressions(nil) {
		t.Error("HasRegressions should return false for nil")
	}
}

func TestHasWarnings_Empty(t *testing.T) {
	if HasWarnings(nil) {
		t.Error("HasWarnings should return false for nil")
	}
}

func TestDefaultRegressionConfig(t *testing.T) {
	config := DefaultRegressionConfig()
	if config.WarningThreshold != 10.0 {
		t.Errorf("expected WarningThreshold=10.0, got %f", config.WarningThreshold)
	}
	if config.CriticalThreshold != 25.0 {
		t.Errorf("expected CriticalThreshold=25.0, got %f", config.CriticalThreshold)
	}
	if config.MinNsPerOp != 100.0 {
		t.Errorf("expected MinNsPerOp=100.0, got %f", config.MinNsPerOp)
	}
}

func TestNewComparator_ZeroValues(t *testing.T) {
	store := &BaselineStore{Baseline: map[string]BenchmarkResult{}}
	config := RegressionConfig{} // all zero — should default
	comp := NewComparator(store, config)

	if comp.config.WarningThreshold != 10.0 {
		t.Errorf("expected WarningThreshold=10.0 for zero config, got %f", comp.config.WarningThreshold)
	}
	if comp.config.CriticalThreshold != 25.0 {
		t.Errorf("expected CriticalThreshold=25.0 for zero config, got %f", comp.config.CriticalThreshold)
	}
}

func TestBaselineStore_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewBaselineStore(path)
	err := store.Load()
	if err == nil {
		t.Error("expected error for invalid JSON file")
	}
}

func TestParseBenchOutput_Microseconds(t *testing.T) {
	// Some benchmarks report in µs/op instead of ns/op
	input := `BenchmarkSlow-4    50    1500000 ns/op    100000 B/op    500 allocs/op`
	results := ParseBenchOutput(input)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].NsPerOp != 1500000 {
		t.Errorf("expected 1500000 ns/op, got %f", results[0].NsPerOp)
	}
	if results[0].BPerOp != 100000 {
		t.Errorf("expected 100000 B/op, got %f", results[0].BPerOp)
	}
}
