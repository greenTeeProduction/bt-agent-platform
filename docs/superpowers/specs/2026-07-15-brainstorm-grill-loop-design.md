# Brainstorm Grill-Driven Design-Improvement Loop — Design

**Date:** 2026-07-15
**Status:** Approved (user-directed: loop until clear, cap 10 rounds, split on
exhaustion with goap program-store pickup of the deferred scope)

## Problem

The `superpowers_workflow` BrainstormBranch grills a design exactly once and
treats the grill purely as a gate:

1. `GenerateDesignArtifact` writes a **static template**
   (`buildDeterministicDesign`, `actions_superpowers_prod.go`) — generic
   architecture/criteria/risks boilerplate around the task text.
2. `GrillDesignArtifact` has Claude generate ≤12
   `Q [critical|normal] <branch>:` questions, answers them via NotebookLM
   (batches of ≤5), appends one `## Grill Q&A` section to `design.md`, and
   fails iff any `[critical]` question is OPEN (= unanswered; the web
   fallback answerer is nil).

Two consequences:

- **Answers never improve the design.** A NotebookLM answer that reveals a
  design flaw is recorded and ignored; the template body is never revised.
- **Grill failure is terminal.** Open criticals fail the branch outright;
  the Q&A work is discarded and no one gets to act on it.

## Goals

- Each grill round feeds back into a design revision: Claude rewrites the
  design body incorporating **all** answers and resolving open criticals
  (answering them from codebase knowledge or reshaping the design to
  eliminate the risk they probe).
- The revise → validate → grill loop runs until a round ends with zero open
  criticals, bounded at **10 rounds total**.
- On exhaustion (or no-progress exit), **split**: `design.md` is reduced to
  the clear scope and proceeds to approval/implementation; the deferred
  scope becomes a follow-up spec picked up autonomously by the scheduled
  goap-fusion loop via the shared program store.
- All loop state is durable in the run JSON (resume-safe); Q&A history is an
  append-only audit trail in `design.md`.

## Non-goals

- Changing the plan phase, TDD loop, or any branch other than
  BrainstormBranch.
- Wiring a web fallback answerer (stays nil; unanswered still degrades to
  OPEN).
- Changing the goap-fusion loop itself — the follow-up program enters
  through the existing store, seeding, budget, and refund machinery.
- A new node type (see Alternatives).

## Design

### Tree change (`internal/domains/superpowers_workflow.go`)

```
MemSequence BrainstormBranch
├─ GenerateDesignArtifact              (unchanged, template seed)
├─ ValidateDesignArtifact              (unchanged)
├─ Selector GrillConvergenceRouter
│  ├─ ReviewCycle GrillLoop            (reviewer_action: GrillDesignArtifact,
│  │  │                                 max_iterations: 10 ← 10 rounds total)
│  │  └─ MemSequence GrillRound
│  │     ├─ ReviseDesignArtifact       (NEW — no-op when no feedback yet)
│  │     └─ ValidateRevisedDesign      (validation logic reused, unique name)
│  └─ MemSequence SplitPath                    ← exhausted or no-progress
│     ├─ SplitDesignArtifact           (NEW)
│     └─ ValidateSplitDesign
└─ ApproveDesign HITL gate             (prompt enriched)
```

The loop node is the existing `ReviewCycle` (`internal/engine/review_cycle.go`),
which already implements run-child → run-reviewer → `approved` ⇒ success /
`needs_work` ⇒ re-run child with `ChainState["review_feedback"]`, bounded by
`max_iterations`, FAIL on exhaustion — and FAIL when the reviewer itself
fails, which the Selector routes to SplitPath. `GrillDesignArtifact` becomes
the reviewer (it has no other consumer — verified: zero references outside
`superpowers_workflow.go`).

The originally sketched `Retry(MemSequence …)` shape is **wrong** for this
engine: the library decorator behind the `"Retry"` node type
(`btdec.NewRepeat`) fails immediately on child failure and repeats on child
*success* — it never re-runs a failed round (verified in
`go-bt@v0.1.0/decorators/repeat.go`; the tree's existing `DebugRetry` shares
this behavior). ReviewCycle is the platform's actual retry-with-feedback
primitive. Confirmed alongside: the library MemSequence deletes its cursor on
failure, so each ReviewCycle iteration re-runs the full round.

### ReviseDesignArtifact (new action)

- Reads `design.md`. No `## Grill Q&A` section yet (equivalently: no
  `ChainState["review_feedback"]`) → success no-op (round 1 grills the
  generated template).
- Otherwise: one Claude call rewrites the design **body** (Goal /
  Architecture / Acceptance Criteria / Test Strategy / Risks) incorporating
  every recorded answer and resolving each OPEN critical — either answering
  it from codebase knowledge or changing the design so the risk it probes no
  longer exists. The prompt forbids editing the Q&A appendix.
- Q&A sections are preserved append-only: `## Grill Q&A — round N`.
- Bumps `run.DesignRevision`, persists run JSON. Claude failure → log and
  **succeed unchanged** (a ReviewCycle child failure would end the whole
  loop; an unchanged design instead lets the reviewer's no-progress breaker
  exit after 2 stale rounds if the failure persists).

### GrillDesignArtifact (modified — becomes the ReviewCycle reviewer)

- Tags its appended section with the round number
  (`## Grill Q&A — round N`).
