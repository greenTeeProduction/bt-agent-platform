# arc42 Conformance Checklists (companion to 2026-07-16-arc42-docs-consolidation-design.md)

Distilled 2026-07-16 from the official per-section tips at https://docs.arc42.org (144 tips).
During implementation this content becomes `docs/arc42/GUIDELINES.md` (one `## Section N — <title>`
block per section) and is embedded per-section into the SyncArc42SectionNN node prompts.

---

# arc42 Section 1 (Introduction and Goals) — Conformance Checklist

## Required structure (arc42 template)
- [ ] Section contains exactly three subsections: **1.1 Requirements Overview**, **1.2 Quality Goals**, **1.3 Stakeholders**.
- [ ] 1.1 gives a compact summary of driving forces: what the system is, why it exists, and its essential functional requirements (Tip 1-1).
- [ ] 1.2 lists the **top 3–5 quality goals** most important to major stakeholders — no more (Tip 1-16).
- [ ] 1.3 contains a stakeholder table with columns Role/Name, Contact, and **Expectations** (Tips 1-20, 1-21).

## Do's
- [ ] Limit 1.1 to the *essential* tasks and use cases — not an exhaustive feature list (Tip 1-2).
- [ ] State the **business goals** of the system explicitly (Tip 1-3).
- [ ] Group/cluster requirements into a scannable overview rather than a flat dump (Tip 1-4).
- [ ] Reference existing requirements documents by stable ID or link instead of duplicating them (Tip 1-5).
- [ ] Describe functional requirements in a checkable form: numbered list, semi-formal text, or activity/BPMN/exemplary-business-process diagrams (Tips 1-6 to 1-10).
- [ ] Make every quality goal **explicit**; phrase each as a concrete scenario (stimulus → expected response/measure), not a bare "-ility" word like "flexible" or "performant" (Tips 1-11, 1-12).
- [ ] If stakeholders supplied no quality requirements, write your own assumptions and mark them explicitly as assumptions (Tip 1-13).
- [ ] Use a quality checklist/model (e.g. the open-source arc42 Quality Model, ISO 25010) to derive and name goals; work them out with stakeholders via examples (Tips 1-14, 1-15, 1-24).
- [ ] Search broadly for stakeholders (ops, support, security, management, adjacent teams — not just devs and product) and record each one's expectations of the architecture/documentation (Tips 1-19, 1-20).
- [ ] Optionally classify stakeholders by interest and influence to prioritize communication (Tip 1-23).
- [ ] Ensure each quality goal traces forward to concrete measures in Section 4 (Solution Strategy) (Tip 1-17).

## Don'ts / anti-patterns
- [ ] Don't paste the full requirements specification here — summarize and reference it (Tips 1-1, 1-5).
- [ ] Don't list more than ~5 quality goals — an unranked laundry list of "-ilities" gives no design guidance (Tip 1-16).
- [ ] Don't state quality goals without a measurable scenario or acceptance criterion (Tip 1-12).
- [ ] Don't duplicate a stakeholder register that management already maintains — link to it instead (Tip 1-22).
- [ ] Don't leave stakeholder rows without an "expectations" statement — role + name alone is insufficient (Tip 1-20).

## Belongs elsewhere in arc42 (move it out)
- [ ] Detailed/complete quality requirements and full quality tree → **Section 10 (Quality Requirements)**; Section 1 keeps only the top 3–5 (Tip 1-18).
- [ ] How quality goals are achieved (technology/architecture decisions) → **Section 4 (Solution Strategy)**, not here (Tip 1-17).
- [ ] Technical/organizational constraints → Section 2; system scope, external interfaces, and context diagrams → Section 3; solution details → Sections 5+.

---

## arc42 Section 2 (Constraints) — Conformance Checklist

### Required content & structure
- [ ] Section contains ONLY requirements that constrain architects' freedom in design, implementation, or development-process decisions — nothing else.
- [ ] Constraints are presented as simple tables: one row per constraint, with columns for the constraint and a brief explanation/background.
- [ ] Constraints are grouped into categories: (1) technical constraints, (2) organizational/political constraints, (3) conventions (only differentiate if there are enough entries to warrant it — Tip 2-5).
- [ ] Technical constraints cover, where applicable: mandated technologies, hardware specs, operational guidelines, framework/product selection rules, reference-architecture mandates (Tip 2-4).
- [ ] Organizational constraints cover, where applicable: time and budget limits, mandated development process, third-party contracting obligations, legal/compliance requirements (Tip 2-3).
- [ ] Conventions cover, where applicable: programming guidelines, versioning, documentation standards, naming conventions.

