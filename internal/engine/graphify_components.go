package engine

// Graphify-backed reuse anchoring (2026-07-18): arc42 quality goal Q5
// "Consistency & Reuse" (docs/arc42/01-introduction-goals.md §1.2) demands one
// canonical implementation per concept — new capabilities must reuse or extend
// existing components, not duplicate them. The GOAP runners used to formulate
// research goals and implementation plans with NO inventory of related
// existing code, so the autonomous loop kept re-implementing concerns. This
// file makes the runners consult the graphify knowledge graph (pure lexical
// match + BFS over graphify-out/graph.json — offline, ~1.5s, no LLM):
//
// Consumers:
//   - buildGrillRound1Query (actions_goap_fusion.go) — answers its own
//     "What existing platform components can we leverage?" question.
//   - buildGoapFusionNotebookLMQuery (actions_goap_fusion.go) — topic = task.
//   - buildClaudeReviewPrompt (actions_goap_fusion_claude_review.go) — fourth
//     context block, topic = task.
//   - WriteSuperpowersImplementationPlan (actions_goap_fusion_prod_additions.go)
//     — graphifyScopeGoalLine appends REUSE-EXISTING hits per goal line, right
//     after the lexical scopeGoapGoalLine grep scoping. The enrichment is
//     TRANSIENT and ADVISORY: applied only to the composed plan/task text and
//     stripped everywhere that text flows back into durable state or file
//     scope — stripGoapGoalTransientNotes (actions_goap_fusion_goal_budget.go)
//     is the single owner of that stripping, used by goapResearchGoalKey
//     (budget/dedup identity), recordImplementedGoals and
//     superpowersPlanAlreadyImplemented (the goap:implemented store must key
//     on stable goal text — graph loc=L<n> coordinates are volatile per
//     rebuild), and the plan builder's file-scope extraction (lexical-noise
//     graph hits must never define a task's modify scope).
//
// buildSeedProgramPrompt is deliberately NOT wired: no specific topic exists
// pre-proposal (the seeder brainstorms the topic itself), and seeded
// milestones get coverage later via the plan-composition enrichment.
//
// Everything degrades to "" / line-unchanged on any failure (graphify absent,
// torn graph.json mid-rebuild, empty match) so callers embed the block
// unconditionally and no cycle ever fails on reuse anchoring. No logging in
// the block path — these run inside prompt builders.

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

const (
	// graphifyComponentsTimeout bounds one graphify query exec; queries are
	// ~1.5s against the current graph, so 60s only guards a wedged rebuild.
	graphifyComponentsTimeout = 60 * time.Second
	// graphifyComponentsBudget is the graphify --budget (output token cap).
	graphifyComponentsBudget = 400
	// graphifyComponentsMaxHits caps the rendered prompt-block bullet list.
	graphifyComponentsMaxHits = 8
	// graphifyScopeMaxHits caps the REUSE-EXISTING goal-line suffix hits.
	graphifyScopeMaxHits = 3
	// graphifyComponentsTopicCap bounds the query topic length.
	graphifyComponentsTopicCap = 120
	// graphifyScopeSuffixCap bounds the joined REUSE-EXISTING suffix body so
	// an enriched goal line stays a readable single line.
	graphifyScopeSuffixCap = 240
	// graphifyComponentsFailureCooldown suspends ALL graphify queries after a
	// failed one. Plan composition execs one query per goal line SERIALLY plus
	// one per prompt builder; without this latch a wedged graphify (the exact
	// case the 60s timeout guards) would cost the full timeout per call —
	// ~10+ minutes of dead latency in one cycle. With it, one query proves
	// graphify down and every sibling call fails fast for the cooldown window,
	// bounding the aggregate stall to a single timeout.
	graphifyComponentsFailureCooldown = 5 * time.Minute
	// graphifyComponentsCacheTTL bounds how long a successful query result is
	// reused for an identical topic (the review and NotebookLM builders often
	// query the same task text within one cycle).
	graphifyComponentsCacheTTL = 10 * time.Minute
	// graphifyComponentsCacheMax caps the success cache; on overflow the whole
	// map is dropped (topics churn per cycle — precision pruning is not worth
	// the code).
	graphifyComponentsCacheMax = 32
)

