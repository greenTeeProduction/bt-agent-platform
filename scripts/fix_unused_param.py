#!/usr/bin/env python3
"""Fix revive unused-parameter from golangci-lint output."""
import re
import sys
from pathlib import Path

ROOT = Path("/workspace")
lint_path = Path(sys.argv[1] if len(sys.argv) > 1 else "/tmp/lint_final.txt")

pat = re.compile(
    r"^([^:]+):(\d+):(\d+): unused-parameter: parameter '(\w+)' seems to be unused"
)

changes = []
for line in lint_path.read_text().splitlines():
    m = pat.match(line)
    if not m:
        continue
    path, lno, col, pname = m.groups()
    changes.append((path, int(lno), int(col), pname))

# apply from bottom of each file to top to preserve columns
by_file = {}
for path, lno, col, pname in changes:
    by_file.setdefault(path, []).append((lno, col, pname))
for path, items in by_file.items():
    p = ROOT / path
    lines = p.read_text().splitlines()
    for lno, col, pname in sorted(items, key=lambda x: -x[0]):
        idx = lno - 1
        if idx >= len(lines):
            continue
        line = lines[idx]
        # col is 1-based byte/char position in line for param name
        pos = col - 1
        if pos < len(line) and line[pos : pos + len(pname)] == pname:
            lines[idx] = line[:pos] + "_" + line[pos + len(pname) :]
        else:
            # fallback: replace first occurrence of pname as param
            lines[idx] = re.sub(rf"\b{pname}\b", "_", line, count=1)
    p.write_text("\n".join(lines) + ("\n" if p.read_text().endswith("\n") else ""))

print(f"fixed {len(changes)} unused-parameter issues")