- Persists to the run JSON: `run.GrillRound`, the current open-critical
  branch set, and the design-body hash. `run.GrillRound` is the
  authoritative bound: rounds beyond 10 are refused even if ChainState was
  lost to a run restart.
- **Reviewer protocol:** zero open criticals → set
  `ChainState["review_verdict"] = "approved"`, return success (loop exits,
  branch proceeds). Open criticals remain → set verdict `needs_work` and
  `ChainState["review_feedback"]` to a digest of the round's answers plus
  the open critical questions, return success (ReviewCycle re-runs the
  round; the reviser consumes the feedback). Protocol failures (Claude call
  failed, zero parseable questions) → return failure, which fails the
  ReviewCycle and routes to SplitPath.
- **No-progress breaker:** if the open-critical set AND the body hash are
  unchanged for 2 consecutive rounds, stamp `run.NoProgressTripped = true`
  and return failure with outcome `grill_no_progress` — reviewer failure
  ends the ReviewCycle immediately (no remaining-iteration burn) and the
  Selector falls to SplitPath. This is also the quota guard:
  NotebookLM-unavailable rounds produce OPEN answers and no body change, so
  the loop degrades to SplitPath after ~2 rounds instead of 10 (worst case
  is otherwise ~30 of the 50/day nlm calls; expected convergence ≤3
  rounds).
- Existing behavior retained: batched answering, OPEN degradation, dry-run
  short-circuit, zero-parsed-questions = protocol failure.

### SplitDesignArtifact (new action)

- Grill questions carry a `<branch>` label; open criticals therefore map to
  design branches. One Claude call partitions the design:
  - `design.md` rewritten to contain **only the clear scope** (branches with
    no open criticals), still passing strict section validation.
  - `design-followup.md` (artifact dir) gets the deferred scope, its open
    critical questions, and enough context to stand alone.
- The follow-up is persisted as a program via the existing
  `persistGoapProgram` path: source `design-followup`, file-scoped
  milestones, subject to the existing milestone validation
  (`validateGoapProgramMilestones` partitioning). The scheduled
  goap-fusion loop picks it up in later cycles with budgets/refunds/seeding
  untouched.
- Milestones rejected by validation → the artifact still exists; the run
  report and HITL prompt say pickup is manual.
- **Nothing clear** (every branch has an open critical) → the branch fails;
  there is nothing to implement.
- Dry-run: no-op with marker, like the existing grill dry-run.

### ApproveDesign HITL enrichment

The gate prompt includes: rounds used, criticals resolved vs deferred, and —
after a split — the deferred-scope summary plus follow-up program ID. The
human approves the clear design knowing exactly what was deferred.

### Run-state fields (run JSON, resume-safe)

`GrillRound int`, `DesignRevision int`, `OpenCriticalBranches []string`,
`DesignBodyHash string`, `NoProgressRounds int`, `NoProgressTripped bool`,
`FollowupPath string`, `FollowupProgramID string`. ChainState carries
nothing the loop needs to survive a restart.

## Error handling

- Claude revision failure → revision no-ops (see above); reviewer protocol
  failure or no-progress → ReviewCycle fails → SplitPath; a failed split
  fails the branch.
- nlm unavailable → answers degrade to OPEN (existing) → no-progress breaker
  exits the loop early.
- Killed run resumes: PersistentMemSequence re-enters the branch; round
  bookkeeping comes from the run JSON, so the loop continues at the correct
  round instead of restarting from 1.

## Testing

- Pure functions extracted (style of `superpowers_grill.go`): round/no-
  progress bookkeeping, open-critical branch extraction, design-body
  hashing, clear/deferred branch partitioning.
- Action tests with fake Claude runner + fake answerers: revision
  incorporates answers, round-1 no-op, reviewer verdicts (approved /
  needs_work with feedback), breaker fails the reviewer after 2 stale
  rounds, round bound refuses round 11 without runner/answerer calls,
  split writes both artifacts and persists the program, nothing-clear fails.
- Tree contract test: new BrainstormBranch structure, node-name uniqueness.
- Resume test: kill between rounds, re-run, assert continuation at the
  persisted round.
- Program-store tests must isolate `goapProgramsPath` (the 0977b1fa
  pollution lesson).

## Risks

- **ReviewCycle runs its whole loop inside one action closure** (a for-loop
  over `child.Run`, not tree re-ticks): 10 genuinely-progressing rounds ×
  (2 Claude calls + ≤3 nlm calls) can approach the tree's 1h root timeout.
  Accepted: the breaker keeps expected convergence ≤3 rounds, and on a
  timeout the resume path re-enters with `run.GrillRound` as the
  authoritative bound.
- **`"Retry"` node-type footgun (documented, not fixed here):** the engine's
  `Retry` maps to a repeat-on-success decorator; anyone extending this loop
  must not "simplify" it back to Retry. A one-line comment goes next to the
  GrillLoop node.
- **Claude rewrites drifting the Q&A appendix:** revision prompt forbids it;
  the action additionally re-appends any Q&A sections missing after the
  rewrite (defensive re-assembly).
- **Quota pressure:** bounded by the no-progress breaker; grill already
  skips answering when the quota stamp is active.

## Alternatives considered

- **Loop inside `GrillDesignArtifact`:** no tree change, but a monolithic
  action the tree cannot see, one tick spanning up to 10 Claude+nlm rounds
  (timeout risk), and loss of MemSequence checkpointing.
- **New `DesignLoop` node type:** explicit semantics but new serialization +
  gardener support for something `Retry`+`Selector` already expresses.