// goapGoalReuseNoteMarker introduces the transient reuse annotation appended
// to a composed goal line. goapResearchGoalKey strips it (like the failure
// note marker), so even if an enriched line ever reaches a key computation
// the goal's budget/dedup identity is preserved.
const goapGoalReuseNoteMarker = "[REUSE-EXISTING:"

// graphifyComponent is one knowledge-graph hit: an existing platform
// component candidate for reuse.
type graphifyComponent struct {
	Label string
	File  string
	Line  int
}

// graphifyComponentsQueryFn is the injectable graphify query runner (test
// seam — unit tests must never exec the real binary). The default implements
// the real exec with a single retry for torn-rebuild races.
var graphifyComponentsQueryFn = runGraphifyComponentsQuery

// graphifyComponentsExecFn runs ONE graphify query attempt (test seam for the
// retry/cooldown/cache wrapper below — its unit tests must never exec the real
// binary). timedOut distinguishes a context-deadline kill (wedged graphify;
// the process dies with "signal: killed", NOT context.DeadlineExceeded, so the
// wrapper must key on ctx.Err(), never on the returned error) from a fast
// nonzero exit (torn-rebuild race).
var graphifyComponentsExecFn = func(topic string, budget int) (out string, timedOut bool, err error) {
	bin, err := resolveGraphifyBin()
	if err != nil {
		return "", false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), graphifyComponentsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "query", topic, "--budget", strconv.Itoa(budget))
	cmd.Dir = goapFusionRepo
	b, runErr := cmd.CombinedOutput()
	return string(b), ctx.Err() == context.DeadlineExceeded, runErr
}

// graphifyComponentsMu guards the failure latch and success cache below.
var graphifyComponentsMu sync.Mutex

// graphifyComponentsDownUntil is the failure latch: while now < DownUntil,
// every query short-circuits with an error instead of re-paying an exec (and
// possibly the full timeout) that one failed sibling call already proved
// pointless.
var graphifyComponentsDownUntil time.Time

type graphifyComponentsCacheEntry struct {
	out string
	at  time.Time
}

// graphifyComponentsCache memoizes successful query output per topic|budget.
var graphifyComponentsCache = map[string]graphifyComponentsCacheEntry{}

// runGraphifyComponentsQuery execs `graphify query <topic> --budget <n>` in
// the main repo via the canonical resolver (resolveGraphifyBin — PATH-robust
// since the 2026-07-13 reboot PATH loss), wrapped in a success cache and a
// failure cooldown latch. graph.json can be torn mid-rebuild, so a FAST
// nonzero exit is retried once; a context-deadline kill is never retried — an
// identical immediate attempt cannot help a wedged graphify and would only
// double the stall to 2x the timeout. Any failure latches the cooldown so the
// serial per-goal-line callers in plan composition fail fast instead of each
// re-paying the timeout.
func runGraphifyComponentsQuery(topic string, budget int) (string, error) {
	key := strconv.Itoa(budget) + "|" + topic
	now := time.Now()
	graphifyComponentsMu.Lock()
	if e, ok := graphifyComponentsCache[key]; ok && now.Sub(e.at) < graphifyComponentsCacheTTL {
		graphifyComponentsMu.Unlock()
		return e.out, nil
	}
	if now.Before(graphifyComponentsDownUntil) {
		until := graphifyComponentsDownUntil
		graphifyComponentsMu.Unlock()
		return "", fmt.Errorf("graphify queries suspended until %s after a failed query (wedged-graphify fail-fast)", until.Format(time.RFC3339))
	}
	graphifyComponentsMu.Unlock()

	out, timedOut, err := graphifyComponentsExecFn(topic, budget)
	if err != nil && !timedOut {
		out, _, err = graphifyComponentsExecFn(topic, budget)
	}
	graphifyComponentsMu.Lock()
	defer graphifyComponentsMu.Unlock()
	if err != nil {
		graphifyComponentsDownUntil = time.Now().Add(graphifyComponentsFailureCooldown)
		return out, err
	}
	if len(graphifyComponentsCache) >= graphifyComponentsCacheMax {
		graphifyComponentsCache = map[string]graphifyComponentsCacheEntry{}
	}
	graphifyComponentsCache[key] = graphifyComponentsCacheEntry{out: out, at: time.Now()}
	return out, nil
}