### Do's (from the official tips)
- [ ] Each constraint states WHO imposed it or where it comes from (management directive, law, contract, organization standard).
- [ ] Each constraint's consequences are clarified (e.g., added cost or effort) so stakeholders can see the impact (Tip 2-2).
- [ ] Negotiable vs. non-negotiable is distinguishable; problematic constraints are flagged for discussion with management/stakeholders rather than silently accepted (Tips 2-2, 2-3).
- [ ] If no constraints are known, check constraints applying to other systems in the organization before declaring "none" (Tip 2-1).
- [ ] Constraints are disclosed early and explicitly so the development team can adjust in time (Tip 2-4).
- [ ] Each entry is externally imposed and limits freedom — verifiable/checkable, not a vague aspiration.

### Don'ts / anti-patterns
- [ ] No self-chosen architecture or technology decisions listed as constraints (own decisions belong in Section 4 "Solution Strategy" or Section 9 "Architecture Decisions").
- [ ] No optional preferences, recommendations, or unenforced guidelines dressed up as constraints.
- [ ] No constraints accepted at face value without stating their consequences or noting they were challenged.
- [ ] No long prose essays — keep to compact tables/lists with short explanations.

### Does NOT belong here (lives elsewhere in arc42)
- [ ] Goals, requirements, and stakeholders → Section 1 (Introduction and Goals).
- [ ] Quality requirements/attributes → Sections 1.2 and 10.
- [ ] External interfaces and system scope → Section 3 (Context and Scope).
- [ ] Decisions the team made itself → Sections 4 and 9.
- [ ] Risks arising from constraints → Section 11 (Risks and Technical Debt); Section 2 may cross-reference them only.

---

# arc42 Section 3 (Context and Scope) — Conformance Checklist

## Required content & structure
- [ ] Section contains two subsections per the arc42 template: **Business Context** (domain-level communication partners and data exchanged) and **Technical Context** (channels, protocols, transmission media) — or Business Context plus an explicit note that technical context is deferred to the Deployment View (Section 7) (Tip 3-19).
- [ ] The system boundary is explicitly demarcated: the system appears as one discrete box, clearly separated from all external partners (users, neighbor systems, hardware) (Tip 3-1).
- [ ] A **context diagram** is present (Tip 3-2) — a table alone is insufficient.
- [ ] The diagram is complemented by a **table of interfaces/partners** (partner, input/output data, purpose; technical context adds channel/protocol) (Tip 3-3).
- [ ] **All external interfaces** are shown — no known partner or interface omitted (Tip 3-9).

## Do's
- [ ] Business context shows **data/information flows**, not mere dependency arrows (Tip 3-11).
- [ ] Business and technical context are clearly differentiated (separate diagrams or clearly labeled layers) (Tip 3-10); if combined into one diagram, business elements and their technical realization are both labeled (Tips 3-17, 3-18).
- [ ] Each domain interface can be traced to its technical realization (protocol/channel) somewhere (here or Section 7) (Tip 3-18).
- [ ] Risky or critical external interfaces are explicitly flagged (e.g., unstable partner, unreliable channel) (Tip 3-4).
- [ ] Relevant external influences beyond call interfaces (e.g., regulations, batch feeds, physical environment) are noted (Tip 3-12).
- [ ] Architecturally relevant transitive/indirect dependencies on external systems are mentioned (Tip 3-13).
- [ ] Quality requirements that apply at external interfaces (latency, throughput, availability, security) are called out or cross-referenced (Tip 3-14).
- [ ] If hardware/embedded aspects are central, technical context is given full weight here (Tips 3-15, 3-16).

## Anti-patterns / don'ts
- [ ] No internal structure of the system in the diagram — the system stays a black box; internals belong in Building Block View (Section 5).
- [ ] Overview only: no protocol payloads, field-level schemas, or implementation details (Tip 3-5).
- [ ] If there are many (>~10) external partners, they are grouped/aggregated by explicit, stated criteria or via ports — not drawn as an unreadable spider web (Tips 3-6, 3-7, 3-8).

