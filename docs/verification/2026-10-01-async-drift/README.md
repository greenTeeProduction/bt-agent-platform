# Dashboard restart ownership checkpoint — 2026-10-01

Dashboard self-adoption now retains HTTP requests and detached agent/sprint/
pipeline execution through evidence, cleanup and panic handling, then atomically
seals admission before asynchronous restart handoff (ADR-278). A rejected
handoff reopens admission; accepted handoff keeps it closed until process exit.
Sealed routes return documented JSON 503 and explicit non-admission.

This is implemented local behavior with real local-action handler fixtures and
fake systemd handoff. It is not production restart evidence or fleet exclusion.
The separate-process result-save failure/restart contract remains qualified by
the [previous recovery report](../2026-10-01-restart/README.md).

## Snapshot and verification

[Source manifest](source-snapshot.json) binds changed implementation/architecture
files to base `e8be1842024c04bb52cf91f9103ca5cfa894827a`; canonical payload SHA-256
`d1a06d703072ca43bb01ba1e582e2f191393682a952bce483a03ce77f5a4de19`. Evidence and derived graph files are
excluded. [Complete Go identity](qualified-code.json) and [command outcomes](commands.json)
retain exact scope and the lint-only source variant. Private coverage, binaries
and full file manifest survive under
`/mnt/ssd/bt-checkpoints/20261001-async-drift`.

| Command / check | Outcome |
|---|---|
| Pre-fix detached sprint guard fixture | Red: HTTP finished while action remained owned; [red.log](red.log). |
| Four changed packages with `-short -count=1 -race` | Pass; [race.log](race.log). |
| Full short suite with race/atomic coverage, two package workers | Pass, 77.8%; [full-race.log](full-race.log). |
| Final API package with race | Pass after literal 503 → standard constant correction; [api-final.log](api-final.log). |
| Final `make build` | Vet/fmt/tidy, zero-issue lint, local high-security, thirteen binaries and AST-only graph pass; [build.log](build.log), [binary hashes](checked-binaries.json). |
| Documentation checker and drift | Eleven fixtures and structural/traceability checks pass; [docs.log](docs.log). |

The [initial build](build-before.log) failed lint because it required the standard
HTTP status constant. It is not counted as a passing gate. The full race run
began before that literal-equivalent correction; final API race/build checks
qualify the corrected file and its identity is explicit above. Check binaries
are dirty-worktree artifacts, not deployed clean-build evidence. Graphify updated
14,534 nodes/28,797 edges, AST-only with no model calls. Local high-severity
security does not establish GitHub's separate gosec result. The new PR head
must be observed separately; the previous e8be1842 head passed all actual checks.
Fixture checks invoke no model/coding provider.

## Remaining acceptance

`bt-agent` sibling restarts bypass the target dashboard admission gate;
agent/gardener retain snapshot checks. A target-process idle/seal handshake,
failure recovery and real bounded fleet handoff are P1 before enabling automatic
restart. Accepted but ineffective supervision leaves the seal closed until an
operator restarts. Uncooperative actions/long-lived requests defer adoption;
failed panic finalization may retain conservative running metadata. Pipeline
retention, operator reconciliation, power-loss/volume-loss and remaining transport
journals remain open.

C01–C05 and C07 complete; C06/C08–C12 partial; blocked none. The
[goal table/backlog](../../plans/2026-09-30-arc42-cleanup.md#stable-checkpoint-c01c12-status--2026-10-01)
retains scope. Prior real Codex delivery, deployed clean-build identity and actual
state backup/restore remain qualified at f60dcf42 in the
[operational checkpoint](../2026-10-01-checkpoint/README.md); this increment
has not been deployed and does not extend that operational qualification.
