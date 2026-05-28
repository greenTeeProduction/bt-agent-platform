#!/usr/bin/env bash
# Generate CHANGELOG.md from conventional commits.
#
# Usage:
#   ./scripts/changelog.sh                     # all commits since last tag
#   ./scripts/changelog.sh --since v0.1.0       # since a specific tag
#   ./scripts/changelog.sh --all                # entire history
#   ./scripts/changelog.sh --next v0.2.0        # prepend a version header
#
# Output format: Keep a Changelog (https://keepachangelog.com)

set -euo pipefail

SINCE=""
NEXT_VERSION=""
ALL=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --since) SINCE="$2"; shift 2 ;;
    --next) NEXT_VERSION="$2"; shift 2 ;;
    --all) ALL=true; shift ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

# Determine git range
if [ -n "$SINCE" ]; then
  RANGE="${SINCE}..HEAD"
elif $ALL; then
  RANGE=""
elif LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null); then
  RANGE="${LATEST_TAG}..HEAD"
else
  RANGE=""
fi

# Fetch commits in reverse chronological order (tab-delimited)
DELIM=$'\t'
if [ -n "$RANGE" ]; then
  COMMITS=$(git log "$RANGE" --pretty=format:"%H%x09%s%x09%ad" --date=short 2>/dev/null || true)
else
  COMMITS=$(git log --pretty=format:"%H%x09%s%x09%ad" --date=short 2>/dev/null || true)
fi

if [ -z "$COMMITS" ]; then
  echo "No commits found."
  exit 0
fi

# Categorize commits by conventional commit type
declare -A CATEGORIES
CATEGORIES=(
  ["feat"]="Added"
  ["fix"]="Fixed"
  ["perf"]="Performance"
  ["refactor"]="Changed"
  ["test"]="Testing"
  ["docs"]="Documentation"
  ["style"]="Style"
  ["chore"]="Chores"
  ["ci"]="CI/CD"
  ["build"]="Build"
)

declare -A BUCKETS
declare -A FIRST_COMMIT_DATE
for cat in "${!CATEGORIES[@]}"; do
  BUCKETS[$cat]=""
done
UNCATEGORIZED=""

# Conventional commit regex: type(scope)!: description
CONV_RE='^([a-z]+)(\([^)]+\))?!?: (.*)'

while IFS=$'\t' read -r hash subject date; do
  # Parse conventional commit: type(scope): description
  if [[ "$subject" =~ $CONV_RE ]]; then
    ctype="${BASH_REMATCH[1]}"
    scope="${BASH_REMATCH[2]}"
    desc="${BASH_REMATCH[3]}"
    
    if [ -n "$scope" ]; then
      line="- **${scope}:** ${desc} (${hash:0:7})"
    else
      line="- ${desc} (${hash:0:7})"
    fi
    
    if [ -n "${CATEGORIES[$ctype]:-}" ]; then
      BUCKETS[$ctype]="${BUCKETS[$ctype]}${line}"$'\n'
      FIRST_COMMIT_DATE[$ctype]="$date"
    else
      UNCATEGORIZED="${UNCATEGORIZED}- ${subject} (${hash:0:7})"$'\n'
    fi
  else
    UNCATEGORIZED="${UNCATEGORIZED}- ${subject} (${hash:0:7})"$'\n'
  fi
done <<< "$COMMITS"

# Calculate date range
if [ -n "$COMMITS" ]; then
  LAST_DATE=$(echo "$COMMITS" | tail -1 | cut -f3)
  FIRST_DATE=$(echo "$COMMITS" | head -1 | cut -f3)
  if [ "$LAST_DATE" = "$FIRST_DATE" ]; then
    DATE_STR="$LAST_DATE"
  else
    DATE_STR="${LAST_DATE} to ${FIRST_DATE}"
  fi
else
  DATE_STR="unknown"
fi

# Output
if [ -n "$NEXT_VERSION" ]; then
  echo "## [${NEXT_VERSION}] — $(date +%Y-%m-%d)"
else
  echo "## [Unreleased]"
fi
echo ""

HAS_CONTENT=false

# Print in Keep a Changelog order
ORDER=("feat" "fix" "perf" "refactor" "test" "docs" "style" "ci" "build" "chore")
for ctype in "${ORDER[@]}"; do
  content="${BUCKETS[$ctype]}"
  if [ -n "$content" ]; then
    echo "### ${CATEGORIES[$ctype]}"
    echo ""
    echo -n "$content"
    echo ""
    HAS_CONTENT=true
  fi
done

if [ -n "$UNCATEGORIZED" ]; then
  echo "### Miscellaneous"
  echo ""
  echo -n "$UNCATEGORIZED"
  echo ""
  HAS_CONTENT=true
fi

if ! $HAS_CONTENT; then
  echo "_No changes in this release._"
  echo ""
fi
