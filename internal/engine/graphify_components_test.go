package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
	btcore "github.com/rvitorper/go-bt/core"
)

// Graphify-backed reuse anchoring (arc42 Q5 "Consistency & Reuse"): the GOAP
// runners must consult the graphify knowledge graph for existing components
// before formulating research goals and implementation plans, so the
// autonomous loop reuses/extends canonical owners instead of re-implementing
// concerns. These tests drive graphify_components.go entirely through the
// graphifyComponentsQueryFn seam — engine unit tests must never exec the real
// graphify binary (the sole exception is the explicitly-guarded real-artifact
// pin test at the bottom).

// init pins the package-wide test default of the graphify query seam to an
// empty-and-healthy answer, so the many existing prompt-builder tests (grill,
// NotebookLM, Claude review, plan writer) stay offline and fast instead of
// each execing graphify ~1.5s. Tests that need hits install their own stub
// via withGraphifyQueryStub.
func init() {
	graphifyComponentsQueryFn = func(string, int) (string, error) {
		return "No matching nodes found.\n", nil
	}
}

// withGraphifyQueryStub replaces the query seam for one test and records the
// topics queried, so consumer tests can assert the topic each builder passes.
func withGraphifyQueryStub(t *testing.T, out string, err error) *[]string {
	t.Helper()
	topics := &[]string{}
	prev := graphifyComponentsQueryFn
	graphifyComponentsQueryFn = func(topic string, budget int) (string, error) {
		*topics = append(*topics, topic)
		return out, err
	}
	t.Cleanup(func() { graphifyComponentsQueryFn = prev })
	return topics
}

// graphifyQuerySampleOut mirrors the real `graphify query` output shape:
// a Traversal header, a blank line, NODE lines, and (budget-truncated runs) a
// NODE line cut off mid-way plus a trailing truncation marker.
const graphifyQuerySampleOut = `Traversal: BFS depth=2 | Start: ['Backoff()'] | 125 nodes found

NODE NewCircuitBreaker() [src=internal/reliability/reliability.go loc=L59 community=81]
NODE Shared Claude Rate-Limit Backoff Plan [src=docs/superpowers/plans/2026-07-02-backoff.md loc=L1 community=3]
NODE prompt intro [src=docs/superpowers/runs/20260704T110402-be3aa61c/prompt.md loc=L12 community=9]
NODE red output [src=docs/superpowers/runs/20260704T110402-be3aa61c/red-claude-output.md loc=L3 community=9]
NODE finish note [src=docs/superpowers/runs/20260704T110402-be3aa61c/finish.md loc=L2 community=9]
NODE superpowers_20260704T110402 worktree [src=internal/engine/foo.go loc=L1 community=2]
NODE Fleet Backoff Design [src=docs/arc42/08-crosscutting-concepts.md loc=L10 community=5]
NODE Prio
NODE Duration() [src=internal/config/config.go loc=L1220 community=64]
... (truncated to ~400 token budget)
`

func TestParseGraphifyComponentsParsesNodeLinesAndFiltersNoise(t *testing.T) {
	comps := parseGraphifyComponents(graphifyQuerySampleOut)
	if len(comps) != 3 {
		t.Fatalf("expected 3 components after noise filtering, got %d: %+v", len(comps), comps)
	}
	if comps[0].Label != "NewCircuitBreaker()" || comps[0].File != "internal/reliability/reliability.go" || comps[0].Line != 59 {
		t.Fatalf("first component parsed wrong: %+v", comps[0])
	}
	// Labels with spaces must parse whole.
	if comps[1].Label != "Fleet Backoff Design" || comps[1].File != "docs/arc42/08-crosscutting-concepts.md" || comps[1].Line != 10 {
		t.Fatalf("spaced label parsed wrong: %+v", comps[1])
	}
	if comps[2].Label != "Duration()" || comps[2].Line != 1220 {
		t.Fatalf("last component parsed wrong: %+v", comps[2])
	}
	for _, c := range comps {
		if strings.Contains(c.File, "docs/superpowers/runs/") || strings.Contains(c.File, "docs/superpowers/plans/") {
			t.Fatalf("ephemeral superpowers run/plan artifact not filtered: %+v", c)
		}
		if strings.Contains(c.Label, "superpowers_20") {
			t.Fatalf("superpowers_20 worktree label not filtered: %+v", c)
		}
	}
}

