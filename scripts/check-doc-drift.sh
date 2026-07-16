#!/usr/bin/env bash
# check-doc-drift.sh — Validate documentation consistency with codebase
#
# Checks:
# 1. API_REFERENCE.md package list matches actual internal/ packages
# 2. GETTING_STARTED.md binary list matches actual cmd/ directories
# 3. TUTORIAL.md commands reference existing files and binaries
# 4. TROUBLESHOOTING.md references existing tool commands
# 5. arc42 section files: presence, required headings, footers, ADR log, README paths
# 6. VIDEO_WALKTHROUGH.md commands work (syntax check)
#
# Returns: number of drift issues found (0 = clean)

set -euo pipefail
# Resolve ROOT from the *invoking* working tree, not the script's own location:
# the shared pre-commit hook calls the MAIN repo's copy of this script, so a
# dirname-based ROOT made cycle worktrees validate the MAIN repo's materialized
# docs — drift they could never self-fix (2026-07-09 fleet commit-gate wedge).
# Outside a git work tree (e.g. invoked from the bare repo dir) fall back to
# the script-relative root.
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$ROOT" ]; then
    ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fi
ERRORS=0
WARNINGS=0

red()    { printf '\033[31m%s\033[0m\n' "$1"; }
green()  { printf '\033[32m%s\033[0m\n' "$1"; }
yellow() { printf '\033[33m%s\033[0m\n' "$1"; }

check() {
    local file="$1" label="$2" result="$3"
    if [ "$result" -eq 0 ]; then
        green "  ✓ $label"
    else
        red "  ✗ $label"
    fi
}

echo "=== Doc Drift Validation ==="
echo "Root: $ROOT"
echo

# ----- 1. API_REFERENCE.md package list -----
echo "--- API_REFERENCE.md package listing ---"

# Extract documented packages from API_REFERENCE.md (lines matching `[`package`](#package-xxx)`)
DOC_PKGS=$(grep -oP '\[\`([a-z]+)\`\]\(#package-\1\)' "$ROOT/docs/API_REFERENCE.md" | sed 's/\[`\(.*\)`\](.*)/\1/' | sort || true)
# Extract actual internal packages (top-level dirs)
ACTUAL_PKGS=$(find "$ROOT/internal" -maxdepth 1 -type d ! -name 'internal' | sed 's|.*/||' | sort)

MISSING_FROM_DOCS=$(comm -13 <(echo "$DOC_PKGS") <(echo "$ACTUAL_PKGS"))
# Internal utility packages that are implementation details, not public API
SKIP_INTERNALS="a2a benchreg cicd dashboard eval persistence tools util"
FILTERED_MISSING=""
for pkg in $MISSING_FROM_DOCS; do
    skip=false
    for s in $SKIP_INTERNALS; do
        if [ "$pkg" = "$s" ]; then skip=true; break; fi
    done
    if ! $skip; then
        FILTERED_MISSING="$FILTERED_MISSING $pkg"
    fi
done

if [ -n "$FILTERED_MISSING" ]; then
    red "  Packages in code but NOT in docs:"
    echo "$FILTERED_MISSING" | tr ' ' '\n' | sed 's/^/    - /'
    ERRORS=$((ERRORS + $(echo "$FILTERED_MISSING" | wc -w)))
else
    green "  All code packages are documented"
fi

check "API_REFERENCE.md" "package listing consistent" 0

# ----- 2. GETTING_STARTED.md binary list -----
echo
echo "--- GETTING_STARTED.md binary listing ---"

# Extract binary references (lines matching bt-xxx/ or bin/bt-xxx)
DOC_BINS=$(grep -oP '(bin/)?bt-[-a-z]+' "$ROOT/docs/GETTING_STARTED.md" | sed 's|bin/||; s|/$||' | sort -u || true)
# Extract actual command dirs
ACTUAL_BINS=$(find "$ROOT/cmd" -maxdepth 1 -type d ! -name 'cmd' | sed 's|.*/||' | sort)

