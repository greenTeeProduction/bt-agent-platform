# ADR-008: Composable Behavior-Tree Building Blocks

## Status

Accepted (2026-06-04)

## Context

The BT Agent Platform grew from monolithic domain trees toward reusable, evolvable units. Agents need consistent pre-gates, tool execution, quality checks, and human oversight without duplicating large JSON trees.

## Decision

1. **Building blocks** live in `internal/blocks` with stable IDs (`core:pre_gate`, `core:quality_gate`, …).
2. **Composition** uses `SubTreeRef` nodes expanded at build time via `blocks.Expand` registered on `engine.RegisterTreeExpander`.
3. **Domain trees** remain for specialized routing; composed presets (`agentic`, `full`, `hitl`) provide default task pipelines.
4. **HITL** gates (`HumanApprovalGate`, tiered/post-review blocks) fail closed when approval is pending.
5. **Evolution** may mutate only `Mutable` custom blocks; `core:*` builtins are frozen unless promoted to `custom:*_vN`.
6. **Fitness** is recorded per `block_id` metadata (`bt_block_fitness_score`) for gardener ordering.
7. **Ops blocks** (`trace_checkpoint`, `audit_log`, `dlq_escalate`) integrate tracing, JSONL audit, and DLQ without MCP-only paths.

## Consequences

- Positive: Smaller diffs, shared reliability wrappers, MCP `bt_blocks_*` tooling, dashboard HITL API.
- Negative: Expand step required before `BuildTree`; unknown `SubTreeRef` fails at runtime if not expanded.
- Migration: Domain trees can adopt composed presets incrementally; expert archetypes reference block IDs.

## References

- `.hermes/plans/2026-06-04-agentic-blocks-master-roadmap.md`
- Phases 0–5 implementation plans under `.hermes/plans/`
