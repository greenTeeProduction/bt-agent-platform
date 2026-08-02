# Scheduler Throughput: Bounded Concurrent Lanes

Date: 2026-08-02
Status: Design approved, implementation pending

## Problem

The fleet is a single-lane queue. `Scheduler.tick()` (`internal/agent/scheduler.go`)
runs every due job sequentially and inline, and while a job runs the `Start()`
ticker cannot fire another tick at all. One 37-minute goap cycle blocks all nine
scheduled agents.

Measured over 24h (2026-08-01, `journalctl` cycle-complete lines):

| agent | runs | total | avg |
|---|---:|---:|---:|
| goap-fusion-loop-runner | 19 | 696.5m | 36.7m |
| goap-fusion-runner | 13 | 383.5m | 29.5m |
| notebooklm-researcher | 11 | 151.4m | 13.8m |
| bt-fusion | 19 | 45.8m | 2.4m |
| notebooklm-auth-guardian | 4 | 36.6m | 9.1m |
| notebooklm-pipeline-monitor | 11 | 14.6m | 1.3m |
| self-review | 1 | 11.6m | 11.6m |
| hermes-daily-updater | 1 | 2.0m | 2.0m |
| arc42-program-seeder | 6 | 0.4m | 0.1m |

85 cycles, **22.4h busy — 93% duty on the single lane**. The host meanwhile is
idle: 12 cores, load average 0.6, 61 GB RAM, 1.4 TB free on the SSD.

Throughput is bounded by the queue, not by hardware. The ceilings behind the
queue are Claude quota (Opus 5 at max effort since 2026-08-01) and the shared
main checkout, `goapFusionRepo = /home/nico/go-bt-evolve`, whose materializer
preflight runs `git checkout -f HEAD -- .`.

## Scope

This spec covers **bounded concurrent dispatch with exclusion groups and
operator-tunable configuration**. Coding cycles remain mutually exclusive.

Explicitly out of scope, deferred to a follow-up spec: giving each coding cycle
its own checkout so several can run in parallel. That requires `goapFusionRepo`
(a package-level global read across ~8 engine files) to become a per-run value
threaded through the run context, and touches the materializer, apply path, and
PR shepherd. It is the change that multiplies code output; this spec is its
prerequisite and will supply the utilization data to size it.

### Expected gain

Small agents stop queueing: their latency drops from up to 37 minutes to ~0.
Coding cycles reclaim the ~4.4h/day the small agents consume, raising goap lane
time from 18h to ~22h — roughly **+18% coding-cycle headroom**. Code output
gains are modest by design; the large gain is the follow-up spec.

## Architecture

### Dispatch

`tick()` no longer runs jobs inline. It builds the due list, sorts it by
`NextRun` (oldest first — today's map-iteration order is random and can starve a
job), and hands each entry to a **bounded dispatcher**: a semaphore of
`maxConcurrent` lanes.

Lane acquisition is **non-blocking**. A job that cannot get a lane stays due and
is retried on the next tick. There is no queue and no unbounded goroutine
growth; backpressure is expressed by the job simply remaining due.

Because `tick()` returns promptly, `Start()`'s ticker fires on schedule during
runs, which makes the tick interval meaningful again.

### Dispatch guards

Evaluated atomically under `s.mu` before a lane is taken, in this order:

1. **Job in-flight** — `job.InFlight` is set at dispatch time, not inside
   `runJob`. Today double-dispatch is impossible by construction; under
   concurrency it is the primary hazard, because `NextRun` is not advanced until
   completion, so a running job stays "due" for its entire duration.
2. **Agent in-flight** — an agent never runs twice concurrently, even if
   duplicate jobs survive reconciliation.
3. **Exclusion group** — at most `group_limits[group]` in-flight jobs per group
   (default 1).
4. **Circuit breaker** — `cbStore.Allowed()` is consumed **last**, only once the
   job will actually run. Ordering is load-bearing: `Allowed()` consumes a
   half-open probe, and dropping the run after taking that probe wedges the
   breaker in HalfOpen forever (the hazard already documented in `runJob`'s
   registry-miss branch).

`runJob` is unchanged internally. Every collaborator it touches — `History`,
`JobStore`, `AgentCircuitBreakerStore`, `knowledge.GlobalGraph`, `AgentBus` — is
already mutex-protected.

### Exclusion groups

A new `concurrency_group:` field on the agent definition
(`internal/agent/registry.go`, `Definition`). Default is derived from the tree ID:

| tree prefix | default group |
|---|---|
| `domain:goap_fusion*` | `repo-main` |
| `domain:superpowers*` | `repo-main` |
| `domain:self_fix*` | `repo-main` |
| anything else | the agent's own name |