MISSING_BINS=""
CORE_BINS="bt-dashboard bt-agent bt-evaluator bt-langagent bt-gardener"
for b in $CORE_BINS; do
    if ! echo "$DOC_BINS" | grep -q "$b"; then
        MISSING_BINS="$MISSING_BINS $b"
    fi
done

if [ -n "$MISSING_BINS" ]; then
    red "  Core binaries NOT mentioned in GETTING_STARTED.md:"
    for b in $MISSING_BINS; do echo "    - $b"; done
    ERRORS=$((ERRORS + $(echo "$MISSING_BINS" | wc -w)))
else
    green "  All core binaries mentioned in Getting Started"
fi

# ----- 3. TUTORIAL.md command validation -----
echo
echo "--- TUTORIAL.md command validation ---"

# Extract `go test`, `go build`, `./bin/bt-*` commands from TUTORIAL.md
TUT_CMDS=$(grep -oP '(go (test|build|run) |\./bin/bt-[-a-z]+|hermes mcp [a-z]+)' "$ROOT/docs/TUTORIAL.md" 2>/dev/null || true)

# Check that referenced binaries are buildable
TUT_BINS=$(echo "$TUT_CMDS" | grep -oP 'bin/bt-[-a-z]+' | sort -u || true)
MISSING_TUT_BINS=""
for b in $TUT_BINS; do
    cmd_name=$(echo "$b" | sed 's|bin/||')
    if [ ! -d "$ROOT/cmd/$cmd_name" ]; then
        MISSING_TUT_BINS="$MISSING_TUT_BINS $cmd_name"
    fi
done

if [ -n "$MISSING_TUT_BINS" ]; then
    red "  Tutorial references non-existent commands:"
    for b in $MISSING_TUT_BINS; do echo "    - $b (no cmd/ dir)"; done
    ERRORS=$((ERRORS + $(echo "$MISSING_TUT_BINS" | wc -w)))
else
    green "  All tutorial command references are valid"
fi

# Check go test/build commands for correctness
BAD_GO_CMDS=$(echo "$TUT_CMDS" | grep 'go test\|go build' | while read -r cmd; do
    # Extract just the go arguments
    args=$(echo "$cmd" | sed 's/^go //')
    case "$args" in
        test*|-short*) ;;
        build*|-o*) ;;
        *) echo "$args" ;;
    esac
done || true)
if [ -n "$BAD_GO_CMDS" ]; then
    yellow "  Unusual Go commands in tutorial (review manually):"
    echo "$BAD_GO_CMDS" | sed 's/^/    - /'
fi

# ----- 4. TROUBLESHOOTING.md command validation -----
echo
echo "--- TROUBLESHOOTING.md command validation ---"

TR_CMDS=$(grep -oP 'bt-[-a-z]+|hermes [a-z]+|pkill|systemctl|journalctl|grep|curl|go (test|build|run|vet|mod)' "$ROOT/docs/TROUBLESHOOTING.md" 2>/dev/null || true)
# Directories or paths that look like commands but aren't
KNOWN_PATH_REFS="bt-evolve bt-reflections bt-gardener"
# Quick check: commands that should exist as directories or are standard
UNKNOWN_CMDS=""
for c in $(echo "$TR_CMDS" | sort -u); do
    case "$c" in
        bt-gardener|bt-dashboard|bt-agent|bt-evaluator|bt-langagent) ;; # core binaries
        bt-*) 
            if echo "$KNOWN_PATH_REFS" | grep -qw "$c"; then
                : # known non-command path reference
            elif [ ! -d "$ROOT/cmd/$c" ]; then
                UNKNOWN_CMDS="$UNKNOWN_CMDS $c"
            fi
            ;;
        hermes|pkill|systemctl|journalctl|grep|curl|go) ;; # standard tools
        *) ;; # skip words that happen to match
    esac
done

