// Package benchreg provides benchmark regression detection for Go benchmarks.
// It parses `go test -bench` output, stores baseline results, and compares
// new runs against stored baselines to detect significant performance regressions.
package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// BenchmarkResult represents a single parsed benchmark line.
type BenchmarkResult struct {
	Name    string  `json:"name"`
	NsPerOp float64 `json:"ns_per_op"`
	BPerOp  float64 `json:"b_per_op"`
	Allocs  int     `json:"allocs"`
}

// ComparisonResult represents how a benchmark compares against its baseline.
type ComparisonResult struct {
	Name       string  `json:"name"`
	Baseline   float64 `json:"baseline_ns_per_op"`
	Current    float64 `json:"current_ns_per_op"`
	DeltaPct   float64 `json:"delta_pct"` // positive = slower (regression)
	Regression bool    `json:"regression"`
	Severity   string  `json:"severity"` // "ok", "warning", or "critical"
}

// BaselineStore persists and loads benchmark baselines.
type BaselineStore struct {
	path     string
	Baseline map[string]BenchmarkResult `json:"baseline"`
}

// NewBaselineStore creates a store backed by the given file path.
func NewBaselineStore(path string) *BaselineStore {
	return &BaselineStore{
		path:     path,
		Baseline: make(map[string]BenchmarkResult),
	}
}

// Load reads baseline data from the file. Returns nil if the file doesn't exist.
func (s *BaselineStore) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh start
		}
		return fmt.Errorf("read baseline: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &s)
}

// Save writes the current baseline to disk.
func (s *BaselineStore) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

// UpdateBaseline replaces the stored baseline with the given results.
func (s *BaselineStore) UpdateBaseline(results []BenchmarkResult) error {
	s.Baseline = make(map[string]BenchmarkResult, len(results))
	for _, r := range results {
		s.Baseline[r.Name] = r
	}
	return s.Save()
}

// RegressionConfig controls regression detection thresholds.
type RegressionConfig struct {
	// WarningThreshold is the percentage slowdown that triggers a warning (default 10%).
	WarningThreshold float64
	// CriticalThreshold is the percentage slowdown that triggers a critical alert (default 25%).
	CriticalThreshold float64
	// MinNsPerOp ignores benchmarks below this threshold (avoids noise on trivial benchmarks, default 100).
	MinNsPerOp float64
}

// DefaultRegressionConfig returns sensible defaults.
func DefaultRegressionConfig() RegressionConfig {
	return RegressionConfig{
		WarningThreshold:  10.0,
		CriticalThreshold: 25.0,
		MinNsPerOp:        100.0,
	}
}

// Comparator detects regressions by comparing current results against a baseline.
type Comparator struct {
	store  *BaselineStore
	config RegressionConfig
}

// NewComparator creates a Comparator with the given store and config.
func NewComparator(store *BaselineStore, config RegressionConfig) *Comparator {
	if config.WarningThreshold == 0 {
		config.WarningThreshold = 10.0
	}
	if config.CriticalThreshold == 0 {
		config.CriticalThreshold = 25.0
	}
	if config.MinNsPerOp == 0 {
		config.MinNsPerOp = 100.0
	}
	return &Comparator{store: store, config: config}
}

// Compare compares current benchmark results against stored baselines.
// Results are returned sorted by severity (critical first, then warning, then ok).
func (c *Comparator) Compare(current []BenchmarkResult) []ComparisonResult {
	// Build a lookup for fast access
	currentMap := make(map[string]BenchmarkResult, len(current))
	for _, r := range current {
		currentMap[r.Name] = r
	}

	var results []ComparisonResult

	// Check existing baselines
	for name, baseline := range c.store.Baseline {
		cur, ok := currentMap[name]
		cr := ComparisonResult{
			Name:     name,
			Baseline: baseline.NsPerOp,
			Severity: "ok",
		}
		if ok {
			cr.Current = cur.NsPerOp
			cr.DeltaPct = pctChange(baseline.NsPerOp, cur.NsPerOp)
			if baseline.NsPerOp >= c.config.MinNsPerOp || cur.NsPerOp >= c.config.MinNsPerOp {
				if cr.DeltaPct > c.config.CriticalThreshold {
					cr.Regression = true
					cr.Severity = "critical"
				} else if cr.DeltaPct > c.config.WarningThreshold {
					cr.Severity = "warning"
				}
			}
		} else {
			// benchmark existed in baseline but not in current run
			cr.Current = 0
			cr.Severity = "warning"
		}
		results = append(results, cr)
		delete(currentMap, name)
	}

	// New benchmarks (in current but not in baseline)
	for _, r := range current {
		if _, exists := c.store.Baseline[r.Name]; !exists {
			results = append(results, ComparisonResult{
				Name:     r.Name,
				Current:  r.NsPerOp,
				Severity: "ok", // new benchmarks aren't regressions
			})
		}
	}

	// Sort: critical > warning > ok, then by name
	sort.Slice(results, func(i, j int) bool {
		order := map[string]int{"critical": 0, "warning": 1, "ok": 2}
		if order[results[i].Severity] != order[results[j].Severity] {
			return order[results[i].Severity] < order[results[j].Severity]
		}
		return results[i].Name < results[j].Name
	})

	return results
}

