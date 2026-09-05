# arc42 Docs Consolidation & Per-Section Sync Nodes — Design

**Date:** 2026-07-16
**Status:** Approved (user-reviewed brainstorm)
**Companion:** [2026-07-16-arc42-guidelines-checklists.md](2026-07-16-arc42-guidelines-checklists.md) — distilled per-section conformance checklists from docs.arc42.org; becomes `docs/arc42/GUIDELINES.md` during implementation.

## Problem

`docs/arc42/` holds two competing representations of the architecture doc:

- **The monolith** `go-bt-evolve-arc42.md` (3,343 lines) — the *live* document, auto-updated by the
  goap-fusion sync stage (`internal/engine/superpowers_arc42.go`). ~63% of it is §9, an append-only
  log of 130 inline ADRs with its own numbering series. Code reads it at runtime
  (`arc42_goals.go` parses §1.2 to steer all research prompts).
- **12 per-section files** (`01-…` – `12-…`) — a mostly stale mirror (last touched 2026-06-03 –
  2026-07-08), *except* they exclusively hold the ADR-010 personalization layer (§1.1a roadmap,
  Q4 quality goal, §5.0/§5.6, QS9–13, risks R9–R13) that was never back-merged into the monolith.

Additionally `docs/adr/` holds 10 standalone ADR files whose numbers 008–010 **collide** with the
monolith's ADR-008/009/010 (different decisions, same IDs), risks R9–R13 mean different things in
the two docs, `09-test-scenarios.md` is a misnamed stub duplicating section 1, README.md is
hand-written with stale counts and is checked by nothing, and there is no per-section drift
detection — only the monolith-level Claude sync plus `scripts/check-doc-drift.sh` (which never
looks at arc42 content).

## Decisions (user-approved)

1. **The monolith is deleted.** The 12 section files become the single source of truth; the
   `arc42:assemble` machinery is retired.
2. **ADR collision resolved by renumbering docs/adr's unique decisions.** The monolith's 130-entry
   log is canonical (~130 Go comments cite its series). docs/adr's ADR-008/009/010 are folded in as
   **ADR-131/132/133** with "(formerly docs/adr ADR-00x)" aliases; docs/adr ADR-001–007 are
   verified as duplicates of log entries 001–007 and dropped; `docs/adr/` is then deleted.
3. **Hybrid drift detection.** Deterministic per-section checks in `scripts/check-doc-drift.sh`
   (enforced by pre-commit, the superpowers verify gate, and the self-heal stage), plus one
   registered BT node per section running a section-scoped Claude update pass after each change.
4. **Architecture: shared sync engine + 13 thin registered nodes + one composing tree**; the
   superpowers pipeline calls the same shared engine (existing injection-hook conventions).
5. **The merged docs must conform to the official arc42 guidance** (docs.arc42.org, 144 tips) —
   distilled checklists are committed to the repo and embedded in the sync-node prompts.

## Part A — Content merge

**Merge rule:** the monolith wins for shared content (newer, auto-synced); the section files'
personalization layer (2026-07-08) is preserved on top. Where hard counts conflict (e.g. 63 vs 73
tools), recount from the code at merge time (registered MCP tools in `cmd/bt-agent/tools.go` et
al., trees in the domains tree map, node types via `RegisteredActionNames`); trust neither doc.