## Does NOT belong here (lives elsewhere in arc42)
- [ ] Requirements, goals, stakeholders → Section 1.
- [ ] Constraints (organizational/technical) → Section 2.
- [ ] Solution decisions, technology choices, internal decomposition → Sections 4, 5, 9.
- [ ] Deployment nodes, infrastructure topology, detailed protocol/channel specifics (when lean docs preferred) → Section 7 Deployment View (Tip 3-19).

---

# arc42 Section 4 (Solution Strategy) — Conformance Checklist

## Required content (per arc42 template)
- [ ] Covers the fundamental decisions and solution approaches shaping the architecture — the "cornerstones" that drive all detailed decisions.
- [ ] Names key **technology decisions** (languages, frameworks, platforms) at cornerstone level only.
- [ ] States the **top-level decomposition** approach (e.g., architectural pattern/style chosen: layers, microservices, hexagonal).
- [ ] Explains **how the top quality goals will be achieved** — each key quality goal from section 1.2 maps to at least one solution approach.
- [ ] Includes relevant **organizational/process decisions** if they shape the architecture (e.g., team split, build-vs-buy, open-source strategy).
- [ ] Every decision is **motivated/justified**: traceable to the problem statement, quality goals (section 1.2), or constraints (section 2) (Tip 4-6).

## Structure & style (do's)
- [ ] Section is **compact** — keyword list or short bullets, not prose essays (Tip 4-1); a reader grasps the strategy in ~1 page.
- [ ] Prefer a **table** mapping: quality goal → scenario → solution approach → link to details (Tip 4-2).
- [ ] Solution approaches are stated **in the context of the quality requirement/scenario** they serve, preserving traceability (Tip 4-3).
- [ ] **Cross-references** point to where details live — building block view (§5), runtime view (§6), concepts (§8), decisions (§9) — instead of duplicating them (Tip 4-4).
- [ ] Section may start small and **grow iteratively** with the architecture; incompleteness early on is acceptable, staleness is not (Tip 4-5).

## Anti-patterns (don'ts)
- [ ] No long narrative justifications or design essays — motivation is one or two sentences per decision, details linked elsewhere.
- [ ] No unmotivated technology name-dropping ("we use Kafka") without stating which goal/constraint it serves.
- [ ] No decisions listed that don't trace to any quality goal, constraint, or problem-statement need.
- [ ] Not written as a one-time upfront document that never changes; it must reflect current strategy.

## Does NOT belong here (lives elsewhere in arc42)
- [ ] Detailed component structure/diagrams → §5 Building Block View.
- [ ] Runtime/interaction scenarios → §6 Runtime View; deployment/infrastructure detail → §7.
- [ ] Full cross-cutting concept descriptions (security, persistence, logging mechanics) → §8 Concepts.
- [ ] Detailed decision records with alternatives considered and trade-off analysis → §9 Architecture Decisions (ADRs).
- [ ] Quality goals/requirements definitions themselves → §1.2 and §10 Quality Requirements; constraints → §2.

---

## arc42 Section 5 (Building Block View) — Conformance Checklist

### Required content & structure
- [ ] Section documents the *static* decomposition of the system into building blocks (modules, packages, components, subsystems, …) and their dependencies — nothing else.
- [ ] Level 1 is always present: a whitebox of the overall system (diagram + contained blocks) with blackbox descriptions of each contained block. (Tips 5-2, 5-3)
- [ ] Structure is hierarchical: Level 2 zooms into *selected* Level-1 blocks; Level 3+ only where needed. Include multiple levels only where they add value. (Tips 5-2, 5-11)
- [ ] Every whitebox has: an overview diagram, a stated motivation/justification for the chosen decomposition, and a list of contained blackboxes. (Tip 5-8)
- [ ] Every important blackbox states its purpose/responsibility (one or two sentences) and its interfaces; quality/performance characteristics optional. (Tip 5-5)
- [ ] Use a *uniform* structure/template for every whitebox and blackbox section — same headings, same order. (Tip 5-1)
- [ ] External interfaces at deeper levels stay consistent with Level 1 (and with the context boundary in section 3). (Tip 5-4)

