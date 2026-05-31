#!/usr/bin/env bash
# Generate concise release notes from conventional commits.
#
# Usage:
#   ./scripts/release-notes.sh                     # since last tag
#   ./scripts/release-notes.sh --since v0.1.0       # since a specific tag
#   ./scripts/release-notes.sh --all                # entire history
#   ./scripts/release-notes.sh --next v0.2.0        # with version header
#   ./scripts/release-notes.sh --format md          # markdown (default)
#   ./scripts/release-notes.sh --format json        # JSON for API consumers
#
# Output: Concise, user-facing release notes grouped by change type.
# Different from CHANGELOG.md: focused on features, fixes, breaking changes.

set -euo pipefail

SINCE=""
NEXT_VERSION=""
ALL=false
FORMAT="md"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --since) SINCE="$2"; shift 2 ;;
    --next) NEXT_VERSION="$2"; shift 2 ;;
    --all) ALL=true; shift ;;
    --format) FORMAT="$2"; shift 2 ;;
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

# Fetch commits
DELIM=$'\t'
COMMIT_FMT="%H%x09%s%x09%an%x09%ad"
if [ -n "$RANGE" ]; then
  COMMITS=$(git log "$RANGE" --pretty=format:"$COMMIT_FMT" --date=short 2>/dev/null || true)
else
  COMMITS=$(git log --pretty=format:"$COMMIT_FMT" --date=short 2>/dev/null || true)
fi

if [ -z "$COMMITS" ]; then
  if [ "$FORMAT" = "json" ]; then
    echo '{"version":"","commits":0,"contributors":[],"categories":{}}'
  else
    echo "_No changes in this release._"
  fi
  exit 0
fi

# Categorize commits
declare -A BUCKETS
declare -A LABELS
LABELS=(
  ["feat"]="🚀 Features"
  ["fix"]="🐛 Bug Fixes"
  ["perf"]="⚡ Performance"
  ["refactor"]="♻️ Refactoring"
  ["test"]="✅ Testing"
  ["docs"]="📝 Documentation"
  ["ci"]="🔧 CI/CD"
  ["build"]="📦 Build"
  ["chore"]="🧹 Chores"
)

# BREAKING changes bucket
BREAKING_BUCKET=""
# Contributors
declare -A CONTRIBUTORS

for cat in "${!LABELS[@]}"; do
  BUCKETS[$cat]=""
done
UNCATEGORIZED=""

# Conventional commit regex: type(scope)!: description
CONV_RE='^([a-z]+)(\([^)]+\))?(!)?: (.*)'

while IFS=$'\t' read -r hash subject author date; do
  CONTRIBUTORS["$author"]=1

  if [[ "$subject" =~ $CONV_RE ]]; then
    ctype="${BASH_REMATCH[1]}"
    scope="${BASH_REMATCH[2]}"
    breaking="${BASH_REMATCH[3]}"
    desc="${BASH_REMATCH[4]}"

    # Scope formatting
    if [ -n "$scope" ]; then
      line="  - **${scope}:** ${desc} (${hash:0:7})"
    else
      line="  - ${desc} (${hash:0:7})"
    fi

    # BREAKING changes
    if [ "$breaking" = "!" ] || [[ "$desc" == *"BREAKING CHANGE"* ]]; then
      BREAKING_BUCKET="${BREAKING_BUCKET}${line}"$'\n'
    fi

    if [ -n "${LABELS[$ctype]:-}" ]; then
      BUCKETS[$ctype]="${BUCKETS[$ctype]}${line}"$'\n'
    else
      UNCATEGORIZED="${UNCATEGORIZED}  - ${subject} (${hash:0:7})"$'\n'
    fi
  else
    UNCATEGORIZED="${UNCATEGORIZED}  - ${subject} (${hash:0:7})"$'\n'
  fi
done <<< "$COMMITS"

# Counts
TOTAL_COMMITS=$(echo "$COMMITS" | wc -l)
NUM_CONTRIBUTORS=${#CONTRIBUTORS[@]}

# Date range
LAST_DATE=$(echo "$COMMITS" | tail -1 | cut -f4)
FIRST_DATE=$(echo "$COMMITS" | head -1 | cut -f4)
if [ "$LAST_DATE" = "$FIRST_DATE" ]; then
  DATE_STR="$LAST_DATE"
else
  DATE_STR="${LAST_DATE} → ${FIRST_DATE}"
fi

# Contributor list sorted
CONTRIB_LIST=$(printf '%s\n' "${!CONTRIBUTORS[@]}" | sort)

# --- JSON OUTPUT ---
if [ "$FORMAT" = "json" ]; then
  echo -n '{'
  echo -n "\"version\":\"${NEXT_VERSION:-unreleased}\","
  echo -n "\"commits\":${TOTAL_COMMITS},"
  echo -n "\"contributors\":["
  first=true
  while IFS= read -r c; do
    if $first; then first=false; else echo -n ","; fi
    echo -n "\"$c\""
  done <<< "$CONTRIB_LIST"
  echo -n "],"
  echo -n "\"date_range\":\"${DATE_STR}\","
  echo -n "\"categories\":{"
  cat_first=true
  if [ -n "$BREAKING_BUCKET" ]; then
    echo -n "\"breaking\":"
    echo -n "["
    echo "$BREAKING_BUCKET" | grep -v '^$' | while IFS= read -r line; do
      echo "\"${line:4}\""
    done | paste -sd, -
    echo -n "]"
    cat_first=false
  fi
  ORDER=("feat" "fix" "perf" "refactor" "test" "docs" "ci" "build" "chore")
  for ctype in "${ORDER[@]}"; do
    content="${BUCKETS[$ctype]}"
    if [ -n "$content" ]; then
      if $cat_first; then cat_first=false; else echo -n ","; fi
      echo -n "\"$ctype\":["
      echo "$content" | grep -v '^$' | while IFS= read -r line; do
        echo "\"${line:4}\""
      done | paste -sd, -
      echo -n "]"
    fi
  done
  echo -n "}"
  echo "}"
  exit 0
fi

# --- MARKDOWN OUTPUT ---
echo "## ${NEXT_VERSION:-Unreleased}"
echo ""
echo "**${DATE_STR}** | ${TOTAL_COMMITS} commits by ${NUM_CONTRIBUTORS} contributor$([ "$NUM_CONTRIBUTORS" -ne 1 ] && echo "s")"
echo ""

# Breaking changes first
if [ -n "$BREAKING_BUCKET" ]; then
  echo "### ⚠️ BREAKING CHANGES"
  echo ""
  echo -n "$BREAKING_BUCKET"
  echo ""
fi

HAS_CONTENT=false

# Categorized sections
ORDER=("feat" "fix" "perf" "refactor" "test" "docs" "ci" "build" "chore")
for ctype in "${ORDER[@]}"; do
  content="${BUCKETS[$ctype]}"
  if [ -n "$content" ]; then
    echo "### ${LABELS[$ctype]}"
    echo ""
    echo -n "$content"
    echo ""
    HAS_CONTENT=true
  fi
done

if [ -n "$UNCATEGORIZED" ]; then
  echo "### 📋 Other"
  echo ""
  echo -n "$UNCATEGORIZED"
  echo ""
  HAS_CONTENT=true
fi

if ! $HAS_CONTENT; then
  echo "_No changes in this release._"
  echo ""
fi

# Contributor thanks
echo "---"
echo "**Contributors:**"
for c in $CONTRIB_LIST; do
  echo "  - @${c// /}"
done
echo ""
echo "_Generated from conventional commits by scripts/release-notes.sh_"
