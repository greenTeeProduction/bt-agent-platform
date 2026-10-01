# Target-owned restart control — 2026-10-01

Sibling adoption requires the target's atomic admission owner; missing, busy,
disabled, wrong-owner or wrong-artifact targets have no direct systemd fallback.
Dashboard/gardener serve same-UID Linux control, attest their systemd MainPID,
verify exact clean artifact identity and request their own restart. A shared gate
owns detached callbacks, cycles and gardener rescan/analysis/tools/metadata.
Accepted or uncertain handoff stays sealed until process exit (ADR-279).

## Snapshot and settled verification

[Source manifest](source-snapshot.json) binds implementation, tests and current
architecture to base `a1b4ff7f60c5b5327bf01dbb1040122a02040b8e`. Evidence and
regenerated graph files are excluded. Canonical payload SHA-256:
`64b00b5f468f17799f66f068b2be8b42fa7e7abdd0ed5d580f1db213a5588bbb`. [Go identity](qualified-code.json) and
[commands](commands.json) record exact scope and the comment-only race variant.
Private full Go manifest, coverage, binaries, earlier failed checks and raw
SARIF survive under `/mnt/ssd/bt-checkpoints/20261001-restart-control` (root 0700).
Tracked logs replace operator/worktree/fixture paths and strip terminal escapes.
No model/coding provider runs in these fixture gates.

| Check | Outcome |
|---|---|
| Pre-fix missing-owner regression | Red: old controller directly restarted dashboard without its owner; [red.log](red.log). |
| Full short race suite with atomic coverage, two package workers | Pass, 77.8% statements; [full-race.log](full-race.log). |
| Final focused control/owner race fixtures | Pass; [race-final.log](race-final.log). |
| Final `make build` | Vet/fmt/tidy, zero-issue lint, local high-security, thirteen binaries and AST-only graph pass; [build.log](build.log). |
| Documentation checker and drift | Eleven fixtures and structural/traceability checks pass; [docs.log](docs.log). |
| Medium gosec SARIF | 256 existing findings; zero new control/gate findings. `-no-fail` exit 0 does **not** mean a clean scan; [scanner summary](scanner-summary.json). |

The full local race run began before the three-line G204 explanation on the
fixed, allowlisted systemctl MainPID query. That comment changes no Go behavior;
its earlier file hash is retained. Final focused race/build gates qualify the
annotated source. Initial fixture compile/setup and revive lint failures are
retained privately and are not counted as passing gates. The build reruns
`graphify update .` without model calls (14,580 nodes, 28,895 edges, 1,161 files).
Dirty-worktree gate artifacts are fingerprinted in [binary hashes](checked-binaries.json),
and are not clean deployment evidence. Local high-security acceptance does not
establish GitHub's separate gosec or CodeQL result.

## Fixture scope and recovery

The new dashboard fixture uses actual authenticated sprint admission and a real
local action in one subprocess; a different process requests control. Busy work
blocks restart; cleanup permits one handoff; the sealed owner rejects new work.
After that owner exits, missing control rejects requests; a second independent
current-revision process attests without a second restart. Kernel credentials,
framing, malformed requests, lost acknowledgements, owner failures, MainPID
mismatch and exact artifact metadata have regressions. Gardener fixtures own
actual provider-free cycle metadata plus post-cycle analysis activity. These
use controlled version/systemctl callbacks or executables, **not real systemd
handoff or deployed service execution**. The different-UID predicate test is
not an actual cross-account deployment test.

Successful-action/result-record-failure safety is separately established by
[the process-restart recovery report](../2026-10-01-restart/README.md): durable
action counters and abrupt exit followed by two independent new processes;
held completed/uncertain work cannot silently replay through ordinary admission,
registry reconciliation or reapproval. This remains single-owner process-restart
evidence, not power/volume-loss or distributed stale-writer proof.

## GitHub and operational qualification

The implementation commit, actual separate GitHub gosec/CodeQL results and clean
artifact metadata are appended in a subsequent evidence-only commit. Until those
identities exist, previous PR #83 results apply only to their previous heads.
No current artifact has been deployed, and no host provider or real service
restart runs in this increment. Clean version invocation alone will not prove
serving identity or provider readiness.

Prior deployed dashboard executable/hash/version/build_info, one actual controlled
Codex adapter delivery and actual inactive-state backup/restore remain qualified
at `f60dcf420baafe5a65bf91f2a9e8661808d977f2` in the
[operational checkpoint](../2026-10-01-checkpoint/README.md). That report records
account rejection of gpt-6.1-sol, private gpt-5.5 success, a temporary supported
generic-provider override for read probes, restored file/link hashes and scope
limits. It does not qualify production execution or deployment of this new code.

## Goals and prioritized limitations

[C01–C12 and backlog](../../plans/2026-09-30-arc42-cleanup.md#stable-checkpoint-c01c12-status--2026-10-01):
C01–C05/C07 complete; C06/C08–C12 partial; none blocked.

P1: bt-agent self still samples scheduler state; daemon-wide scheduler/A2A/DLQ
and callback ownership, old-controller rollout and real bounded all-unit handoff/
rollback remain open. Upgrade controllers and targets with automatic flags
**disabled** before enabling target-owned control. UID and MainPID authentication
do not prove provider readiness or end-user identity. Non-Linux control safely
defers. Accepted but ineffective supervision retains the seal until operator
restart. P2: retention, operator recovery reconciliation, ignored metric writes,
remaining schemas/transports, power/volume loss and numeric RPO/RTO remain open.