### Do's
- [ ] Use tables for compact blackbox specs (name | responsibility | interface). (Tip 5-7)
- [ ] Refine consistently: every lower-level block traces to exactly one parent block; state the parent ("origin") of each refined block explicitly. (Tips 5-12, 5-26)
- [ ] Refine only a *few* selected blocks — the risky, complex, or novel ones — not all of them. (Tip 5-27)
- [ ] Explain the mapping of building blocks to source code (directories/repos/packages); ideally align blocks with the language's modularization constructs and the directory layout, so every source file is locatable in exactly one block. (Tips 5-13 to 5-16, 5-18)
- [ ] Let cohesion drive block boundaries — group by related responsibility. (Tip 5-17)
- [ ] Specify internal interfaces with minimal effort — link to code, unit tests, or a runtime scenario instead of prose duplication. (Tips 5-21 to 5-23)
- [ ] Include third-party software only in exceptional cases (when architecturally significant), and clearly mark such elements as third-party. (Tips 5-19, 5-20)

### Don'ts / anti-patterns
- [ ] Don't expose blackbox internals at the level where it is a blackbox — internals belong one level deeper. (Tip 5-6)
- [ ] Don't decompose everything to the same depth ("balanced-tree" over-documentation); depth should follow risk/importance. (Tip 5-27)
- [ ] Don't create unjustified structure — a whitebox diagram without a stated rationale fails the check. (Tip 5-8)
- [ ] Don't leave orphan code: no source code that maps to no block, and no block with no code location. (Tip 5-18)
- [ ] Don't repeat a pattern's structure per block — if several blocks share structure/behavior, describe it once as a concept. (Tips 5-10, 5-28, 5-25)

### Belongs elsewhere in arc42
- [ ] Runtime behavior, interactions, sequences → section 6 (Runtime View); reference scenarios from here rather than inlining them. (Tips 5-9, 5-23)
- [ ] Crosscutting patterns, conventions, shared mechanisms (logging, persistence, error handling) → section 8 (Concepts). (Tips 5-10, 5-28)
- [ ] Deployment, hardware, infrastructure → section 7; external systems/context boundary → section 3; decisions/rationale beyond decomposition motivation → sections 4/9.
- [ ] Miscellaneous other information keyed to Level-1 blocks (e.g., ownership, status) may reference Level 1 but must not bloat the decomposition itself. (Tip 5-24)

---

## arc42 Section 6 (Runtime View) — Conformance Checklist

### Required content & structure
- [ ] Section documents **concrete runtime scenarios**: how instances of building blocks interact step-by-step at runtime.
- [ ] Scenarios cover only **architecturally relevant** cases: the most important use cases, interactions at critical external interfaces, operation/administration scenarios, and important error/exception scenarios.
- [ ] Each scenario has a short title/heading and (optionally) a one-line note on *why* it is architecturally relevant.
- [ ] Acceptable forms: numbered step lists, sequence diagrams, activity diagrams (with swimlanes/partitions), BPMN, or state machines — in markdown, prefer numbered steps or mermaid sequence/activity diagrams.

### Do's (from official tips)
- [ ] Every participant/actor/lane in a scenario **maps to a named building block from Section 5** (or a documented external system) — no phantom components that exist only in this section (Tip 6-1).
- [ ] Keep the count low: document **only a few scenarios** (roughly 3–7); each additional one must earn its place (Tip 6-2).
- [ ] Keep scenarios **schematic**, not exhaustive — show the essential interaction flow, omit internal detail (Tip 6-3).
- [ ] Detailed scenarios are allowed **only with explicit justification** (e.g., contract/specification need); flag them as such (Tip 6-4).
- [ ] Partial scenarios/excerpts are fine — a scenario may cover only the interesting fragment of a flow, not end-to-end (Tip 6-6).
- [ ] Mixing granularity is fine: a scenario may combine coarse (level-1) and fine (level-2+) building blocks (Tip 6-10).
- [ ] Textual notation (numbered steps: "1. X calls Y with Z…") is a fully valid alternative to diagrams (Tip 6-9).

### Anti-patterns to reject
- [ ] A scenario for every use case / a large scenario catalog (documentation bloat).
- [ ] Sequence diagrams reproducing message-level or code-level detail (method signatures, every getter/setter).
- [ ] Scenarios kept only as a design-discovery artifact — scenarios used to *find* building blocks need not all be *documented* (Tip 6-5); prune them.
- [ ] Participants whose names don't match Section 5 building-block names exactly.

