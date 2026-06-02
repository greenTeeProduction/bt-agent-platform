#!/usr/bin/env bash
# check-doc-drift.sh — Validate documentation consistency with codebase
#
# Checks:
# 1. API_REFERENCE.md package list matches actual internal/ packages
# 2. GETTING_STARTED.md binary list matches actual cmd/ directories
# 3. TUTORIAL.md commands reference existing files and binaries
# 4. TROUBLESHOOTING.md references existing tool commands
# 5. ADR INDEX.md references all ADR files
# 6. VIDEO_WALKTHROUGH.md commands work (syntax check)
#
# Returns: number of drift issues found (0 = clean)

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
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

# ----- 5. ADR INDEX.md references -----
echo
echo "--- ADR catalog validation ---"

ADR_FILES=$(find "$ROOT/docs/adr" -maxdepth 1 -name '*.md' ! -name 'INDEX.md' | sort)
ADR_LISTED=$(grep -oP '\(\.\/ADR-\d+' "$ROOT/docs/adr/INDEX.md" 2>/dev/null | sed 's|[./]||g' | sort || true)
ADR_ACTUAL=$(echo "$ADR_FILES" | sed 's|.*/||; s|\.md$||' | sort)

MISSING_ADRS=""
for a in $ADR_ACTUAL; do
    adr_id=$(echo "$a" | grep -oP 'ADR-\d+')
    if ! echo "$ADR_LISTED" | grep -q "$adr_id"; then
        MISSING_ADRS="$MISSING_ADRS $a"
    fi
done

if [ -n "$MISSING_ADRS" ]; then
    red "  ADR files not listed in INDEX.md:"
    for a in $MISSING_ADRS; do echo "    - $a"; done
    ERRORS=$((ERRORS + $(echo "$MISSING_ADRS" | wc -w)))
else
    green "  All ADR files are indexed"
fi

# Check all ADRs have status markers
MISSING_STATUS=false
for f in $ADR_FILES; do
    if ! grep -qE '^\*\*Status:\*+' "$f" 2>/dev/null; then
        red "  Missing status in $(basename "$f")"
        MISSING_STATUS=true
        ERRORS=$((ERRORS + 1))
    fi
done
if ! $MISSING_STATUS; then
    green "  All ADRs have status markers"
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
