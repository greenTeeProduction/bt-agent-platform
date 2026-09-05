package benchmark

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBaselineStore(t *testing.T) {
	s := NewBaselineStore("/tmp/does-not-matter.json")
	if s.path != "/tmp/does-not-matter.json" {
		t.Errorf("path = %q, want /tmp/does-not-matter.json", s.path)
	}
	if s.Baseline == nil {
		t.Error("Baseline map should be initialized, not nil")
	}
	if len(s.Baseline) != 0 {
		t.Errorf("Baseline should start empty, got %d entries", len(s.Baseline))
	}
}

func TestBaselineStore_Load_MissingFile(t *testing.T) {
	dir := t.TempDir()
	s := NewBaselineStore(filepath.Join(dir, "nope.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load() on missing file should return nil, got %v", err)
	}
	if len(s.Baseline) != 0 {
		t.Errorf("Baseline should remain empty after loading a missing file, got %d entries", len(s.Baseline))
	}
}

func TestBaselineStore_Load_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := NewBaselineStore(path)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() on empty file should return nil, got %v", err)
	}
}

func TestBaselineStore_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	s := NewBaselineStore(path)
	s.Baseline["BenchmarkFoo"] = BenchmarkResult{Name: "BenchmarkFoo", NsPerOp: 123.4, BPerOp: 56, Allocs: 2}

	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded := NewBaselineStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok := loaded.Baseline["BenchmarkFoo"]
	if !ok {
		t.Fatal("expected BenchmarkFoo to be present after round trip")
	}
	if got.NsPerOp != 123.4 || got.BPerOp != 56 || got.Allocs != 2 {
		t.Errorf("round-tripped result = %+v, want NsPerOp=123.4 BPerOp=56 Allocs=2", got)
	}
}

func TestBaselineStore_Save_MissingParentDirErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "baseline.json")
	s := NewBaselineStore(path)
	s.Baseline["BenchmarkFoo"] = BenchmarkResult{Name: "BenchmarkFoo", NsPerOp: 1}

	if err := s.Save(); err == nil {
		t.Fatal("Save() into a missing parent directory should error, got nil")
	}
}

func TestBaselineStore_UpdateBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	s := NewBaselineStore(path)
	s.Baseline["Stale"] = BenchmarkResult{Name: "Stale", NsPerOp: 999}

	results := []BenchmarkResult{
		{Name: "BenchmarkA", NsPerOp: 10},
		{Name: "BenchmarkB", NsPerOp: 20},
	}
	if err := s.UpdateBaseline(results); err != nil {
		t.Fatalf("UpdateBaseline() error = %v", err)
	}

	if _, stale := s.Baseline["Stale"]; stale {
		t.Error("UpdateBaseline should replace the baseline, not merge into it")
	}
	if len(s.Baseline) != 2 {
		t.Errorf("Baseline len = %d, want 2", len(s.Baseline))
	}
	if s.Baseline["BenchmarkA"].NsPerOp != 10 {
		t.Errorf("BenchmarkA NsPerOp = %v, want 10", s.Baseline["BenchmarkA"].NsPerOp)
	}

	// UpdateBaseline persists to disk too.
	reloaded := NewBaselineStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load() after UpdateBaseline error = %v", err)
	}
	if len(reloaded.Baseline) != 2 {
		t.Errorf("persisted baseline len = %d, want 2", len(reloaded.Baseline))
	}
}

func TestDefaultRegressionConfig(t *testing.T) {
	cfg := DefaultRegressionConfig()
	if cfg.WarningThreshold != 10.0 {
		t.Errorf("WarningThreshold = %v, want 10.0", cfg.WarningThreshold)
	}
	if cfg.CriticalThreshold != 25.0 {
		t.Errorf("CriticalThreshold = %v, want 25.0", cfg.CriticalThreshold)
	}
	if cfg.MinNsPerOp != 100.0 {
		t.Errorf("MinNsPerOp = %v, want 100.0", cfg.MinNsPerOp)
	}
}

