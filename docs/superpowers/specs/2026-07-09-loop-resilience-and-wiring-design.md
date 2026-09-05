# Loop Resilience, Telemetry Wiring, and Fleet Streamlining — Design

**Date:** 2026-07-09 · **Author:** Claude (interactive session, user-directed)
**Scope:** Fixes for all findings of the 2026-07-09 evening fleet review, plus
loop-resilience and agent-streamlining improvements the user requested.
**Process note:** brainstorming-skill flow executed autonomously (user
pre-approved scope: "fix all findings and apply recommended actions"); design
decisions below record the approach options considered and the pick.

## 1. Loop resilience — infra failures must not burn milestone attempts

**Problem.** `PrioritizeGoapGoals` charges `Milestone.Attempts` at queue time
(actions_goap_fusion.go). When a cycle later dies for *infrastructure* reasons —
Claude rate-limit carryover, `applied_uncommitted` (commit gate wedged by
external landing), `pending_patch` (apply refused), worktree/sync failure — the
attempt stays burned. Three such cycles block the milestone. This wrongly
blocked 2 programs on 2026-07-09 (doc-drift wedge) and a69ef9d1 on 2026-07-08
(rate limit). Agent *declines* and genuine verification failures must keep
counting (that is the fabricated-milestone abandon path working as designed).

**Options.** (a) refund the charge when the cycle ends in an infra-class
failure; (b) move charging to cycle end; (c) raise the cap. (b) is a wider
refactor of working code; (c) only delays wrong blocks. **Pick: (a).**

**Design.**
- `research.ProgramStore.RefundAttempt(programID, idx)`: `Attempts--` (floor 0);
  if `Status=="blocked"` and the refund brings `Attempts` back under the cap,
  restore `Status="pending"` (clears `BlockedAt`). Only a block created by the
  charge being refunded can be undone this way — milestones blocked in earlier
  cycles stay blocked.
- Engine helper `refundGoapMilestoneAttemptForInfraFailure(bb)`: reads the head
  `goap_fusion_program_milestone` ref stamped by PrioritizeGoapGoals, refunds
  it, saves. No-op without a stamp.
- Classifier `isGoapInfraCycleFailure(bb)`: `bb.Outcome ∈
  {goap_fusion_rate_limited, pending_patch}` OR `bb.Result` carries
  `applied_uncommitted` / `Pending Patch` / `Worktree Failed` markers.
- Wire: the existing deferred failure handler in
  `runSuperpowersRuntimeFromExistingPlanAction` (result == -1) refunds when the
  classifier matches, plus a `program_milestone_charged` stamp written by
  PrioritizeGoapGoals so the refund targets exactly the charged milestone even
  when the charge just blocked it. (Implementation note: a separate
  success-path reconciliation proved unnecessary — every apply/commit/worktree
  failure exits the implementation action with -1 before the success path, so
  the deferred handler covers all infra exits; the tree's analysis fallback
  then reports cycle "success" without ever reaching milestone completion.)

## 2. Duplicate work between the two runners — verified non-issue + staggering

