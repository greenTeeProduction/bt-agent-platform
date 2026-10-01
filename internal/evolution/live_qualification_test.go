package evolution_test

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
)

// Missing capabilities are a visible skip, never evidence of task success.
// Model failures remain test failures even when a fixture is also missing.
func requireQualifiedBenchmark(t *testing.T, metrics *benchmark.RunMetrics) {
	t.Helper()
	if metrics == nil || metrics.TotalTasks == 0 {
		t.Fatal("benchmark returned no task results")
	}
	if metrics.ModelEvidence.Errors > 0 {
		t.Fatalf("real inference failed: %+v", metrics.ModelEvidence)
	}
	if strings.Contains(metrics.Warning, "missing isolated capability fixture") || strings.Contains(metrics.Warning, "no model calls executed") {
		t.Skipf("UNQUALIFIED: %s", metrics.Warning)
	}
	if metrics.Warning != "" || metrics.ModelEvidence.Calls == 0 {
		t.Fatalf("benchmark lacks real inference evidence: %+v", metrics)
	}
}