func TestParseGraphifyComponentsEmptyMatchIsHealthyEmpty(t *testing.T) {
	if comps := parseGraphifyComponents("No matching nodes found.\n"); len(comps) != 0 {
		t.Fatalf("expected no components for empty match, got %+v", comps)
	}
	if comps := parseGraphifyComponents(""); len(comps) != 0 {
		t.Fatalf("expected no components for empty output, got %+v", comps)
	}
}

func TestGraphifyComponentsPromptBlockRendersHits(t *testing.T) {
	topics := withGraphifyQueryStub(t, graphifyQuerySampleOut, nil)
	block := graphifyComponentsPromptBlock("retry backoff handling")
	if block == "" {
		t.Fatal("expected a non-empty block for a query with hits")
	}
	if !strings.HasPrefix(block, "\n") || !strings.HasSuffix(block, "\n") {
		t.Fatalf("block must carry leading+trailing newline for unconditional %%s embedding, got %q", block)
	}
	for _, want := range []string{"Q5", "REUSE", "NewCircuitBreaker()", "internal/reliability/reliability.go:59"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
	if len(*topics) == 0 || !strings.Contains((*topics)[0], "retry backoff handling") {
		t.Fatalf("expected the topic to reach the query seam, got %v", *topics)
	}
}

func TestGraphifyComponentsPromptBlockEmptyOnSeamError(t *testing.T) {
	withGraphifyQueryStub(t, "", errors.New("graphify exploded"))
	if block := graphifyComponentsPromptBlock("retry backoff"); block != "" {
		t.Fatalf("expected empty block on seam error, got %q", block)
	}
}

func TestGraphifyComponentsPromptBlockEmptyOnNoHitsOrEmptyTopic(t *testing.T) {
	withGraphifyQueryStub(t, "No matching nodes found.\n", nil)
	if block := graphifyComponentsPromptBlock("retry backoff"); block != "" {
		t.Fatalf("expected empty block for empty match, got %q", block)
	}
	if block := graphifyComponentsPromptBlock("   "); block != "" {
		t.Fatalf("expected empty block for blank topic, got %q", block)
	}
}

func TestGraphifyComponentsPromptBlockCapsHits(t *testing.T) {
	var b strings.Builder
	b.WriteString("Traversal: BFS depth=2 | Start: ['X'] | 12 nodes found\n\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "NODE Comp%d() [src=internal/engine/comp%d.go loc=L%d community=1]\n", i, i, i+1)
	}
	withGraphifyQueryStub(t, b.String(), nil)
	block := graphifyComponentsPromptBlock("components")
	if got := strings.Count(block, "\n- "); got != graphifyComponentsMaxHits {
		t.Fatalf("expected exactly %d rendered hits, got %d:\n%s", graphifyComponentsMaxHits, got, block)
	}
}

func TestGraphifyScopeGoalLineAppendsCappedSuffix(t *testing.T) {
	var b strings.Builder
	b.WriteString("Traversal: BFS depth=2 | Start: ['X'] | 5 nodes found\n\n")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, "NODE Comp%d() [src=internal/engine/comp%d.go loc=L%d community=1]\n", i, i, i+1)
	}
	withGraphifyQueryStub(t, b.String(), nil)
	line := "[P0] Improve retry coordination across agents"
	scoped := graphifyScopeGoalLine(line)
	if !strings.HasPrefix(scoped, line) {
		t.Fatalf("enriched line must keep the original line as prefix, got %q", scoped)
	}
	if !strings.Contains(scoped, "[REUSE-EXISTING: ") || !strings.HasSuffix(scoped, "]") {
		t.Fatalf("expected a bracket-closed REUSE-EXISTING suffix, got %q", scoped)
	}
	if strings.Contains(scoped, "\n") {
		t.Fatalf("enriched line must stay single-line, got %q", scoped)
	}
	if got := strings.Count(scoped, "Comp"); got != graphifyScopeMaxHits {
		t.Fatalf("expected the suffix capped at %d hits, got %d: %q", graphifyScopeMaxHits, got, scoped)
	}
}

