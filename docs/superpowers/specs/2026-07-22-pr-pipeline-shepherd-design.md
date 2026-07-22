# PR Pipeline Shepherd — Design

**Date:** 2026-07-22
**Status:** approved (placement + auto-create confirmed by Nico)
**Context:** `origin/master` is branch-protected since PR #13; direct pushes are
rejected. Fleet landings therefore accrue on local master as
`committed_unpushed` (refunded infra since `caefb02`). Branch
`fix/goap-staging-treadmill-salvage` holds 3 unmerged commits with no PR. No
`gh` CLI on the host; GitHub is reachable via HTTPS API.

## Goal

Extend the goap-fusion trees so the fleet ships its own work end-to-end:

1. **Auto-create/update** a PR whenever local master is ahead of origin.
2. **Fix the PR pipeline** when CI fails (bounded Claude fix loop).
3. **Merge the PR** when the pipeline is green, then sync local master.

## Non-goals

- No in-cycle waiting on CI (a 90-min run budget killed cycles before —
  `dfffeb1`). The shepherd is strictly non-blocking; CI progresses between
  tree touches.
- No autonomous resolution of a diverged master (local and origin both ahead):
  that is an operator decision, surfaced via outcome.
- No changes to GitHub branch-protection settings.

## Placement (per Nico: extend the goap-fusion tree)

A single always-healthy action node, `ShepherdFleetPR`, inserted right after
`SetupFusionTools` in **both** `GoapFusionTree` and `GoapFusionLoopTree`
(internal/domains/goap_fusion.go, goap_fusion_loop.go). Per the
`SelfReviewTree` lesson: one composite registered action, returning SUCCESS on
every path, with `bb.Outcome`/`bb.OutcomeRefinement` carrying the story — an
early child failure would otherwise bubble into the ClaudeErrorHandler wrapper
and spuriously trigger recovery for routine steady states.

Additionally the **landing tail** (`superpowers_apply.go`, both
`git push origin master` sites): on a protected-branch rejection, fall back to
pushing the fleet branch + ensuring the PR (`ApplyStatus =
"committed_pr_opened"`, success) instead of erroring `committed_unpushed`.
`committed_unpushed` remains for total failure and stays a refunded infra
marker.

## The shepherd state machine (one non-blocking pass per cycle)

```
token absent ──────────────► skip (pr_shepherd_no_token, no_change)
fetch origin master
origin ahead, ff possible ─► ff local master (non-forced `git fetch .`),
                             delete merged fleet branch, clear state
                             (pr_shepherd_synced_master)
diverged (both ahead) ─────► skip + operator-visible outcome (pr_shepherd_diverged)
local == origin, no PR ────► skip (pr_shepherd_idle, no_change)
local ahead:
  fleet branch != master ──► push --force-with-lease master:fleet/landing
  no open PR ──────────────► POST /pulls (pr_shepherd_pr_opened)
  check-runs for head SHA:
    any queued/in_progress ► skip (pr_shepherd_ci_pending)
    any failed ────────────► fix loop (below)
    all green ─────────────► PUT /pulls/N/merge (merge_method=merge)
                             → ff local master from origin, delete branch
                             (pr_shepherd_merged); API refusal →
                             pr_shepherd_merge_blocked skip
```

**Invariant: `fleet/landing` is always pushed from local master.** Fix commits
land on local master first (worktree → non-forced `git fetch . <branch>:master`,
the `reapplyRunBranchOntoMaster` primitive), then the branch is re-pushed with
`--force-with-lease`. The branch is a mirror, never a fork — so force-with-lease
can never destroy work.

## Fix loop (CI red)

- Durable per-head-SHA attempt count in `~/.go-bt-evolve/pr_shepherd/state.json`;
  max `BT_PR_SHEPHERD_MAX_FIX_ATTEMPTS` (default 3). Exhausted →
  `pr_shepherd_fix_exhausted` skip (monitoring surfaces it).
- Honors `claudeBackoffActive`; a rate-limited fix stamps the shared backoff
  (`isClaudeRateLimit` / `saveClaudeBackoffState`), same as every Claude path.
- Evidence: failing check-run names + conclusions + up to ~50 annotations
  (`GET /check-runs/{id}/annotations` — vet/gofmt/tidy/golangci `::error`
  lines carry file:line messages).
- Execution: fresh git worktree of local master (`/tmp/worktrees/pr-fix-*`),
  `execClaudeRunner` with the goap implementation tool allowlist, prompt =
  failing jobs + annotations + "reproduce with `make check-quick`, fix,
  commit". Commit normally (worktree hooks work; CI is the final arbiter),
  land via the non-forced ff primitive, re-push the fleet branch. One attempt
  per tree touch (pr_shepherd_fix_pushed).

## GitHub API client

Minimal `net/http` client in the new file (no new module deps): token from
`BT_GITHUB_TOKEN` → `GITHUB_TOKEN` → `GH_TOKEN` (daemon gets these via the
unit's EnvironmentFile — absence is a visible healthy skip naming the vars);
owner/repo parsed from `git remote get-url origin`; endpoints: list/create
PR, list check-runs for SHA, list annotations, merge PR, delete ref. 15s
timeout per call; any API error → operator-visible healthy skip, never a tree
failure.

## Config

- `BT_PR_SHEPHERD=off` — kill switch (skip, pr_shepherd_disabled).
- `BT_FLEET_PR_BRANCH` — default `fleet/landing`.
- `BT_PR_SHEPHERD_MAX_FIX_ATTEMPTS` — default 3.

## Files

- `internal/engine/actions_pr_shepherd.go` (+`_test.go`) — action, state
  machine, API client, fix loop. Deps-injectable (`prShepherdDeps` +
  `prShepherdDepsOverride`) mirroring `selfReviewDeps`.
- `internal/domains/goap_fusion.go`, `goap_fusion_loop.go` — insert the node.
- `internal/engine/superpowers_apply.go` — landing-tail protected-branch
  fallback (`shipLandingToPR`).
- Domain wiring tests updated (registered-action coverage; no silent no-ops).

## Testing

Unit tests with `httptest` GitHub fake + scripted `CommandRunner` + fake
`ClaudeRunner`: no-token skip; idle; ahead→branch push + PR create; pending
skip; green→merge+ff+branch delete; red→one fix attempt (Claude invoked,
master ff'd, branch re-pushed); attempt cap; diverged skip; landing-tail
fallback on protected-branch rejection; force-with-lease always from master.
Tree tests: `ShepherdFleetPR` present in both trees and registered.

## Rollout

Land on local master (hook-bypass only for the known-environmental
`TestNewGoBuildTool_*` pair, as documented), rebuild + restart bt-agent. The
first shepherd touch adopts the 3 waiting salvage commits: pushes
`fleet/landing`, opens the PR, and from then on drives it to merge. The
ad-hoc `fix/goap-staging-treadmill-salvage` branch is deleted once the
canonical PR exists.
