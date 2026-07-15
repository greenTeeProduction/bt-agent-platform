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
│  ├─ Retry(MaxRetries=9) GrillLoop            ← 10 rounds total
│  │  └─ MemSequence GrillRound
│  │     ├─ GrillLoopViable            (NEW condition — fails once the
│  │     │                              no-progress breaker has tripped)
│  │     ├─ ReviseDesignArtifact       (NEW — no-op on round 1)
│  │     ├─ ValidateRevisedDesign      (validation logic reused, unique name)
│  │     └─ GrillDesignArtifact        (modified)
│  └─ MemSequence SplitPath                    ← exhausted or no-progress
│     ├─ SplitDesignArtifact           (NEW)
│     └─ ValidateSplitDesign
└─ ApproveDesign HITL gate             (prompt enriched)
```

`Retry` maps to `btdec.NewRepeat` and re-runs its child on failure — the same
`Retry(MemSequence …)` looping pattern the tree already uses in `DebugRetry`.
Node names are unique per tree (MemSequence naming rule).

### ReviseDesignArtifact (new action)

- Reads `design.md`. No `## Grill Q&A` section yet → success no-op (round 1
  grills the generated template).
- Otherwise: one Claude call rewrites the design **body** (Goal /
  Architecture / Acceptance Criteria / Test Strategy / Risks) incorporating
  every recorded answer and resolving each OPEN critical — either answering
  it from codebase knowledge or changing the design so the risk it probes no
  longer exists. The prompt forbids editing the Q&A appendix.
- Q&A sections are preserved append-only: `## Grill Q&A — round N`.
- Bumps `run.DesignRevision`, persists run JSON. Claude failure → round
  fails (Retry consumes it).

### GrillDesignArtifact (modified)

- Tags its appended section with the round number
  (`## Grill Q&A — round N`).
- Persists to the run JSON: `run.GrillRound`, the current open-critical
  branch set, and the design-body hash.
- **No-progress breaker:** if the open-critical set AND the body hash are
  unchanged for 2 consecutive rounds, stamp `run.NoProgressTripped = true`
  and return failure with outcome `grill_no_progress`. A failure inside
  `Retry` triggers another retry, not an exit — that is what the
  `GrillLoopViable` guard is for: it reads the stamp and fails instantly, so
  every remaining retry is a microsecond no-op (no Claude or nlm calls) and
  Retry drains straight through to SplitPath. This is also the quota guard:
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

- Claude revision/split call fails → that round/path fails; Retry absorbs
  round failures; a failed split fails the branch.
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
  incorporates answers, round-1 no-op, breaker stamps after 2 stale rounds,
  `GrillLoopViable` fails fast once tripped (zero runner/answerer calls),
  split writes both artifacts and persists the program, nothing-clear fails.
- Tree contract test: new BrainstormBranch structure, node-name uniqueness.
- Resume test: kill between rounds, re-run, assert continuation at the
  persisted round.
- Program-store tests must isolate `goapProgramsPath` (the 0977b1fa
  pollution lesson).

## Risks

- **MemSequence cursor reset under Retry:** the design assumes the library
  MemSequence resets its cursor after failure so Retry re-runs the full
  round (DebugRetry precedent). Verify in the implementation plan's first
  task; if it does not reset, GrillRound needs an explicit cursor-clearing
  wrapper.
- **Repeat attempt semantics:** `Retry` maps to `btdec.NewRepeat(child,
  MaxRetries)`; whether that is N attempts total or 1+N decides if the tree
  needs MaxRetries 9 or 10 for "10 rounds total". Pin it with a unit test in
  the same first task; the run-state `GrillRound` counter is the
  authoritative bound either way (GrillDesignArtifact refuses rounds > 10).
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