func TestGraphifyScopeGoalLineUnchangedOnErrorEmptyOrBlank(t *testing.T) {
	withGraphifyQueryStub(t, "", errors.New("boom"))
	line := "[P1] Some goal without matches"
	if got := graphifyScopeGoalLine(line); got != line {
		t.Fatalf("expected line unchanged on seam error, got %q", got)
	}
	withGraphifyQueryStub(t, "No matching nodes found.\n", nil)
	if got := graphifyScopeGoalLine(line); got != line {
		t.Fatalf("expected line unchanged on empty match, got %q", got)
	}
	if got := graphifyScopeGoalLine(""); got != "" {
		t.Fatalf("expected blank line passthrough, got %q", got)
	}
}

// The CRITICAL goal-identity invariant: goapResearchGoalKey charges failure
// budgets and dedups by goal line. Graphify enrichment must never shift a
// goal's key — the suffix is stripped exactly like the failure-note marker.
func TestGraphifyScopeGoalLinePreservesResearchGoalKey(t *testing.T) {
	withGraphifyQueryStub(t, graphifyQuerySampleOut, nil)
	line := "[P0] Improve retry coordination across agents"
	scoped := graphifyScopeGoalLine(line)
	if scoped == line {
		t.Fatal("test needs an actually-enriched line")
	}
	if goapResearchGoalKey(scoped) != goapResearchGoalKey(line) {
		t.Fatalf("goal key must be invariant under graphify enrichment:\n  %q\n  %q", line, scoped)
	}
}

func TestBuildGrillRound1QueryIncludesGraphifyComponents(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	topics := withGraphifyQueryStub(t, graphifyQuerySampleOut, nil)
	q := buildGrillRound1Query("graph snippet here", "improve scheduler retry")
	if !strings.Contains(q, "graph snippet here") {
		t.Fatal("grill query must keep the graph snippet")
	}
	for _, want := range []string{"NewCircuitBreaker()", "REUSE", "Q5"} {
		if !strings.Contains(q, want) {
			t.Fatalf("grill round-1 query missing graphify components content %q", want)
		}
	}
	if len(*topics) == 0 || !strings.Contains((*topics)[0], "improve scheduler retry") {
		t.Fatalf("grill builder must query graphify with its reuse topic, got %v", *topics)
	}
}

func TestBuildGoapFusionNotebookLMQueryIncludesGraphifyComponents(t *testing.T) {
	topics := withGraphifyQueryStub(t, graphifyQuerySampleOut, nil)
	q := buildGoapFusionNotebookLMQuery("improve blackboard persistence", "graph report body")
	for _, want := range []string{"NewCircuitBreaker()", "REUSE", "Q5"} {
		if !strings.Contains(q, want) {
			t.Fatalf("NotebookLM query missing graphify components content %q", want)
		}
	}
	if len(*topics) == 0 || !strings.Contains((*topics)[0], "improve blackboard persistence") {
		t.Fatalf("NotebookLM builder must query graphify with its task, got %v", *topics)
	}
}