### Does NOT belong here (goes elsewhere in arc42)
- [ ] Static structure, responsibilities, and interfaces of building blocks → **Section 5 (Building Block View)**.
- [ ] Hardware/infrastructure, nodes, and mapping of software to environments → **Section 7 (Deployment View)**.
- [ ] Cross-cutting rules and recurring patterns (error handling policy, logging, security concepts, transaction handling in general) → **Section 8 (Concepts)** — Section 6 shows only their *concrete occurrence* in a specific scenario.
- [ ] Reasons/trade-offs behind interaction design decisions → **Section 9 (Architecture Decisions)**.
- [ ] Quality scenarios (stimulus/response requirements) → **Section 10 (Quality Requirements)**.

---

# arc42 Section 7 (Deployment View) — Conformance Checklist

## Required content & structure
- [ ] Describes the technical infrastructure the system executes on: environments, machines/VMs/containers, processors, network topology, channels, and geographic locations where relevant.
- [ ] Explicitly maps software building blocks (from Section 5) onto infrastructure elements — every deployed building block is assigned to a node.
- [ ] Follows the arc42 sub-structure per level: **Infrastructure Level 1** with (a) an overview diagram, (b) motivation (why this structure), (c) quality/performance features of the infrastructure, (d) mapping of building blocks to infrastructure; **Infrastructure Level 2** only for nodes whose internals need refinement.
- [ ] Uses hierarchical refinement for complex infrastructure: Level 1 = whole-system overview; Level 2 = zoom into individual nodes. No flat mega-diagram.
- [ ] Documents each relevant environment (dev, test, staging, production) separately when they differ; states which environment each diagram shows.

## Do's (from the arc42 tips)
- [ ] Every node (server, VM, container, cluster, device) has a short explanation: name, purpose, key characteristics (CPU/RAM/OS/region) where decision-relevant. (Tip 7-8)
- [ ] Infrastructure decisions are justified — the "why" (cost, availability, latency, compliance) accompanies the "what". (Tip 7-2)
- [ ] Software-to-hardware mapping is shown either as a UML deployment diagram (Tip 7-6) or as a lean mapping table (Tip 7-7) — at least one of the two is present and complete.
- [ ] Operationally relevant facts for productive use are included: scaling, failover/redundancy, monitoring hooks, backup, required middleware/runtime. (Tip 7-9)
- [ ] Channels/connections between nodes are labeled (protocol, port, direction) where relevant to understanding.

## Don'ts / anti-patterns
- [ ] Does NOT invent or over-specify hardware sizing the team doesn't own — defer detailed hardware specs to infrastructure/hardware experts; reference their docs instead. (Tip 7-10)
- [ ] No single diagram mixing multiple environments or all abstraction levels at once.
- [ ] No unmapped nodes (infrastructure with no software assigned) and no unmapped building blocks (software with no home) without an explicit note.
- [ ] Diagrams are not left unexplained — every diagram has accompanying prose/table.

## Does NOT belong here (lives elsewhere in arc42)
- [ ] Static software structure/decomposition → Section 5 (Building Block View), not here.
- [ ] Runtime interactions/scenarios between components → Section 6 (Runtime View).
- [ ] Deployment/infra concepts that apply crosscutting (e.g., general logging, security concepts) → Section 8 (Crosscutting Concepts).
- [ ] Major technology/platform choices as decisions with alternatives → Section 9 (Architecture Decisions); Section 7 only references them.
- [ ] External systems/interfaces as scope boundaries → Section 3 (Context); here they appear only as deployment targets/endpoints.

---

# arc42 Section 8 (Crosscutting Concepts) — Conformance Checklist