if [ -n "$UNKNOWN_CMDS" ]; then
    red "  Troubleshooting references non-existent commands:"
    for c in $UNKNOWN_CMDS; do echo "    - $c"; done
    ERRORS=$((ERRORS + $(echo "$UNKNOWN_CMDS" | wc -w)))
else
    green "  All troubleshooting command references are valid"
fi

# ----- 5. arc42 section files validation -----
echo
echo "--- arc42 section validation ---"

ARC42_ERRORS_BEFORE=$ERRORS
ARC42_DIR="$ROOT/docs/arc42"
ARC42_SECTIONS="01-introduction-goals.md 02-constraints.md 03-context-scope.md 04-solution-strategy.md 05-building-blocks.md 06-runtime-view.md 07-deployment.md 08-crosscutting-concepts.md 09-decisions.md 10-quality.md 11-risks-debt.md 12-glossary.md"

# 5a. Presence: 12 sections + GUIDELINES.md; retired artifacts absent
for f in $ARC42_SECTIONS; do
    if [ ! -f "$ARC42_DIR/$f" ]; then
        red "  arc42 drift: missing section file $f"
        ERRORS=$((ERRORS + 1))
    fi
done
if [ -f "$ARC42_DIR/go-bt-evolve-arc42.md" ]; then
    red "  arc42 drift: retired monolith go-bt-evolve-arc42.md present (sections are the source of truth)"
    ERRORS=$((ERRORS + 1))
fi
if [ -d "$ROOT/docs/adr" ]; then
    red "  arc42 drift: retired docs/adr/ present (the ADR log lives in docs/arc42/09-decisions.md)"
    ERRORS=$((ERRORS + 1))
fi
if [ -f "$ARC42_DIR/GUIDELINES.md" ]; then
    GUIDE_COUNT=$(grep -c '^## Section [0-9]' "$ARC42_DIR/GUIDELINES.md" || true)
    if [ "${GUIDE_COUNT:-0}" -ne 12 ]; then
        red "  arc42 drift: GUIDELINES.md has ${GUIDE_COUNT:-0} '## Section N' blocks (want 12)"
        ERRORS=$((ERRORS + 1))
    fi
else
    red "  arc42 drift: docs/arc42/GUIDELINES.md missing"
    ERRORS=$((ERRORS + 1))
fi

# 5b. Required headings (mirror of internal/engine/arc42_sections.go — keep in lockstep)
check_arc42_heading() {
    local file="$1" heading="$2"
    if [ -f "$ARC42_DIR/$file" ] && ! grep -qF "$heading" "$ARC42_DIR/$file"; then
        red "  arc42 drift: $file missing required heading: $heading"
        ERRORS=$((ERRORS + 1))
    fi
}
check_arc42_heading 01-introduction-goals.md    "# 1. Introduction and Goals"
check_arc42_heading 01-introduction-goals.md    "## 1.1 Requirements Overview"
check_arc42_heading 01-introduction-goals.md    "## 1.2 Quality Goals"
check_arc42_heading 01-introduction-goals.md    "## 1.3 Stakeholders"
check_arc42_heading 02-constraints.md           "# 2. Architecture Constraints"
check_arc42_heading 02-constraints.md           "## Technical Constraints"
check_arc42_heading 02-constraints.md           "## Organizational Constraints"
check_arc42_heading 02-constraints.md           "## Conventions"
check_arc42_heading 03-context-scope.md         "# 3. Context and Scope"
check_arc42_heading 03-context-scope.md         "## 3.1 Business Context"
check_arc42_heading 03-context-scope.md         "## 3.2 Technical Context"
check_arc42_heading 04-solution-strategy.md     "# 4. Solution Strategy"
check_arc42_heading 04-solution-strategy.md     "## Quality Goals → Solution Approaches"
check_arc42_heading 04-solution-strategy.md     "## Key Technology Decisions"
check_arc42_heading 05-building-blocks.md       "# 5. Building Block View"
check_arc42_heading 05-building-blocks.md       "## 5.1 Whitebox Overall System"
check_arc42_heading 06-runtime-view.md          "# 6. Runtime View"
check_arc42_heading 07-deployment.md            "# 7. Deployment View"
check_arc42_heading 07-deployment.md            "## 7.1 Infrastructure Level 1"
check_arc42_heading 08-crosscutting-concepts.md "# 8. Crosscutting Concepts"
check_arc42_heading 08-crosscutting-concepts.md "## 8.1"
check_arc42_heading 09-decisions.md             "# 9. Architecture Decisions"
check_arc42_heading 10-quality.md               "# 10. Quality Requirements"
check_arc42_heading 10-quality.md               "## 10.1 Quality Tree"
check_arc42_heading 10-quality.md               "## 10.2 Quality Scenarios"
check_arc42_heading 11-risks-debt.md            "# 11. Risks and Technical Debt"
check_arc42_heading 12-glossary.md              "# 12. Glossary"
if [ -f "$ARC42_DIR/06-runtime-view.md" ]; then
    RT_SCENARIOS=$(grep -c '^## 6\.' "$ARC42_DIR/06-runtime-view.md" || true)
    if [ "${RT_SCENARIOS:-0}" -lt 3 ]; then
        red "  arc42 drift: 06-runtime-view.md has ${RT_SCENARIOS:-0} scenario subsections (want >= 3)"
        ERRORS=$((ERRORS + 1))
    fi