| Section file | Merge action |
|---|---|
| `01-introduction-goals.md` | Monolith base + file's §1.1a roadmap, Q4 Personalization goal (table becomes Q1–Q4), End-User stakeholder. Keeps the exact `## 1.2 Quality Goals` heading (parsed by `arc42_goals.go`). |
| `02-constraints.md`, `06-runtime-view.md`, `07-deployment.md`, `12-glossary.md` | Replace with monolith version (strict supersets). |
| `03-context-scope.md`, `04-solution-strategy.md` | Monolith base + file's unique personalization rows / Key Decisions 5–7. |
| `05-building-blocks.md` | Monolith's richer 5.1–5.5 + file's §5.0 (composable blocks) and §5.6 (planned blocks). |
| `08-crosscutting-concepts.md` | Monolith's 16 concepts + file's orphaned "Composable blocks / expand-at-build" as §8.17, moved *before* the generated footer. |
| `09-decisions.md` | Canonical ADR log: full monolith §9 (ADR-001–130 verbatim) + ADR-131 Composable Blocks, ADR-132 Blackboard Context Offloading, ADR-133 Personalized Self-Evolving Agents — each with full content and "(formerly docs/adr ADR-00x)" alias. Log quirks (duplicate ADR-024 heading, ADR-104/105 referenced-but-unlogged) get an explanatory note, **no renumbering** (code cites these numbers). |
| `09-test-scenarios.md` | **Delete** (misnamed stub duplicating section 1). |
| `10-quality.md` | Monolith base + file's #personalized quality-tree branch + QS9–13 (IDs don't overlap). |
| `11-risks-debt.md` | Monolith base (R1–R13) + file's 5 personalization risks **renumbered R14–R18** + its 3 unique debt rows. |

**Deletions:** `docs/arc42/go-bt-evolve-arc42.md`, `docs/arc42/09-test-scenarios.md`, entire
`docs/adr/` (after fold-in, including `INDEX.md`).

### arc42 conformance restructures (from the official tips)

New artifact **`docs/arc42/GUIDELINES.md`**: the 12 distilled checklists (see companion file).
Used as merge reference, embedded per-section into sync-node prompts, and drift-checked for
presence. Structure: one `## Section N — <title>` block per section, 12 blocks.

Applied during the merge:

- **§1** stays compact — inventory dumps (55+ trees, 63 tools, 35 node types) summarized to
  essentials, details live in §5; quality goals capped at Q1–Q4 (tips allow 3–5); stakeholder
  table gains an **Expectations** column.
- **§2** pure constraint tables with "imposed by" + "consequences"; self-chosen decisions move to §4/§9.
- **§3** business vs technical context split; system stays a black box; interface table present.
- **§4** compact goal→approach→cross-ref table (~1 page); details linked, not duplicated.
- **§5** static decomposition only; uniform blackbox tables (name | responsibility | interface);
  dense inline ADR-history paragraphs slimmed to `(→ ADR-NNN)` cross-references.
- **§6** keep 5 scenarios (3–7 rule); every participant maps to a §5 building block by name.
- **§8** each concept concretely explained, linked to implementing §5 blocks; one-off decisions
  become §9 cross-refs (tip 8-9).
- **§9** gains an overview index table (`| ID | Title | Status | Date |`) above the log (tip 9-4);
  every entry keeps Context/Decision/Status/Consequences + date.
- **§10** every scenario gets a measurable response criterion; refines §1.2, never duplicates it.
- **§11** single priority-ordered table; every row has a mitigation or explicit "accepted" status.
- **§12** sorted two-column table; generally-known terms dropped; synonyms collapse to one entry.

### Reference updates (living docs only)

- `README.md`: arc42/ADR links → section files + `docs/arc42/09-decisions.md`; fix stale "(7 ADRs)".
- `docs/agents.md`: `./adr/ADR-*` links → `./arc42/09-decisions.md` anchors (008→131, 009→132).
- `docs/TUTORIAL.md`, `docs/GETTING_STARTED.md`: `docs/adr/` prose paths.
- Go doc comments: docs/adr-series `ADR-010` → `ADR-133`, `ADR-009` → `ADR-132` (~55 hits across
  persona/gardener/goap/agentexec/knowledge/evolution/cmd). **Disambiguation rule:** rewrite only
  comments meaning the docs/adr decision (personalization / blackboard offloading); comments citing
  the arc42 log's ADR-009/ADR-010 (deterministic evolution tools / GOAP circuit gates) stay
  untouched. `ADR-003`/`ADR-004`/`ADR-007` comments stay (series 001–007 identical in both).
