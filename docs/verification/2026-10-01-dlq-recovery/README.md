# Durable DLQ restart recovery — 2026-10-01

Replay now commits an exact claim before dispatch and retains it when completed
or uncertain work cannot be recorded. Ordinary scanner/requeue, purge and
capacity do not release claims. Transactions apply deltas against current disk
membership under a bounded lock; stale cooperating owners cannot erase claims
or resurrect deleted work. Unreadable state stays in place and closes admission.
Scheduler/engine terminal failures enter already held; DLQ replay cannot bypass
the original execution stop. ADR-280 records this contract and its limits.

## Snapshot and verification scope

Base implementation: `7da5b8a7f20846d3ca9806fcb85f4c433600dcf8`.
DLQ implementation/architecture commit: `0017825211f90d6bc8a2d24715b58496474c9fd2`.
[Source manifest](source-snapshot.json) binds changed implementation, tests and
current documentation; graph/evidence outputs are excluded to avoid circular
identity. [Complete Go identity](qualified-code.json) includes every Go source
and dependency file. [Commands and outcomes](commands.json) identify individual
checks, the earlier race variant and the final exact-snapshot repeat. Sanitized logs and summaries are retained here;
full manifests, coverage, raw scanner output and private binaries survive at
`/mnt/ssd/bt-checkpoints/20261001-agent-admission` (root 0700). Temporary files
are not the sole evidence. No provider/real supervision runs in fixture gates.

The unchanged pre-fix process fixture reproduced duplicate actions in three
cases and dispatch despite failed admission. The fixed fixture syncs an actual
local action counter, fails final recording or exits immediately, then starts
two independent consumers using preserved queue bytes. Success, uncertainty,
immediate exit and double executor/DLQ recording failure retain exactly one
action; failed admission executes zero. Original scheduler uncertain/stopped
entries also survive two fresh process consumers. Sibling transaction, panic,
exact-claim resolution, bounded-lock, failed-write and capacity tests cover
maintenance/admission paths. Actual authenticated HTTP responses and MCP tools
reject held original uncertainty and report storage failures. These are fixture
contracts, not production action/recovery qualification.

| Check | Outcome |
|---|---|
| Identical process fixture, old/new owner | Red on baseline; green with race on final owner. [Fixture identity](fixture-identity.json) and retained exact fixture bind both runs. |
| Focused replay/origin and final consumer races | Pass; five replay faults plus scheduler-origin uncertainty/stops, exact decisions, sibling deltas, maintenance and authenticated HTTP/MCP. |
| Final full short race / atomic coverage | Pass, 77.9%, on the exact settled Go snapshot; [final log](full-race-final.log). Earlier full-race variant is retained separately. Hosted full race is checked independently. |
| Final build gates | Pass: vet/fmt/tidy, zero-issue lint, local high-security, thirteen binaries and AST-only graph. Initial two lint issues corrected; failed log remains private. |
| Docs and Arc42 engine contract | Pass: eleven checker fixtures, drift and goal-loading test. |
| Medium SARIF parity | 252 baseline findings; zero new DLQ-owner findings. No clean-scan claim. |

[Build fingerprints](checked-binaries.json) are dirty-worktree gates, not serving
identity. Graph update: 1,169 files / 14,630 nodes / 29,060 edges / 727
communities, AST-only without provider calls. Snapshot SHA-256:
`00615fd5e0164399381ebf790118eeaf8af302b96e90c4cd3c6eab621c23a067`.
Complete Go/dependency SHA-256:
`0f6deaa76b36d7c69aef94d0ffbeba5d79d8ffff1a4a710ffbbb57eaf50adaa3`.

## Operational evidence and limitations

The latest prior stable implementation `7da5b8a7` passed all actual GitHub PR
checks, including the separate gosec/CodeQL checks; [revalidated base checks](github-base-checks.json)
retain their exact identities as predecessor evidence. A local
high-severity scan never establishes the separate GitHub gosec result. Current
increment final-head checks and a clean version-only artifact are retained
privately in `github-final.json`, `clean-final-artifact.json` and `FINAL_REPORT.md`
after they complete, and on PR #83. This committed source report does not infer
checks that have not yet run at its eventual evidence commit.

