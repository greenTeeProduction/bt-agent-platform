# Self-Fixing Fleet — Design

- Date: 2026-07-17
- Status: approved (design review with Nico, 2026-07-17)
- Decisions locked: (1) trigger = a new *needs-code-fix* verdict, not any unresolvable; (2) scope = error-handler escalation AND a proactive self-audit agent; (3) gate = auto-apply through the goap loop's existing verification (no human approval).

## Problem

The fleet writes code autonomously (goap-fusion loop) but its self-correction is blind to bugs that don't break its own execution: the ClaudeErrorHandler only recovers *behavior-tree* failures at runtime (composing registered actions) and cannot edit source; the goap loop only implements research-goal-driven improvements. The two defects a human review found this session (dashboard breaker misclassification, LLM retry amplification) live in infra code that never fails a tree — so nothing autonomous would have caught them.

## Goal

Close the loop between "a defect exists" and "the code is fixed," autonomously, in two directions:
- **Reactive:** when the ClaudeErrorHandler can't recover a tree failure at runtime AND it is a genuine source bug, seed a code-fix program.
- **Proactive:** a scheduled agent periodically reviews recent autonomous commits for defects and seeds code-fix programs (this is what catches non-tree bugs like the ones found this session).

Both route to the goap-fusion loop's existing pipeline: `ProgramStore.Add` → the loop implements the milestone via Claude Code TDD → build/test/lint verify → auto-apply to master.

## Non-goals

- No human-approval gate (Nico chose auto-apply; the goap verification gate is the backstop).
- No change to how the goap loop implements/verifies/applies a program — code-fix programs are ordinary programs with a distinct `Source` tag.
- Not a general static analyzer; the self-audit uses Claude Code review over recent diffs, not a new analysis engine.

## Architecture

Three components. A shared seeding primitive, plus the two producers.

```
[ClaudeErrorHandler]  tree -1, unresolvable + is-bug ──┐
                                                        ├─→ seedCodeFixProgram(sig, title, milestone, source)
[self-review agent]   recent commits → Claude review ──┘        │  guards: dedup ledger + cap + kill switch
                                                                ▼
                                            research.ProgramStore.Add(...)  (~/.go-bt-evolve/research/programs.json)
                                                                ▼
                                    goap-fusion loop (one program at a time): Claude Code TDD → verify → auto-apply master
```

### 1. Shared primitive — `seedCodeFixProgram`

New file `internal/engine/self_fix_seed.go`. A guarded function:

```go
func seedCodeFixProgram(sig, title, milestoneGoal, source string) (seeded bool, reason string)
```

- Appends a program via `research.OpenPrograms(goapProgramsPath)` → `ps.Add(title, source, []string{milestoneGoal})` → `ps.Save()`, mirroring `arc42_seeder.go`/`persistGoapProgram`. `source` is tagged `self-fix:error-handler:<sig>` or `self-fix:self-review:<sig>` so these are distinguishable from research/arc42 programs in the store and logs.
- **Dedup + cooldown:** a durable ledger under `~/.go-bt-evolve/self_fix/ledger.json` (reuse the error-handler store pattern — atomic tmp+rename, in-proc mutex + file lock) keyed by `sig`. Skip if the sig was seeded within `BT_SELF_FIX_COOLDOWN` (default 24h) — a recurring defect is seeded once, not every failure. Records last-seeded time + the resulting program title.
- **Cap:** skip if the store already holds ≥ `BT_SELF_FIX_MAX_OPEN` (default 3) *open* (not-all-done) `self-fix:*` programs — a backlog of unaddressed fixes must not grow unbounded, and code-fix programs must not starve the single active-program slot indefinitely.
- **Kill switch:** `BT_SELF_FIX=off` → no-op (both producers respect it).
- Returns `(seeded, reason)` for the caller to log/observe; never blocks, never errors upward.

The milestone goal string must be a well-formed, file-scoped, TDD-able instruction (the same shape the goap loop expects — name the file(s), the defect, and the fix, so `ClaudeSuperpowersPath` can RED→GREEN it). Producers are responsible for generating a good milestone (see below).

### 2. Part A — Error-handler escalation (reactive)

Extend the ClaudeErrorHandler's proposal call (`error_handler_claude.go`).

- **Prompt/verdict extension:** `buildErrorHandlerPrompt` gains a third branch in its reply contract. Today: `{resolvable:true,node}` or `{resolvable:false,reason}`. Add an optional field so an unresolvable verdict may carry a code-fix escalation:
  `{"resolvable": false, "reason": "...", "code_fix": {"is_bug": true, "title": "...", "milestone": "<file-scoped TDD instruction>", "files": ["..."], "rationale": "..."}}`.
  The prompt instructs: if the failure is a genuine SOURCE-CODE bug (not transient/rate-limit/config) that a small fix would resolve, describe it as a `code_fix` milestone naming the specific file(s); otherwise omit `code_fix`.
- **Parsing:** `errorHandlerProposal` gains an optional `CodeFix *errorHandlerCodeFix` field; `parseErrorHandlerProposal` populates it. `validateCodeFix` requires non-empty title + milestone + at least one plausible repo file path, and `is_bug==true`.
- **Wiring:** in `BuildClaudeErrorHandler`'s tick, when `requestErrorHandlerProposal` returns `!Resolvable` AND a valid `CodeFix`, call `seedCodeFixProgram(sig, codeFix.Title, codeFix.Milestone, "self-fix:error-handler:"+sig)` before passing the failure through. The tree failure still returns `-1` immediately (async fix). Ledger verdict becomes `escalated` (distinct from `unresolvable`) so re-firings within cooldown don't re-call Claude or re-seed.
- Gated by the existing error-handler signature/cooldown so a recurring tree failure escalates once; further bounded by `seedCodeFixProgram`'s own dedup/cap. Kill switch: `BT_SELF_FIX=off` (and the existing `BT_CLAUDE_ERROR_HANDLER=off` disables the whole handler).

