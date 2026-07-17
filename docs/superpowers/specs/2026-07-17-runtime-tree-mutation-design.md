# Runtime Tree Mutation — Design

Date: 2026-07-17
Status: approved (brainstorming session)

## Goal

Make the BT engine dynamic: nodes can be **added or removed while a tree is being
processed**, taking effect within the same run. One mechanism serves two entry
points — action nodes mutating their own tree (generalizing the ad-hoc
`ClaudeErrorHandler` graft) and external callers (MCP) editing a live run.

## Decisions (from brainstorming)

| Question | Decision |
|----------|----------|
| Who mutates | Both tree self-mutation and external callers, one shared mechanism |
| Persistence | Per-op flag: ephemeral (default) or persisted back to the stored tree |
| Scope | Any node, guarded (validation gate, root protected, reject-don't-crash) |
| In-flight state | Carried over by node correspondence; mutated parents get cursor arithmetic |
| Approach | Copy-on-write rebuild of the compiled tree at tick boundaries |

## Background constraints

- Trees are `evolution.SerializableNode` (JSON) compiled by `BuildTree` into
  immutable go-bt `Command[Blackboard]` closures; composites hold private child
  slices fixed at construction. The serializable tree is the source of truth;
  the compiled tree is derived.
- `RunTask` owns a tick loop (≤1000 ticks, tree timeout). `Parallel` /
  `ReactiveParallel` goroutines join within their parent's tick, so **between
  `tree.Run()` calls nothing is executing** — a safe quiescent point.
- Library node state (MemSequence cursor, Repeat/Retry counters, Timeout/Sleep
  stamps) is keyed by compiled-command **pointer** in `BTContext` maps; it does
  not survive a naive rebuild. Engine-own state (`PersistentMemSequence`,
  `MemSelector`) is name-keyed in `ChainState` and survives rebuilds.
- Only memory-node names are validated unique (`validate.go`), so general nodes
  cannot be addressed by name.
- Engine must not import higher-level packages — cross-layer calls use
  nil-checked injection-hook vars wired from `cmd/bt-agent`.
- Persistence is atomic JSON under `~/.go-bt-evolve/` (ADR-003).

## Architecture

New entry point:

```go
// LiveRunInfo names the run for registry listing and persistence.
type LiveRunInfo struct{ Agent, TreeID string }

func RunTaskMutable(bb *Blackboard, serTree *evolution.SerializableNode, info LiveRunInfo) (string, error)
```

`internal/agent/runner.go` switches from `BuildAndValidate` + `RunTask` to this.
It validates and builds exactly as today, registers the run in a process-wide
**live-run registry** (`sync.Map` keyed by `bb.RunID`; a RunID is generated if
the caller disabled blackboard management), runs the tick loop, and deregisters
on completion (deferred). `RunTask` stays unchanged for callers with pre-built
trees (tests, gardener sandbox scoring) — those runs are not mutable.

**Tick-boundary apply:** before the first tick and between subsequent ticks the
loop drains the run's thread-safe mutation queue and applies pending ops
copy-on-write:

1. Deep-copy the current serializable tree (the copy walk records an
   old-node → new-node correspondence map).
2. Apply each op to the copy **individually**: resolve path (+ `ExpectName`),
   apply structurally, run `ValidateTree` on the result, run the llm-origin
   allowlist when applicable. A failing op is rolled back and journaled as
   rejected; remaining ops still apply.
3. If any op survived: rebuild the compiled tree, migrate node state (below),
   atomically swap both trees, bump the run's mutation generation.
4. Journal per-op results; log via the run-scoped logger; publish
   `tree_mutated` on `bb.EventBus` when present; record via `internal/audit`.
5. For ops with `Persist: true`, invoke the persistence hook (below).

Since the apply point is quiescent, the tree itself needs no lock. The queue,
the journal, and the generation counter are mutex-guarded — MCP handlers read
them from other goroutines. Each apply is bounded: builds are closure
construction over small trees.

## Mutation API

```go
type MutationOp struct {
    Kind       string                      // "add" | "remove"
    ParentPath string                      // add: index path of parent ("" = root, "0.2" = root.Children[0].Children[2])
    Index      int                         // add: insertion position; -1 = append
    Path       string                      // remove: index path of the node to remove
    ExpectName string                      // optional: resolved node's Name must match, else reject
    Subtree    *evolution.SerializableNode // add: subtree to graft
    Persist    bool                        // snapshot resulting tree to the store
    Origin     string                      // "operator" | "tree" | "llm"
}
```

Addressing is by **index path** resolved against the current serializable tree
at apply time. Within a batch, ops apply sequentially and later ops see earlier
index shifts. `ExpectName` guards against stale paths.

Entry points feeding the same queue:

- **Self-mutation:** `bb.EnqueueMutation(op)` — callable from any action
  implementation during a tick; effective at the next tick boundary.
  `ClaudeErrorHandler` keeps its existing closure-local mechanism; migrating it
  is out of scope.
- **External (MCP tools on the bt-agent server):**
  - `bt_live_runs` — list registered live runs (RunID, agent, tree, generation).
  - `bt_live_mutate` — enqueue one op by RunID; fire-and-forget, returns an op
    ID (tick boundaries can be minutes away during long leaf actions, so
    blocking would hang callers).
  - `bt_live_mutations` — the run's mutation journal (applied/rejected, errors,
    generations).