Deployed dashboard executable/hash/version/build_info, one controlled actual
Codex adapter artifact, and offline backup/hash-verified restore of actual
inactive state remain scoped to `f60dcf420baafe5a65bf91f2a9e8661808d977f2` in
[the operational report](../2026-10-01-checkpoint/README.md). Account rejection
of gpt-6.1-sol, private gpt-5.5 delivery, temporary supported generic-provider
read probes, and external vault/worktree/reauthentication exclusions remain
explicit. That evidence does not qualify deployment or production execution of
this new DLQ code. No shared host settings or active build were changed here.

Rollout must stop/drain **all** legacy writers before adopting this protocol;
old complete-snapshot writers and rollback can erase unfamiliar claim fields.
The trusted Go reconciliation seam requires an exact claim and independently
established owner quiescence plus completed/provably-unstarted evidence. It
neither dispatches nor provides an authenticated recovery endpoint. Unknown
outcome remains held. In-memory queues, arbitrary filesystem-I/O cancellation,
lost output, state deletion/replacement and power/volume loss remain outside
acceptance. Ordinary purge/eviction cannot be used as reconciliation.

[C01–C12/backlog](../../plans/2026-09-30-arc42-cleanup.md#stable-checkpoint-c01c12-status--2026-10-01):
C01–C05/C07 complete; C06/C08–C12 partial; blocked none. P1 continues with
daemon-wide self admission/callbacks, safe DLQ rollout/operator recovery,
provider readiness and real bounded fleet handoff. C09/C12 remain partial.

## Actual hosted scanner correction

At `fb87abef1efaee5355bdc8827787e4fac00a2c7b`, actual separate
[gosec 110374854223](https://github.com/greenTeeProduction/bt-agent-platform/runs/110374854223)
failed eight errors; [CodeQL 110374709225](https://github.com/greenTeeProduction/bt-agent-platform/runs/110374709225)
failed one high/seven medium. [Exact failed-head checks](github-failed.json),
[gosec annotations](gosec-failed-annotations.json) and
[CodeQL annotations](CodeQL-failed-annotations.json) preserve the failures.
That head was not landable despite local gate success.

The annotations identify existing history traversal/permissions, rollback I/O,
a fixed deploy Git query and request-derived logger fields. History now uses
local identifiers and configured-root append/read, new 0600/0750 modes and
commit-before-cache acknowledgement. Outward links fail before append/read.
Rollback roots backup access and atomically replaces a complete executable
without truncating the live image or following its outward link. Runner,
scheduler and validator operator logs use generated/catalog identities and
status/counts; protected results and encoded client diagnostics retain detail.
A narrow fixed-argv Git G204 explanation is preserved; no security category or
severity gate is disabled. The historical invalid-cron +1h fallback is unchanged
and has a narrow nilerr lint explanation after removing raw parser log text.

[Correction source manifest](scanner-correction-snapshot.json),
[complete Go identity](scanner-correction-code.json),
[transport races](scanner-transport-final.log), [full corrective race](scanner-corrective-full-race.log),
[build gates](scanner-settled-build.log), [graph update](scanner-graph-final.log),
[documentation](scanner-docs.log), [scanner summary](scanner-corrected-summary.json)
and [binary fingerprints](scanner-corrected-binaries.json) qualify this correction
separately. Full corrective coverage is 77.8%. Medium findings drop from 252
to 244; all eight actual gosec locations are removed locally. Hosted corrected-
head scanning still requires direct observation.

Earlier fixture/lint/full-suite failures remain private: the old log assertion
expected raw validation details, the intentional cron fallback needed an explicit
nilerr explanation, and the SDK fixture expected ENOSPC from an outward
`/dev/full` link now rejected before append. The SDK test now asserts the actual
injected history error survives cancellation as a stopped diagnostic; this does
not claim a real history ENOSPC injection. Under concurrent gates, a 20ms output-
phase deadline expired before the action and legitimately rejected admission.
Its fixture now permits 250ms for input setup while still testing a real caller
deadline and <1s termination, well below the default lock budget; five repeated
races pass. The final whole suite passes after these updates. No failed or
superseded run is counted as final acceptance. Conventions re-review found no
remaining actionable issues.

[Read-only host recheck](host-state-readonly.json) observes all three units
inactive with MainPID 0. The current disk artifact differs from the earlier
f60 dashboard used in serving acceptance and was not invoked. Earlier f60
serving/Codex/restore evidence remains dated qualification, not a current serving
claim. No host unit/configuration/binary/state was changed here. The corrected
code still has no deployed execution or real handoff qualification.