fi

# 5c. Generated footer must be the LAST line of every section file
for f in $ARC42_SECTIONS; do
    [ -f "$ARC42_DIR/$f" ] || continue
    if ! tail -n 1 "$ARC42_DIR/$f" | grep -q 'Generated by bt-agent arc42 pipeline'; then
        red "  arc42 drift: $f generated footer is not the last line (content appended after the footer?)"
        ERRORS=$((ERRORS + 1))
    fi
done

# 5d. ADR log integrity in 09-decisions.md (entries are '## ADR-NNN: Title')
DEC="$ARC42_DIR/09-decisions.md"
if [ -f "$DEC" ]; then
    ADR_HEADINGS=$(grep -cE '^## ADR-[0-9]+' "$DEC" || true)
    ADR_STATUS=$(grep -cE '^\*\*Status:\*\*' "$DEC" || true)
    if [ "${ADR_STATUS:-0}" -lt "${ADR_HEADINGS:-0}" ]; then
        red "  arc42 drift: 09-decisions.md has ${ADR_HEADINGS:-0} ADR headings but only ${ADR_STATUS:-0} '**Status:**' lines"
        ERRORS=$((ERRORS + 1))
    fi
    if ! grep -qE '^\| *ADR-[0-9]+ *\|' "$DEC"; then
        red "  arc42 drift: 09-decisions.md is missing the ADR overview index table"
        ERRORS=$((ERRORS + 1))
    else
        MAX_HEADING=$(grep -oE '^## ADR-[0-9]+' "$DEC" | grep -oE '[0-9]+$' | sort -n | tail -1)
        MAX_INDEXED=$(grep -oE '^\| *ADR-[0-9]+' "$DEC" | grep -oE '[0-9]+$' | sort -n | tail -1)
        if [ "${MAX_HEADING:-0}" != "${MAX_INDEXED:-0}" ]; then
            red "  arc42 drift: 09-decisions.md index table max ADR-${MAX_INDEXED:-none} != log max ADR-${MAX_HEADING:-none}"
            ERRORS=$((ERRORS + 1))
        fi
    fi
fi

# 5e. Countable arc42 rules
if [ -f "$ARC42_DIR/01-introduction-goals.md" ]; then
    GOALS=$(grep -cE '^\| *Q[0-9]+ *\| *\*\*' "$ARC42_DIR/01-introduction-goals.md" || true)
    if [ "${GOALS:-0}" -lt 3 ] || [ "${GOALS:-0}" -gt 5 ]; then
        red "  arc42 drift: 01-introduction-goals.md quality-goal table has ${GOALS:-0} rows (arc42 wants 3-5)"
        ERRORS=$((ERRORS + 1))
    fi
