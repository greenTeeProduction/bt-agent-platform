# BT platform architecture

This is the current architecture specification for the BT Agent Platform.
The 2026-09-16 alignment review used source baseline `012612e1` and a
separately identified snapshot of the deployed host. It corrected stale
personalization, provider, authentication, persistence, deployment and
failure-recovery descriptions. It also replaced repeated change histories
with current views and explicit acceptance evidence.

| Section | Read it for |
|---|---|
| [1. Introduction and goals](01-introduction-goals.md) | Capabilities, stakeholders and the five quality goals |
| [2. Constraints](02-constraints.md) | Imposed environment/process constraints and conventions |
| [3. Context and scope](03-context-scope.md) | External partners, interfaces and system boundary |
| [4. Solution strategy](04-solution-strategy.md) | How each quality goal shapes the architecture |
| [5. Building blocks](05-building-blocks.md) | Package/binary responsibilities and important internals |
| [6. Runtime view](06-runtime-view.md) | Execution, GOAP, failure, login and personalization scenarios |
| [7. Deployment](07-deployment.md) | Defaults versus observed host settings, release and recovery |
| [8. Crosscutting concepts](08-crosscutting-concepts.md) | Shared execution, persistence, security and provider policies |
| [9. Decisions](09-decisions.md) | Historical rationale, alternatives and consequences |
| [10. Quality requirements](10-quality.md) | Goal-linked, measurable scenarios with evidence/status |
| [11. Risks and debt](11-risks-debt.md) | Prioritized unresolved gaps, owners and closure criteria |
| [12. Glossary](12-glossary.md) | Canonical meanings used throughout the specification |

## How to interpret claims

**Implemented** means source and entrypoint wiring establish the stated
behavior. A **tested contract** names reproducible regression evidence for
that behavior; it is not a production SLO. **Observed deployment** describes
a dated host/configuration snapshot. A **target** is an acceptance criterion
still needing evidence. **Open/partial risks** explicitly identify gaps;
describing the desired response does not implement it.

Current behavior belongs in §§3–8, measurable acceptance in §10, and remaining
work in §11. Historical ADRs preserve the facts and limitations known at
their recorded dates; later decisions or current source can supersede them.
An Accepted label does not close all later risks. New ADRs record significant
choices with rationale, alternatives, consequences and evidence, not every
routine bug fix.

The review found several implementation/operations gaps that remain open:

unverified network isolation/TLS and backup recovery, incomplete evolution
provenance and cooperative cancellation. The 2026-10-01 cleanup adds cursor bounds, shared feedback transactions,
validated commit and proposal replay controls (ADR-262), recoverable approval
commit/admission and nested gate isolation (ADR-263), serialized snapshot revision
commit and orphan preservation (ADR-264), configured owner initialization
(ADR-265), context-aware dashboard admission and execution ownership
(ADR-266), terminal distributed execution diagnostics (ADR-267), A2A execution ownership
and tree-level replay stops (ADR-268), typed known-stop dispositions
and admitted parallel failure evidence (ADR-269), shared workflow consent/control
and completed-prefix replay protection (ADR-270), commit-before-cache blackboard
and workflow metadata acknowledgement (ADR-271), rooted pipeline selection
and status-specific response validation (ADR-272), exact tree inspection and
safe presentation (ADR-273), blackboard owner admission and atomic run promotion
(ADR-274), sprint task-result acknowledgement and metadata-only repair
(ADR-275), shared sprint capacity and owned batch budgets
(ADR-276), conservative process-restart recovery holds
(ADR-277), island acceptance
and the Codex-only policy (ADR-261). The hardcoded health toolchain
field was corrected on 2026-09-16 (D7).
Provider/model readiness and clean-repository preconditions also require
operational verification; a closed GOAP breaker does not prove a successful
code delivery. Their acceptance steps are in §11.

## Maintenance and verification

1. Read the repository's Graphify report before source exploration; prefer
   its wiki/query interfaces when available. Keep source, configuration and
   observations distinct.
2. Update affected current views together: a new interface needs §3/§5, its
   important/error flow needs §6, and deployment/security implications need
   §7/§8. Connect quality goals → strategy → scenarios → evidence/risks.
3. Preserve existing Q, QS, R and ADR identifiers. Use stable ADR anchors;
   the two historic ADR-024 records have distinct anchors. Reconstructed
   records must state provenance and avoid invented dates.
4. Keep the required headings and the generated footer as the last line of
   every numbered section. This is compatibility with the automated sync
   pipeline, not a claim that all prose was generated. Preserve the
   three-column `| Qn | **Name** | criterion |` goal table: the GOAP goal
   loader reads it.
5. Run the checks below. For code changes, update the Graphify graph with
   `graphify update .`. Review behavioral claims against implementation and
   meaningful tests; a clean link checker cannot prove runtime behavior.

From the repository root, with the configured Go toolchain on PATH:

```bash
python3 -B scripts/test_check_arc42.py
bash scripts/check-doc-drift.sh
BT_SKIP_LLM_TESTS=1 go test -short -count=1 ./internal/engine -run Arc42
```

The drift gate checks required section structure, local Markdown links and
anchors, package/binary inventory, quality-goal/scenario references, risk IDs,
and every ADR index/record association. It covers this directory, the repository README and the
linked coding-delegation runbook plus the setup/API/tutorial/troubleshooting guides, not arbitrary prose claims or the entire
repository's historical plan archive. The Python checker requires Python 3
standard library only. Regression tests deliberately break references,
inventories and traceability to ensure the gate fails.

[Per-section guidelines](GUIDELINES.md) are consumed by automatic section
updates. Those updates are best-effort; the independent drift gate must pass
before the resulting documentation is accepted.

## arc42 basis

The views follow the [official twelve-section template](https://arc42.org/overview/).
The review applies its guidance for
[representative runtime scenarios](https://docs.arc42.org/section-6/),
[decision records](https://docs.arc42.org/section-9/),
[measurable quality scenarios](https://docs.arc42.org/section-10/) and
[prioritized risks](https://docs.arc42.org/section-11/).
