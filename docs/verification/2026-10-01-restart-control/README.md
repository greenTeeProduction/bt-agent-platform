# Target-owned restart control — 2026-10-01

Sibling adoption requires the target's atomic admission owner; missing, busy,
disabled, wrong-owner or wrong-artifact targets have no direct systemd fallback.
Dashboard/gardener serve same-UID Linux control, attest their systemd MainPID,
verify exact clean artifact identity and request their own restart. A shared gate
owns detached callbacks, cycles and gardener rescan/analysis/tools/metadata.
Accepted or uncertain handoff stays sealed until process exit (ADR-279).

## Initial implementation snapshot and settled verification

At implementation `f7fa8dc65756a6be0cc91a2f88d2d156f99620ec`,
[source manifest](source-snapshot.json) binds implementation, tests and current
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
its earlier file hash is retained. The original focused race/build gates qualify that
annotated implementation; the scanner correction below has separate identities. Initial fixture compile/setup and revive lint failures are
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

At `f7fa8dc6`, actual GitHub full short race, build, lint, Actions Security Scan
and separate CodeQL passed, while **separate gosec failed with three errors**.
[Actual initial checks](github-before.json) and [annotations](github-failed-annotations.json)
retain that failure. The local 256 medium baseline did not establish acceptance.
The findings identify the fixed restart command, an existing workflow file read
and its signed duration conversion. A narrow unit/MainPID command exception,
rooted workflow loading and nonnegative duration conversion address them.
[Correction manifest](scanner-correction-snapshot.json) and
[corrected Go identity](scanner-correction-code.json) bind these changes to f7fa8dc6.
[Focused race](scanner-fix-race.log), [all build gates](scanner-fix-build.log),
[documentation checks](scanner-fix-docs.log) and
[corrected scanner summary](scanner-fix-summary.json) record their separate scope.
The corrected local medium scan retains 253 findings, three fewer than the
initial scan; `-no-fail` exit zero is not clean acceptance. Corrective build
fingerprints are [retained separately](scanner-fix-binaries.json). Initial drift
footer failures were corrected; the failed log remains private. Graph rebuild:
1,162 files, 14,588 nodes, 28,904 edges, without model calls. The corrective
manifest canonical SHA-256 is
`c6d72b251b69eda4899e71a17ba0fa1caa50ab9985ba3e632cd87ac0036dcaaa`.
All actual GitHub checks pass at corrected implementation
`2693548055f9f710701305e0b0a75d11a2690198`, including separate
[gosec 110339650368](https://github.com/greenTeeProduction/bt-agent-platform/runs/110339650368)
and [CodeQL 110339555530](https://github.com/greenTeeProduction/bt-agent-platform/runs/110339555530).
[Qualified identities](github-qualified.json) retain exact heads and outcomes.
[BT CI](https://github.com/greenTeeProduction/bt-agent-platform/actions/runs/36852996372)
and [CodeQL Analysis](https://github.com/greenTeeProduction/bt-agent-platform/actions/runs/36852996264)
pass; Release is skipped. The actual full short race check qualifies the final
corrected Go source. The subsequent evidence-only commit preserves every Go/
dependency byte; final head outcomes are separately retained on PR #83.
[Evidence documentation checks](evidence-docs.log) pass eleven fixtures and drift;
complete Go-manifest equality confirms the documentation-only qualification.

[Corrected clean artifact](clean-corrected-artifact.json) reports this exact
revision and `dirty=false`. Only `--version` ran; build commit time is unknown.
It is not deployed serving identity or real handoff evidence.

[Initial clean artifact](clean-artifact.json) reports f7fa8dc6 and `dirty=false`;
only its fixed `--version` was invoked. It predates the scanner correction.
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


## Final-head directory permission correction

`1127c804e7c8cc90c9fa3012c518b8e9f2fcf042` preserved all Go/dependency bytes
from the qualified `26935480` implementation, but its actual separate
[gosec 110342342627](https://github.com/greenTeeProduction/bt-agent-platform/runs/110342342627)
failed with one further existing G301 annotation in `ApplySchedule`.
[That annotation](github-evidence-failed-annotations.json) and
[actual head checks](github-evidence-before.json) are retained; source
equivalence did not establish final-head acceptance. New jobs directories now
use 0750 instead of 0755, without chmodding existing host directories.

[Permission source manifest](permission-snapshot.json),
[complete Go identity](permission-code.json), [scheduler/restart race](permission-race.log),
[build gates](permission-build.log), [documentation](permission-docs.log) and
[scanner summary](permission-summary.json) qualify this final small correction
separately. Local scanner acceptance remains separate from GitHub. Final-head
check identities and clean committed artifact metadata are retained in the
private `github-final.json` / `clean-final-artifact.json` and on PR #83 after
those checks complete; this report does not infer an unobserved outcome.
Canonical permission payload SHA-256:
`8f208864c5a7b0c209c482bed95c8e1516e855b73badb82869dcd5d5ee138c6e`.
The local scan has 252 baseline findings; `-no-fail` is not clean acceptance.
All preceding snapshot/operational scopes and goal statuses remain as stated.
