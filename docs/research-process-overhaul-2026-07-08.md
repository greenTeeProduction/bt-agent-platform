# Research Process Overhaul — 2026-07-08

A coordinated repair of the platform's autonomous research pipeline, covering the
goap-fusion-loop-runner deadlock, arc42 goal anchoring of every research lane,
NotebookLM corpus hygiene, and two chronic runner failures.

## The runaway-backstop deadlock

The goap-fusion-loop-runner was permanently wedged from 2026-07-06 19:00 to
2026-07-08 05:37 (67 consecutive failures). The Claude CLI weekly rate limit
(hit 2026-07-05 18:48) prevented all landings; each idle cycle published the
identical state hash to the CIRCUITPOLICY history; the repeated-hash bypass
(commit 4ae9d61) removed the early halt; and 50 accumulated identical hashes
tripped the runaway-loop backstop in `RunScheduledGoapFusionLoop`
(`internal/engine/actions_superpowers.go`). Because the backstop runs in
preflight, the runner could never land a commit to clear its own history — a
self-locking failure.

The structural fix already existed on master: commit 247b2a5 (authored by the
loop itself on 2026-07-05) made the backstop half-open (HALT clears the durable
history) and added an idle-tick guard to `PublishGoapFusionStateHash`. The
running binary predated it by four hours. Remediation: manual clear of
`goap_fusion_state_hashes` in the agent-scope blackboard, redeploy of bt-agent
and bt-agent-cli from master, restart. Verification: the 06:00 cycle ran 36
minutes and landed commit 4b85744 — the first loop-runner landing since
2026-07-05 — declining a fabricated `adaptor.go` milestone exactly as the
seeder-grounding safeguards intend.

## arc42 goal anchoring (commit 604b1e4)

Research direction previously came from static hardcoded topic lists with no
tie to the platform's documented goals. The new `internal/engine/arc42_goals.go`
parses the arc42 §1.2 Quality Goals table (`docs/arc42/go-bt-evolve-arc42.md`):
Q1 Correctness, Q2 Evolvability, Q3 Reliability. It degrades gracefully when
the doc is missing, and a test pins the production doc's parseability.

Wired consumers:

- `deriveNotebookLMResearchQuery` (`internal/engine/nlm_quota.go`): the
  scheduled researcher's idle rotation now uses `arc42ResearchTopics()` — one
  research question per quality goal.
- `buildClaudeReviewPrompt` (`internal/engine/actions_goap_fusion_claude_review.go`):
  the Claude review fallback embeds `arc42GoalsPromptBlock()` and requires each
  GAP to name the quality goal it advances.
- `buildSeedProgramPrompt` (`internal/engine/goap_seed_program.go`): seeded
  programs must advance a named quality goal.
- `buildGrillRound1Query` (`internal/engine/actions_goap_fusion.go`, extracted
  from the inline grill action): gaps are judged against the quality goals.
- `SearchForBTPatterns` (`internal/engine/actions_btfusion.go`): previously a
  static 5-item list that produced zero new findings across 383 consecutive
  hourly runs; now asks one arc42-anchored NotebookLM question per day through
  the `nlmFusionResearchRun` seam, with the nlm query cache bounding the hourly
  action to at most one live call per day.

Live verification: the first post-deploy Claude-review artifact
(2026-07-08T070956) tagged its findings "GAP1: Q3 Reliability", "GAP2: Q2
Evolvability".

## NotebookLM corpus hygiene

A research task wedged "in progress" for 2+ days made every 2-hour researcher
run re-import the same 10 discovered sources, regrowing the notebook corpus
from the hand-pruned 69 sources back to 336 (individual papers duplicated up
to 8 times). Root loophole: research imports were exempt from the daily budget
by design.

Fixes:

- Import budget gate (commit 604b1e4): `research import` now consumes
  `BT_NLM_IMPORT_BUDGET` (default 2/day) in `nlmPreflight`/`nlmPostflight`;
  failed imports do not count.
- Corpus re-pruned 336 → 71 sources using `notebooklm source clean`
  (notebooklm-py). The `nlm source delete` command is broken as of 2026-07-08
  (fails with fresh auth and fresh source ids — API drift since the 2026-07-04
  prune).
- Auth recovery for both CLI tools from the still-live nlm Chrome session
  (CDP :9222): a bare cookie re-export for the nlm CLI goes stale in ~25
  minutes; notebooklm-py needs full browser-profile seeding to self-sustain.
- Vault quarantine: 381 quota-error syntheses plus 1 exact duplicate moved to
  `syntheses-quarantine-20260708/` (vault 1418 → 1037 files, nothing deleted).

## Chronic runner fixes

- hermes-daily-updater (commit a81352e): its up-to-date report never contained
  the literal keyword "version" that its own YAML quality gate requires, so a
  healthy daily run failed three days straight. New testable report builders
  `hermesUpdateReportHeader` / `hermesUpToDateStatus` (`internal/engine/registry.go`)
  are pinned against the gate keywords.
- hermes-gateway: the webhook publisher's "connection refused" on :8644 was a
  startup-window race — the gateway had self-restarted at 06:08:22 and was
  healthy; no action needed.

## Landing procedure

The changes were landed with the platform's mid-cycle discipline: wait for the
in-flight cycle to complete, stop bt-agent in the gap (nothing killed), local
fast-forward of master to the merged branch tip (974ba26), rebuild both
binaries with the `.previous` convention, restart (81 seconds of downtime),
then push to origin with explicit user authorization.

## Research quality findings (review that motivated the overhaul)

- The Claude-review lane produced high-quality, fully grounded findings (zero
  fabricated file paths) but re-diagnosed the same four defects for days
  because the stale deployed binary never picked up its fixes — deploy drift,
  which the lane itself correctly root-caused.
- The knowledge store (`~/.go-bt-evolve/research/knowledge.json`) held 1,671
  entries with 70 implemented goal objectives; program af4c755 (RemoteExecutor
  + AgentRouter substrate, 5/5 milestones) traced cleanly from research to
  landed commits, validating the research-to-code pipeline when engines are up.
- The nlm-research file class was pipeline logs (95% source-list JSON dumps),
  not research; bt_fusion pattern research was static and yield-free until the
  arc42 anchoring.
