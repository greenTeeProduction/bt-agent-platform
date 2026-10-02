# Runtime release, real cron task and native rebuild provenance

On 2026-10-02 the canonical host adopted clean native release
`b615595b9cb55cdadf6664a8c9d306602abf60d6`, including PR83 head
`af780cdbf4745432b4b3a2d890a9c9e88443237e`. All fourteen executable outputs were
built out of place, backed up and atomically installed while all three BT
services were stopped. Resolved state directories were archived separately;
archiving only the home symlinks would not have backed up their data.

## Observed acceptance

- `service-smoke.json` records actual running executable hashes, native clean
  revision and allowlisted effective Sol 6.1 settings for agent, dashboard and
  gardener. Protected routes returned 401 without credentials and 200 with them.
- `enabled-services.json` records all three services active and enabled.
- `cron-proof.json` records an actual daemon wall-clock dispatch, not RunNow.
  The on-demand factory created a scoped file task from observed service data.
  Sol produced the exact independently specified service-count/revision JSON;
  file readback, output digest, two result checks, native build identity, exact
  tree version and terminal history were retained. The host's existing policy
  auto-approved the automation. An initial verification script mistakenly tried
  manual approval after that; it did not rerun or duplicate the task. The observer
  retained the original successful run, then returned its schedule to on-demand.
- `canonical-source.json` records the local master fast-forward because the
  coding loop checks out master. `legacy-cli-alias.json` records backup of an old
  root CLI and its replacement with a symlink to `bin/bt-agent-cli`.

## Rebuild defect and correction

Actual compiler/Git tests failed on the previous linked-worktree rebuild: the
installed Go toolchain omitted native revision/time metadata. A display ldflag
cannot qualify research adoption. Rebuild now captures HEAD, uses a private
ordinary shared clone, scrubs inherited Git variables for checkout and compiler,
and verifies clean native module/revision/time before swapping any executable.
Uncommitted source files and worktree registrations remain untouched. The CLI
rebuild target now matches the installed `bin/` path.

`native-red.txt` retains the failing real builds; `native-focused.txt` retains
passing builds across ordinary, bare and linked source repositories, poisoned
Git environment, and rejected dirty output without overwriting the live binary.
The broader initial race run found an old CLI path expectation; `initial-race.txt`
retains that failure and passing util/agent/dashboard/gardener command suites.
After updating that expectation, `agent-race-passed.txt` passes. `initial-lint.txt`
retains a preallocation finding; `lint-passed.txt` reports zero issues after repair.
These are real native builds and protocol tests, not LLM benchmark substitutes.

## Limits and next acceptance

The b615 release and one scoped personal file task are deployed evidence. The
new rebuild guard is source/test evidence until separately installed. This does
not establish a research-caused improvement, broad assistant competence, a
representative file-task evolution corpus, or safe autonomous fleet handoff.
Automatic rebuild/restart stay disabled: bt-agent's self path still lacks
process-wide admission ownership. An active scheduled coding cycle must drain
before manual deployment. Existing source/state/backups and uncertain execution
claims are retained; no legacy DLQ entries were replayed or discarded.

All twelve arc42 sections were considered: current runtime/deployment/quality/
risk views and the decision log changed; goals, constraints, external interfaces,
strategy, package ownership, persistence semantics and glossary are unchanged.
The change adds only agent → util ownership, no engine import or new database.

Upstream subsequently merged PR83 as `06fd71c350183cf7734f250aa003918bb1f2403b`.
Its merge tree adds no code differences to the already integrated review branch;
the upstream ancestry is retained alongside the native rebuild correction.
`implementation-commit-checks.txt` records full normal vet, lint, tidy, documentation,
44 CI Doctor and short-suite acceptance for implementation commit `5cfeb412`.