func TestNewComparator_DefaultsZeroFields(t *testing.T) {
	store := NewBaselineStore("/tmp/unused.json")
	c := NewComparator(store, RegressionConfig{})
	if c.config.WarningThreshold != 10.0 {
		t.Errorf("WarningThreshold default = %v, want 10.0", c.config.WarningThreshold)
	}
	if c.config.CriticalThreshold != 25.0 {
		t.Errorf("CriticalThreshold default = %v, want 25.0", c.config.CriticalThreshold)
	}
	if c.config.MinNsPerOp != 100.0 {
		t.Errorf("MinNsPerOp default = %v, want 100.0", c.config.MinNsPerOp)
	}
}

func TestNewComparator_PreservesExplicitFields(t *testing.T) {
	store := NewBaselineStore("/tmp/unused.json")
	c := NewComparator(store, RegressionConfig{WarningThreshold: 5, CriticalThreshold: 50, MinNsPerOp: 1})
	if c.config.WarningThreshold != 5 {
		t.Errorf("WarningThreshold = %v, want 5", c.config.WarningThreshold)
	}
	if c.config.CriticalThreshold != 50 {
		t.Errorf("CriticalThreshold = %v, want 50", c.config.CriticalThreshold)
	}
	if c.config.MinNsPerOp != 1 {
		t.Errorf("MinNsPerOp = %v, want 1", c.config.MinNsPerOp)
	}
}

func TestComparator_Compare(t *testing.T) {
	tests := []struct {
		name        string
		baseline    map[string]BenchmarkResult
		current     []BenchmarkResult
		config      RegressionConfig
		wantResults []ComparisonResult
	}{
		{
			name:     "matching benchmark within thresholds is ok",
			baseline: map[string]BenchmarkResult{"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000}},
			current:  []BenchmarkResult{{Name: "BenchmarkA", NsPerOp: 1050}}, // +5%
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkA", Baseline: 1000, Current: 1050, DeltaPct: 5, Severity: "ok"},
			},
		},
		{
			name:     "delta just above warning threshold is a warning",
			baseline: map[string]BenchmarkResult{"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000}},
			current:  []BenchmarkResult{{Name: "BenchmarkA", NsPerOp: 1101}}, // +10.1%
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkA", Baseline: 1000, Current: 1101, DeltaPct: 10.1, Severity: "warning"},
			},
		},
		{
			name:     "delta exactly at warning threshold is ok, not warning",
			baseline: map[string]BenchmarkResult{"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000}},
			current:  []BenchmarkResult{{Name: "BenchmarkA", NsPerOp: 1100}}, // exactly +10%
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkA", Baseline: 1000, Current: 1100, DeltaPct: 10, Severity: "ok"},
			},
		},
		{
			name:     "delta above critical threshold is a regression",
			baseline: map[string]BenchmarkResult{"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000}},
			current:  []BenchmarkResult{{Name: "BenchmarkA", NsPerOp: 1300}}, // +30%
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkA", Baseline: 1000, Current: 1300, DeltaPct: 30, Severity: "critical", Regression: true},
			},
		},
		{
			name:     "below MinNsPerOp on both sides suppresses regression despite large delta",
			baseline: map[string]BenchmarkResult{"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 10}},
			current:  []BenchmarkResult{{Name: "BenchmarkA", NsPerOp: 50}}, // +400%, but both < MinNsPerOp(100)
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkA", Baseline: 10, Current: 50, DeltaPct: 400, Severity: "ok"},
			},
		},
		{
			name:     "MinNsPerOp only needs ONE side to cross the floor",
			baseline: map[string]BenchmarkResult{"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 10}},
			current:  []BenchmarkResult{{Name: "BenchmarkA", NsPerOp: 1000}}, // baseline below floor, current above
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkA", Baseline: 10, Current: 1000, DeltaPct: 9900, Severity: "critical", Regression: true},
			},
		},
		{
			name:     "benchmark present in baseline but missing from current run is a warning",
			baseline: map[string]BenchmarkResult{"BenchmarkGone": {Name: "BenchmarkGone", NsPerOp: 1000}},
			current:  []BenchmarkResult{},
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkGone", Baseline: 1000, Current: 0, Severity: "warning"},
			},
		},
		{
			name:     "new benchmark with no baseline is ok, not a regression",
			baseline: map[string]BenchmarkResult{},
			current:  []BenchmarkResult{{Name: "BenchmarkNew", NsPerOp: 1000}},
			config:   DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkNew", Baseline: 0, Current: 1000, Severity: "ok"},
			},
		},
		{
			name: "results are sorted critical, warning, ok, then by name",
			baseline: map[string]BenchmarkResult{
				"BenchmarkZOk":       {Name: "BenchmarkZOk", NsPerOp: 1000},
				"BenchmarkACritical": {Name: "BenchmarkACritical", NsPerOp: 1000},
				"BenchmarkBWarning":  {Name: "BenchmarkBWarning", NsPerOp: 1000},
			},
			current: []BenchmarkResult{
				{Name: "BenchmarkZOk", NsPerOp: 1000},
				{Name: "BenchmarkACritical", NsPerOp: 1300},
				{Name: "BenchmarkBWarning", NsPerOp: 1150},
			},
			config: DefaultRegressionConfig(),
			wantResults: []ComparisonResult{
				{Name: "BenchmarkACritical", Baseline: 1000, Current: 1300, DeltaPct: 30, Severity: "critical", Regression: true},
				{Name: "BenchmarkBWarning", Baseline: 1000, Current: 1150, DeltaPct: 15, Severity: "warning"},
				{Name: "BenchmarkZOk", Baseline: 1000, Current: 1000, DeltaPct: 0, Severity: "ok"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewBaselineStore("/tmp/unused.json")
			store.Baseline = tt.baseline
			c := NewComparator(store, tt.config)
			got := c.Compare(tt.current)

			if len(got) != len(tt.wantResults) {
				t.Fatalf("Compare() returned %d results, want %d: %+v", len(got), len(tt.wantResults), got)
			}
			for i, want := range tt.wantResults {
				g := got[i]
				if g.Name != want.Name || g.Baseline != want.Baseline || g.Current != want.Current ||
					math.Abs(g.DeltaPct-want.DeltaPct) > 1e-9 || g.Regression != want.Regression || g.Severity != want.Severity {
					t.Errorf("result[%d] = %+v, want %+v", i, g, want)
				}
			}
		})
	}
}

