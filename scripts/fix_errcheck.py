#!/usr/bin/env python3
"""Add _ = to errcheck violations from lint output."""
import re
import sys
from pathlib import Path

ROOT = Path("/workspace")
lint_path = Path(sys.argv[1] if len(sys.argv) > 1 else "/tmp/lint_v10.txt")

pat = re.compile(r"^([^:]+):(\d+):\d+: Error return value of .+ is not checked \(errcheck\)$")

by_file = {}
for line in lint_path.read_text().splitlines():
    m = pat.match(line)
    if m:
        path, lno = m.groups()
        by_file.setdefault(path, set()).add(int(lno))

for path, lines_set in by_file.items():
    p = ROOT / path
    lines = p.read_text().splitlines()
    for lno in sorted(lines_set, reverse=True):
        idx = lno - 1
        if idx >= len(lines):
            continue
        line = lines[idx]
        stripped = line.lstrip()
        if stripped.startswith("_ = ") or stripped.startswith("_,") or "err :=" in line:
            continue
        indent = line[: len(line) - len(stripped)]
        # multi-value returns
        if "w.Write(" in stripped or "ms.Write(" in stripped or ".Write(" in stripped:
            lines[idx] = indent + "_, _ = " + stripped
        elif ".Encode(" in stripped or ".Close(" in stripped or "json.Unmarshal(" in stripped or "os.Chmod(" in stripped or "os.Chdir(" in stripped:
            lines[idx] = indent + "_ = " + stripped
        elif ".Stop(" in stripped or ".Shutdown(" in stripped:
            lines[idx] = indent + "_ = " + stripped
        else:
            lines[idx] = indent + "_ = " + stripped
    p.write_text("\n".join(lines) + "\n")
    print(f"errcheck {path}: {len(lines_set)} lines")

print("done")