### 3. Part B — Proactive self-audit agent (proactive)

A new scheduled agent that automates the manual fleet-review workflow.

- **Domain tree** `self_review` (new `internal/domains/self_review.go`, added to `AllDomainTrees()` — and therefore auto-wrapped by ClaudeErrorHandler like every tree). A `Sequence`:
  1. `GatherSelfReviewScope` — `git log --oneline <last-reviewed>..HEAD` for autonomous (`superpowers: apply` + fleet) commits since the last review; if none new → `no_change` (healthy, throttled). Reads/writes the last-reviewed SHA in `~/.go-bt-evolve/self_review/state.json`.
  2. `RunSelfReviewClaude` — a read-only Claude Code review over those commits' diffs, modeled on `runClaudeCodeReviewResearch` + `goapReviewAllowedTools` (Read/Glob/Grep + `git log/show/diff`). The prompt: review these autonomous commits for correctness/logic/regression defects the automated gate can't catch; for each CONFIRMED defect return `{title, milestone (file-scoped TDD fix), files, severity, signature}` as JSON. Bounded to the top few findings.
  3. `SeedSelfReviewFixes` — for each finding, `seedCodeFixProgram(finding.signature, finding.title, finding.milestone, "self-fix:self-review:"+finding.signature)`. Advance the last-reviewed SHA to HEAD only after seeding (so a crash re-reviews rather than skips).
  4. `ReportSelfReview` — summarize seeded fixes / clean verdict.
- **Cron/YAML** `~/.go-bt-evolve/agents/self-review.yaml` — daily (or every-N-hours), tree `domain:self_review`, low priority so it never preempts the goap loop's active work. Registered like the other agent YAMLs.
- Kill switch: `BT_SELF_FIX=off` (skips seeding; the review still runs but seeds nothing) and simply not scheduling the YAML.

## Safety (auto-apply, bounded)

Autonomy is real here — a failure or an audit finding leads to an autonomous master commit — so the bounds are the design's core:

1. **The goap verification gate is the backstop.** Every seeded program runs through `ClaudeSuperpowersPath`: RED must fail then GREEN pass, then build + changed-package tests + changed-package lint (now Claude-repaired, per this session's fix 1) + doc-drift, then the apply verification. A wrong fix fails verification and *degrades* (does not land). No seeded fix reaches master unverified.
2. **Dedup + cooldown + cap** (in `seedCodeFixProgram`) stop a recurring failure or a re-reviewed commit from flooding the store or starving the single active-program slot.
3. **Kill switch** `BT_SELF_FIX=off` halts all seeding instantly; `BT_CLAUDE_ERROR_HANDLER=off` halts Part A's whole handler.
4. **Observability:** `self-fix:*`-tagged programs in the store; `Info` logs on seed (sig, title, source); the self-review report; the ledger. A human can see and cull seeded programs.
5. **Loop-safety:** the self-review must not seed a fix for a defect *it itself* introduced in a prior seeded fix without bound — the dedup ledger (per-signature cooldown) plus the goap loop's own `RedPassStreak`/abandon budget prevent a fix→bug→fix oscillation from running unbounded; a defect that keeps recurring seeds once per cooldown, not per cycle.

## Files

New: `internal/engine/self_fix_seed.go` (+ `_test.go`), `internal/engine/actions_self_review.go` (+ `_test.go`), `internal/domains/self_review.go` (+ `_test.go`), `~/.go-bt-evolve/agents/self-review.yaml`.
Modified: `internal/engine/error_handler_claude.go` (CodeFix verdict + parse + validate), `internal/engine/error_handler_node.go` (seed on escalation), `internal/domains/trees.go` (register `self_review` in `AllDomainTrees()`).

## Testing

- `seedCodeFixProgram`: seeds a program (store round-trip); dedup within cooldown skips; cap reached skips; `BT_SELF_FIX=off` no-ops; concurrent seeds don't double-add (mutex+lock).
- Part A: an unresolvable proposal carrying a valid `code_fix` seeds a program (fake ClaudeRunner) and stamps ledger `escalated`; an unresolvable WITHOUT code_fix (transient) seeds nothing; an invalid code_fix (no files / is_bug false) seeds nothing; escalation respects the cooldown.
- Part B: `GatherSelfReviewScope` with no new commits → no_change; with new commits → passes them to the review; `RunSelfReviewClaude` (fake runner) findings → `SeedSelfReviewFixes` seeds one program per finding (deduped); last-reviewed advances only after seeding; kill switch seeds nothing.
- Regression: existing error-handler tests unchanged (the code_fix field is optional; a resolvable proposal path is untouched).

## Open questions / risks

- **Milestone quality:** a vague milestone goal string makes the goap loop flail (RED-pass or churn). Mitigation: `validateCodeFix` requires file paths; consider reusing `fetchAcceptableGoapProgram`'s validate-and-retry to harden the milestone before seeding. Start strict; loosen if too few escalations land.
- **Single active-program slot:** code-fix programs compete with research/arc42 programs for the one active slot. This design queues them normally (no forced preemption) to avoid starving research work; the cap keeps the backlog bounded. If bug-fixes need priority, a follow-up can insert them ahead — deferred.
- **Feedback-loop stability:** a self-audit that seeds a fix which itself has a defect could oscillate. Bounded by per-signature cooldown + the goap abandon budget, but worth watching the `self-fix:*` program outcomes for the first days after deploy.