## Required content & structure
- [ ] Section documents overall solution approaches/patterns/rules that apply across MULTIPLE building blocks (goal: conceptual integrity — consistency, homogeneity).
- [ ] Structured as level-2 subsections (8.1, 8.2, …), one per concept, each with a clear topic heading.
- [ ] Only the MOST IMPORTANT topics for this system are covered — a small curated set, not one subsection per possible topic. Do NOT attempt to cover the entire arc42 concept topic catalog (tip 8-3, 8-10).
- [ ] Each concept is actually explained — a heading plus one vague sentence is non-conformant; state the problem, the chosen approach, and its rationale (tip 8-1).
- [ ] Each concept explains HOW it works in this system — concrete mechanism, ideally with source-code snippets, config examples, or test references, not just theory (tip 8-4, 8-8).
- [ ] The business/domain (data) model is documented here (or explicitly linked from here), preferably as a diagram, e.g. PlantUML/Mermaid (tips 8-5, 8-7).
- [ ] Domain-model terms are consistent with and cross-referenced to Section 12 (Glossary) (tip 8-6).
- [ ] Concepts hyperlink to the building blocks (Section 5) that implement/use them, and vice versa (tip 8-11).

## Do's
- [ ] Treat "concepts" broadly: approaches, rules, principles, tactics, strategies, conventions (tip 8-2) — e.g. persistence, error handling, logging, security, concurrency, configuration, i18n.
- [ ] Keep each concept lean: shortest text that lets a developer apply the rule consistently.
- [ ] Where a concept boils down to a single choice, prefer recording it as a decision in Section 9 (ADR) and linking it, instead of a full concept subsection (tip 8-9).

## Anti-patterns to reject
- [ ] Topic-catalog dumping: empty or boilerplate subsections for every conceivable concern (UX, security, persistence, …) with no system-specific content.
- [ ] Textbook prose: generic explanations of a pattern with no statement of how THIS system applies it.
- [ ] Duplicated rules: the same convention restated inside multiple building-block descriptions instead of centralized here once.
- [ ] Orphan concepts: subsections no building block references and that constrain nothing.

## Does NOT belong here (lives elsewhere in arc42)
- [ ] Structure/decomposition of individual building blocks → Section 5.
- [ ] Runtime scenarios/interaction sequences → Section 6.
- [ ] Deployment/infrastructure mapping → Section 7.
- [ ] One-off architecture decisions with alternatives/rationale → Section 9 (link from here if concept-relevant).
- [ ] Risks and technical debt → Section 11; term definitions themselves → Section 12 (Glossary).

---

## arc42 Section 9 (Architecture Decisions) — Conformance Checklist

**Purpose (per arc42 template):** "Important, expensive, large scale or risky architecture decisions including rationales."

### Required content & structure
- [ ] Section exists as `9. Architecture Decisions` and contains only architecturally significant decisions (important, expensive, large-scale, or risky) — not minor implementation details (Tip 9-1).
- [ ] Every decision includes an explicit rationale — the motivation/reasoning behind the choice, not just the outcome (Tip 9-3).
- [ ] Every decision states the evaluation criteria/factors that drove the choice (e.g., cost, risk, team skills, quality goals) (Tip 9-2).
- [ ] Every decision has a timestamp/date (Tip 9-8).
- [ ] Rejected alternatives are recorded per decision, with the reason each was rejected (Tip 9-6).
- [ ] Decisions use a consistent, structured format — preferably ADRs (title, status, date, context, decision, consequences), or alternatively a table/mind-map overview (Tips 9-4, 9-5, 9-9).
- [ ] If many decisions exist, provide an overview table/list (ID, title, status, date) linking to individual ADRs for navigability (Tip 9-4).

### Do's
- [ ] Follow ADR good-practice conventions: one decision per record, immutable once accepted, status lifecycle (proposed/accepted/superseded), consequences stated (Tip 9-9).
- [ ] Keep tooling and process lightweight (plain markdown ADR files are fine; no heavyweight process) (Tips 9-7, 9-10).
- [ ] Superseded/revised decisions stay visible with their status updated, preserving decision history rather than silently editing.
- [ ] Link decisions to the quality goals (section 1.2/10) or constraints (section 2) that motivated them, where applicable.

### Anti-patterns to reject
- [ ] Decision recorded without rationale ("we use X") — outcome-only entries fail.
- [ ] Catalog of every trivial choice (library versions, naming, formatting) diluting the significant decisions.
- [ ] Undated decisions or decisions with no owner/status, making historical context unrecoverable.
- [ ] "Only option considered" entries — no alternatives or criteria mentioned for a genuinely contested decision.

