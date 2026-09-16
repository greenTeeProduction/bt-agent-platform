# 1. Introduction and Goals

## 1.1 Requirements Overview

The BT Agent Platform (`go-bt-evolve`) executes AI workflows as serializable
behavior trees. Operators invoke them through MCP, the dashboard, A2A, or
scheduled agents. The platform can generate and evolve trees and run a
separate research-to-code improvement pipeline.

**Business goals:** automate recurring development and operations work;
retain reviewable execution and change evidence; improve workflows without
silently accepting regressions; control the cost of local and metered model
calls. “Self-improving” describes a capability, not a promise that each run
lands a change. Model availability, quotas, verification and repository
preconditions can prevent implementation ([§6.4](06-runtime-view.md#64-self-improvement-cycle-goap-fusion-loop)).

| Requirement | Essential behavior | Architecture/evidence |
|---|---|---|
| FR1 | Build, validate and execute registered or persisted behavior trees; report a terminal outcome or an explicit approval/defer state. | [Engine](05-building-blocks.md#52-core-engine), [task runtime](06-runtime-view.md#61-task-execution-scenario) |
| FR2 | Run named agents on demand or on a persisted schedule; record history and retain failed work for inspection/replay. | [Agent service](05-building-blocks.md#51-whitebox-overall-system), [recovery](06-runtime-view.md#65-error-recovery) |
| FR3 | Research a change, create an isolated implementation worktree, collect RED/GREEN and verification evidence, and land only eligible changes. | [GOAP cycle](06-runtime-view.md#64-self-improvement-cycle-goap-fusion-loop), [delegation](../coding-delegation.md) |
| FR4 | Propose tree improvements from observed execution and evaluate them before adoption; retain rollback evidence. | [Evolution](05-building-blocks.md#53-evolution-engine), [remaining gate differences](11-risks-debt.md) |
| FR5 | Store per-user profiles/goals/trees, compile plans into executable trees, require approval for tracked automations, and use explicit feedback in evolution. | [Personalization](05-building-blocks.md#56-personalization-and-generated-trees), ADR-133 |
| FR6 | Expose authenticated operator controls and observable build/run state without treating the dashboard as an end-user identity service. | [Security](08-crosscutting-concepts.md#818-security-and-trust-boundaries), [deployment](07-deployment.md) |

The source registries, rather than copied counts, define the current node,
tree, package and MCP-tool inventories ([§5](05-building-blocks.md)). The
dashboard includes Overview, ThinkTank, Company, Tasks, Tree View, Evolution,
Agents, MindMap, Workflows, Scalability and DoorMate; navigation is defined by
[`static/index.html`](../../cmd/bt-dashboard/static/index.html).

**Implemented versus intended:** ADR-133's core personalization phases are
implemented. Personalization is still limited by the quality of observed
feedback, provider output and isolation at each caller. Implicit-feedback
learning and adoption-rate targets are not established production guarantees.
The [personalization plan](../plans/2026-07-08-personalized-self-evolving-agents.md)
is historical delivery context; current behavior is in §§5–8 and remaining
work in [§11](11-risks-debt.md).

## 1.2 Quality Goals

Stable goal IDs connect strategy (§4), acceptance scenarios (§10), and risks
(§11). The order below expresses architectural importance; numeric service
targets without measurements remain targets.

| # | Quality Goal | Scenario / acceptance criterion |
|---|---|---|
| Q1 | **Correctness and access control** | A valid tree runs the intended actions; malformed state cannot bypass validation. A protected HTTP request without valid credentials is rejected before execution. See QS5, QS20, QS22, QS26–QS28. |
| Q2 | **Evolvability** | A candidate is adopted only through its documented acceptance path; failed gates leave or restore a usable tree. Different paths and remaining gaps are explicit. See QS2, QS12, QS24–QS25 and R23. |
| Q3 | **Reliability and operability** | A failed, deferred or analysis-only cycle has a distinguishable outcome and evidence. Operators can compare running build identity with committed source and recover after restart. See QS1, QS6–QS8, QS19, QS21, QS29–QS32. |
| Q4 | **Personalization and consent** | A user-attributed lookup resolves that user's generated tree; pending, rejected or flagged tracked automations do not execute. See QS9–QS13. |
| Q5 | **Consistency and reuse** | Shared outcome, persistence and planning policies have identified owners; new capabilities reuse registered actions/blocks. Documentation traces claims to those owners. See QS14–QS17 and QS33. |

## 1.3 Stakeholders

Roles are used where no separate person or contact has been assigned. External
model services are dependencies in §3, not human stakeholders.

| Role | Contact / responsibility | Expectations |
|---|---|---|
| Platform owner and architect | Nico | Prioritized improvements, explicit costs and risks, reviewable decisions. |
| Maintainer / change reviewer | Nico; repository review workflow | Source-aligned architecture, reproducible checks, comprehensible boundaries. |
| Service operator | Nico; systemd user-service operation | Diagnose failed GOAP runs, preserve state, deploy and roll back known builds. |
| Dashboard / MCP operator | Authorized platform user; Hermes is an integration client | Correct results, working authentication, visible approvals and run evidence. |
| Persona owner | User identified by the calling integration | Isolated personal learning state and control over proposed automations. |
| Security / data custodian | Platform owner until separately assigned | Explicit network and filesystem trust boundaries, protected credentials, recovery responsibilities. |

---

*Generated by bt-agent arc42 pipeline — section1IntroGoals tree*