func TestBuildClaudeReviewPromptIncludesGraphifyComponents(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	topics := withGraphifyQueryStub(t, graphifyQuerySampleOut, nil)
	p := buildClaudeReviewPrompt("review focus task", goapReviewContext{mode: "structure", rangeDesc: "r", body: "b"})
	for _, want := range []string{"NewCircuitBreaker()", "REUSE", "Q5"} {
		if !strings.Contains(p, want) {
			t.Fatalf("claude review prompt missing graphify components content %q", want)
		}
	}
	if len(*topics) == 0 || !strings.Contains((*topics)[0], "review focus task") {
		t.Fatalf("claude review builder must query graphify with its task, got %v", *topics)
	}
}

// Plan composition: graphify enrichment must appear in the composed plan text
// while remaining TRANSIENT — the goal queue in ChainState (everything hashed
// by goapResearchGoalKey / budget charging) stays byte-identical.
func TestWriteSuperpowersImplementationPlanGraphifyEnrichmentIsTransient(t *testing.T) {
	prevRepo := goapFusionRepo
	goapFusionRepo = t.TempDir()
	t.Cleanup(func() { goapFusionRepo = prevRepo })
	prevAttempts := goapGoalAttemptsPath
	goapGoalAttemptsPath = filepath.Join(t.TempDir(), "attempts.json")
	t.Cleanup(func() { goapGoalAttemptsPath = prevAttempts })
	prevGrep := goapScopeGrepFn
	goapScopeGrepFn = func(string) []string { return nil }
	t.Cleanup(func() { goapScopeGrepFn = prevGrep })
	withGraphifyQueryStub(t, graphifyQuerySampleOut, nil)

	fn := GetAction("WriteSuperpowersImplementationPlan")
	if fn == nil {
		t.Fatal("WriteSuperpowersImplementationPlan not registered")
	}
	const queue = "[P0] Improve retry coordination in internal/engine/actions_goap_fusion.go"
	bb := &Blackboard{
		Task: "scheduled goap fusion",
		ChainState: map[string]any{
			"goap_fusion_goal_queue":       queue,
			"goap_fusion_improvement_gaps": "gap analysis text",
		},
	}
	if code := fn(btcore.NewBTContext(t.Context(), bb)); code != 1 {
		t.Fatalf("WriteSuperpowersImplementationPlan = %d, want 1; result: %s", code, bb.Result)
	}
	if !strings.Contains(bb.Plan, "[REUSE-EXISTING: ") || !strings.Contains(bb.Plan, "NewCircuitBreaker()") {
		t.Fatalf("composed plan must carry the graphify enrichment:\n%s", bb.Plan)
	}
	if got, _ := bb.ChainState["goap_fusion_goal_queue"].(string); got != queue {
		t.Fatalf("goal queue must never be enriched (persisted identity!):\n  want %q\n  got  %q", queue, got)
	}
	// The budget key of the queued goal is computed on the unenriched line and
	// must survive the whole composition unchanged.
	if goapResearchGoalKey(queue) != goapResearchGoalKey(graphifyScopeGoalLine(scopeGoapGoalLine(queue))) {
		t.Fatal("goal key drifted through the scope+graphify enrichment pipeline")
	}
}

