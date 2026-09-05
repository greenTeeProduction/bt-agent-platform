#!/usr/bin/env python3
"""Apply bulk lint fixes from golangci-lint output."""
import re
import subprocess
from pathlib import Path

ROOT = Path("/workspace")

# SA1012: nil context -> context.TODO()
NIL_CTX_FILES = [
    ("internal/benchmark/benchmark_test.go", "GenerateCtx(nil,", "GenerateCtx(context.TODO(),"),
    ("internal/engine/edge_case_coverage_test.go", "btcore.NewBTContext(nil,", "btcore.NewBTContext(context.TODO(),"),
    ("internal/reliability/scalability_probe_test.go", "ProbeSingleNodeDashboard(nil,", "ProbeSingleNodeDashboard(context.TODO(),"),
    ("internal/tracing/tracing_test.go", "StartSpan(nil,", "StartSpan(context.TODO(),"),
    ("internal/tracing/w3c_test.go", "ContextWithTraceParent(nil,", "ContextWithTraceParent(context.TODO(),"),
]

# Prealloc: var name []Type -> make with capacity
PREALLOC = [
    ("internal/benchreg/benchreg.go", "var results []ComparisonResult", "results := make([]ComparisonResult, 0, len(c.store.Baseline)+len(current))"),
    ("internal/benchreg/benchreg.go", "var results []BenchmarkResult", "results := make([]BenchmarkResult, 0, 32)"),
    ("internal/reflection/store.go", "var records []Record", "records := make([]Record, 0, 64)"),
    ("internal/config/config.go", "var msgs []string", "msgs := make([]string, 0, 8)"),
    ("internal/dashboard/agents.go", "var agents []AgentInfo", "agents := make([]AgentInfo, 0, 16)"),
    ("internal/metrics/metrics.go", "var b []byte", "b := make([]byte, 0, 256)"),
    ("cmd/bt-dashboard/main.go", "var r2 []map[string]interface{}", "r2 := make([]map[string]interface{}, 0, 8)"),
    ("cmd/bt-dashboard/main.go", "var ff []map[string]interface{}", "ff := make([]map[string]interface{}, 0, 8)"),
    ("cmd/bt-docgen/main.go", "var result []int", "result := make([]int, 0, 16)"),
    ("cmd/bt-gardener/main.go", "var items []r", "items := make([]r, 0, 16)"),
    ("cmd/bt-gardener/main.go", "var recs []rec", "recs := make([]rec, 0, 16)"),
    ("internal/agent/catalog.go", "var result []CatalogEntry", "result := make([]CatalogEntry, 0, 32)"),
    ("internal/agent/memory.go", "var results []MemoryEntry", "results := make([]MemoryEntry, 0, 32)"),
    ("internal/agent/memory.go", "var lines []string", "lines := make([]string, 0, 16)"),
    ("internal/benchmark/benchmark.go", "var results []Result", "results := make([]Result, 0, 32)"),
    ("internal/benchmark/bfcl_v3.go", "var results []BFCLV3Result", "results := make([]BFCLV3Result, 0, 32)"),
    ("internal/benchmark/btpg.go", "var results []BTPGTaskResult", "results := make([]BTPGTaskResult, 0, 32)"),
    ("internal/benchmark/external.go", "var results []BFCLEvalResult", "results := make([]BFCLEvalResult, 0, 32)"),
    ("internal/benchmark/swebench_verified.go", "var results []SWEVerifiedResult", "results := make([]SWEVerifiedResult, 0, 32)"),
    ("internal/benchmark/taubench.go", "var entries []TauBenchEntry", "entries := make([]TauBenchEntry, 0, 32)"),
    ("internal/benchmark/taubench.go", "var results []TauBenchResult", "results := make([]TauBenchResult, 0, 32)"),
    ("internal/benchmark/external_test.go", "var scores []treeScore", "scores := make([]treeScore, 0, 16)"),
    ("internal/benchmark/integration_test.go", "var allTrees []namedTree", "allTrees := make([]namedTree, 0, 16)"),
    ("internal/benchmark/integration_test.go", "var results []treeResult", "results := make([]treeResult, 0, 16)"),
    ("internal/benchmark/integration_test.go", "var treeNames []string", "treeNames := make([]string, 0, 16)"),
    ("internal/engine/chains.go", "var results []string", "results := make([]string, 0, 8)"),
    ("internal/engine/chains.go", "var parts []string", "parts := make([]string, 0, 8)"),
    ("internal/engine/utility_selector.go", "var scores []UtilityScore", "scores := make([]UtilityScore, 0, 16)"),
    ("internal/engine/stress_test.go", "var failures []string", "failures := make([]string, 0, 8)"),
    ("internal/eval/eval.go", "var allResults []SuiteEvalResult", "allResults := make([]SuiteEvalResult, 0, 8)"),
    ("internal/evaluator/stockfish.go", "var candidates []MutationCandidate", "candidates := make([]MutationCandidate, 0, 16)"),
    ("internal/evaluator/stockfish.go", "var result []string", "result := make([]string, 0, 16)"),
    ("internal/evaluator/evaluator_test.go", "var records []reflection.Record", "records := make([]reflection.Record, 0, 8)"),
    ("internal/evolution/experience_bank.go", "var candidates []scored", "candidates := make([]scored, 0, 16)"),
    ("internal/evolution/pareto.go", "var parts []string", "parts := make([]string, 0, 8)"),
    ("internal/gardener/evolve_v2.go", "var results []CycleMetrics", "results := make([]CycleMetrics, 0, 8)"),
    ("internal/gardener/gardener.go", "var results []CycleMetrics", "results := make([]CycleMetrics, 0, 8)"),
    ("internal/langagent/tools.go", "var items []summary", "items := make([]summary, 0, 16)"),
    ("internal/mcp/mcp_test.go", "var msgs []Message", "msgs := make([]Message, 0, 8)"),
    ("internal/tracing/reader.go", "var traces []*AggregatedTrace", "traces := make([]*AggregatedTrace, 0, 16)"),
    ("internal/tracing/reader.go", "var opList []string", "opList := make([]string, 0, 16)"),
    ("internal/workflow/orchestrator.go", "var outputs []string", "outputs := make([]string, 0, 8)"),
]

