# GOAP Runner: honest outcome signal, commit auto-fix, and build stamping

Date: 2026-07-13
Branch: `fix/goap-runner-signal-autofix-stamp`

Driven by an in-depth review of `~/.go-bt-evolve/history/goap-fusion-*.jsonl` (998 runs).
Three defects: (1) `success` overstates work — only ~12% of runs land a commit yet
most no-code runs score ~0.8; (2) the deployed daemon binary is **unstamped**, so
deploy-drift detection is permanently inert; (3) a commit rejected by the pre-commit
hook (lint/tests) is abandoned as `applied_uncommitted` with no fix attempt.

## Item 1 — Honest outcome labels + authoritative quality

**Problem.** `internal/engine/tree.go:515-522` overwrites `bb.Outcome` to
success/failure/partial from the root node's return code, so no action-set outcome
reaches history. `domain:goap_fusion*` have no `QualitySpec`, so quality =
`estimateQuality(output)` (text-shape heuristic → ~0.8 for any long report). The
runner takes `max(estimateQuality, bb.QualityScore)` (runner.go:217), so a low score
cannot be asserted downward.

**Design.**
- Add two non-clobbered fields to `Blackboard` (tree.go): `OutcomeRefinement string`
  and `QualityAuthoritative bool` (reuses existing `QualityScore float64`).
- `VerifyGoapFusionEvidence` (actions_goap_fusion_prod_additions.go) classifies the
  terminal output at its two `return 1` sites and sets:
  - Committed impl (`## Superpowers Implementation Complete` + `Apply status: \`committed\``):
    leave outcome `success`; `QualityScore=0.9`, `QualityAuthoritative=true`.
  - Degraded fallback (output contains `Implementation Degraded (Fallback)` /
    `degraded to deterministic`): `OutcomeRefinement="degraded"`, `QualityScore=0.3`.
  - Cycle-only (`## GOAP Fusion Cycle Complete`, no degraded marker):
    `OutcomeRefinement="no_change"`, `QualityScore=0.5`.
- `runner.go` after `result.Outcome = bb.Outcome`:
  - if `bb.OutcomeRefinement != ""` and `result.Outcome == "success"` →
    `result.Outcome = bb.OutcomeRefinement`.
  - if `bb.QualityAuthoritative` → `result.Quality = bb.QualityScore` (bypass MAX).
  - treat `no_change` / `degraded` as **healthy terminal** states: do NOT
    agent-promote (already gated on `== "success"`), and return `(result, nil)` —
    NOT an error — so the scheduler does not retry/DLQ them.

Recorded outcomes become: `success` (committed) · `no_change` (analysis-only) ·
`degraded` (fallback) · `failure` (error/bad evidence) · `partial` (unchanged HITL).

## Item 2 — VCS build stamping (fix inert drift detection)

**Problem.** The main repo is bare; `go build` from it cannot resolve VCS info, so
`vcs.revision` is absent → `BuildIdentityFromBuildInfo` yields `unknown` →
`DriftStatus` returns "never stale" (deploy_drift.go:51). Every deployed daemon is
blind to drift.

**Design.**
- Add `var stampedRevision string` in `internal/dashboard/metrics_utils.go`.
- In `BuildIdentityFromBuildInfo`, when `vcs.revision` is absent and
  `stampedRevision != ""`, use `stampedRevision` (buildinfo still wins when present).
- Stamp `-X github.com/nico/go-bt-evolve/internal/dashboard.stampedRevision=$(git rev-parse HEAD)`
  in every build path that produces a deployed binary:
  - `scripts/check.sh` `run_build`
  - `internal/agent/rebuild.go` `defaultRebuildBuild` (the drift auto-rebuild)
- Operational: rebuild `./bt-agent` from HEAD, confirm `go version -m` / the
  bt_build_info gauge now carries a revision, then stop the running daemon
  (PID 462265) and restart it from the stamped binary.

## Item 3 — Commit auto-fix loop (up to 10 Claude attempts)

**Problem.** `stageAndCommitSuperpowersRunInDir` (superpowers_apply.go:284) abandons
a hook-rejected commit as `applied_uncommitted`.

**Design.** Replace the give-up with a bounded fix loop in the apply worktree:
1. Attempt commit. On success, done.
2. Loop up to `maxAttempts` (default **10**, env `BT_SUPERPOWERS_COMMIT_FIX_ATTEMPTS`):
   a. Classify the hook output: `gofmt` / `mod-tidy` / `lint-fixable` / `doc-drift` /
      `vet` / `test` / `lint-logic`.
   b. Apply deterministic fixes every pass (idempotent): `gofmt -w` changed `.go`,
      `go mod tidy`, `golangci-lint run --fix`, regenerate doc-drift.
   c. If a residual **test / vet / logic-lint** failure remains AND Claude is not in
      rate-limit backoff: invoke `RunClaude(worktree, prompt)` with the hook output as
      context to fix the code. Skip this step (deterministic-only) when rate-limited.
   d. `git add -A`; retry commit. On success, break.
3. If still failing after `maxAttempts`, fall through to the existing
   `applied_uncommitted` evidence path, now annotated with the final classification.

This also removes the most common `applied_uncommitted` cause (formatting/imports/lint),
directly addressing the Jul-12 refund-treadmill recurrence.

## Verification
- Unit tests (TDD) per item: refinement/quality mapping (runner), classifier
  (VerifyGoapFusionEvidence), stamp fallback (BuildIdentityFromBuildInfo), and the
  commit fix loop (fake CommandRunner + fake ClaudeRunner asserting attempt count,
  rate-limit skip, and fall-through).
- `make check-quick` green before commit; push to master; rebuild + restart daemon.
- Post-deploy: confirm new `no_change`/`degraded` labels appear in history and the
  drift gauge carries a revision.

## Out of scope
Runaway-loop / circuit-breaker tuning (review finding 3) and weekly-rate-limit
scheduling backpressure (finding 4) — separate change.