// graphifyNodeLineRe matches one graphify query hit line:
//
//	NODE <label> [src=<file> loc=L<n> community=<id>]
//
// Labels may contain spaces; budget-truncated lines (no bracket group) simply
// do not match and are skipped.
var graphifyNodeLineRe = regexp.MustCompile(`^NODE\s+(.*?)\s+\[src=([^\s\]]+)\s+loc=L(\d+)[^\]]*\]`)

// graphifyNoiseDocBasenames are per-run artifact documents from superpowers
// runs; hits inside them describe ephemeral run transcripts, not platform
// components.
var graphifyNoiseDocBasenames = map[string]bool{
	"prompt.md":              true,
	"red-claude-output.md":   true,
	"green-claude-output.md": true,
	"claude-output.md":       true,
	"finish.md":              true,
}

// isGraphifyNoiseComponent drops knowledge-graph noise: roughly a third of
// nodes come from ephemeral superpowers run/plan artifacts, which must never
// be presented as reusable platform components.
func isGraphifyNoiseComponent(label, src string) bool {
	if strings.Contains(src, "superpowers_20") || strings.Contains(label, "superpowers_20") {
		return true
	}
	if strings.Contains(src, "docs/superpowers/runs/") || strings.Contains(src, "docs/superpowers/plans/") {
		return true
	}
	return graphifyNoiseDocBasenames[path.Base(src)]
}

// parseGraphifyComponents parses graphify query output into components,
// applying the noise filter. "No matching nodes found." (and any output with
// no parseable NODE lines) yields an empty, healthy result.
func parseGraphifyComponents(out string) []graphifyComponent {
	var comps []graphifyComponent
	for _, line := range splitNonEmptyLines(out) {
		m := graphifyNodeLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		label, src := m[1], m[2]
		if label == "" || isGraphifyNoiseComponent(label, src) {
			continue
		}
		n, err := strconv.Atoi(m[3])
		if err != nil {
			continue
		}
		comps = append(comps, graphifyComponent{Label: label, File: src, Line: n})
	}
	return comps
}

// graphifyReuseTopic normalizes a free-form topic for the lexical matcher:
// single line, bounded length, cut at a word boundary.
func graphifyReuseTopic(topic string) string {
	t := collapseToSingleLine(topic)
	if len(t) > graphifyComponentsTopicCap {
		t = t[:graphifyComponentsTopicCap]
		if i := strings.LastIndexByte(t, ' '); i > 0 {
			t = t[:i]
		}
	}
	return strings.TrimSpace(t)
}

// queryGraphifyComponents runs one reuse query end to end: topic
// normalization, the seam, parsing, noise filtering. Nil on any failure or
// empty topic — callers degrade silently.
func queryGraphifyComponents(topic string) []graphifyComponent {
	t := graphifyReuseTopic(topic)
	if t == "" {
		return nil
	}
	out, err := graphifyComponentsQueryFn(t, graphifyComponentsBudget)
	if err != nil {
		return nil
	}
	return parseGraphifyComponents(out)
}

// graphifyComponentsPromptBlock renders existing components related to topic
// as a reuse instruction block for research/review prompts. Empty on any
// failure or empty match so callers embed it unconditionally via plain %s;
// non-empty rendering carries leading+trailing newline like
// implementedGoalsPromptBlock.
func graphifyComponentsPromptBlock(topic string) string {
	comps := queryGraphifyComponents(topic)
	if len(comps) == 0 {
		return ""
	}
	if len(comps) > graphifyComponentsMaxHits {
		comps = comps[:graphifyComponentsMaxHits]
	}
	var b strings.Builder
	b.WriteString("\nExisting platform components related to this task (graphify knowledge graph). REUSE or EXTEND these instead of writing new implementations — one canonical owner per concept (arc42 Q5):\n")
	for _, c := range comps {
		fmt.Fprintf(&b, "- %s — %s:%d\n", collapseToSingleLine(c.Label), c.File, c.Line)
	}
	return b.String()
}