// TestSeedCodeFixProgram_ConcurrentWithPersistGoapProgramAllSurvive pins the
// LAST call site of the engine-wide research.ProgramStore concurrent-writer
// lost-update gap (see research.UpdatePrograms's doc comment, and the sibling
// migrations already covered by TestPersistGoapProgram_ConcurrentCallersAllSurvive
// / TestCompleteGoapProgramMilestone_ConcurrentCallersAllSurvive in
// goap_research_goals_test.go and TestRefundGoapMilestoneAttempt_ConcurrentWritersAllSurvive
// in actions_goap_fusion_refund_test.go). seedCodeFixProgram
// (self_fix_seed.go) still does a bare research.OpenPrograms + ps.Save(),
// serialized only against OTHER self-fix seeds via selfFixStoreMu and its own
// on-disk self_fix/store.lock — it never takes research.UpdatePrograms' shared
// flock on programs.json itself. So a genuinely concurrent, already-migrated
// writer (persistGoapProgram, guarded only by that shared flock) can load its
// in-memory copy, get pre-empted by a self-fix seed's full
// read→ledger-write→save cycle that lands its own program to disk, and then
// Save its own stale copy — silently clobbering the self-fix program the
// other writer's load never saw (or vice versa).
func TestSeedCodeFixProgram_ConcurrentWithPersistGoapProgramAllSurvive(t *testing.T) {
	_, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "1000")

	const workers = 20
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sig := fmt.Sprintf("cross-sig-%d", i)
			seedCodeFixProgram(sig, fmt.Sprintf("Cross fix %d", i), fmt.Sprintf("fix cross_%d.go: defect", i), "self-fix:test:"+sig)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			bb := &Blackboard{ChainState: map[string]any{}}
			spec := &goapProgramSpec{
				Title:      fmt.Sprintf("Cross persist %d", i),
				Milestones: []string{"m1"},
			}
			persistGoapProgram(bb, spec, "test")
		}()
	}
	wg.Wait()

	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(ps.Programs), workers*2; got != want {
		t.Fatalf("lost programs under concurrent seedCodeFixProgram + persistGoapProgram (self-fix's own write bypasses the shared programs.json flock): got %d, want %d", got, want)
	}
}

func TestDeriveGraphifyReuseTopicPrefersConcreteTask(t *testing.T) {
	prev := goapProgramsPath
	goapProgramsPath = filepath.Join(t.TempDir(), "programs.json")
	t.Cleanup(func() { goapProgramsPath = prev })
	if got := deriveGraphifyReuseTopic("improve scheduler retry semantics"); got != "improve scheduler retry semantics" {
		t.Fatalf("concrete task must be its own topic, got %q", got)
	}
	// Boilerplate task and no active program: empty topic (block degrades to "").
	if got := deriveGraphifyReuseTopic("domain: goap_fusion — Runs every 4h"); got != "" {
		t.Fatalf("boilerplate task without a program must derive an empty topic, got %q", got)
	}
}