Limits: per-run op cap (constant, 100) against runaway self-growth; the
existing 1000-tick cap and tree timeout still bound the run. Enqueue to an
unknown or ended RunID returns an error. Dashboard/CLI surfaces are out of
scope (they can consume the MCP tools later).

## In-flight state migration

`buildNode` records `map[*evolution.SerializableNode]Command` on the blackboard
during builds (one capture in one function; no signature changes to composite
builders — the captured command is `buildNodeInner`'s return, the pointer the
library keys state by). Migration walks the deep-copy correspondence and copies
each `BTContext` state-map entry (`MemSequenceState`, `RepeaterState`,
`RetryState`, `RetryTimeState`, `TimeoutState`, `SleepState`) from old command
pointer to new command pointer. Correspondence comes from the copy walk, so
sibling index shifts do not disturb it: an unrelated graft never restarts an
in-progress MemSequence elsewhere in the tree.

Edge rules:

- A composite whose **direct child list was mutated** gets cursor arithmetic
  derived from the op: insert at index ≤ cursor → cursor+1; remove below
  cursor → cursor−1; remove at cursor → cursor unchanged (next child slides
  in). No spurious re-runs of completed side-effectful children.
- State of nodes inside a **removed subtree** drops with the subtree.
- Name-keyed ChainState (`PersistentMemSequence`, `MemSelector`) is untouched
  and survives as today.

## Safety

Per-op gate order: path resolution (+ `ExpectName`) → structural apply →
`ValidateTree` on the whole mutated tree → for `Origin: "llm"` adds, the
error-handler proposal policy (13-action exact-match allowlist — the existing
security boundary for auto-executed grafts). Removing the root is rejected.
Adding under a leaf-type parent (`Action`, `Condition`, `ChainAction`,
`AlwaysSucceed`, `SubTreeRef`) is rejected — the builder ignores leaf children,
so the graft would silently never execute. Operator- and tree-origin ops skip
the allowlist: operators already edit stored trees freely, and tree-origin
subtrees are authored code, not LLM output. Origin is enforced at the entry
point: the MCP tool stamps every op `"operator"`; in-engine callers of
`bb.EnqueueMutation` declare `"tree"` or `"llm"`, and anything grafting
LLM-proposed structure must declare `"llm"`.

## Persistence

`Persist: true` calls a new injection hook:

```go
// internal/engine — nil-checked, wired from cmd/bt-agent.
var PersistMutatedTreeFn func(info LiveRunInfo, tree *evolution.SerializableNode) error
```

The wiring writes atomically (tmp + rename) per ADR-003. **Semantics: persist
snapshots the entire current live tree**, so persisting an op after earlier
ephemeral ops in the same run persists those too. A separately maintained
persist-only tree was rejected: once the trees diverge, live-tree paths can
mis-resolve against it. Runs are expected to use uniform flags (error-handler
style growth persists; planner scratch stays ephemeral). Persist failure is
journaled but fails neither the mutation nor the run — the live tree keeps the
change.

## Error handling

- Rejected op → journal entry with error; run continues on the old tree.
- Defensive: if the post-validation rebuild fails, discard the batch, keep the
  old compiled tree running, journal the error.
- Registry enqueue on an ended run → error to the caller.
- `RunTask`'s existing panic recovery encloses the apply step.

## Testing

Unit: index-path resolution incl. `ExpectName` and sequential shift semantics;
per-op accept/reject; cursor arithmetic rules; correspondence-based state
migration — the load-bearing case being a MemSequence mid-run keeping its place
across an unrelated graft; llm-origin allowlist enforcement; root-removal
rejection; per-run op cap. Concurrency: external enqueues racing a ticking tree
under `-race`. Integration: graft an action mid-run and observe it execute in
the same run; remove a pending branch and observe it skipped; `Persist`
round-trip through a temp store via the hook. All within `make test`
(`-short -race`).

## Out of scope

- Migrating `ClaudeErrorHandler` onto the new mechanism.
- Dashboard/CLI mutation surfaces (MCP tools only for now).
- Mutating runs driven through plain `RunTask` (pre-built trees).
- `Replace` op kind (expressible as remove + add at the same index).
