# Shared Claude Rate-Limit Backoff — Design

**Date:** 2026-07-15
**Status:** Approved (user-selected "option 3" after the 2026-07-14/15 oversleep diagnosis)

## Problem

The Claude rate-limit backoff is a per-agent, fixed-duration stamp
(`goap_fusion_claude_backoff_until` in the agent-scope blackboard, default 6h
via `claudeBackoffWindow()`, 1h via `goapClaudeBackoffWindow` on the review
path). Two failure modes observed on 2026-07-14/15:

1. **Oversleep past the real reset.** The fixed window ignores the reset time
   the Claude CLI reports (its output contains `resets <time>` or the
   `…limit reached|<epoch>` form — `isClaudeRateLimit` already keys on
   "resets"). goap-fusion-loop-runner re-armed 6h stamps at 05:30, 12:00, and
   18:44 and finally slept until 00:44 CEST, ~3h after the quota demonstrably
   recovered (~23:45).
2. **Per-agent stamps diverge.** goap-fusion-runner's own stamp expired at
   23:47, its probe succeeded, and it landed commits all night while its
   sibling fast-failed on a stale private stamp. One agent's knowledge ("the
   quota is closed until X" / "the quota is open") never reached the others,
   so each agent independently probes and independently oversleeps.

## Goals

- Backoff deadline derives from the CLI-reported reset time when parseable;
  the existing fixed windows remain only as fallback.
- One fleet-wide stamp: any agent's rate-limit detection closes Claude for
  all agents; any agent's expiry-clear (or successful probe window) reopens it
  for all.

## Non-goals

- Changing the fallback window defaults (6h / 1h) or `BT_GOAP_CLAUDE_BACKOFF`
  semantics.
- Coordinating probes across *processes* beyond shared-file visibility (the
  scheduler serializes runs in-daemon; manual `bt-agent-cli` runs read the
  same file).
- Touching the circuit breaker or scheduler retry logic.

## Design

### Fleet-wide store (ADR-003)

New file `~/.go-bt-evolve/claude_backoff.json`, written atomically
(tmp + rename), guarded by a package mutex, path-seamed as
`goapClaudeBackoffPath` for tests:

```json
{"until":"2026-07-14T22:44:33Z","set_by":"goap-fusion-loop-runner","set_at":"2026-07-14T16:44:33Z"}
```

`saveClaudeBackoffState` writes this file (plus the existing ChainState
fallback for scope-off deployments) and **stops writing the agent-scope key**.
`loadClaudeBackoffState` returns the latest valid deadline among: shared file,
legacy agent-scope key, ChainState. `clearClaudeBackoffState` (and the
half-open self-clear in `claudeBackoffActive`) wipes all three, so legacy
per-agent stamps from the previous deployment are honored until they expire
and then disappear — no migration step. Missing or corrupt file reads as
inactive (never wedge). The ChainState copy is run-local by construction
(RunOnce discards it), so cross-run state lives only in the file + legacy key.

### Reset-time parsing

`parseClaudeRateLimitReset(text, now) (time.Time, bool)` handles the two
observed CLI shapes:

- `…limit reached|<epoch>` — 10-digit seconds or 13-digit millis, accepted
  when the instant is after `now` and within 7 days (else garbage).
- `resets [at ]H[:MM][am|pm]` — wall-clock with no date/zone; the CLI prints
  the user's local time, so the next occurrence after `now` in `time.Local`
  is used. A bare hour with neither `am/pm` nor `:MM` is too ambiguous and is
  rejected.

`claudeBackoffDeadline(errText, now, fallback)` converts a rate-limited
outcome into the stamp: parsed reset + 2-minute boundary margin, capped at
`now+24h` (a mis-parse must not idle the fleet for days; a genuine multi-day
weekly-cap reset re-detects after 24h at the cost of one cheap probe),
otherwise `now + fallback` — exactly the pre-change behavior.

Both save sites adopt it:

- `actions_superpowers_prod.go` (runtime path): fallback `claudeBackoffWindow()` (6h default, env-overridable).
- `actions_goap_fusion_claude_review.go` (review fallback): fallback `goapClaudeBackoffWindow` (1h), whose "not machine-parsed" comment is now stale and gets updated.

### Behavior matrix

| CLI output contains | Stamp |
|---|---|
| `resets 3am` (now 01:00) | today 03:02 local |
| `resets 3am` (now 04:00) | tomorrow 03:02 local |
| `…reached\|<epoch +3h>` | epoch + 2m |
| `…reached\|<epoch +48h>` | now + 24h (cap) |
| `weekly limit · resets Jul 7` (real DLQ shape) | now + fallback window — the date form is intentionally unparsed; the 24h cap would dominate a multi-day sleep anyway |
| no parseable hint | now + fallback window (unchanged) |

## Testing

All backoff tests (new and the five existing ones) MUST call
`isolateClaudeBackoffStore(t)` — the file store's sibling to
`isolateGoapProgramStore(t)`; a test using the default path would arm or
clear a **live** fleet-wide backoff on the operator's machine. New coverage:
parser table test, deadline fallback/cap test, shared-across-agents test
(agent A saves → agent B with a different manager sees it; clear reopens
fleet-wide), legacy agent-scope stamp honored-until-expiry test, and a
review-path test proving a `resets <time>` hint beats the 1h fallback.

## Ops

- Inspect: `cat ~/.go-bt-evolve/claude_backoff.json`; `set_by`/`set_at` say
  which agent armed it and when.
- Manual unstick: delete the file (plus any legacy
  `goap_fusion_claude_backoff_until` blackboard entries).
- `BT_GOAP_CLAUDE_BACKOFF` unchanged (fallback-window override).