func TestHasRegressions(t *testing.T) {
	tests := []struct {
		name    string
		results []ComparisonResult
		want    bool
	}{
		{"empty", nil, false},
		{"no regressions", []ComparisonResult{{Severity: "ok"}, {Severity: "warning"}}, false},
		{"has regression", []ComparisonResult{{Severity: "ok"}, {Regression: true, Severity: "critical"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasRegressions(tt.results); got != tt.want {
				t.Errorf("HasRegressions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasWarnings(t *testing.T) {
	tests := []struct {
		name    string
		results []ComparisonResult
		want    bool
	}{
		{"empty", nil, false},
		{"only ok", []ComparisonResult{{Severity: "ok"}}, false},
		{"has warning", []ComparisonResult{{Severity: "ok"}, {Severity: "warning"}}, true},
		{"has critical", []ComparisonResult{{Severity: "ok"}, {Severity: "critical"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasWarnings(tt.results); got != tt.want {
				t.Errorf("HasWarnings() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatReport_Empty(t *testing.T) {
	got := FormatReport(nil)
	want := "No benchmark results to report."
	if got != want {
		t.Errorf("FormatReport(nil) = %q, want %q", got, want)
	}
}

func TestFormatReport_AllOk(t *testing.T) {
	results := []ComparisonResult{{Name: "BenchmarkA", Baseline: 100, Current: 100, Severity: "ok"}}
	got := FormatReport(results)
	if !strings.Contains(got, "All benchmarks within acceptable thresholds") {
		t.Errorf("FormatReport() missing all-ok closing line, got: %s", got)
	}
	if !strings.Contains(got, "1 benchmarks | 0 critical | 0 warning | 1 ok") {
		t.Errorf("FormatReport() missing correct summary line, got: %s", got)
	}
}

func TestFormatReport_WithWarning(t *testing.T) {
	results := []ComparisonResult{{Name: "BenchmarkA", Baseline: 100, Current: 115, DeltaPct: 15, Severity: "warning"}}
	got := FormatReport(results)
	if !strings.Contains(got, "Review warnings") {
		t.Errorf("FormatReport() missing warning closing line, got: %s", got)
	}
}

func TestFormatReport_WithCritical(t *testing.T) {
	results := []ComparisonResult{{Name: "BenchmarkA", Baseline: 100, Current: 200, DeltaPct: 100, Severity: "critical", Regression: true}}
	got := FormatReport(results)
	if !strings.Contains(got, "ACTION REQUIRED") {
		t.Errorf("FormatReport() missing critical closing line, got: %s", got)
	}
}

func TestFormatReport_NewBenchmarkNoBaseline(t *testing.T) {
	results := []ComparisonResult{{Name: "BenchmarkNew", Current: 100, Severity: "ok"}}
	got := FormatReport(results)
	if !strings.Contains(got, "new benchmark, no baseline") {
		t.Errorf("FormatReport() missing new-benchmark annotation, got: %s", got)
	}
}

func TestParseBenchOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []BenchmarkResult
	}{
		{
			name:   "empty output",
			output: "",
			want:   []BenchmarkResult{},
		},
		{
			name:   "full line with mem stats and GOMAXPROCS suffix",
			output: "BenchmarkFoo-8    1000000    123.4 ns/op    56 B/op    2 allocs/op",
			want: []BenchmarkResult{
				{Name: "BenchmarkFoo", NsPerOp: 123.4, BPerOp: 56, Allocs: 2},
			},
		},
		{
			name:   "line without mem stats",
			output: "BenchmarkBar-4    500000    250 ns/op",
			want: []BenchmarkResult{
				{Name: "BenchmarkBar", NsPerOp: 250},
			},
		},
		{
			name:   "line without GOMAXPROCS suffix",
			output: "BenchmarkBaz    100000    99 ns/op",
			want: []BenchmarkResult{
				{Name: "BenchmarkBaz", NsPerOp: 99},
			},
		},
		{
			name: "skips goos/goarch/pkg/cpu/PASS/ok/FAIL/? noise lines",
			output: `goos: linux
goarch: amd64
pkg: github.com/nico/go-bt-evolve/internal/benchmark
cpu: Intel
BenchmarkFoo-8    1000000    123 ns/op
PASS
ok      github.com/nico/go-bt-evolve/internal/benchmark    1.234s
FAIL    something
? some/package [no test files]`,
			want: []BenchmarkResult{
				{Name: "BenchmarkFoo", NsPerOp: 123},
			},
		},
		{
			name: "multiple benchmark lines preserve order",
			output: `BenchmarkA-4    1000    10 ns/op
BenchmarkB-4    2000    20 ns/op
BenchmarkC-4    3000    30 ns/op`,
			want: []BenchmarkResult{
				{Name: "BenchmarkA", NsPerOp: 10},
				{Name: "BenchmarkB", NsPerOp: 20},
				{Name: "BenchmarkC", NsPerOp: 30},
			},
		},
		{
			name:   "malformed line without ns/op is skipped",
			output: "this is not a benchmark line at all",
			want:   []BenchmarkResult{},
		},
		{
			name:   "blank lines are skipped",
			output: "\n\n  \nBenchmarkFoo-4    100    50 ns/op\n\n",
			want: []BenchmarkResult{
				{Name: "BenchmarkFoo", NsPerOp: 50},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseBenchOutput(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseBenchOutput() returned %d results, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, want := range tt.want {
				g := got[i]
				if g.Name != want.Name || g.NsPerOp != want.NsPerOp || g.BPerOp != want.BPerOp || g.Allocs != want.Allocs {
					t.Errorf("result[%d] = %+v, want %+v", i, g, want)
				}
			}
		})
	}
}
