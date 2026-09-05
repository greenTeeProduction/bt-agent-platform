# GOAP Fusion: Claude Code Review Fallback on NotebookLM Quota Exhaustion

**Date:** 2026-07-02
**Status:** Approved (design defaults accepted — user AFK during review; revisit trigger scope if desired)

## Problem

The scheduled GOAP fusion runner (`GoapFusionLoopTree`, cron :00/:30) depends on
NotebookLM for its research phase. The `nlm` daily quota is routinely exhausted by
scheduled queries (`RESOURCE_EXHAUSTED`, error code 8). Today:

- `GrillMeNotebookLM` (phase 1) fails soft (returns 0) and continues.
- `RunGoapFusionNotebookLMResearch` (phase 2) detects the failure via
  `isGoapNotebookLMFailure` and returns -1, killing the whole
  `GoapFusionLoop_Main` sequence.

Result: once the quota is gone, every remaining 30-minute cycle that day burns
retried `nlm` calls (3 attempts each, tripping the circuit breaker) and produces
nothing.

Separately: the daemon auto-approves Claude Code implementations and commits them
straight to master. **Nothing reviews those commits.**

## Goal

When NotebookLM is unavailable (quota or otherwise), the fusion cycle stays
productive: Claude Code performs a code review of the daemon's recent commits and
its findings become the cycle's research input, in the same
`GOAL:/GAP:/FILES:/TESTS:` contract the downstream pipeline already consumes.

## Decisions (defaults chosen during design)

1. **Trigger:** any `isGoapNotebookLMFailure` on the phase-2 research action falls
   back to Claude review — not just quota. The cycle should never die merely
   because NotebookLM is unavailable. Auth failures still surface loudly in the
   synthesis file header and cycle report.
2. **Architecture:** tree-level `Selector` — idiomatic for a BT platform;
   fallback is visible in the tree, dashboard, and traces.
3. **Review scope:** recent daemon commits first (highest value — they are
   unreviewed auto-approved changes to master); graphify-guided codebase review
   when there are no new commits.
4. **Quota memory:** persisted. Once a quota error is seen, skip all `nlm` calls
   (GrillMe and research) until the quota window resets, going straight to the
   Claude review path.

## Design

### 1. Tree change (`internal/domains/goap_fusion_loop.go`)

Phase 2 becomes a Selector:

```go
sel("ResearchRouter",
    act("RunGoapFusionNotebookLMResearch",
        "Query BT Platform Research notebook directly and save GOAP-owned findings to vault"),
    act("RunClaudeCodeReviewResearch",
        "Fallback when NotebookLM is unavailable: Claude Code reviews recent daemon commits (or graphify hotspots) and emits GOAL/GAP/FILES/TESTS findings to the vault"),
),
```

No other tree changes. GrillMe stays phase 1 (already fail-soft).

### 2. Quota memory (`internal/engine/actions_goap_fusion_claude_review.go`)

- New classifier `isGoapNotebookLMQuotaError(out string) bool` — matches
  `resource_exhausted` / `error code 8` / `google rejected the query`
  (case-insensitive). Strictly a subset of `isGoapNotebookLMFailure`.
- Persistence mirrors the grill-state pattern: agent-scope blackboard key
  `goap_fusion_nlm_quota_until` (RFC3339), ChainState fallback.
- `saveNlmQuotaExhausted(bb)` stores the next quota reset:
  next midnight `America/Los_Angeles` (Google daily quotas reset at midnight
  Pacific); if the TZ database is unavailable, fall back to now+12h.
- `nlmQuotaExhausted(bb) bool` reads and compares against `time.Now()`.
- `GrillMeNotebookLM` and `RunGoapFusionNotebookLMResearch` check
  `nlmQuotaExhausted` **before** calling `nlmRun`: GrillMe returns 0
  ("skipped — quota window exhausted until <ts>"), the research action returns
  -1 immediately so the Selector falls through without burning retries.
- Both actions call `saveNlmQuotaExhausted` when a fresh response classifies as
  a quota error.

### 3. New action `RunClaudeCodeReviewResearch`

Registered alongside the other GOAP fusion actions. Flow:

1. **Pick review target.** Read `goap_fusion_last_reviewed_sha` from the
   agent-scope blackboard.
   - If set and `git merge-base --is-ancestor` confirms it, review
     `<sha>..HEAD`.
   - If unset/invalid, review commits from the last 24h
     (`git log --since="24 hours ago"`).
   - Collect `git log --stat` for the range plus `git diff <range>` truncated
     to ~12k chars (existing `truncateGoap`).
   - If the range is empty → **graphify mode**: reuse the extracted
     GRAPH_REPORT sections (summary, god nodes, low-cohesion files) as review
     context instead.
2. **Build prompt.** Same output contract as
   `buildGoapFusionNotebookLMQuery`: demand exactly
   `GOAL:/GAP:/FILES:/TESTS:` (+ `FINDINGS:` free text). Commit mode asks for a
   review of the diff (bugs, regressions, missing tests, convention
   violations); graphify mode asks for the highest-impact structural fix.
3. **Run Claude Code** via a new package var
   `defaultGoapReviewClaudeRunner ClaudeRunner` (test seam), defaulting to
   `execClaudeRunner{AllowedTools: goapReviewAllowedTools}` where

   ```go
   const goapReviewAllowedTools = "Read,Glob,Grep," +
       "Bash(git log:*),Bash(git show:*),Bash(git diff:*),Bash(git status:*)"
   ```

   Env override: `BT_GOAP_REVIEW_ALLOWED_TOOLS`. `execClaudeRunner` gains an
   optional `AllowedTools` field honored when non-empty (existing behavior
   unchanged when empty). Read-only: the review must not edit code — the
   implementation phase does that. Timeout: 15 min (`context.WithTimeout`),
   inside the tree's 1h ceiling.
4. **Detect Claude-side rate limiting** with the existing superpowers
   rate-limit classifier (`actions_superpowers_prod.go`) so a Claude usage
   limit is reported distinctly, not parsed as findings.
5. **Parse** GOAL/GAP with `extractGoapNotebookLMRecommendation`; fall back to
   `firstNonEmptyGoapLine`. No parseable goal → return -1 (both research
   branches failed; the cycle fails exactly as today).
6. **Write synthesis** `goap-fusion-claude-review-<ts>.md` to
   `goapFusionSynthesesDir` with a header recording: source
   (`claude_code_review`), why NotebookLM was skipped (quota-cached / failure
   output), the reviewed commit range, and the raw findings. `ReadVaultResearch`
   picks it up next phase with no changes.
7. **Set ChainState** exactly as the NotebookLM action does
   (`notebooklm_research`, `notebooklm_goal`, `notebooklm_gap`,
   `notebooklm_research_path`) so `AnalyzeImprovementGaps` →
   `PrioritizeGoapGoals` → plan/implement work untouched; additionally
   `research_source=claude_code_review`.
8. **Advance** `goap_fusion_last_reviewed_sha` to current HEAD on success.

### Error handling summary

| Condition | Behavior |
|---|---|
| NotebookLM quota error | Cache until next Pacific midnight; Selector → Claude review |
| Any other NotebookLM failure | No cache; Selector → Claude review |
| Quota cache active | GrillMe soft-skips; research action fails fast; Claude review runs |
| Claude review rate-limited/errors/no goal | Action returns -1 → cycle fails (status quo) |
| Empty commit range | Graphify-guided review instead |

### Testing

Pure helpers tested directly (existing file style,
`goap_fusion_claude_review_test.go`):

- quota classifier: positive (both real RESOURCE_EXHAUSTED shapes) and negative
  (mid-answer "Error:" text, ordinary failures like auth expiry)
- quota-until computation (Pacific midnight, +12h fallback)
- prompt build: commit mode vs graphify mode contract markers
- parse of a realistic Claude review answer into goal/gap

Action-level test with a fake `ClaudeRunner` (swap
`defaultGoapReviewClaudeRunner`): success path writes synthesis + ChainState
keys; failure path returns -1. Domains test asserts `ResearchRouter` Selector
shape and that both child actions are registered.

### Out of scope

- Quarantining the ~298 quota-error garbage syntheses already in the vault
  (separate user decision pending).
- Fixing the daemon's /tmp/worktrees leak.
- Making GrillMe itself fall back to Claude (it is already fail-soft).