`Scheduler.tick` collects due jobs and runs them **inline, sequentially** in one
goroutine — scheduled cycles can never overlap, so a milestone lease is
unnecessary (YAGNI; verified in internal/agent/scheduler.go). The real
streamlining lever is the `:00` schedule pile-up (loop-runner, bt-fusion,
notebooklm-researcher, auth-guardian all fire at :00 and serialize, pushing runs
late — historically up to +1h). **Change (ops, agent YAML):** stagger to
researcher `5 */2 * * *`, bt-fusion `10 * * * *`, auth-guardian `15 */6 * * *`,
pipeline-monitor `40 */2 * * *` (clears the loop-runner's `:30` slot). Archive
the six agents dormant since ≤Jul 2 (hermes-cron-doctor, hermes-monitor,
notebooklm-planner, notification-router, superpowers-pipeline,
superpowers-prod-runner) to `agents-archived/`.

## 3. Doc-drift check validates the invoking worktree

`scripts/check-doc-drift.sh` resolves `ROOT` from the script's own location, so
cycle worktrees validate the MAIN repo's materialized docs and can never
self-heal (root cause of the 16 h fleet wedge). **Fix:** resolve `ROOT` from the
invoking working tree — `git rev-parse --show-toplevel` — falling back to the
script-relative path outside a repo. The hook and the syncDriftDocs stage then
both act on the same tree the commit is happening in.

## 4. DLQ replay consumer (finishes program c8094002 ms1–3)

- `reliability`: `SetReplayExecutor(func(DeadLetterEntry) error)`;
  `Replay(id)` becomes drop-safe: refuse when abandoned or no executor; run the
  executor; **only on success** remove + persist; on failure `Attempts++`
  (terminal `Abandoned` past `MaxReplayAttempts`), entry retained, `RequeuedAt`
  cleared so the scan loop does not hot-loop it.
- `SelectRequeuedForReplay(entries)`: requeued, not abandoned → replay order.
- bt-agent (owns the tree runner): extract the scheduler runner closure to a
  named function; `dlq.SetReplayExecutor` adapts it (`entry.Agent`,
  `entry.Task`); a background scan every 5 min replays requeued entries.
- MCP tools `bt_dlq_list` and `bt_dlq_replay` on the shared `engine.TaskDLQ`.

## 5. Selector telemetry + specialists: writer always on, consumers opt-in

**Problem.** The 2026-07-09 landings shipped the whole selector-ordering loop
and specialist resurrection with no producer, no writer, and no consumer wiring
(`domains.SelectorStatsPath` assigned nowhere despite a comment claiming
otherwise; `RecordSelectorOutcomes`/`TraceStep.ParentName` caller-less;
`Population.Specialists` never set).

**Design decisions.**
- **Producer:** `observedCommand.Run` (engine observability wrapper — already
  knows child name, parent name, status) appends terminal child ticks to a new
  bounded `bb.ChildTicks` (cap 1024; per-run blackboard, mutex-guarded for
  Parallel). No hook-signature changes.
- **Writer:** at the end of `RunDeps.RunOnce`, the runner (has bb, tree ID, and
  `ResolveTree`) filters ticks to children of *Selector* nodes (by walking the
  resolved tree definition) and merges them into a **per-tree** durable stats
  file `~/.go-bt-evolve/selector-stats/<sanitized-tree-id>.json` via a new
  `knowledge.RecordSelectorChildOutcomes(path, ticks)`. Per-tree files remove
  the cross-tree selector-name collision hazard flagged in review.
- **Consumers stay opt-in (default off):** resolve-time reorder wires
  `domains.SelectorStatsPathFn func(treeID) string` from `agentexec` **only**
  when `BT_SELECTOR_REORDER=1` — success-rate reordering inverts cost-first
  routers (e.g. nlm-before-Claude quota economy), so it must never be an
  ambient default. `bt_evolve_selectors` MCP tool gains real accumulated data
  to run on. The false tree_resolver comment is corrected. (Implementation
  note: gardener enablement is DEFERRED — its `Config.SelectorStatsPath` is a
  single shared file while telemetry is now keyed per tree; enabling it as-is
  would reintroduce the collision hazard. The flag stays available and off by
  default; a follow-up program should port the gardener pass to per-tree
  files before enabling.)
- **Specialists:** evolution MCP tools that build populations
  (`bt_evolve_bottlenecks`, `bt_evolve_selection_pressure`, QD/island paths)
  attach `Population.Specialists = NewSpecialistRegistry()` seeded via
  `ExpertKnowledge.SeedSpecialists()`, making crisis resurrection reachable in
  production rather than test-only.

## 6. Small fixes

- **`SaveSelectorStats` double-count:** switch to an unsaved-delta model —
  `Record` accumulates into both live stats and a pending delta; `Save` merges
  *delta* (not the whole in-memory state) onto disk under the flock, then
  clears the delta and adopts the merged view. Repeated `Save` from a
  long-lived optimizer becomes idempotent. Regression test: record 5 → Save →
  Save → reload = 5.
- **`Factory.selectParents` fallback dedup:** the any-category fallback loop
  appends into the already-populated candidate slice → duplicates → a template
  can be drawn as both parents. Rebuild the slice (or skip already-added IDs).
- **A2A `:8686` bind noise:** `address already in use` is expected sibling
  contention (CLAUDE.md documents it) — log at WARN with an explanatory
  message; every other listen error stays ERROR.
- **HITL store bloat (9.4 MB):** cap persisted records — keep every
  non-terminal request plus the most recent `hitlMaxStoredRequests=1000`
  terminal (skipped/expired/approved/rejected by UpdatedAt); compact JSON
  (drop MarshalIndent). One-time ops compaction with backup at deploy.
- **`bt-scalability-probe`:** 8.5 MB binary accidentally committed (357144a) —
  `git rm --cached` + `.gitignore` entry.

## 7. Deployment & reconciliation (ops, after landing)

Rebuild bt-agent, bt-agent-cli, bin/bt-dashboard, bin/bt-gardener from the new
HEAD in a cycle gap; swap with `.previous` convention; restart services (picks
up staggered schedules + archived agents). Then reconcile
`~/.go-bt-evolve/research/programs.json` (backup first): reset a69ef9d1
(rate-limit-era blocks) to pending/attempts 0; mark c8094002 ms1–3 done (this
change implements them); reconcile f3f356fe milestones already shipped via the
2026-07-08 branch rescues; leave 3f0e1868 for the loop with its genuinely
unshipped milestones reset to pending. Compact HITL store with backup.

## Testing

Every code change lands TDD (RED test first) in its package;
`make check-quick` gates the landing commit; deploy is verified by watching the
next scheduled cycles run green on the new binaries and a Telegram
`bt-task-complete` delivery arriving.