// Decoration strippers for graphifyGoalQueryTopic: goal-queue lines arrive
// wrapped in scaffolding that would dominate the 120-char lexical query topic
// and return hits keyed on "Program"/"milestone"/title boilerplate instead of
// the goal (observed live: a decorated milestone line matched
// ProgramStore/TaskStore scaffolding while the bare goal matched the right
// component).
var (
	goapGoalPriorityPrefixRe = regexp.MustCompile(`^\[P\d\]\s*`)
	goapGoalProgramPrefixRe  = regexp.MustCompile(`^Program\s+"(?:[^"\\]|\\.)*"\s+milestone\s+\d+/\d+:\s*`)
	goapGoalFilesSuffixRe    = regexp.MustCompile(`\s*\(files:[^)]*\)\s*$`)
)

// graphifyGoalQueryTopic reduces a decorated goal-queue line to the goal text
// itself before it becomes a query topic: the "[Pn]"/"NotebookLM research:"
// queue prefixes, the `Program "…" milestone n/m:` scaffolding, the grep
// "(files: …)" suffix, and any transient failure/reuse notes are all lexical
// noise for the graph matcher.
func graphifyGoalQueryTopic(line string) string {
	t := stripGoapGoalTransientNotes(collapseToSingleLine(line))
	t = goapGoalPriorityPrefixRe.ReplaceAllString(t, "")
	t = strings.TrimSpace(strings.TrimPrefix(t, "NotebookLM research:"))
	t = goapGoalProgramPrefixRe.ReplaceAllString(t, "")
	t = goapGoalFilesSuffixRe.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

// graphifyScopeGoalLine appends the top graph hits for a goal line as a
// single-line, bracket-closed REUSE-EXISTING suffix, complementing
// scopeGoapGoalLine's lexical grep scoping. The query topic is the UNDECORATED
// goal text (graphifyGoalQueryTopic), not the raw line. The line is returned
// unchanged on any failure or empty match. TRANSIENT and ADVISORY by contract:
// apply only to composed plan/prompt text, never to persisted goal state, and
// never let the hit paths define a task's file scope (the plan builder
// extracts modify files from the stripped goal text).
func graphifyScopeGoalLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return line
	}
	comps := queryGraphifyComponents(graphifyGoalQueryTopic(line))
	if len(comps) == 0 {
		return line
	}
	if len(comps) > graphifyScopeMaxHits {
		comps = comps[:graphifyScopeMaxHits]
	}
	parts := make([]string, 0, len(comps))
	for _, c := range comps {
		parts = append(parts, fmt.Sprintf("%s %s:L%d", collapseToSingleLine(c.Label), c.File, c.Line))
	}
	suffix := collapseToSingleLine(truncateGoap(strings.Join(parts, "; "), graphifyScopeSuffixCap))
	return collapseToSingleLine(line) + " " + goapGoalReuseNoteMarker + " " + suffix + "]"
}

// deriveGraphifyReuseTopic picks the reuse-query topic for the grill opener,
// which only has the scheduled task text at hand: the task itself when it
// reads as a genuine topic, else the active program's next milestone (the
// same priority idea as deriveNotebookLMResearchQuery), else "" — a generic
// task simply yields no reuse block.
func deriveGraphifyReuseTopic(task string) string {
	t := strings.TrimSpace(task)
	if !isBoilerplateResearchTopic(t) {
		return t
	}
	if ps, err := research.OpenPrograms(goapProgramsPath); err == nil {
		if p := ps.Active(); p != nil {
			if _, m := p.NextMilestone(); m != nil && !isBoilerplateResearchTopic(m.Goal) {
				return strings.TrimSpace(m.Goal)
			}
		}
	}
	return ""
}