// resetGraphifyQueryRuntimeState clears the failure latch and success cache of
// the real query wrapper before and after a test that exercises it, so wrapper
// tests cannot leak a latched cooldown or cached output into each other.
func resetGraphifyQueryRuntimeState(t *testing.T) {
	t.Helper()
	reset := func() {
		graphifyComponentsMu.Lock()
		graphifyComponentsDownUntil = time.Time{}
		graphifyComponentsCache = map[string]graphifyComponentsCacheEntry{}
		graphifyComponentsMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// withGraphifyExecStub replaces the single-attempt exec seam for one test and
// counts invocations, so wrapper tests can assert retry/short-circuit/cache
// behavior without ever execing the real binary.
func withGraphifyExecStub(t *testing.T, fn func(topic string, budget int) (string, bool, error)) *int {
	t.Helper()
	resetGraphifyQueryRuntimeState(t)
	calls := new(int)
	prev := graphifyComponentsExecFn
	graphifyComponentsExecFn = func(topic string, budget int) (string, bool, error) {
		*calls++
		return fn(topic, budget)
	}
	t.Cleanup(func() { graphifyComponentsExecFn = prev })
	return calls
}

// A fast nonzero exit (torn-rebuild race) gets exactly one retry; the double
// failure latches the cooldown so every subsequent query fails fast without
// another exec, until the cooldown expires.
func TestRunGraphifyComponentsQueryRetriesFastFailureOnceThenLatchesCooldown(t *testing.T) {
	calls := withGraphifyExecStub(t, func(string, int) (string, bool, error) {
		return "error: could not load graph", false, errors.New("exit status 1")
	})
	if _, err := runGraphifyComponentsQuery("Backoff", graphifyComponentsBudget); err == nil {
		t.Fatal("expected the double fast failure to surface an error")
	}
	if *calls != 2 {
		t.Fatalf("fast failure must be retried exactly once (torn-rebuild race), got %d exec attempts", *calls)
	}
	if _, err := runGraphifyComponentsQuery("Backoff", graphifyComponentsBudget); err == nil {
		t.Fatal("expected the latched cooldown to fail fast")
	}
	if *calls != 2 {
		t.Fatalf("latched cooldown must short-circuit without an exec, got %d attempts", *calls)
	}
	// An expired cooldown re-allows queries.
	graphifyComponentsMu.Lock()
	graphifyComponentsDownUntil = time.Now().Add(-time.Second)
	graphifyComponentsMu.Unlock()
	if _, err := runGraphifyComponentsQuery("Backoff", graphifyComponentsBudget); err == nil {
		t.Fatal("expected the stub failure again after cooldown expiry")
	}
	if *calls != 4 {
		t.Fatalf("expired cooldown must re-allow the exec (with its one retry), got %d attempts", *calls)
	}
}

// A context-deadline kill (wedged graphify — the exact case the 60s timeout
// guards) must NOT be retried: an identical immediate attempt cannot help a
// wedge and would double the stall to 2x the timeout per query.
func TestRunGraphifyComponentsQueryDoesNotRetryTimeout(t *testing.T) {
	calls := withGraphifyExecStub(t, func(string, int) (string, bool, error) {
		return "", true, errors.New("signal: killed")
	})
	if _, err := runGraphifyComponentsQuery("Backoff", graphifyComponentsBudget); err == nil {
		t.Fatal("expected the timeout to surface an error")
	}
	if *calls != 1 {
		t.Fatalf("a deadline kill must never be retried, got %d exec attempts", *calls)
	}
	// The timeout also latches the cooldown: sibling per-goal-line queries in
	// the same plan composition fail fast instead of each re-paying the
	// timeout — the aggregate stall is bounded to a single timeout.
	if _, err := runGraphifyComponentsQuery("other topic", graphifyComponentsBudget); err == nil {
		t.Fatal("expected the latched cooldown to fail fast for sibling topics")
	}
	if *calls != 1 {
		t.Fatalf("latched cooldown must short-circuit sibling queries without an exec, got %d attempts", *calls)
	}
}

// Successful output is memoized per topic|budget, so repeated queries for the
// same task text within one cycle (review + NotebookLM builders) exec once.
func TestRunGraphifyComponentsQueryCachesSuccess(t *testing.T) {
	calls := withGraphifyExecStub(t, func(string, int) (string, bool, error) {
		return graphifyQuerySampleOut, false, nil
	})
	out1, err := runGraphifyComponentsQuery("Backoff", graphifyComponentsBudget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out2, err := runGraphifyComponentsQuery("Backoff", graphifyComponentsBudget)
	if err != nil {
		t.Fatalf("unexpected error on cached query: %v", err)
	}
	if out1 != out2 || out1 != graphifyQuerySampleOut {
		t.Fatal("cached query must return the identical successful output")
	}
	if *calls != 1 {
		t.Fatalf("identical topic must be served from cache, got %d exec attempts", *calls)
	}
	if _, err := runGraphifyComponentsQuery("different topic", graphifyComponentsBudget); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("a different topic must exec, got %d attempts", *calls)
	}
}

// The query topic for a goal line is the UNDECORATED goal text: queue
// priority/program/milestone scaffolding, the grep "(files: …)" suffix, and
// transient notes would dominate the 120-char lexical topic and return
// boilerplate-keyed hits ("Program", "milestone", title words) instead of
// goal-keyed ones.
func TestGraphifyGoalQueryTopicStripsDecoration(t *testing.T) {
	line := `[P0] Program "Self-Improving Fleet Coordination and Recovery Program" milestone 2/5: unify retry backoff across agents (files: internal/engine/a.go, internal/engine/b.go)`
	if got := graphifyGoalQueryTopic(line); got != "unify retry backoff across agents" {
		t.Fatalf("decorated program-milestone line must reduce to the bare goal, got %q", got)
	}
	if got := graphifyGoalQueryTopic("[P1] NotebookLM research: improve blackboard persistence [PREVIOUS-ATTEMPT-FAILURE: lint]"); got != "improve blackboard persistence" {
		t.Fatalf("queue prefixes and failure note must be stripped, got %q", got)
	}
	if got := graphifyGoalQueryTopic("plain goal text"); got != "plain goal text" {
		t.Fatalf("undecorated goal must pass through, got %q", got)
	}
}

func TestGraphifyScopeGoalLineQueriesWithUndecoratedTopic(t *testing.T) {
	topics := withGraphifyQueryStub(t, graphifyQuerySampleOut, nil)
	line := `[P0] Program "Q4 Personalization & Self-Growth Program" milestone 1/3: unify retry backoff across agents (files: internal/engine/a.go)`
	if got := graphifyScopeGoalLine(line); got == line {
		t.Fatal("test needs an actually-enriched line")
	}
	if len(*topics) != 1 {
		t.Fatalf("expected exactly one query, got %v", *topics)
	}
	topic := (*topics)[0]
	if topic != "unify retry backoff across agents" {
		t.Fatalf("scope query must use the undecorated goal text as topic, got %q", topic)
	}
}

// REUSE-EXISTING hits are ADVISORY: their .go paths must never expand — or,
// for a formerly pathless goal, entirely define — a task's modify scope. A
// pathless goal whose only paths sit in the reuse suffix falls back to the
// legacy deterministic template exactly as before enrichment.
func TestBuildGoalDrivenImplementationPlanIgnoresReuseNotePathsForFileScope(t *testing.T) {
	prevAttempts := goapGoalAttemptsPath
	goapGoalAttemptsPath = filepath.Join(t.TempDir(), "attempts.json")
	t.Cleanup(func() { goapGoalAttemptsPath = prevAttempts })
	pathless := "GOAP goals:\n[P0] Improve queue fairness [REUSE-EXISTING: TaskStore internal/dashboard/tasks.go:L12]"
	plan := buildGoalDrivenImplementationPlan(pathless)
	if strings.Contains(plan, "- Modify: internal/dashboard/tasks.go") {
		t.Fatalf("noise reuse paths must not scope a pathless goal:\n%s", plan)
	}

	scoped := "GOAP goals:\n[P0] Improve internal/engine/actions_goap_fusion.go queue fairness [REUSE-EXISTING: TaskStore internal/dashboard/tasks.go:L12]"
	plan = buildGoalDrivenImplementationPlan(scoped)
	if !strings.Contains(plan, "- Modify: internal/engine/actions_goap_fusion.go") {
		t.Fatalf("the goal's own path must scope the task:\n%s", plan)
	}
	if strings.Contains(plan, "- Modify: internal/dashboard/tasks.go") || strings.Contains(plan, "./internal/dashboard") {
		t.Fatalf("reuse-suffix paths must not expand the modify scope or test packages:\n%s", plan)
	}
}

// The durable goap:implemented store must record the STRIPPED objective/title:
// the reuse suffix carries volatile graph loc=L<n> coordinates, so persisting
// enriched text would give the same landed goal a new content key per graphify
// rebuild — breaking SeenCount dedup and re-proposing landed work.
func TestRecordImplementedGoalsStripsTransientEnrichment(t *testing.T) {
	prevKnowledge := btFusionKnowledgePath
	btFusionKnowledgePath = filepath.Join(t.TempDir(), "knowledge.json")
	t.Cleanup(func() { btFusionKnowledgePath = prevKnowledge })
	prevAttempts := goapGoalAttemptsPath
	goapGoalAttemptsPath = filepath.Join(t.TempDir(), "attempts.json")
	t.Cleanup(func() { goapGoalAttemptsPath = prevAttempts })

	clean := "Implement the complete, verified change for this goal: [P0] Improve retry coordination (files: internal/engine/x.go)"
	enrichedV1 := clean + " [REUSE-EXISTING: NewCircuitBreaker() internal/reliability/reliability.go:L59]"
	enrichedV2 := clean + " [REUSE-EXISTING: NewCircuitBreaker() internal/reliability/reliability.go:L444]"
	recordImplementedGoals(&SuperpowersRun{Tasks: []SuperpowersTask{{
		Title:     "Improve retry [REUSE-EXISTING: NewCircuitBreaker() internal/reliability/reliability.go:L59]",
		Objective: enrichedV1,
		Status:    "done",
	}}})
	// A later cycle re-lands the same goal after a graph rebuild shifted the
	// hit coordinates: it must dedup onto the SAME entry.
	recordImplementedGoals(&SuperpowersRun{Tasks: []SuperpowersTask{{
		Title:     "Improve retry",
		Objective: enrichedV2,
		Status:    "done",
	}}})

	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		t.Fatalf("open knowledge store: %v", err)
	}
	if !store.Known(clean) {
		t.Fatal("the stripped objective must be the recorded identity")
	}
	if store.Known(enrichedV1) || store.Known(enrichedV2) {
		t.Fatal("enriched objective text must never be a recorded identity")
	}
	if store.Len() != 1 {
		t.Fatalf("volatile enrichment coordinates must not fork entries, got %d", store.Len())
	}
	for _, e := range store.Entries {
		if strings.Contains(e.Title, "[REUSE-EXISTING") {
			t.Fatalf("persisted title must not carry the transient suffix: %q", e.Title)
		}
		if e.SeenCount != 2 {
			t.Fatalf("re-landing the goal must bump SeenCount on the same entry, got %d", e.SeenCount)
		}
	}

	// The stale-carryover check reads through the same stripping, so a
	// re-enriched plan for an already-landed goal is recognized.
	plan := fmt.Sprintf("### Task 1: t\n\n**Objective:** %s\n\n**Files:**\n- Modify: internal/engine/x.go\n\n**Step 2: Run RED**\nRun: /usr/local/go/bin/go test ./internal/engine -short -count=1\n\n**Step 4: Run GREEN**\nRun: /usr/local/go/bin/go test ./internal/engine -short -count=1\n", enrichedV2)
	if !superpowersPlanAlreadyImplemented(plan) {
		t.Fatal("an enriched carryover plan whose goals all landed must be detected as already implemented")
	}
}

// Real-artifact pin: the production query path must keep parsing the real
// graphify output against the real graph. Skips when graphify or the graph
// artifact is genuinely absent — and on exec failure: the daemons in this
// checkout rebuild graph.json non-atomically every cycle, so a query landing
// inside the write window dies on a torn graph with no code defect. Only a
// SUCCESSFUL exec whose output no longer parses is format drift, which is
// what this pin exists to catch.
func TestGraphifyQueryRealArtifactPin(t *testing.T) {
	if _, err := resolveGraphifyBin(); err != nil {
		t.Skipf("graphify not resolvable: %v", err)
	}
	if _, err := os.Stat(filepath.Join("..", "..", "graphify-out", "graph.json")); err != nil {
		t.Skipf("graph artifact absent: %v", err)
	}
	resetGraphifyQueryRuntimeState(t)
	out, err := runGraphifyComponentsQuery("Backoff", graphifyComponentsBudget)
	if err != nil {
		t.Skipf("real graphify query failed (environmental: concurrent graph.json rebuild or wedged graphify, not format drift): %v\n%s", err, truncateGoap(out, 500))
	}
	comps := parseGraphifyComponents(out)
	if len(comps) < 1 {
		t.Fatalf("real graphify output no longer parses to components — format drift?\n%s", truncateGoap(out, 1500))
	}
}
