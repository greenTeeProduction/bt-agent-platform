#!/usr/bin/env python3
"""Check arc42 structure that can be derived from repository evidence.

Behavioral claims still require source/test review. This deliberately uses
only the standard library so the existing shell/CI gate can invoke it.
"""

import argparse
from collections import Counter
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit


def prose(text, keep_inline=False):
    """Remove fenced/inline code so example links and IDs are not obligations."""
    lines = []
    fence = None
    for line in text.splitlines():
        marker = re.match(r"^\s*(`{3,}|~{3,})", line)
        if marker:
            token = marker[1]
            if fence is None:
                fence = token
            elif token[0] == fence[0] and len(token) >= len(fence):
                fence = None
            continue
        if fence is None:
            lines.append(line)
    return re.sub(r"(`+)(.*?)\1", r"\2" if keep_inline else "", "\n".join(lines))


def anchors(text):
    """GitHub-style heading slugs plus explicit HTML IDs used by ADR records."""
    text = prose(text, keep_inline=True)
    found = set(re.findall(r'<a\s+id=["\x27]([^"\x27]+)["\x27]', text))
    counts = Counter()
    for heading in re.findall(r"^#{1,6}\s+(.+?)\s*#*\s*$", text, re.M):
        heading = re.sub(r"\[([^]]+)\]\([^)]*\)", r"\1", heading)
        slug = re.sub(r"[^\w\- ]", "", heading.lower()).replace(" ", "-")
        suffix = counts[slug]
        counts[slug] += 1
        found.add(slug + (f"-{suffix}" if suffix else ""))
    return found


def rows(text, prefix):
    pattern = rf"^\|\s*({prefix}[0-9]+)\s*\|([^\n]*)"
    return [(m[1], m[2]) for m in re.finditer(pattern, text, re.M)]


