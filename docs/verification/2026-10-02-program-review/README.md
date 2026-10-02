# Program review and verified delivery — 2026-10-02

Passing a test before implementation cannot establish code delivery. The runtime
now holds repeated RED passes for review and stops dependent batching. New runs
capture exact program/milestone goals before implementation and record completion
only after actual Git/task delivery checks. Pending-delivery recovery repairs failed
program bookkeeping without repeating the implementation or changing its Git HEAD.

The source-audit finding is retained in `source-finding.json`. This is a code audit,
not model research qualification or a causal code-change experiment.

The compiled native MCP tool first reconciled a copy of the actual backlog, then
the host backlog with all three BT services inactive/disabled. Both runs corrected
60 unsupported labels, retained exact original-byte backups and preserved unrelated
milestones. A second call produced byte-identical state. Host totals are now 369
legacy unverified done, 44 blocked and 60 needs_review across 145 programs and 473
milestones. None of these records was upgraded to verified code delivery. The
backup and executable fingerprints are retained in the two native result reports.
The executable was built from this uncommitted increment; its digest identifies
the tested artifact, not a clean native VCS deployment attestation.

| Verification | Result |
|---|---|
| Research, engine and bt-agent race suites | Passed; engine 66.208 seconds, MCP 24.573 seconds |
| Changed-package lint | Zero issues |
| Documentation | Structure, local links, traceability and eleven checker regressions passed |
| Graphify AST refresh | 33,306 nodes, 43,223 edges across 1,269 files; no model calls |
| Real-model benchmark package | Passed in 160.748 seconds |
| Gardener real-model checks | Passed in 16.788 seconds |
| Factory, publication and personal automation | Passed in 65.723 seconds; measured 0% → 100% task passes, adoption and rollback |
| Native MCP backlog copy and host | 60 labels corrected, exact backup, repeat byte-identical |
| PR83 fetch | Latest af780cdbf4745432b4b3a2d890a9c9e88443237e already ancestor |

The live suite uses actual qualified Ollama inference and actual Sol fallback;
short/race protocol fixtures never establish model capability. Earlier compile,
fixture, MCP-count and lint failures remain alongside the successful checks.

All twelve arc42 views were reviewed: §§1,2,4 require no new contract, and §§3,5–12
plus API reference describe the interface, runtime, repair, quality boundaries and
remaining risks. The conventions review found no new higher-layer engine dependency,
new database or unbounded shared program transaction. The journal and backlog remain
separate metadata commits, coordinated by retryable reconciliation.

Effective model settings were independently checked using transient systemd
processes with the three services' ordered environment files. All select Codex
and gpt-6.1-sol; older inline provider/auto fields are overridden. No service
configuration was changed or service started.

Limits: corrected labels do not implement their goals. Anchored changed-file scope
is code attribution, not proof of full semantic fulfillment. Source identity through
rewritten programs, causal research benefit, broader task qualification and rollout
remain open. No production deployment is claimed.