# Bulk text replacements per file
REPLACEMENTS = [
    ("internal/benchreg/benchreg.go", "func pctChange(old, new float64)", "func pctChange(old, newVal float64)"),
    ("internal/benchreg/benchreg.go", "if new == 0", "if newVal == 0"),
    ("internal/benchreg/benchreg.go", "return ((new - old) / old)", "return ((newVal - old) / old)"),
    ("internal/api/openapi_test.go", "\tmin := 0.0\n\tmax := 100.0", "\tminBound := 0.0\n\tmaxBound := 100.0"),
    ("internal/api/openapi_test.go", "Minimum: &min,\n\t\tMaximum: &max,", "Minimum: &minBound,\n\t\tMaximum: &maxBound,"),
    ("internal/knowledge/analytics.go", "func min(a, b int)", "func minInt(a, b int)"),
    ("internal/evolution/learning.go", "func max(a, b int)", "func maxInt(a, b int)"),
    ("cmd/evolve_all/main.go", "func min(a, b int)", "func minInt(a, b int)"),
    ("internal/engine/chains.go", "func replaceAll(s, old, new string)", "func replaceAll(s, old, newStr string)"),
    ("internal/agent/scheduler.go", "func parseCronField(field string, min, max int)", "func parseCronField(field string, minVal, maxVal int)"),
    ("internal/agent/bus_test.go", "\tif cap := bus.maxHistory;", "\tif capVal := bus.maxHistory;"),
    ("internal/agent/bus_test.go", "cap != 50", "capVal != 50"),
    ("internal/workflow/orchestrator.go", ", new string)", ", newName string)"),  # only if matches replace pattern
]

def replace_in_file(rel_path: str, old: str, new: str, count=None):
    path = ROOT / rel_path
    text = path.read_text()
    if old not in text:
        return False
    if count:
        text = text.replace(old, new, count)
    else:
        text = text.replace(old, new)
    path.write_text(text)
    return True

def add_context_import(path: Path):
    text = path.read_text()
    if '"context"' in text or "context.TODO" not in text:
        return
    if path.suffix != ".go":
        return
    # add context to import block
    if "import (" in text:
        text = text.replace("import (\n", 'import (\n\t"context"\n', 1)
    else:
        text = text.replace("import ", 'import (\n\t"context"\n)\n\nimport ', 1)
    path.write_text(text)

def main():
    for rel, old, new in PREALLOC:
        replace_in_file(rel, old, new)

    for rel, old, new in REPLACEMENTS:
        replace_in_file(rel, old, new)

    for rel, old, new in NIL_CTX_FILES:
        path = ROOT / rel
        text = path.read_text()
        if old in text:
            text = text.replace(old, new)
            if "context.TODO" in text and '"context"' not in text:
                if "import (" in text:
                    if "\t\"context\"\n" not in text:
                        text = re.sub(
                            r"(import \()\n",
                            r'\1\n\t"context"\n',
                            text,
                            count=1,
                        )
            path.write_text(text)

    # validate: unused treeID
    for f in ["internal/validate/suite_test.go", "internal/validate/coverage_gaps_test.go"]:
        path = ROOT / f
        text = path.read_text()
        text = text.replace("func(_, treeID, task", "func(_, _, task")
        path.write_text(text)

    # reliability: unused task param
    for f in ROOT.rglob("*_test.go"):
        if "internal/reliability" in str(f):
            text = f.read_text()
            new = text.replace("func(_, task string)", "func(_, _ string)")
            if new != text:
                f.write_text(new)

    print("batch replacements done")

if __name__ == "__main__":
    main()