def check(root):
    root = Path(root).resolve()
    arc = root / "docs/arc42"
    errors = []
    if not arc.is_dir():
        return ["missing docs/arc42 directory"]
    docs = {p.name: p.read_text() for p in arc.glob("*.md")}
    required = ("01-introduction-goals.md", "04-solution-strategy.md",
                "05-building-blocks.md", "09-decisions.md",
                "10-quality.md", "11-risks-debt.md")
    for name in required:
        if name not in docs:
            errors.append(f"missing docs/arc42/{name}")
    if errors:
        return errors

    # Local links/anchors, including source evidence and the operator runbook.
    sources = [(arc / name, body) for name, body in docs.items()]
    for related in ("README.md", "docs/coding-delegation.md"):
        path = root / related
        if path.exists():
            sources.append((path, path.read_text()))
    anchor_cache = {}
    for source, body in sources:
        for match in re.finditer(r"\]\(([^)\n]+)\)", prose(body)):
            target = match[1].strip()
            # Optional Markdown title; angle-bracket destinations allow spaces.
            target = target[1:target.index(">")] if target.startswith("<") else target.split()[0]
            parts = urlsplit(target)
            if parts.scheme or parts.netloc:
                continue
            path = (source.parent / unquote(parts.path)).resolve() if parts.path else source
            label = str(source.relative_to(root))
            if not path.exists():
                errors.append(f"{label}: missing link target {target}")
            elif parts.fragment and path.suffix == ".md":
                if path not in anchor_cache:
                    anchor_cache[path] = anchors(path.read_text())
                if unquote(parts.fragment) not in anchor_cache[path]:
                    errors.append(f"{label}: missing anchor {target}")

    # The complete top-level package and binary inventories are source-derived.
    blocks = docs["05-building-blocks.md"]
    packages = set(re.findall(r"^\|\s*`internal/([a-z0-9_-]+)`\s*\|", blocks, re.M))
    entrypoints = re.search(r"^### Entrypoints\s*\n(.*?)(?=^#{1,3} |\Z)", blocks, re.M | re.S)
    binaries = set(re.findall(r"^\|\s*`([^/\x60]+)`\s*\|", entrypoints[1], re.M)) if entrypoints else set()
    for directory, documented in (("internal", packages), ("cmd", binaries)):
        parent = root / directory
        actual = {p.name for p in parent.iterdir() if p.is_dir() and not p.name.startswith(".")} if parent.exists() else set()
        for name in sorted(actual - documented):
            errors.append(f"§5: undocumented {directory}/{name}")
        for name in sorted(documented - actual):
            errors.append(f"§5: nonexistent {directory}/{name}")

    goal_rows = rows(docs["01-introduction-goals.md"], "Q")
    scenario_rows = rows(docs["10-quality.md"], "QS")
    risk_rows = rows(docs["11-risks-debt.md"], "R")
    groups = {"Q": goal_rows, "QS": scenario_rows, "R": risk_rows}
    ids = {}
    for kind, entries in groups.items():
        ids[kind] = {item for item, _ in entries}
        if not entries:
            errors.append(f"missing {kind} table")
        for item, count in Counter(item for item, _ in entries).items():
            if count != 1:
                errors.append(f"duplicate {item}")
    covered = set()
    for scenario, rest in scenario_rows:
        goal_cell = rest.split("|", 1)[0]
        mapped = set(re.findall(r"\bQ\d+\b", goal_cell))
        if not mapped:
            errors.append(f"{scenario}: missing quality goal")
        for goal in sorted(mapped - ids["Q"]):
            errors.append(f"{scenario}: unknown goal {goal}")
        covered.update(mapped)
    strategy = prose(docs["04-solution-strategy.md"])
    strategy_goals = set()
    for cell in re.findall(r"^\|\s*([^|\n]+)\|", strategy, re.M):
        strategy_goals.update(re.findall(r"\bQ\d+\b", cell))
    for goal in sorted(ids["Q"]):
        if goal not in covered:
            errors.append(f"{goal}: no quality scenario")
        if goal not in strategy_goals:
            errors.append(f"{goal}: missing strategy mapping")

    decisions = docs["09-decisions.md"]
    headings = list(re.finditer(r"^## ADR-(\d+): (.+)$", decisions, re.M))
    indexed = re.findall(r"^\|\s*ADR-(\d+)\s*\|\s*\[.*?\]\(#([^)]*)\)", decisions, re.M)
    ids["ADR-"] = {f"ADR-{m[1]}" for m in headings}
    heading_counts = Counter(m[1] for m in headings)
    index_counts = Counter(num for num, _ in indexed)
    legacy_anchors = ("adr-024-composition", "adr-024-experience-bank")
    expected_links = Counter()
    seen = Counter()
    for pos, match in enumerate(headings):
        num = match[1]
        seen[num] += 1
        if num == "024" and heading_counts[num] == 2:
            anchor = legacy_anchors[seen[num] - 1]
        else:
            anchor = f"adr-{num}"
        expected_links[(num, anchor)] += 1
        if not decisions[:match.start()].rstrip().endswith(f'<a id="{anchor}"></a>'):
            errors.append(f"ADR-{num}: missing adjacent stable anchor {anchor}")
        end = headings[pos + 1].start() if pos + 1 < len(headings) else len(decisions)
        if not re.search(r"^\*\*Status:\*\*", decisions[match.end():end], re.M):
            errors.append(f"ADR-{num}: missing status")
    if not headings:
        errors.append("missing ADR records")
    for num, count in heading_counts.items():
        if count != 1 and not (num == "024" and count == 2):
            errors.append(f"duplicate ADR-{num}")
    for num in sorted(heading_counts.keys() | index_counts.keys()):
        if heading_counts[num] != index_counts[num]:
            errors.append(f"ADR-{num}: index/record count mismatch")
    if Counter(indexed) != expected_links:
        errors.append("ADR index must link each ID to its own stable record anchor")
    if heading_counts:
        for n in range(1, max(map(int, heading_counts)) + 1):
            if f"{n:03d}" not in heading_counts:
                errors.append(f"missing ADR-{n:03d} record (preserve numbering/provenance)")
    explicit = re.findall(r'<a id="([^"]+)"></a>', decisions)
    for anchor, count in Counter(explicit).items():
        if count > 1:
            errors.append(f"duplicate ADR anchor {anchor}")

    # Current views may reference existing stable IDs/ranges. Historical prose
    # is deliberately excluded: it retains the terminology of its own date.
    for name, body in docs.items():
        if not re.match(r"(0[1-8]|1[0-2])-", name):
            continue
        for m in re.finditer(r"\b(QS|Q|R|ADR-)(\d+)(?:[–-](?:(?:QS|Q|R|ADR-))?(\d+))?\b", prose(body)):
            kind, start, stop = m.groups()
            first, last = int(start), int(stop or start)
            if last < first or last - first > 1000:
                errors.append(f"{name}: invalid reference range {m[0]}")
                continue
            for n in range(first, last + 1):
                ref = f"ADR-{n:03d}" if kind == "ADR-" else f"{kind}{n}"
                if ref not in ids[kind]:
                    errors.append(f"{name}: unknown reference {ref}")
    return sorted(set(errors))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    errors = check(args.root)
    for error in errors:
        print(f"arc42: {error}", file=sys.stderr)
    if errors:
        return 1
    print("arc42 references, inventories and traceability pass; behavioral claims require evidence review")
    return 0


if __name__ == "__main__":
    sys.exit(main())