An agent in a group named after itself has no cross-agent exclusion; only guard 2
applies. Explicit YAML always wins over the derived default.

The tree-derived default is the safety property: a future repo-mutating agent
added without the field still lands in `repo-main` rather than silently racing
the materializer.

## Configuration

Precedence: **built-in default → env → file**. Deleting the file reverts to env;
unsetting env reverts to the built-in.

### Env (boot defaults)

| var | default |
|---|---|
| `BT_SCHEDULER_MAX_CONCURRENT` | `3` |
| `BT_SCHEDULER_TICK_INTERVAL` | `1m` |

### File (live retuning, no restart)

`~/.go-bt-evolve/throughput.json`:

```json
{
  "max_concurrent": 3,
  "tick_interval": "1m",
  "group_limits": { "repo-main": 1 }
}
```

Re-read each tick behind an mtime check, so steady-state cost is one `stat`.
Malformed JSON keeps the last-good values and logs a WARN — it never crashes the
scheduler and never falls back to unbounded. Each applied change logs one INFO
line with old → new values.

There is deliberately **no per-agent override block** in the file: it would
duplicate the YAML `concurrency_group` field with unclear precedence, leaving two
places to look when an agent will not start.

### Bounds and reload semantics

- `max_concurrent` clamps to `[1, 12]`. **`1` reproduces today's exact serial
  behavior** and is the rollback path — no redeploy, just write the file.
- `group_limits[g]` clamps to `[1, max_concurrent]`.
- Changes affect future dispatch decisions only. In-flight runs are never
  touched, and lowering `max_concurrent` below the current in-flight count kills
  nothing — it withholds new lanes until the fleet drains.
- A changed `tick_interval` resets the ticker on the next iteration.

## Safety invariants

**Main checkout.** The materializer preflight (`git checkout -f HEAD -- .`) and
the apply/commit path both run in `/home/nico/go-bt-evolve`. `repo-main` limit 1
preserves today's guarantee exactly — it is not new protection, it is the
existing serial behavior expressed as configuration. Relaxing it belongs to the
follow-up spec.

**Quota.** The heavy Claude consumers are the `repo-main` agents, and at most one
runs at a time, so added lanes carry cheap monitor runs rather than multiplying
Opus-5-at-max burn. The existing fleet-wide 6h rate-limit backoff stamp is
unchanged.

**CPU.** Only one `go test`-running cycle can exist at a time, which matters on a
box whose systemd drop-in pins `GOMAXPROCS=4`.

**Deploy-drift adoption improves.** Lane-hours stay constant while wall-clock
spreads out, so fully-idle windows become more common. `AnyInFlight()` and
`OnCycleIdle` are untouched, so the rebuild/restart guard still fires only when
the fleet is genuinely idle.

## Failure handling

- The lane semaphore slot, `InFlight` flag, and group counter are released in a
  **single `defer` that runs first** in the dispatch goroutine. A panicking
  runner must not leak a lane or permanently wedge an agent or group.
- Today's per-job `recover` already prevents one bad agent from killing the tick
  loop; under lanes the blast radius is one lane.
- `Stop()` stops dispatching but does **not** drain. In-flight runs die with the
  process exactly as they do today, so restart time does not regress against the
  unit's `TimeoutStopSec=30`.

## Observability

- Lane saturation: DEBUG per deferral, plus one throttled INFO per minute.
  Without throttling a saturated fleet writes a line per tick per deferred job.
- `scheduler: cycle complete` gains `lanes_in_use` and `group`, so the journal
  shows whether throughput is lane-bound or genuinely idle.

## Testing

A fake `AgentRunner` blocking on a channel makes every case deterministic without
sleeps. All run under `-race`.

| # | Assertion |
|---|---|
| 1 | Two different agents run concurrently |
| 2 | The same agent never runs concurrently |
| 3 | Two `repo-main` agents never run concurrently |
| 4 | Lane cap holds under a flood of due jobs |
| 5 | A withheld lane does **not** consume the breaker's half-open probe |
| 6 | A panic in one lane leaves lane, group, and agent state clean |
| 7 | Live config reload changes dispatch mid-flight |
| 8 | Malformed config keeps last-good values |
| 9 | `max_concurrent=1` reproduces today's serial ordering |
| 10 | Group defaults derive correctly from tree ID; explicit YAML wins |

## Rollout

1. Land with `BT_SCHEDULER_MAX_CONCURRENT=1` — behavior identical to today,
   exercising the new dispatch path with no concurrency.
2. Raise to 3 via `throughput.json` (no restart). Watch `lanes_in_use`,
   rate-limit backoff, and cycle outcomes for a day.
3. Feed the observed utilization into the per-cycle-checkout follow-up spec.