### Does NOT belong in section 9 (lives elsewhere in arc42)
- [ ] Overarching solution strategy / fundamental technology & top-level decomposition summary → section 4 (Solution Strategy); section 9 holds the detailed individual decisions, may cross-reference section 4.
- [ ] Structural descriptions of the resulting design (building blocks, interfaces) → section 5; runtime behavior → section 6; deployment → section 7.
- [ ] Cross-cutting implementation rules/patterns that resulted from decisions → section 8 (Concepts).
- [ ] Quality requirements/scenarios themselves → sections 1.2 and 10; risks and technical debt → section 11; term definitions → section 12 (Glossary).

---

## arc42 Section 10 (Quality Requirements) — Conformance Checklist

### Required content & structure
- [ ] Section is titled/numbered as arc42 section 10 "Quality Requirements" and contains two parts: **10.1 Quality overview/tree** and **10.2 Quality scenarios**.
- [ ] 10.1 gives an overview of quality categories as a table or mind-map (a "quality tree"), classified against an established model (ISO 25010:2023 or arc42's Q42) — not free-form prose.
- [ ] 10.2 lists concrete quality scenarios, each in a consistent form: short form = context/background, stimulus (source + event), response with **measurable acceptance criteria**; long form may add artifact, environment, and response measure.
- [ ] Every scenario's response criterion is quantified or objectively checkable (e.g. "p95 < 200 ms under 100 concurrent users"), not vague ("fast", "user-friendly", "scalable").
- [ ] Each leaf of the quality tree links to (or is backed by) at least one scenario in 10.2; scenarios trace back to a tree category.

### Do's (from the official tips)
- [ ] Cover the top-priority quality goals from section 1.2 here in refined, testable detail — section 10 is the elaboration, section 1.2 stays short (max ~3-5 goals).
- [ ] Include **usage/application scenarios** (runtime behavior: how the system reacts to a stimulus, with performance/response measures).
- [ ] Include **change scenarios** (modifiability/evolution: what a specific change costs in effort/time).
- [ ] Include **fault/error/failure scenarios** (behavior under adverse conditions, degradation, recovery).
- [ ] Use the quality tree as a completeness checklist against ISO 25010 categories — record which categories were considered even if deprioritized.
- [ ] Prioritize: mark which scenarios are architecture-critical vs. nice-to-have; scenarios should be usable as input to architecture evaluation (e.g. ATAM).

### Don'ts / anti-patterns
- [ ] Don't duplicate section 1.2's quality goals verbatim — refine them instead; if a quality goal appears in 1.2 it must be refined by scenarios here.
- [ ] Don't write a generic elaborate multi-level quality-taxonomy tree with no project-specific content (the old "document the full quality tree" tip is deprecated) — keep the tree lean and specific.
- [ ] Don't list qualities without scenarios (bare adjectives like "maintainable, secure, performant" with no stimulus/response).
- [ ] Don't mix functional requirements or feature lists into this section.

### Belongs elsewhere in arc42
- [ ] Top 3-5 quality **goals** and stakeholder table → section 1 (Introduction and Goals).
- [ ] Design decisions/trade-offs made to *achieve* qualities → section 4 (Solution Strategy) and section 9 (Architecture Decisions).
- [ ] Concrete implementation concepts (security concept, caching, logging) → section 8 (Cross-cutting Concepts).
- [ ] Known unmet quality requirements / problems → section 11 (Risks and Technical Debt); term definitions → section 12 (Glossary).

---

## arc42 Section 11 (Risks & Technical Debt) — Conformance Checklist

### Required content & structure
- [ ] Section contains a list of identified **technical risks AND technical debts** — both categories, not just one.
- [ ] Items are **ordered by priority** (highest risk/most costly debt first); ordering must be evident (explicit priority column or sorted list).
- [ ] Each item states the problem concretely: what it is, where it lives (component/interface/process/data/code), and its potential impact.
- [ ] Each item names a **suggested mitigation, remediation measure, or accepted-risk decision** (even "accepted, no action" is a valid, explicit status).
- [ ] Prefer a table format: e.g. Risk/Debt | Description | Priority/Severity | Mitigation/Measure (a simple risk matrix is acceptable).
- [ ] Written for a management audience too — "risk management is project management for grown-ups": plain language, no unexplained internal jargon.

### Do's (from the official tips)
- [ ] Risks reflect **multiple stakeholder perspectives** (management, product owner, dev team, ops, users — not only developer concerns).
- [ ] External **interfaces** have been examined as risk sources (unstable APIs, third-party dependencies, protocol assumptions).
- [ ] Risks from **qualitative evaluation** (e.g. ATAM, scenario reviews) are captured here when performed.
- [ ] **Process** risks (build, deploy, release, organizational) and **data/data-structure** risks (schema debt, migration hazards) are considered.
- [ ] **Source-code-level debt** (from static analysis, hotspots, known hacks/TODOs) is represented when architecturally relevant.
- [ ] Keep entries current: remove or mark items that are resolved; date or version entries where lifespan matters.

### Anti-patterns / don'ts
- [ ] No empty boilerplate ("no known risks") when known issues exist elsewhere in the doc or issue tracker.
- [ ] No unranked laundry list — a flat, unprioritized dump defeats the section's purpose.
- [ ] No risk stated without impact or without a measure/decision attached.
- [ ] Don't limit scope to code: missing interface/process/data/stakeholder risks is a red flag.
- [ ] Don't duplicate the full issue tracker — list architecturally significant items; link out for detail.

### Does NOT belong here (lives elsewhere in arc42)
- [ ] Constraints imposed on the project → Section 2 (Constraints), not risks.
- [ ] Quality requirements/scenarios themselves → Section 10 (Quality Requirements); only their *risks of non-fulfillment* belong here.
- [ ] Decisions already made (with rationale/trade-offs) → Section 9 (Architecture Decisions); here only residual risks of those decisions.
- [ ] Solution approaches, mitigation designs in detail → Sections 4/8 (Solution Strategy / Cross-cutting Concepts); here only reference them.
- [ ] Bug lists / routine defects → issue tracker, not the architecture doc.

---

# arc42 Section 12 (Glossary) — Conformance Checklist

## Required content & structure
- [ ] Section exists as "Glossary" (arc42 section 12) and contains the most important domain and technical terms stakeholders use when discussing the system.
- [ ] Purpose is served: every term with potential for misunderstanding (synonyms, homonyms, ambiguous jargon) among stakeholders is defined once, so all readers share one meaning.
- [ ] Content is a simple two-column table: `Term | Definition` (tip 12-2 — tabular, lean format; no prose paragraphs of definitions).
- [ ] Each row defines exactly one term with a concise, unambiguous definition.
- [ ] Terms are sorted (alphabetically) for lookup.

## Do's (from official tips)
- [ ] Treat the glossary as a first-class artifact — keep it current as the doc evolves, not a one-time stub (tip 12-1).
- [ ] Where relationships between domain terms matter, supplement the table with a small graphical/domain model (e.g., a concept or class diagram) (tip 12-3).
- [ ] For international/multi-language teams, add translation column(s) (e.g., `Term | Translation | Definition`) (tip 12-4).
- [ ] Keep it compact: include only terms actually relevant to understanding this system (tip 12-5).
- [ ] Name a single responsible owner (product owner / project manager) for glossary accuracy and completeness (tip 12-6) — record the owner if the doc tracks section ownership.
- [ ] Use glossary terms consistently in the rest of the document — the definition here is the canonical one.

## Don'ts / anti-patterns (tips warn against)
- [ ] No trivia: no generally-known IT terms (HTTP, JSON, REST) or obvious words that need no definition (tip 12-5).
- [ ] No duplicate or conflicting definitions of the same concept; resolve synonyms to one canonical term (list the synonym only as a pointer to it).
- [ ] No unformatted prose glossary — convert bullet or paragraph definitions into the table.
- [ ] No stale/orphaned entries: remove terms no longer used anywhere in the architecture doc or system.

## Does NOT belong here (lives elsewhere in arc42)
- [ ] No architecture/design decisions or their rationale → section 9 (Architecture Decisions).
- [ ] No quality requirements or scenarios → sections 1.2 / 10.
- [ ] No component, interface, or building-block descriptions → sections 5–7 (a glossary entry may name a concept, not document its design).
- [ ] No risks, technical debt, or open issues → section 11.
- [ ] No cross-cutting concept explanations (patterns, frameworks, conventions) → section 8; the glossary only defines the term, not the mechanism.