- **Historical records are not rewritten** (`docs/plans/`, `docs/superpowers/`, `CHANGELOG.md`,
  `.hermes/`): the "formerly" aliases in the log keep old references resolvable.

## Part B — Code, nodes, drift script, testing

### B1. Read-side repoint
`arc42GoalsDocPaths` (`internal/engine/arc42_goals.go:37`) → `docs/arc42/01-introduction-goals.md`
+ absolute fallback `/home/nico/go-bt-evolve/docs/arc42/01-introduction-goals.md`. Parser unchanged
(heading preserved). All five consumers flow through `loadArc42QualityGoals` — no other changes.
Real-repo pin test repoints.

### B2. Section manifest + shared sync engine (new files in `internal/engine`)
- `arc42_sections.go`: `Arc42Section{Num, File, Title, RequiredHeadings}`, 12-entry manifest,
  `arc42DocsDir = "docs/arc42"`. Single Go source of truth; drift script mirrors it.
- `arc42_section_sync.go`: `syncArc42Section(ctx, claude ClaudeRunner, cmd CommandRunner,
  changeCtx docChangeContext, sec Arc42Section) (changed bool, note string)` — modeled on today's
  `syncArc42Docs`: bounded Claude pass (**5 min per section**), same restricted tool allowlist,
  change detection via `git status --short -- docs/arc42/<file>`, **always non-fatal**. Prompt
  embeds: change summary + changed files, section scope, and the section's checklist sliced from
  `GUIDELINES.md` ("update ONLY this file; if the change doesn't affect §N, do nothing; content
  belonging in another section becomes a cross-reference"). Guidelines file missing → proceed
  without checklist, record a note (degrade, don't wedge).
- `syncReadme(...)`: same shape for `README.md` (5 min): verify links/counts/claims touched by the
  change; edit only README.md.
- `docChangeContext{ChangedFiles []string; Summary string}` decouples the engine from
  `SuperpowersRun` so tree-invoked nodes can use it.
- Injection hooks per existing conventions: `ClaudeRunner`/`CommandRunner` interface params with
  production defaults at call sites + package-var test seams.

### B3. Registered nodes + tree
- `arc42_docsync_nodes.go`: `init()` registers **`SyncArc42Section01`…`SyncArc42Section12`** and
  **`SyncReadme`** via `RegisterAction`. Tree-invoked nodes build `docChangeContext` from
  blackboard chain state (`changed_files`, `change_summary`), falling back to
  `git diff --name-only HEAD~1..HEAD`. Each sets `bb.Outcome` = `updated` / `no_change`
  (outcome-refinement convention, keeps notification throttling meaningful).
- `internal/domains/arc42_docsync.go`: tree **`arc42:docsync`** = Sequence of the 13 nodes; every
  node returns Success with the outcome recorded (a Claude failure is noted, never wedges the
  sequence). Registered in the domains tree map.

### B4. Pipeline integration
`actions_superpowers_prod.go:1008` swaps `syncArc42Docs(...)` for a loop over the shared engine.
**Classifier prefilter:** one cheap Claude call (2 min) — "given this diff, which of §1–§12/README
are plausibly affected?" (JSON list) — runs only those passes (typically 1–3); classifier error or
invalid output degrades to running all 13. Notes land in new field `SuperpowersRun.Arc42Sync
[]string` (`superpowers_runtime_types.go`, beside `DocDriftSync`). Ordering preserved: section
sync → `syncDriftDocs` self-heal → hard `doc-drift` verify gate.

### B5. Retirements & docgen
- **Delete** `internal/engine/superpowers_arc42.go` (+ test) — replaced by B2/B4.
- **Retire** monolith assembly: `arc42:assemble` tree (`internal/domains/arc42_trees.go`),
  `SaveDocument`, `CollectAllSections`, `GenerateTOC`, `AllSectionsDone` (`arc42_nodes.go`).
  Generation trees `arc42:section1..12` stay.
- `ReadADRs` (`arc42_nodes.go:67`): glob `docs/adr/ADR-*.md` → read `docs/arc42/09-decisions.md`;
  tree prompts citing `docs/adr/INDEX.md` updated (`arc42_trees.go:41,220,222`).
- `cmd/bt-docgen/main.go`: `sectionSourceFiles` ADR paths → `docs/arc42/09-decisions.md` (also
  fixes the dead-path bug `ADR-001-behavior-trees.md`); assemble step dropped. External output dir
  unchanged (out of scope).

### B6. Drift script
Replace check #5 (docs/adr INDEX) in `scripts/check-doc-drift.sh` with an arc42 block; every
failure fixable by editing `docs/` only (self-healability rule from the 2026-07-09 wedge):

1. Exactly the 12 section files exist; monolith and `docs/adr/` absent; `GUIDELINES.md` present
   with 12 `## Section N` blocks.
2. Per-section required headings (script-embedded manifest mirroring B2).
3. Generated footer is the **last line** of each section file.
4. §9: every `### ADR-` heading followed by a `**Status:**` line; overview index table present;
   its max ID matches the highest ADR heading.
5. Countable arc42 rules: §1.2 goal-table rows ∈ [3,5]; §11 has a priority column.
6. README: every referenced `docs/…` path exists.

Failure text keeps "drift" phrasing so `classifyHookFailure` DocDrift routing
(`superpowers_commit_autofix.go:109`) still matches. Pre-commit / make / verify gate pick it up
with zero Go changes.

### B7. Error handling summary
Every LLM sync is non-fatal and bounded; missing guidelines/section files skip with a note; git
unavailability skips change detection; classifier failure degrades to running all sections; drift
check failures are self-healable by the existing repair stage.

### B8. Testing
- Manifest real-repo pin test: 12 files exist, required headings present (convention of
  `TestLoadArc42QualityGoalsAgainstRealRepoDoc`).
- Section/README sync (fake runners): skip-when-missing, ChangedFiles merge, non-fatal Claude
  error, guideline slice reaches the prompt, classifier prefilter (affected-only; degrade-to-all).
- Nodes: 13 registered (`RegisteredActionNames`), outcome refinement, blackboard/git-diff fallback;
  `arc42:docsync` builds via `BuildTree` (wiring-test pattern).
- Repo hygiene Go test mirroring drift check 1 (docs/adr absent, 12 sections, GUIDELINES.md).
- `arc42_goals_test.go` pin repoint; `bt-docgen` source-file expectations updated.
- Gates: `make doc-drift-check`, `make check-quick`, `make check-full`; run the
  `go-conventions-reviewer` agent (touches `internal/engine`).

### B9. Rollout — one branch, four green commits
1. **Docs + atomic repoints**: merged sections, GUIDELINES.md, deletions (monolith, docs/adr,
   09-test-scenarios), README/agents/tutorial updates, drift-script swap, `arc42_goals` paths,
   `ReadADRs`, bt-docgen paths, Go comment sweep. (Atomic: deleting docs/adr breaks old check #5
   and `ReadADRs` otherwise.)
2. **Node layer**: manifest, sync engine, 13 nodes, `arc42:docsync` tree + tests.
3. **Pipeline swap**: classifier prefilter, `Arc42Sync` field, delete `superpowers_arc42.go`.
4. **Assembly retirement**: `arc42:assemble` + 4 assembly nodes + docgen cleanup.

## Out of scope

- Rewriting historical docs (plans, specs, CHANGELOG, .hermes).
- Changing bt-docgen's external wiki output directory.
- Generating README.md (it stays hand-written; the `SyncReadme` node + link check keep it honest).
- Renumbering anything inside the monolith's ADR-001–130 log.
