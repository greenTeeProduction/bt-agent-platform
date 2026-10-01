# 11. Risks and Technical Debt

Ordinary BT inference shares the Sol account/model dependency with no alternate fallback. NotebookLM generation/research, embeddings/indexing and legacy memory extraction retain their provider dependencies. Consumer Google sessions can still expire or be revoked despite keepalive; a saved browser profile can recover headlessly, while a fully revoked login still needs interactive sign-in. CLI output-token hints are not hard caps. See [model policy](../sol-model-policy.md).

Reviewed against source baseline `012612e1` and selected deployed settings
on 2026-09-16. Risks below describe remaining uncertainty, not a promise
that a documented mitigation has been implemented. “Mitigated” means the
named failure has a specific control; it does not establish zero residual risk.
Provider recovery and domain-catalog controls were also reviewed against the
2026-09-16 working tree; deployment observations retain the baseline above.

## Prioritized Risk Table

Priority combines impact and current evidence. The platform owner (Nico)
owns acceptance and prioritization; the role in each row owns the next step.
Stable R1–R24 IDs are retained, including the historical R14–R18
personalization renumbering. Every open risk has a closure criterion.

| ID | Priority / status | Risk and impact | Owner / next step and closure evidence |
|---|---|---|---|
| R25 | High / open | **Network exposure exceeds the former specification.** Dashboard, A2A and webhook listeners bind beyond localhost; firewall, proxy and TLS protection were not established by this review. Public diagnostic routes may reveal operational data. | Operator: verify intended network reachability and public-route policy, configure effective firewall/TLS boundaries, and record remote positive/negative probes (QS34, [§7](07-deployment.md)). |
| R26 | High / open | **Incomplete recovery qualification.** Offline actual-state hash restore and bounded dashboard reads passed on 2026-10-01; full loss recovery is unverified. Host/SSD loss can destroy file-backed agent, persona, research and evolution state. Atomic individual writes do not make a consistent multi-store backup. | Operator: retain the [bounded checkpoint evidence](../verification/2026-10-01-checkpoint/README.md), include external vault/worktrees/provider authentication, choose retention and RPO/RTO, and exercise full loss recovery/stateful flows (QS32). |
| R2 | High / partial | **Single-host availability.** The local daemon/SSD remains a shared failure domain. A2A AgentRouter/RemoteExecutor wiring exists, but no available peer fleet is established by this review. | Operator: exercise a real registered peer with failover/load shedding and measure recovery; until then operate as a single-host system (QS1, QS6). |
| R23 | Mitigated (2026-10-01) | **Island acceptance/rollback gap.** Whole-tree quick benchmarking, configured quality/meta-validation and predecessor snapshot now precede persistence; live assignment follows successful commit. | [Adoption regressions](../../internal/gardener/island_adoption_regression_test.go) reject meta failures, preserve the exact predecessor snapshot and leave live/disk state unchanged on snapshot/write failure. [Benchmark contract](../../internal/benchmark/candidate_validation_test.go) rejects regressed whole-tree execution. Quick evidence remains bounded; real workload quality is QS2/QS12 acceptance. |
| R27 | High / partial | **Coding-provider/account mismatch can stop every implementation attempt.** A CLI installed and authenticated for one model does not imply access to the source-default model; quotas and authentication fail independently. | Operator: retain the [failover preflight cleanup](../../internal/engine/superpowers_failover.go) and [expiry regressions](../../internal/engine/actions_superpowers_prod_test.go) (QS36), which cover clearing expired Claude/Codex retry state from shared files, agent blackboard and run state while preserving future deadlines. This establishes retry eligibility; provider success still requires probing the effective provider/model under the service account and a complete GOAP cycle with durable verification/landing artifacts. The [2026-10-01 host probe](../verification/2026-10-01-checkpoint/README.md) delivered an artifact through the real Codex adapter with private gpt-5.5; this account rejected gpt-6.1-sol, and host BT_LLM_PROVIDER=codex could not start the committed dashboard. Bounded read qualification used a temporary supported generic setting. As-is startup/full coding remain open. Opt-in quota failover does not fix auth or unsupported models (QS29; → [§8.19](08-crosscutting-concepts.md#819-coding-provider-policy)). |
| R28 | Medium / partial | **Repository preconditions and overlapping work can block GOAP.** Landing now serializes repository writes and preserves staged/unstaged edits; lint waits for competing checks. Separate runners can still select overlapping carryover goals. A coding-provider repair aimed at the production checkout is isolated, so its edits may remain in a retained clone instead of reaching the pending commit. | Maintainer/operator: retain [landing regressions](../../internal/engine/superpowers_main_preservation_test.go), inspect actual commit/patch evidence and reconcile duplicate work. Engine maintainer: extend carryover claim coordination and explicitly transfer and verify isolated repair patches before counting them as applied. The completed live landing `67bc9361` on 2026-09-16 demonstrates one delivered cycle, with operator assistance for the default-config test; it does not close these residual paths (QS30, [§6.4](06-runtime-view.md#64-self-improvement-cycle-goap-fusion-loop)). |
| R29 | Medium / open | **Persona identifiers are namespaces, not authentication.** Shared API credentials and caller-provided user IDs do not provide an independent tenant boundary. | Security maintainer: define trusted caller/identity propagation and authorize every user-scoped endpoint before supporting mutually untrusted users; add cross-user denial tests (QS9/QS27). |
| R13 | Medium / partial | **Stale binaries can continue serving after source changes.** Dashboard self-adoption now atomically owns detached work through cleanup and seals admission (ADR-278). New sibling paths require dashboard/gardener owner control and uncertainty seals (ADR-279); bt-agent self still samples scheduler state. Old controllers and full deployed handoff remain unqualified; current auto-rebuild/restart flags are disabled. | Operator: follow [release checks](07-deployment.md#73-release-recovery-and-operational-checks); compare intended revision with running process identity and exercise rollback (QS31). Do not infer running code from repository HEAD. |
| R20 | Medium / partial (2026-10-01) | **Complete evolution-cycle replay remains unqualified.** Accepted experience now retains generator, score/reason and MCTS seed/search configuration. Seeded concurrent proposal generation and complete deep-search replay/cache evidence are tested. | Evolution maintainer: retain [proposal ownership/replay tests](../../internal/evolution/proposal_replay_test.go) and [deep search evidence tests](../../internal/evaluator/deep_search_replay_test.go); reproduce a whole recorded cycle with matching tree, reflections, optional-pass state and evaluator before claiming production replay (QS24). |
| R24 | Medium / partial | **Evolution passes can be inert without visible failure.** Archive pointer aliasing is fixed by cloning in `recordDiversityObservation`; current live-driven reseed tests cover that defect. Metrics alone still do not prove that all optional passes contribute. | Evolution maintainer: retain [live-driven archive/reseed tests](../../internal/gardener/evolve_v2_test.go) and demonstrate meaningful production deltas for enabled passes before claiming benefit. |
| R30 | Medium / open | **Cancellation is cooperative.** Synchronous nodes and separately rooted coding-phase contexts can outlive the tree's default deadline; legacy blocking file locks are not uniformly time-bounded. HITL transaction mutex/file-lock contention and approval waits now obey caller deadlines or a 30-second default, with failure regressions (ADR-263); arbitrary filesystem I/O remains cooperative. Dashboard admission/execution now inherits HTTP cancellation; reservations remain owned until actual cleanup, and command groups have cancellation cleanup (ADR-266). Router/retry/workflow now stop completed or uncertain execution diagnostics, including lost remote responses (ADR-267). A2A whole-budget polling and context-aware auction delegation now preserve task ownership; explicit SDK cancellation stops a cooperative tree, and shared BT stops retain completed/unknown diagnostics across retry/fallback (ADR-268). HTTP completion does not cancel an asynchronous task. SDK task state remains process-local. Typed known-failure/pause evidence now crosses integer BT boundaries, and actual admitted parallel faults cannot be hidden by a wait; deferred SLO/completion diagnostics are tested (ADR-269). Local workflow approvals now stop every container; full loop/subworkflow bodies retain nested evidence, and completed prefixes stop automatic whole-group replay. Creation inherits caller budgets and HTTP/browser waits remain distinguishable (ADR-270). Workflow blackboard input/output writes now report failures; staged cache commit and caller-bounded scope/sidecar admission prevent false acknowledgement and automatic replay of admitted work (ADR-271). Default blackboard initialization now fails admission without a memory substitute; concurrent/loaded ownership and grouped healthy-run promotion acknowledgement are tested, including recorded diagnostic/replay protection (ADR-274). Sprint task-result commit/repair and failed async observations now preserve completed evidence without execution replay (ADR-275); diagnostics remain process-local, shared sprint capacity now reserves before claims, request-bounded admission and detached five-minute batch contexts retain ownership through actual cleanup, and proven unstarted claims have atomic bounded return/repair (ADR-276). Operator timeout configuration, fleet-wide capacity and restart-safe ownership remain unqualified. Unproven side effects in ordinary failing actions, remaining fleet persistence/cancellation paths, uncooperative actions, durable request identity/idempotency, restart-safe resumption and operator reconciliation still need qualification. | Engine/reliability maintainer: document budget ownership, propagate cancellation where required, inject stuck dependencies and measure bounded termination without losing partial evidence (QS35). Conservative restart holds and separate-process scheduler/sprint fixtures now cover interrupted claims (ADR-277); fleet stale writers, volume loss and authenticated reconciliation remain open. |
| R3 | Medium / open | **Unused-code uncertainty.** Graph isolation identifies review candidates, not proven dead code; callbacks, registration and tests may create implicit edges. | Maintainer: combine graph results with call/registration evidence and tests before removal; close individual candidates with proof, not a target deletion count. |
| R4 | Medium / open | **Boundary complexity.** Engine/entrypoint packages carry many responsibilities and callback dependencies. Package counts alone do not measure cohesion. | Architect: use [§5](05-building-blocks.md) to identify a concrete responsibility split, demonstrate reduced coupling and preserve interfaces; no arbitrary target package count. |
| R6 | Medium / partial | **Protocol security can diverge.** MCP stdio and HTTP/A2A have intentionally different authentication envelopes; shared helpers alone do not ensure equivalent enforcement. | Security maintainer: keep transport-specific threat assumptions explicit and test entrypoint wiring against [§8.18](08-crosscutting-concepts.md#818-security-and-trust-boundaries). |
| R9 | Medium / open | **Research corpus quality is unmeasured.** Stale or irrelevant NotebookLM sources can bias goals and reviews. Historic corpus counts are not current evidence. | Research maintainer: sample citations for relevance, track curated provenance and review quality before accepting research-derived changes. |
| R10 | Medium / open | **Self-improvement can optimize its own machinery without user value.** Repeated analysis or pipeline-only changes need not advance platform goals. | Platform owner: track delivered capability and user-facing evidence by goal/program across cycles; inspect research-only/degraded outcomes separately (QS30). |
| R7 | Low / open | **External and local LLM availability.** DeepSeek and other configured APIs can fail; local Ollama is also a fallible dependency. | Operator: test the configured fallback/error path and non-LLM operations during outages; do not assume unlimited/free fallback (QS6). |
| R8 | Low / open | **Overlapping evolution algorithms increase complexity.** Availability as a library/tool does not prove useful participation in the gardener. | Evolution maintainer: use the [adoption-path matrix](08-crosscutting-concepts.md#85-evolution-pipeline); remove or justify redundant passes with measured results. |
| R21 | Low / open | **Speculative search defaults on for most trees.** Non-archetype trees cannot fall below the current affinity threshold, adding evaluations and potentially changing which mutation wins. | Evolution maintainer: compare cost/fitness with search disabled, then adjust strategy only with reproducible benchmark evidence (QS24/QS25). |
| R22 | Resolved (2026-10-01) | **Persisted cursor bounds.** Sequence/task loops restart invalid negative/high/JSON cursors at zero; exact sequence completion is supported. | Preserve [cursor regressions](../../internal/engine/cursor_regression_test.go) and MemSelector coverage (QS26). |
| R11 | Low / open | **Dormant scaffolding may fail when first wired.** Unit-tested components are not proof of production reachability or integration. | Maintainer: add a representative runtime scenario and evidence at the entrypoint before marking a feature implemented. |
| R12 | Low / open | **Worktree, artifact and DLQ growth.** Durable DLQ recovery claims cannot be expired, purged or evicted without an explicit outcome decision (ADR-280). Retained failure evidence competes with finite disk capacity; premature cleanup can destroy diagnosis data. | Operator: measure usage, set retention per artifact type and test recovery before pruning; preserve referenced run evidence. |
| R5 | Low / partial | **HTTP/browser coverage remains selective.** Login/session/logout and browser expiry now have behavioral tests; pipeline rooted selection/inventory and enforced authentication now have real handler coverage (ADR-272); other management routes and real network deployment still need risk-based coverage. | Dashboard maintainer: map remaining state-changing routes to behavior tests and negative authorization cases ([current tests](../../cmd/bt-dashboard/security_test.go), QS27/QS34). |
| R1 | High / reopened (2026-10-01) | **Persisted tree collapse and proxy fitness.** The host audit found 52 of 53 saved trees reduced to recovery-only skeletons. Offline repair now restored 51 authored definitions and quarantined one retired tree, with exact backups and fresh registry hash verification (ADR-283). Governance scoring and qualified publication address incentives; representative task coverage, historical learning cleanup and deployed impact remain open. | Evolution maintainer: preserve backups, recover executable task trees, qualify versioned adoption/rollback and retain task outcomes; see [§8.5](08-crosscutting-concepts.md#85-evolution-pipeline), R20/R23. |
| R14 | Mitigated | **Generated trees were not executable.** Scoped persisted-tree resolution now connects creation to execution. | [Resolver tests](../../internal/agentexec/wiring_test.go); identity residual is R29, not missing resolution. |
| R15 | Mitigated | **Breeding ignored parent structure.** Structural crossover now consumes parent trees. | [Structural factory tests](../../internal/knowledge/factory_structural_test.go); end-to-end quality remains workload-dependent. |
| R16 | Mitigated | **Duplicate GOAP planning/transient plans.** Canonical planner, durable goal queue and plan compiler are integrated. | [GOAP](../../internal/goap), ADR-133; keep engine adapters thin. |
| R17 | Mitigated | **No user-scoped learning state.** Persona/workspace hooks provide per-user state and feedback. | [Persona](../../internal/persona); this is attribution/isolation support, not account authentication (R29). |
| R18 | Mitigated | **New trees could not pass the evidence prerequisite.** Bootstrap/compiled-tree evidence paths support initial evolution. | ADR-133 and [gardener implementation](../../internal/gardener/evolve_v2.go); no universal success-rate claim. |
| R19 | Mitigated | **Unkeyed A2A card signatures.** Configured HMAC signing/verification protects origin assertions where the trusted key policy is used. | [Signing implementation](../../internal/a2a/signing.go); key distribution and unsigned-card policy remain operator responsibilities. |

## Known Technical Debt

The component maintainer owns debt follow-up unless another role is named.
Resolved source debts remain below active priorities for regression tracking.

| ID | Priority | Item / status | Resolution or next acceptance step |
|---|---|---|---|
| D1 | Medium | **Partial (2026-10-01):** parameterized dashboard response validation | RouteIndex now matches nonempty parameter segments with exact-route precedence. HITL GET and approve/reject/escalate POST templates have authenticated schemas and [matcher regressions](../../internal/api/route_template_regression_test.go); [real handler tests](../../internal/dashboard/hitl_schema_regression_test.go) exercise enforced success and not-found responses. Pipeline selection now rejects traversal and escaping symlinks; actual authenticated inventory/run HTTP cases are tested. Protected route definitions carry 401/403 schemas and validation no longer borrows success schemas for undocumented errors (ADR-272). Tree inspection now preserves qualified IDs, returns actual IR under enforced root/child schema and rejects metadata-only substitutions; browser text/duplicate-branch regressions cover the consuming view (ADR-273). Security-audit and live-metric dynamic maps now have declared-property/actual authenticated response regressions, including enabled empty audit arrays and idle/running/finished sprint status fields (QS38). Audit additional subpath handlers, schema constraint facets, recursive IR validation and undocumented statuses before claiming universal schema enforcement. |
| D4 | Medium | **Open:** per-node token-budget adequacy | Audit representative workloads and truncation/quality evidence before changing budgets; no fleet-wide minimum has been proved. |
| D5 | Medium | **Ongoing:** documentation drift | Structural checks now cover links, inventories and goal/scenario/ADR integrity; runtime assertions still need human/source review (QS33). |
| D6 | Medium | **Partial:** gate, catalog and observability consistency | Engine/domain maintainers: preserve fixed feedback flushing, incremental verification artifacts and ordinary mutation gates. [Domain regressions](../../internal/domains/domains_test.go) now cover registered `domain:<name>` resolution, construction, smoke-task presence, canonical descriptions and condition/guard metadata (→ [§8.14](08-crosscutting-concepts.md#814-fleet-wide-node-description-coverage)). Structural coverage can still miss wrong branch selection or failed live dependencies; close that gap with true/false guard cases and representative runtime scenarios. Island adoption and evolutionary attribution remain R23/R20/R24. |
| D3 | Low | **Clarified:** two factories share a name | `internal/factory` compiles skills; `knowledge.Factory` breeds/discovers trees. They are separate responsibilities, described in §5 and §12; renaming is optional. |
| D2 | None (resolved) | **Resolved in current source:** skill-compiler routing gap | [Generator](../../internal/factory/generator.go) sets model routing with an executable fallback; [regression contract](../../internal/factory/factory_test.go) includes `TestGenerator_StrategyRouterIsModelRouted`. Retire the old “no route writer” open item. |
| D7 | None (resolved) | **Resolved (2026-09-16):** health toolchain identity | `HealthJSON` now derives `go_version` from `runtime.Version()` in [metrics_utils.go](../../internal/dashboard/metrics_utils.go), with a regression in [metrics_test.go](../../internal/dashboard/metrics_test.go). Operator: verify the deployed response against executable metadata after upgrading the service. |

Historic utility extraction, default-tree splitting, old scaffold activation
and individual bug-fix narratives remain in [the ADR log](09-decisions.md).
Their old test counts, line counts and coverage numbers are not current
architectural constraints.


The on-demand response factory now preserves tasks, requires declared output
contracts, validates before publication, and prevents unresolved factory IDs
from running a default tree. This closes the generic-prompt/tiny-budget and
register-before-save paths for the MCP factory. It does not repair the personal
GOAP compiler's assumed external effects, qualify legacy structural breeding,
or demonstrate deployed versioned evolution. Shared discovery is in-process;
personal task text is intentionally not indexed in the shared graph.
DLQ recovery advances C09 without closing R30: independent-process and sibling
fixtures prove conservative replay fencing while state is present. Mixed old
writers can erase claims, and the trusted Go resolution API does not establish
remote owner quiescence or provide an authenticated recovery transport. Stop/
drain rollout, operator evidence handling, full transport journals and
power/volume-loss qualification remain prioritized acceptance.


Versioned gardener promotion and rollback now have a real-model controlled
response-task cycle (ADR-281). MCP manual/genetic-family and selector publication
now share that qualification boundary (ADR-282). This narrows R23 but does not
close it: first-adoption races with legacy file writers, unmanaged resolve-time
ordering, stale lineage-skip/archive estimates, domain capability fixtures and
deployed adoption remain unqualified. The initial tiny benchmark model failed
later trials; the stronger default passed a bounded probe, not a broad assistant
evaluation. Managed `bt_reset` semantics and personal genetic evolution still
need explicit workflows. Search ranking
still uses historical/proxy estimates before the final measured publication
gate. The persisted-tree repair is complete for the observed 52 collapsed files
(51 restored, one retired). Representative task validation, historical learning
cleanup and personal GOAP effect compilation still require work.

Automation approval now reserves before publication, verifies the task/schedule
and existing definition, surfaces commit errors, denies unreadable/contradictory
ledgers and keeps personal descriptions out of shared discovery (ADR-284).
ADR-285 replaces autopilot administrative plans with exact governed task reuse,
adds immutable version consent and missing-ledger holds for marked definitions,
and verifies one real file-task fixture including rejection without overwrite.
General GOAP effect assertions, additional external capabilities, dynamic semantic
oracles, file-task evolution corpora, actual cron dispatch, incomplete-reservation
recovery and stale feedback-review IDs remain open. These changes are not deployed
and do not qualify broad personal assistant behavior.

Typed checkpoint/source mismatches, retry state loss across ticks and standalone
GOAP agent prediction-to-observation conflation are repaired by ADR-286. The
compiled/dynamic model paths still assert planned effects without independent
observation, so matching GOAP state is not sufficient proof of external impact.
Persisted legacy wrappers require reviewed regeneration/version adoption; generic
unreceipted external effects and cross-process retry recovery remain unqualified.

Research attribution is partial after ADR-287: current research goals can retain
source-to-delivery receipts, false legacy/RED-pass delivery credit is removed, and
pending receipt repair precedes new planning. ADR-288 links clean native builds,
exact executed trees and recomputable result contracts where evidence exists.
This observes code presence and checked output, not execution of changed functions
or a causal research benefit. Dirty/unknown builds and changed delivered files
cannot earn adoption credit; stored metadata assumes trusted local build/storage.
Paired code-change experiments, semantic goal fulfillment, program lineage,
legacy milestone RED-precheck completion and historical backfill remain open.
Exact matching intentionally leaves rewritten/automatically scoped goals unlinked.
No production deployment is claimed by local protocol or live model tests.

---

*Generated by bt-agent arc42 pipeline — section11Risks tree*
