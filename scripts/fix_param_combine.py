#!/usr/bin/env python3
"""Apply gocritic paramTypeCombine from lint output."""
import re
import sys
from pathlib import Path

ROOT = Path("/workspace")
lint_path = Path(sys.argv[1] if len(sys.argv) > 1 else "/tmp/lint_final.txt")

pat = re.compile(
    r"^([^:]+):(\d+):\d+: paramTypeCombine: (.+) could be replaced with (.+) \(gocritic\)$"
)

for line in lint_path.read_text().splitlines():
    m = pat.match(line)
    if not m:
        continue
    path, lno, old_frag, new_frag = m.groups()
    p = ROOT / path
    lines = p.read_text().splitlines()
    idx = int(lno) - 1
    if idx >= len(lines):
        continue
    # find func signature fragment on this line
    om = re.search(r"func\([^)]*\)(?:\s*\([^)]*\))?", old_frag)
    nm = re.search(r"func\([^)]*\)(?:\s*\([^)]*\))?", new_frag)
    if not om or not nm:
        continue
    old_sig, new_sig = om.group(0), nm.group(0)
    if old_sig in lines[idx]:
        lines[idx] = lines[idx].replace(old_sig, new_sig, 1)
        p.write_text("\n".join(lines) + "\n")
        print(f"ok {path}:{lno}")