// HasRegressions returns true if any comparison result is a critical regression.
func HasRegressions(results []ComparisonResult) bool {
	for _, r := range results {
		if r.Regression {
			return true
		}
	}
	return false
}

// HasWarnings returns true if any comparison result is a warning or critical.
func HasWarnings(results []ComparisonResult) bool {
	for _, r := range results {
		if r.Severity == "warning" || r.Severity == "critical" {
			return true
		}
	}
	return false
}

// FormatReport produces a human-readable report from comparison results.
func FormatReport(results []ComparisonResult) string {
	if len(results) == 0 {
		return "No benchmark results to report."
	}

	var sb strings.Builder
	sb.WriteString("=== Benchmark Regression Report ===\n\n")

	criticals := 0
	warnings := 0
	for _, r := range results {
		if r.Severity == "critical" {
			criticals++
		} else if r.Severity == "warning" {
			warnings++
		}
	}

	sb.WriteString(fmt.Sprintf("Total: %d benchmarks | %d critical | %d warning | %d ok\n\n",
		len(results), criticals, warnings, len(results)-criticals-warnings))

	for _, r := range results {
		icon := "✅"
		switch r.Severity {
		case "critical":
			icon = "🔴"
		case "warning":
			icon = "🟡"
		}
		sb.WriteString(fmt.Sprintf("%s %s\n", icon, r.Name))
		if r.Baseline > 0 {
			sb.WriteString(fmt.Sprintf("   baseline: %.0f ns/op  current: %.0f ns/op  delta: %+.1f%%\n",
				r.Baseline, r.Current, r.DeltaPct))
		} else {
			sb.WriteString(fmt.Sprintf("   current: %.0f ns/op  (new benchmark, no baseline)\n", r.Current))
		}
		sb.WriteString("\n")
	}

	if criticals > 0 {
		sb.WriteString("⚠ ACTION REQUIRED: Critical regressions detected. Investigate before merging.\n")
	} else if warnings > 0 {
		sb.WriteString("ℹ Review warnings. If acceptable, update baselines.\n")
	} else {
		sb.WriteString("✅ All benchmarks within acceptable thresholds.\n")
	}

	return sb.String()
}

// pctChange computes the percent change from old to new.
// Positive means new is slower (regression). Negative means improvement.
func pctChange(old, new float64) float64 {
	if old == 0 {
		if new == 0 {
			return 0
		}
		return 100.0 // new benchmark, treat as 100% change
	}
	return ((new - old) / old) * 100.0
}

// ParseBenchOutput parses `go test -bench` output lines into BenchmarkResults.
// It handles lines like:
//
//	BenchmarkName-4    100000    1234 ns/op    512 B/op    5 allocs/op
func ParseBenchOutput(output string) []BenchmarkResult {
	// Matches: BenchmarkName-N  iterations  ns/op  [B/op]  [allocs/op]
	// The \S+ in the first capture includes the -N GOMAXPROCS suffix; we strip it below.
	re := regexp.MustCompile(`^(Benchmark\S+?)(?:-\d+)?\s+(\d+)\s+([\d.]+)\s+ns/op(?:\s+([\d.]+)\s+B/op)?(?:\s+(\d+)\s+allocs/op)?`)
	lines := strings.Split(output, "\n")
	var results []BenchmarkResult

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "goos:") || strings.HasPrefix(line, "goarch:") ||
			strings.HasPrefix(line, "pkg:") || strings.HasPrefix(line, "cpu:") || strings.HasPrefix(line, "PASS") ||
			strings.HasPrefix(line, "ok") || strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "?") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		// Name from capture group 1 (suffix already stripped by regex)
		name := matches[1]
		nsPerOp, _ := strconv.ParseFloat(matches[3], 64)

		var bPerOp float64
		var allocs int
		if matches[4] != "" {
			bPerOp, _ = strconv.ParseFloat(matches[4], 64)
		}
		if matches[5] != "" {
			allocs, _ = strconv.Atoi(matches[5])
		}

		results = append(results, BenchmarkResult{
			Name:    name,
			NsPerOp: nsPerOp,
			BPerOp:  bPerOp,
			Allocs:  allocs,
		})
	}

	return results
}
