#!/usr/bin/env python3
"""Apply gocritic suggestions from lint output where message includes replacement."""
import re
from pathlib import Path

ROOT = Path("/workspace")
LINT = Path("/tmp/lint_v7.txt")
if not LINT.exists():
    LINT = Path("/tmp/lint_now.txt")

def apply_param_combine(path: Path, line_no: int, new_sig_fragment: str):
    lines = path.read_text().splitlines(keepends=True)
    idx = line_no - 1
    if idx >= len(lines):
        return False
    # find func( on this line or nearby
    for off in range(0, 5):
        i = idx + off
        if i >= len(lines):
            break
        if "func(" in lines[i] or "func (" in lines[i]:
            # extract old func signature from message
            old_m = re.search(r"func\([^)]*\)(?:\s*\([^)]*\))?", new_sig_fragment)
            if not old_m:
                return False
            new_m = re.search(r"func\([^)]*\)(?:\s*\([^)]*\))?", new_sig_fragment)
            if not new_m:
                return False
            line = lines[i]
            # replace first func(...) occurrence on line
            fm = re.search(r"func\([^)]*\)(?:\s*\([^)]*\))?", line)
            if fm:
                lines[i] = line[: fm.start()] + new_m.group(0) + line[fm.end() :]
                path.write_text("".join(lines))
                return True
    return False

def main():
    text = LINT.read_text()
    for raw in text.splitlines():
        if "(gocritic)" not in raw:
            continue
        m = re.match(
            r"^([^:]+):(\d+):\d+: ([^:]+): (.+) could be replaced with (.+) \(gocritic\)\s*$",
            raw,
        )
        if not m:
            continue
        path, line, check, old_part, new_part = m.groups()
        if check != "paramTypeCombine":
            continue
        p = ROOT / path
        if apply_param_combine(p, int(line), new_part):
            print(f"paramTypeCombine {path}:{line}")

    # Simple assignOp / indexAlloc replacements by line
    simple = [
        ("internal/dashboard/metrics.go", "hours = hours % 24", "hours %= 24"),
        ("internal/api/openapi_test.go", 'strings.Index(string(data), "\\"/api/a\\"")', 'bytes.Index(data, []byte("\\"/api/a\\""))'),
        ("internal/api/openapi_test.go", 'strings.Index(string(data), "\\"/api/b\\"")', 'bytes.Index(data, []byte("\\"/api/b\\""))'),
    ]
    for rel, old, new in simple:
        p = ROOT / rel
        t = p.read_text()
        if old in t:
            t = t.replace(old, new)
            if rel.endswith("_test.go") and "bytes" not in t.split("import")[1][:200]:
                t = t.replace("import (\n", 'import (\n\t"bytes"\n', 1)
            p.write_text(t)
            print("simple", rel)

    # captLocal: rename param C to cExpl in evolution
    for rel in ["internal/evolution/cmaes.go", "internal/evolution/mcts_mutate.go"]:
        p = ROOT / rel
        t = p.read_text()
        t = re.sub(r"\bC float64\b", "cExpl float64", t)
        t = re.sub(r"\(C float64\)", "(cExpl float64)", t)
        t = re.sub(r", C\)", ", cExpl)", t)
        t = re.sub(r"UCB1\(C\)", "UCB1(cExpl)", t)
        t = re.sub(r"BestChild\(C\)", "BestChild(cExpl)", t)
        # matrix vars named C in cmaes - only rename function params not [][]float64 C
        p.write_text(t)
        print("captLocal", rel)

    # unlambda patterns in tests
    unlambda = [
        ("internal/evaluator/cascade_test.go", "quickFn := func(tree *evolution.SerializableNode) float64 {\n\treturn StructuralQuickEval(tree)\n}", "quickFn := StructuralQuickEval"),
        ("internal/evolution/map_elites_test.go", "fitnessFn := func(tree *SerializableNode) float64 {\n\treturn StructuralQuickEval(tree)\n}", "fitnessFn := StructuralQuickEval"),
        ("internal/evolution/multi_objective_test.go", "fitnessFn := func(node *SerializableNode) MultiFitness {\n\treturn StructuralMultiFitness(node)\n}", "fitnessFn := StructuralMultiFitness"),
        ("internal/evolution/pareto_test.go", "fitnessFn := func(tree *SerializableNode) MultiFitness {\n\treturn StructuralMultiFitness(tree)\n}", "fitnessFn := StructuralMultiFitness"),
    ]
    for rel, old, new in unlambda:
        p = ROOT / rel
        t = p.read_text()
        if old in t:
            p.write_text(t.replace(old, new))
            print("unlambda", rel)

if __name__ == "__main__":
    main()
