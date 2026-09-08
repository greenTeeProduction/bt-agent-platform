# Scheduler head-of-line blocking fix

## Scope relative to the throughput plan

Reviewed `docs/superpowers/plans/2026-08-02-scheduler-throughput.md`
(the documentation commit `b32dd33`, not merged as implementation).
This fix takes the bounded, oldest-due-first asynchronous admission and
same-agent exclusion portion. It does not implement hot-reloaded JSON/env
configuration, tree-derived exclusion groups, or a deployment rollout.

`SchedulerConfig.MaxConcurrent` defaults to **3**, clamps to **1–12**.
Setting it to 1 provides one-at-a-time execution, but dispatch remains
nonblocking and saturated jobs wait until a later tick; this is not the old
plan's promise of exact inline serial timing. Tick interval remains unchanged.
Different agent names may execute concurrently even when they share a tree or
repository: resource-level exclusion groups are a separate follow-up.

## Guarantees

- Admission is atomic under the scheduler mutex. There is no unbounded queue
  of goroutines waiting for execution slots. Busy jobs retain their due time.
- An agent keeps its slot through execution and bookkeeping, even if its job
  is deleted, replaced, or reconciled. `RunNow` shares the budget and rejects
  busy/stopped admission rather than overlapping a scheduled run.
- Circuit-breaker probes are acquired only after lane checks.
- `AnyInFlight` includes admitted executions independently of job records.
  Idle notification occurs only after the last lane is released.
- `Stop` permanently closes admission, cancels run contexts, waits for the
  scheduler loop and admitted workers, then flushes pending graph feedback.
  Concurrent/repeated Stop calls are safe, including Stop before Start.
- Runners must honor `RunContext.Context`; Go cannot forcibly kill a runner
  that ignores cancellation. Do not call blocking `Stop` from a runner or
  idle callback (which would wait on itself).

## Verification

The original synchronous implementation failed
`TestSchedulerConcurrentDueAgents` with "due agent blocked behind another run".
Regression coverage also exercises bounded/oldest-first dispatch, serial mode,
repeated ticks, removal/replacement, RunNow exclusion/cancellation, shutdown
and draining, panic/missing-agent cleanup, idle notifications and breaker probes.
Run:

```sh
go test -race ./internal/agent ./cmd/bt-agent -count=1
go test -race ./internal/agent -run 'TestScheduler(Concurrent|Bounded|Removal|RunNowShares|Stop|PanicAndMissing|IdleOnly|BusyLane)' -count=20
```
