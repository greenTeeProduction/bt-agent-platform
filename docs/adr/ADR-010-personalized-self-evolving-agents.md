# ADR-010: Personalized Self-Evolving Agents

**Context:** The platform evolves pre-authored trees well (gardener v2 with quality
gates), but it cannot *create* trees from user intent: generated trees are not
resolvable by `domains.ResolveTreeID`, crossover ignores parent structure, GOAP plans
are transient per-run state, and no per-user identity exists (memory, reflections,
and the experience bank are agent- or globally-scoped). The goal is an agent that
observes its user, derives goals, compiles GOAP plans into persistent behavior trees,
and evolves them from that user's feedback — closing the loop
`observe → goal → plan → tree → run → reflect → evolve`.
Full roadmap: `docs/plans/2026-07-08-personalized-self-evolving-agents.md`.

**Decision:**

1. **Executable-by-construction** — every generated tree must be resolvable
   (dynamic `ResolveTreeID` fallback hook loading `tree-<id>.json`), validated
   (`ValidateTreeFull`), benchmark-smoke-tested (`benchmark.QuickValidate`), and
   KG-registered before it exists.
2. **Persona layer** (`internal/persona`) — per-user profile, interaction log, and
   habit mining; all personalization state under `~/.go-bt-evolve/users/<user>/`
   with ADR-003 atomic writes.
3. **Goal Factory** (in `internal/goap`) — LLM structured extraction of `goap.Goal`
   grounded in a world-state vocabulary registry; recurring patterns become
   automation goals; the existing `GoalQueue` is activated per user.
4. **Plan→BT compiler / Tree Factory v2** — `goap.CompilePlanToTree` turns plans
   into persistent trees (precondition guards → registered actions or ChainActions
   → effect writes) inside the standard PreGate/Reflect scaffold, keeping a dynamic
   replan fallback branch; crossover splices real parent `SerializableNode` JSON.
5. **HITL automation policy** — auto-compiled automations enter the existing HITL
   queue; only on approval is an agent YAML with schedule written. Pattern threshold
   (≥3 occurrences) and a per-user cap on active auto-created agents prevent spam.
6. **Feedback-as-fitness** — a `user_satisfaction` dimension in `MultiFitness` fed
   by explicit (`bt_feedback`) and implicit signals; per-user gardener registries
   and experience banks evolve personal trees under the existing quality-gate,
   snapshot, and rollback rails.
7. **Reuse over rebuild** — activate the idle `goap.Agent`, `GoalQueue`,
   `BlackboardBridge`, `Goal.Deadline`, `HumanApprovalGate`, and `ExperienceBank`;
   unify `engine.PlannerNode` onto the `internal/goap` A* planner instead of
   maintaining two implementations.

**Status:** Accepted — implemented (Phases 0–6 core landed 2026-07-08; see the
plan for per-phase status). Phase 6 consolidation: `engine.PlannerNode` now
delegates to the `internal/goap` A* planner (single search implementation);
the gardener v1 `RunCycle`/`evolveTree` pipeline, unused `Config.TT`, and
`factory.SkillSpec` were removed, with the v1-only rails (evidence gate, bloat
cap, crisis detection) ported into `evolveTreeV2`.

**Consequences:**

- ✅ The agent grows with its user: recurring work becomes approved, scheduled,
  evolvable automations
- ✅ Closes the creation↔execution disconnect (risk R9) — generated trees actually run
- ✅ No new persistence technology: ADR-003 file layout extends to per-user workspaces
- ✅ Q3 reliability rails (quality gate, rollback, circuit breakers, budgets) apply to
  personal trees unchanged
- ⚠️ LLM goal extraction can produce unplannable goals — mitigated by world-state
  grounding + `ValidatePlan` with a capped repair loop
- ⚠️ Per-user gardener scans add cycle load — mitigated by round-robin budgets and
  the existing evidence gate
- ⚠️ New trees start without reflections — seeded with compile-time validation and
  first supervised runs so the evidence gate does not freeze them (risk R13)
