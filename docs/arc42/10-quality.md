# 10. Quality Requirements

The goals in [§1.2](01-introduction-goals.md#12-quality-goals) are realized by
[§4](04-solution-strategy.md) and refined below. These are acceptance
contracts, with evidence and limitations stated separately. A test reference
identifies a reproducible check; it is not a production measurement.

## 10.1 Quality Tree

```text
BT Agent Platform
├── Q1 Correctness and access control
│   ├── Valid execution, routing and persisted state: QS5, QS20, QS22, QS23, QS26
│   └── HTTP authentication, bounded abuse controls and exposure: QS27, QS28, QS34
├── Q2 Evolvability
│   ├── Measured adoption and rollback: QS2, QS12, QS24
│   └── Evidence-directed search: QS25
├── Q3 Reliability and operability
│   ├── Recovery, dependency failure and persistence: QS1, QS6, QS7, QS8, QS19, QS32
│   ├── Responsiveness and detection: QS3, QS4, QS18, QS21, QS35
│   └── Provider, repository and release readiness: QS29, QS30, QS31
├── Q4 Personalization and consent
│   └── Scoped execution, habits, compilation and consent: QS9, QS10, QS11, QS13
└── Q5 Consistency and reuse
    └── Shared owners, reusable capabilities and traceable docs: QS14–QS17, QS33
```

## 10.2 Quality Scenarios

**Status:** *Tested contract* names automated behavioral evidence, sometimes
only for one path. *Partial* means implementation covers only part of the
acceptance criterion. *Target* is a proposed acceptance measure, not an
achieved SLO. Numeric targets retained from earlier specifications need an
operator-owned measurement before promotion to an operational guarantee.

| ID | Goal | Context and stimulus | Required response and measure | Evidence / status |
|---|---|---|---|---|
| QS1 | Q3 | A goroutine launched through SafeGo panics. | Recover at that boundary and invoke its configured error callback; the process survives. Retry, breaker updates and DLQ insertion occur only where the caller wires them. | Tested contract: [panic-handler tests](../../internal/reliability/panic_handler_test.go). No universal recovery-latency or automatic-DLQ guarantee; [§6.5](06-runtime-view.md#65-error-recovery). |
| QS2 | Q2 | An operator evaluates 100 evolution cycles against a fixed benchmark baseline. | Record before/after fitness for every accepted candidate; reject candidates outside the configured regression threshold and retain rollback evidence. | Partial: [gardener tests](../../internal/gardener/evolve_v2_test.go). The old “no drop >20%” is a benchmark target, not a measured fleet result; island-adoption gap R23 remains. |
| QS3 | Q3 | An authenticated client lists the full catalog with GET /api/trees on the deployed host. | Return the catalog and metadata; proposed p95 latency <500 ms over 100 requests at concurrency 1, with catalog size and hardware recorded. | Target: no current production latency series establishes this. Route and payload: [dashboard](../../cmd/bt-dashboard/main.go). |
| QS4 | Q3 | A committed regression becomes visible to the scheduled test watchdog. | Detect and report it within 4 hours, measured from commit to alert. | Target: check schedule, enabled state and recent successful history; a declared cron alone is insufficient. See [operations](07-deployment.md#73-release-recovery-and-operational-checks). |
| QS5 | Q1 | Three concurrent MCP tools mutate the server's shared blackboard. | Serialize each registered blackboard tool's write/run/read region; return each caller's own task/result without races or deadlock. | Tested contract: [server concurrency tests](../../internal/engine/mcp_server_test.go). Serialization does not guarantee all calls finish inside one tree's timeout. |
| QS6 | Q3 | Ollama is unreachable during a local-model request. | Surface provider failure or use a configured fallback; unrelated non-LLM operations remain usable. | Partial: [LLM adapters](../../internal/llm). Fallback also depends on its credentials/network; no “always available” provider is assumed. |
| QS7 | Q3 | A durable store write fails, including disk exhaustion. | Propagate the error and preserve the previous complete file on atomic-replace paths; no successful acknowledgement of a failed write. | Partial: [storage helpers](../../internal/reliability), [storage security tests](../../internal/agent/storage_security_test.go). Fleet-wide ENOSPC/crash injection is not established; JSONL appenders have different recovery semantics. |
| QS8 | Q3 | Configuration loading receives malformed or unsupported settings. | Report validation/load failure clearly; use defaults only for documented optional/missing settings, without silently widening permissions. | Tested contracts: [config tests](../../internal/config/config_test.go), [listener tests](../../internal/security/listener_test.go). Entrypoint response is part of its startup contract. |
| QS9 | Q4 | Two users have generated trees with the same local ID. | Resolve the requesting user's tree and block execution of disallowed tracked automations; never silently select another user's tree. | Tested contract: [resolver tests](../../internal/agentexec/wiring_test.go). The historic ≥90% end-to-end success goal remains an unmeasured target, separate from resolution correctness. |
| QS10 | Q4 | A user's interactions meet the configured recurrence count/window and confidence rules. | Emit a recurring pattern eligible for consideration; with default policy, create a proposal by the next eligible consideration pass. | Tested pattern detection: [persona tests](../../internal/persona/persona_test.go). “3 in 14 days” depends on miner configuration; next-session latency is a target, not a timer guarantee. |
| QS11 | Q4 | Goal factory produces a grounded plan for compilation. | Produce a resolvable tree with executable actions, precondition/effect guards and provenance; reject invalid plans before registration. | Tested contract: [compiler tests](../../internal/goap/compile_test.go). ≥80% first-compile benchmark success remains a workload target; compilation tests do not establish that rate. |
| QS12 | Q2/Q4 | A personal tree receives explicit satisfaction feedback across ten evolution cycles. | Apply its configured quality/evidence gates and preserve attribution; reject unacceptable fitness regressions. | Partial: [personalization integration](../../cmd/bt-agent/feedback_tools_test.go), [gardener](../../internal/gardener/evolve_v2.go). Non-decreasing satisfaction and a universal “floor 30 / 20%” are not established for every adoption path; R23. |
| QS13 | Q4 | Many patterns are considered, including pending/rejected/flagged automations. | Respect per-user caps and configured approval policy; execute zero disallowed tracked automations. Policy-approved auto-approval is permitted. | Tested contract: [autopilot tests](../../cmd/bt-agent/autopilot_test.go), [resolver tests](../../internal/agentexec/wiring_test.go). Untracked manual trees are outside this automation-status gate. |
| QS14 | Q5 | A change introduces a second owner for outcome, retry, planning or persistence policy. | Review identifies the owner in §5/§8 and consolidates or records a justified distinction before acceptance. | Target/process rule: [building blocks](05-building-blocks.md); no general automatic semantic-duplication detector is claimed. |
| QS15 | Q5 | At least two trees need a new capability. | Reuse or add a registered action/block with one documented implementation and tests. | Target/process rule: [block registry](../../internal/blocks), [engine registry](../../internal/engine/registry.go). A KG query is supporting evidence, not proof of uniqueness. |
| QS16 | Q5 | Factory/breeding proposes a tree similar to the catalog. | Inspect capability/structural similarity and either reuse, merge, or record a meaningful distinction before broad adoption. | Partial: [knowledge factory](../../internal/knowledge/factory.go). Universal creation blocking at one similarity threshold is not enforced. |
| QS17 | Q5 | A change duplicates existing Go logic. | Run configured lint/review gates and consolidate detected duplication before landing. | Partial: [pre-commit checks](../../scripts/git-hooks/pre-commit). Lint coverage does not imply zero semantic clones. |
| QS18 | Q3 | Concurrent workflow wrappers share one CompanyState. | Synchronize shared fields through the state's mutex and avoid holding a wrapper lock while re-entering orchestration. | Tested contract: [workflow tests](../../internal/dashboard/workflow_engine_test.go), race-enabled. |
| QS19 | Q3 | A genuine or evolved run updates KG feedback, followed by flush and reload. | Mark feedback dirty and preserve counters, structural fitness and recent-run data. | Tested contracts: [feedback tests](../../internal/knowledge/feedback_test.go), [snapshot tests](../../internal/knowledge/feedback_persist_test.go); ADR-105, ADR-235. |
| QS20 | Q1 | HITL mutation receives malformed JSON, or a response cannot be encoded. | Reject malformed input with 400; encoding failure produces one 500 response before success headers/body are written. | Tested contract: [dashboard tests](../../cmd/bt-dashboard/main_test.go). |
| QS21 | Q3 | A company-state reader runs while a sprint is waiting for tree execution. | Acquire/release the state lock within the test's 250 ms budget; tree work occurs outside the state lock. | Tested contract: [orchestrator tests](../../internal/startup/orchestrator_test.go). This is a lock-duration check, not a production HTTP-latency SLO. |
| QS22 | Q1 | CheckIndexInRange receives a JSON-decoded task batch. | Compare the index against the actual interface-slice length, without silently using an unrelated fallback size. | Tested contract: [condition tests](../../internal/engine/conditions_superpowers_test.go). |
| QS23 | Q1 | Alert/trading tasks include realistic phrases and words containing short indicator substrings. | Reach the declared alert/trading path; word-bound short indicators to avoid accidental substring matches. | Tested contracts: [domain tests](../../internal/domains/domains_test.go). |
| QS24 | Q2 | MCTS is enabled and structural strategy selects augmentation. | Merge its candidates with heuristic candidates into one scored competition and use the ordinary acceptance gates; search stays within configured iteration budget. | Tested contracts: [MCTS tests](../../internal/evolution/mcts_mutate_test.go), [gardener tests](../../internal/gardener/evolve_v2_test.go). Default budget 12 evaluations; reproducibility/provenance gap R20. |
| QS25 | Q2 | A specialist archetype with strong selector evidence is evaluated for speculative search. | Use the affinity threshold to select heuristic-only or augmented search; preserve deterministic decision logic for fixed inputs. | Tested contract: [strategy selector](../../internal/evolution). Non-archetype selection asymmetry remains R21. |
| QS26 | Q1/Q3 | A MemSelector reads a negative persisted cursor. | Restart from child zero without panic; out-of-range high cursors exhaust and clear normally. | Tested contract: [MemSelector tests](../../internal/engine/mem_selector_test.go). PersistentMemSequence and ForEachTask still need equivalent negative-cursor defenses (R22). |
| QS27 | Q1 | Browser starts without credentials, logs in, expires, or logs out. | Protected requests return 401 without valid key/session; successful login establishes an HttpOnly cookie; logout/expiry removes protected UI state. Session validation returns a detached snapshot. | Tested contracts: [HTTP security](../../cmd/bt-dashboard/security_test.go), [session regressions](../../internal/security/session_review_test.go), [browser flow](../../tests/e2e/auth.test.js). Shared operator credential is not user identity. |
| QS28 | Q1/Q3 | Session or rate-limit state reaches its configured capacity. | Reclaim expired sessions before refusing new ones; evict a cold client bucket to admit a new client while retaining active-client throttling. | Tested contracts: [session regressions](../../internal/security/session_review_test.go), [rate-limiter tests](../../internal/security/security_test.go). Current dashboard cap 100 sessions; bucket cap 10,000. |
| QS29 | Q3 | Selected coding CLI returns quota, authentication, unsupported-model, or cancellation errors. | With failover enabled, try at most one alternate on quota only; preserve read-only/write policy and separate cooldowns. Other failures remain explicit. | Tested contracts: [provider tests](../../internal/engine/superpowers_provider_test.go), [failover tests](../../internal/engine/superpowers_failover_contract_test.go), [delegation](../coding-delegation.md). Account/model availability requires a live probe; R27. |
| QS30 | Q3 | GOAP attempts implementation with a dirty/diverged checkout or missing runtime prerequisite. | Refuse unsafe implementation and retain a precise preflight/phase artifact. Report degraded/no-change/deferred distinctly from verified landed work. | Tested contract: [runtime preflight tests](../../internal/engine/superpowers_runtime_contract_test.go); [§6.4](06-runtime-view.md#64-self-improvement-cycle-goap-fusion-loop). Closed breaker alone is insufficient evidence. |
| QS31 | Q3 | A service binary differs from intended source, with work in flight. | Report drift; rebuild/restart only under configured policy, defer restart while guarded work runs, and preserve rollback binary. | Tested contracts: [deploy-drift tests](../../internal/agent/deploy_drift_test.go), [restart tests](../../internal/agent/deploy_drift_restart_test.go), [rebuild tests](../../internal/agent/rebuild_test.go). Current auto-rebuild/restart disabled; [§7.3](07-deployment.md#73-release-recovery-and-operational-checks). |
| QS32 | Q3 | The host/state volume is lost and recovery is attempted. | Restore a consistent backup and committed source; validate representative tasks and state. Record measured recovery time and data loss before agreeing RTO/RPO. | Target: no verified restore drill or numeric RTO/RPO is recorded (R26). [Recovery procedure](07-deployment.md#73-release-recovery-and-operational-checks). |
| QS33 | Q5 | A package, binary, section, ADR, quality goal or linked evidence changes. | Documentation checks reject missing inventory entries, broken local links/anchors, duplicate IDs and untraceable goals/scenarios. Review separately verifies behavioral claims. | Automated structural contract: [checker](../../scripts/check-arc42.py), [checker regression tests](../../scripts/test_check_arc42.py), [maintenance](README.md). |
| QS34 | Q1/Q3 | A service binds to a non-loopback address. | Require credentials where the listener helper applies; operator verifies intended reachability, public routes, firewall and TLS termination from the relevant network. | Partial: [listener tests](../../internal/security/listener_test.go); observed all-interface binds are recorded in §7. Remote isolation/TLS not verified (R25). |
| QS35 | Q3 | A task exhausts its configured execution budget. | Cooperative nodes stop on cancellation; subprocess workflows obey their separately configured phase/cycle budgets and persist partial evidence. | Partial: [RunTask](../../internal/engine/tree.go), [coding runtime](../../internal/engine/superpowers_task_executor.go). The default 120-second tree context is not a hard process-wide wall-clock guarantee (R30). |

The platform owner owns acceptance of targets; maintainers own regression
evidence and operators own deployed measurements. Record workload, commit,
configuration, host and date with measurements. Current test/file counts,
coverage percentages and benchmark scores belong in generated CI artifacts,
not copied into this specification.

---

*Generated by bt-agent arc42 pipeline — section10Quality tree*
