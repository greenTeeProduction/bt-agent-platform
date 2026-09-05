#!/usr/bin/env python3
"""Fix remaining lint issues from /tmp/lint_v5.txt patterns."""
import re
from pathlib import Path

ROOT = Path("/workspace")

# unused-parameter bulk
FILES_TASK = [
    "internal/validate/suite_test.go",
    "internal/validate/coverage_gaps_test.go",
    "internal/workflow/integration_test.go",
    "internal/workflow/orchestrator_test.go",
]
for f in FILES_TASK:
    p = ROOT / f
    t = p.read_text()
    t = re.sub(r"func\(([^)]*),\s*task\s+string\)", r"func(\1, _ string)", t)
    p.write_text(t)

# plan unused in Reflect-like signatures
PLAN_FILES = [
    "internal/llm/testing.go",
    "internal/llm/fallback_test.go",
    "internal/startup/simulation_test.go",
    "internal/thinktank/thinktank_test.go",
    "internal/engine/chains_test.go",
    "internal/engine/merged_test.go",
    "internal/engine/engine_test.go",
    "internal/benchmark/benchmark.go",
    "internal/factory/factory_test.go",
    "internal/factory/factory_regression_test.go",
    "internal/eval/eval.go",
    "internal/langagent/langagent_test.go",
]
for f in PLAN_FILES:
    p = ROOT / f
    t = p.read_text()
    # Reflect(task, outcome, plan string) -> plan to _
    t = re.sub(
        r"Reflect\(([^,]+),\s*([^,]+),\s*plan\s+string\)",
        r"Reflect(\1, \2, _ string)",
        t,
    )
    t = re.sub(
        r"func\(([^)]*),\s*plan\s+string\)",
        lambda m: m.group(0).replace(", plan string", ", _ string"),
        t,
    )
    p.write_text(t)

# opts unused
for f in ["internal/langagent/langagent_test.go", "internal/langagent/coverage_test.go"]:
    p = ROOT / f
    t = p.read_text()
    t = t.replace(", opts ...", ", _ ...")
    p.write_text(t)

# engine integration t unused
p = ROOT / "internal/engine/integration_test.go"
t = p.read_text()
t = t.replace("func(t *testing.T) {", "func(_ *testing.T) {", 2)  # only first two in TestIntegration loops - risky
# safer: only lines 65 and 134
lines = t.splitlines()
for i, line in enumerate(lines):
    if "t.Run(tt.name, func(t *testing.T)" in line:
        lines[i] = line.replace("func(t *testing.T)", "func(_ *testing.T)")
p.write_text("\n".join(lines) + ("\n" if t.endswith("\n") else ""))

# goap err unused
p = ROOT / "internal/goap/goap_test.go"
t = p.read_text()
t = t.replace("func(_ int, a *Action, err error)", "func(_ int, a *Action, _ error)")
p.write_text(t)

# errors_test delay
p = ROOT / "internal/reliability/errors_test.go"
t = p.read_text()
if "delay time.Duration" in t:
    t = t.replace("delay time.Duration", "_ time.Duration", 1)
p.write_text(t)

# empty blocks - read specific lines from lint
EMPTY = {
    "internal/cicd/workflow_test.go": ("} else {\n\t\t// Expected: no runner in temp dir\n\t}", "} else {\n\t\t// intentionally empty\n\t}"),
    "internal/tracing/tracing_test.go": None,  # handle below
    "internal/reliability/weighted_router.go": None,
    "internal/reliability/errors.go": None,
    "internal/evolution/mcts_mutate_test.go": None,
    "internal/gardener/evolve_v2.go": None,
}
for f, pair in EMPTY.items():
    if pair:
        p = ROOT / f
        t = p.read_text()
        if pair[0] in t:
            t = t.replace(pair[0], pair[1])
            p.write_text(t)

# prealloc workflow
p = ROOT / "internal/workflow/orchestrator.go"
t = p.read_text()
t = t.replace("var outputs []string", "outputs := make([]string, 0, 8)", 1)
p.write_text(t)

# monitoring time-naming
p = ROOT / "internal/monitoring/alerts.go"
t = p.read_text()
t = t.replace("lowActivitySuppressHours", "lowActivitySuppressDur")
p.write_text(t)

# unused: remove dead test helpers
def remove_func_from_file(path, func_name):
    p = ROOT / path
    t = p.read_text()
    # remove func minimalCodeQL ... }
    pat = rf"\nfunc {re.escape(func_name)}\([^)]*\)[^{{]*\{{"
    m = re.search(pat, t)
    if not m:
        return
    start = m.start()
    brace = 0
    i = m.end() - 1
    while i < len(t):
        if t[i] == "{":
            brace += 1
        elif t[i] == "}":
            brace -= 1
            if brace == 0:
                end = i + 1
                while end < len(t) and t[end] in "\n":
                    end += 1
                t = t[:start] + t[end:]
                p.write_text(t)
                return
        i += 1

remove_func_from_file("internal/cicd/workflow_test.go", "minimalCodeQL")
remove_func_from_file("internal/validate/coverage_gaps_test.go", "contains")
remove_func_from_file("internal/agent/memory_test.go", "memoryTestDir")

# reliability test SA4006
p = ROOT / "internal/reliability/reliability_test.go"
t = p.read_text()
t = t.replace('result, err = router.Execute("agent", "task3")', '_, err = router.Execute("agent", "task3")')
p.write_text(t)

# langagent field tree - use blank identifier in struct or remove
p = ROOT / "internal/langagent/tools.go"
t = p.read_text()
if "tree *evolution.SerializableNode" in t and "field `tree`" in open("/tmp/lint_v5.txt").read():
    t = t.replace("tree *evolution.SerializableNode", "_tree *evolution.SerializableNode")
    t = t.replace("a.tree", "a._tree").replace("t.tree", "t._tree")
p.write_text(t)

print("fix_remaining done")