fi
if [ -f "$ARC42_DIR/11-risks-debt.md" ] && ! grep -qi 'priority' "$ARC42_DIR/11-risks-debt.md"; then
    red "  arc42 drift: 11-risks-debt.md has no priority column/marker"
    ERRORS=$((ERRORS + 1))
fi

# 5f. README referenced doc paths must exist
README_PATHS=$(grep -oE 'docs/[A-Za-z0-9_/.-]+\.md' "$ROOT/README.md" | sort -u || true)
for p in $README_PATHS; do
    if [ ! -f "$ROOT/$p" ]; then
        red "  README drift: referenced path $p does not exist"
        ERRORS=$((ERRORS + 1))
    fi
done
if [ "$ERRORS" -eq "$ARC42_ERRORS_BEFORE" ]; then
    green "  arc42 sections, ADR log, and README paths are consistent"
fi

# ----- 6. VIDEO_WALKTHROUGH.md command syntax check -----
echo
echo "--- VIDEO_WALKTHROUGH.md command syntax check ---"

VW_FILE="$ROOT/docs/VIDEO_WALKTHROUGH.md"
if [ -f "$VW_FILE" ]; then
    # Extract code-block commands
    VW_CMDS=$(grep -oP '(go test|go build|\./bin/bt-[-a-z]+|curl|hermes|pkill)' "$VW_FILE" 2>/dev/null | sort -u || true)
    VW_BINS=$(echo "$VW_CMDS" | grep -oP 'bt-[-a-z]+' | sort -u || true)
    MISSING_VW_BINS=""
    for b in $VW_BINS; do
        if [ ! -d "$ROOT/cmd/$b" ]; then
            MISSING_VW_BINS="$MISSING_VW_BINS $b"
        fi
    done
    if [ -n "$MISSING_VW_BINS" ]; then
        red "  Video walkthrough references non-existent commands:"
        for b in $MISSING_VW_BINS; do echo "    - $b"; done
        ERRORS=$((ERRORS + $(echo "$MISSING_VW_BINS" | wc -w)))
    else
        green "  All video walkthrough command references are valid"
    fi
else
    yellow "  VIDEO_WALKTHROUGH.md not found (skip)"
fi

# ----- 7. Walkthrough evidence artifact freshness -----
echo
echo "--- Walkthrough evidence artifact ---"

WE_FILE="$ROOT/docs/walkthrough-evidence.md"
if [ -f "$WE_FILE" ]; then
    WE_AGE=$(( $(date +%s) - $(stat -c %Y "$WE_FILE") ))
    WE_AGE_HOURS=$(( WE_AGE / 3600 ))
    if [ "$WE_AGE_HOURS" -le 24 ]; then
        green "  walkthrough-evidence.md is fresh (${WE_AGE_HOURS}h old)"
    else
        yellow "  walkthrough-evidence.md is ${WE_AGE_HOURS}h old — consider running 'make walkthrough' to refresh"
        WARNINGS=$((WARNINGS + 1))
    fi
else
    yellow "  walkthrough-evidence.md not found — run 'make walkthrough' to produce it"
    WARNINGS=$((WARNINGS + 1))
fi

# ----- Summary -----
echo
echo "=== Results ==="
if [ "$ERRORS" -gt 0 ]; then
    red "  $ERRORS drift error(s) found"
fi
if [ "$WARNINGS" -gt 0 ]; then
    yellow "  $WARNINGS warning(s) found"
fi
if [ "$ERRORS" -eq 0 ] && [ "$WARNINGS" -eq 0 ]; then
    green "  ✓ Documentation is fully in sync with codebase"
fi

echo
echo "Exit code: $ERRORS"
exit "$ERRORS"
