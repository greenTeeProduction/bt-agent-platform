#!/usr/bin/env python3
"""Apply mechanical fixes from golangci-lint output (errcheck + unused-parameter)."""

from __future__ import annotations

import re
import subprocess
import sys
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

ISSUE_RE = re.compile(
    r"^(?P<file>[^:]+):(?P<line>\d+):(?P<col>\d+): "
    r"(?P<msg>.+?) \((?P<linter>[^)]+)\)$"
)
UNUSED_PARAM_RE = re.compile(
    r"unused-parameter: parameter '(?P<name>[^']+)' seems to be unused"
)
ERRCHECK_RE = re.compile(
    r"Error return value of (?P<expr>.+?) is not checked"
)


def collect_issues() -> list[dict]:
    proc = subprocess.run(
        [
            "golangci-lint",
            "run",
            "--timeout=10m",
            "./...",
        ],
        cwd=ROOT,
        env={**subprocess.os.environ, "PATH": subprocess.os.environ.get("PATH", ""), "GOTOOLCHAIN": "go1.26.3"},
        capture_output=True,
        text=True,
    )
    issues = []
    for line in (proc.stdout + proc.stderr).splitlines():
        m = ISSUE_RE.match(line.strip())
        if m:
            issues.append({**m.groupdict(), "line": int(m["line"]), "col": int(m["col"])})
    return issues


def load_lines(path: Path) -> list[str]:
    return path.read_text(encoding="utf-8").splitlines(keepends=True)


def save_lines(path: Path, lines: list[str]) -> None:
    path.write_text("".join(lines), encoding="utf-8")


def fix_unused_parameter(path: Path, line_no: int, param: str) -> bool:
    lines = load_lines(path)
    idx = line_no - 1
    if idx < 0 or idx >= len(lines):
        return False
    line = lines[idx]
    # Only rename in signature on the reported line.
    pat = re.compile(rf"\b{re.escape(param)}\b")
    if not pat.search(line):
        return False
    new_line = pat.sub("_", line, count=1)
    if new_line == line:
        return False
    lines[idx] = new_line
    save_lines(path, lines)
    return True


def wrap_errcheck_line(line: str, expr: str) -> str | None:
    stripped = line.strip()
    indent = line[: len(line) - len(line.lstrip())]

    # Already handles errors.
    if "if err :=" in stripped or "if err=" in stripped or stripped.startswith("_ = "):
        return None

    # fs.Parse / flag.Parse in main-style one-liners.
    if "Parse(" in expr and stripped.endswith(expr + ")"):
        call = expr
        if call.startswith("(*"):
            # method call — use assignment form
            return f"{indent}if err := {call}); err != nil {{\n{indent}\treturn err\n{indent}}}\n"
        return f"{indent}if err := {call}); err != nil {{\n{indent}\treturn err\n{indent}}}\n"

    # Simple statement: expr at end of line.
    if stripped.endswith(expr) or stripped.endswith(expr + ")"):
        call = stripped
        if call.endswith(";"):
            call = call[:-1]
        return f"{indent}if err := {call.lstrip()}; err != nil {{\n{indent}\treturn err\n{indent}}}\n"

    # json.Encode / Write / Close / Save / MkdirAll as trailing call.
    for suffix in (")", ");"):
        if expr in stripped and stripped.rstrip().endswith(suffix):
            call = stripped.rstrip().rstrip(";")
            return f"{indent}if err := {call}; err != nil {{\n{indent}\treturn err\n{indent}}}\n"

    return f"{indent}_ = {stripped.lstrip()}\n"


def fix_errcheck(path: Path, line_no: int, expr: str) -> bool:
    lines = load_lines(path)
    idx = line_no - 1
    if idx < 0 or idx >= len(lines):
        return False
    replacement = wrap_errcheck_line(lines[idx], expr)
    if not replacement:
        return False
    lines[idx] = replacement
    save_lines(path, lines)
    return True


def main() -> int:
    issues_path = Path(sys.argv[1]) if len(sys.argv) > 1 else None
    if issues_path and issues_path.exists():
        raw = issues_path.read_text(encoding="utf-8").splitlines()
        issues = []
        for line in raw:
            m = ISSUE_RE.match(line.strip())
            if m:
                issues.append({**m.groupdict(), "line": int(m["line"]), "col": int(m["col"])})
    else:
        print("Collecting issues from golangci-lint...", file=sys.stderr)
        issues = collect_issues()

    unused = []
    errchecks = []
    for it in issues:
        if it["linter"] != "revive":
            if it["linter"] == "errcheck":
                em = ERRCHECK_RE.search(it["msg"])
                if em:
                    errchecks.append((it["file"], it["line"], em.group("expr")))
            continue
        um = UNUSED_PARAM_RE.search(it["msg"])
        if um:
            unused.append((it["file"], it["line"], um.group("name")))

    # Process bottom-up so line numbers stay valid within a file.
    unused.sort(key=lambda x: (x[0], -x[1]))
    errchecks.sort(key=lambda x: (x[0], -x[1]))

    u_fixed = 0
    for rel, line_no, param in unused:
        if fix_unused_parameter(ROOT / rel, line_no, param):
            u_fixed += 1

    e_fixed = 0
    for rel, line_no, expr in errchecks:
        if fix_errcheck(ROOT / rel, line_no, expr):
            e_fixed += 1

    print(f"unused-parameter fixed: {u_fixed}/{len(unused)}")
    print(f"errcheck fixed: {e_fixed}/{len(errchecks)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
