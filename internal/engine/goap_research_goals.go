package engine

// Multi-goal research plumbing: the research prompts ask for up to three
// ranked GOALn/GAPn/FILESn blocks (optionally a PROGRAM with milestones for
// changes too large for one cycle), every research source APPENDS to a shared
// goal list instead of overwriting a single slot, and goals that name no Go
// files get deterministically scoped via git grep before planning. Together
// these feed the goal-driven plan builder enough file-scoped goals to
// actually fan out to multi-task cycles.

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/research"
)

type goapResearchGoal struct {
	Goal  string
	Gap   string
	Files string
}

// Line renders the goal as one queue line, embedding FILES paths when the
// goal text itself names none — the plan builder extracts file scope from
// the line text, so paths must appear inline.
func (g goapResearchGoal) Line() string {
	line := strings.TrimSpace(g.Goal)
	if g.Files == "" {
		return line
	}
	var missing []string
	for _, f := range extractGoFilePaths(g.Files) {
		if !strings.Contains(line, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		line += " (files: " + strings.Join(missing, ", ") + ")"
	}
	return line
}

var goapGoalBlockRe = regexp.MustCompile(`^(GOAL|GAP|FILES)(\d?):\s*(.*)$`)

// extractGoapResearchGoals parses GOAL/GAP/FILES blocks from a research
// answer. Numbered blocks (GOAL1..GOAL3) and the legacy unnumbered GOAL:
// format both parse; unnumbered maps to index 1.
func extractGoapResearchGoals(answer string) []goapResearchGoal {
	byIndex := map[string]*goapResearchGoal{}
	var order []string
	for _, line := range strings.Split(answer, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "-*• \t"))
		trimmed = strings.TrimSpace(strings.ReplaceAll(trimmed, "**", ""))
		m := goapGoalBlockRe.FindStringSubmatch(strings.ToUpper(trimmed))
		if m == nil {
			continue
		}
		idx := m[2]
		if idx == "" {
			idx = "1"
		}
		// Re-extract the value from the original (case-preserved) line.
		value := strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
		value = strings.Trim(value, `"`)
		if _, ok := byIndex[idx]; !ok {
			byIndex[idx] = &goapResearchGoal{}
			order = append(order, idx)
		}
		switch m[1] {
		case "GOAL":
			byIndex[idx].Goal = value
		case "GAP":
			byIndex[idx].Gap = value
		case "FILES":
			byIndex[idx].Files = value
		}
	}
	sort.Strings(order)
	var goals []goapResearchGoal
	for _, idx := range order {
		if g := byIndex[idx]; strings.TrimSpace(g.Goal) != "" {
			goals = append(goals, *g)
		}
	}
	if len(goals) > maxGoalDrivenTasks {
		goals = goals[:maxGoalDrivenTasks]
	}
	return goals
}

// appendGoapResearchGoals accumulates goals from every research source
// (grill, NotebookLM, Claude review) into shared ChainState lists — the old
// single notebooklm_goal slot meant last-writer-wins and starved the
// multi-task plan builder. The first goal also fills the legacy single keys
// for downstream compatibility.
func appendGoapResearchGoals(bb *Blackboard, goals []goapResearchGoal) {
	if len(goals) == 0 {
		return
	}
	existingGoals, _ := bb.ChainState["goap_fusion_notebooklm_goals"].(string)
	existingGaps, _ := bb.ChainState["goap_fusion_notebooklm_gaps"].(string)
	goalLines := splitNonEmptyLines(existingGoals)
	gapLines := splitNonEmptyLines(existingGaps)
	seen := map[string]bool{}
	for _, l := range goalLines {
		seen[research.Key(l)] = true
	}
	for _, g := range goals {
		// Both lists are persisted as independent \n-joined strings and
		// re-split line-by-line, then goal[i] is paired with gap[i]. A goal
		// or gap that itself spans multiple lines would add extra entries to
		// one list only and silently desync the pairing, so collapse each to
		// a single physical line before storing.
		line := collapseToSingleLine(g.Line())
		if line == "" || seen[research.Key(line)] {
			continue
		}
		// Prose that slipped through GOAL-block parsing must not reach the
		// plan builder as a task.
		if !isActionableGoapGoal(line) {
			continue
		}
		seen[research.Key(line)] = true
		goalLines = append(goalLines, line)
		gap := collapseToSingleLine(g.Gap)
		if gap == "" {
			gap = "research-backed improvement"
		}
		gapLines = append(gapLines, gap)
	}
	setGoapState(bb, "notebooklm_goals", strings.Join(goalLines, "\n"))
	setGoapState(bb, "notebooklm_gaps", strings.Join(gapLines, "\n"))
	if len(goalLines) > 0 {
		if cur, _ := bb.ChainState["goap_fusion_notebooklm_goal"].(string); strings.TrimSpace(cur) == "" {
			setGoapState(bb, "notebooklm_goal", goalLines[0])
			if len(gapLines) > 0 {
				setGoapState(bb, "notebooklm_gap", gapLines[0])
			}
		}
	}
}

// goapProseGoalRe matches summary/prose openers that must never become
// goals — run 20260704T080615 planned a task titled "Review complete.
// Summary of what I found…" because the fallback grabbed the first prose
// line of a review answer.
var goapProseGoalRe = regexp.MustCompile(`(?i)^(review complete|summary|here is|here's|overall|in summary|analysis|the following|no issues|everything looks|i found|i reviewed|looking at)`)

// goapImperativeVerbs are the verbs an actionable goal may open with when it
// names no Go file path.
var goapImperativeVerbs = map[string]bool{
	"add": true, "implement": true, "fix": true, "make": true, "wire": true,
	"extend": true, "refactor": true, "create": true, "move": true,
	"replace": true, "guard": true, "persist": true, "clear": true,
	"ensure": true, "define": true, "introduce": true, "harden": true,
	"split": true, "extract": true, "update": true, "remove": true,
	"stop": true, "prevent": true, "convert": true, "rename": true,
	"cache": true, "record": true, "validate": true, "verify": true,
	"scope": true, "unblock": true, "build": true, "prototype": true,
	"expand": true, "reduce": true, "delete": true, "migrate": true,
}

// isActionableGoapGoal reports whether a candidate goal line is an
// implementable instruction rather than review prose: it must not open with
// a summary phrase, be a sensible length, and either name a Go file or open
// with an imperative verb.
func isActionableGoapGoal(goal string) bool {
	g := strings.TrimSpace(goal)
	if len(g) < 12 || len(g) > 400 {
		return false
	}
	if goapProseGoalRe.MatchString(g) {
		return false
	}
	if len(extractGoFilePaths(g)) > 0 {
		return true
	}
	first := strings.ToLower(strings.Trim(strings.Fields(g)[0], ".,:;"))
	return goapImperativeVerbs[first]
}

// fallbackGoapGoal scans a research answer for the first ACTIONABLE line —
// the replacement for the old first-non-empty-line fallback, which turned
// prose summaries into planned tasks. All-prose answers yield "" (the caller
// then treats the research as having produced nothing).
func fallbackGoapGoal(answer string) string {
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "-*# "))
		if line == "" || strings.HasPrefix(line, "{") || strings.HasPrefix(line, "}") {
			continue
		}
		if isActionableGoapGoal(line) {
			return line
		}
	}
	return ""
}

// goapResearchGoalLines returns the accumulated goal lines for this cycle.
func goapResearchGoalLines(bb *Blackboard) []string {
	goals, _ := bb.ChainState["goap_fusion_notebooklm_goals"].(string)
	lines := splitNonEmptyLines(goals)
	if len(lines) == 0 {
		if single, _ := bb.ChainState["goap_fusion_notebooklm_goal"].(string); strings.TrimSpace(single) != "" {
			lines = []string{strings.TrimSpace(single)}
		}
	}
	return lines
}

func goapResearchGapLines(bb *Blackboard) []string {
	gaps, _ := bb.ChainState["goap_fusion_notebooklm_gaps"].(string)
	return splitNonEmptyLines(gaps)
}

// goapFusionNotebookLMGoalsFromGaps returns every research goal recorded in
// the gap analysis (the singular legacy variant returns only the first).
func goapFusionNotebookLMGoalsFromGaps(gaps string) []string {
	var out []string
	for _, line := range strings.Split(gaps, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "NOTEBOOKLM_GOAL:") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(t, "NOTEBOOKLM_GOAL:")))
		}
	}
	return out
}

// collapseToSingleLine flattens any embedded newlines (and surrounding
// whitespace) into single spaces so a value stored in a \n-joined ChainState
// list occupies exactly one physical line. This keeps the parallel
// goap_fusion_notebooklm_goals / goap_fusion_notebooklm_gaps lists the same
// length, so index-based goal/gap pairing stays aligned.
func collapseToSingleLine(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func splitNonEmptyLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}

// goapProgramSpec is a research-proposed multi-cycle program: a change too
// large for one scheduled run, split into file-scoped milestones that
// successive cycles execute one at a time.
type goapProgramSpec struct {
	Title      string
	Milestones []string
}

var goapMilestoneRe = regexp.MustCompile(`^MILESTONE(\d+):\s*(.+)$`)

func extractGoapProgram(answer string) *goapProgramSpec {
	var spec *goapProgramSpec
	for _, line := range strings.Split(answer, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "-*• \t"))
		trimmed = strings.TrimSpace(strings.ReplaceAll(trimmed, "**", ""))
		if strings.HasPrefix(strings.ToUpper(trimmed), "PROGRAM:") {
			title := strings.TrimSpace(trimmed[len("PROGRAM:"):])
			if title != "" {
				spec = &goapProgramSpec{Title: title}
			}
			continue
		}
		if spec == nil {
			continue
		}
		if m := goapMilestoneRe.FindStringSubmatch(strings.ToUpper(trimmed)); m != nil {
			value := strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
			if value != "" {
				spec.Milestones = append(spec.Milestones, value)
			}
		}
	}
	if spec == nil || len(spec.Milestones) == 0 {
		return nil
	}
	return spec
}

// goapProgramsPath is the multi-cycle program backlog (test seam).
var goapProgramsPath = research.DefaultProgramsPath()

// persistGoapProgram registers a research-proposed multi-cycle program;
// Add dedupes by title so re-proposals across cycles are harmless.
func persistGoapProgram(bb *Blackboard, spec *goapProgramSpec, source string) {
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		setGoapState(bb, "program_error", err.Error())
		return
	}
	ps.Add(spec.Title, source, spec.Milestones)
	if err := ps.Save(); err != nil {
		setGoapState(bb, "program_error", err.Error())
		return
	}
	setGoapState(bb, "program_registered", spec.Title)
}

// recentImplementedGoals lists the newest implemented-goal titles from the
// shared research knowledge store, so research prompts can say "already
// done — do not re-propose". Best-effort: an unreadable store yields nil.
func recentImplementedGoals(n int) []string {
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		return nil
	}
	var entries []*research.Entry
	for _, e := range store.Entries {
		if strings.HasPrefix(e.Source, "goap:implemented") {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].LastSeen.After(entries[j].LastSeen) })
	var titles []string
	for _, e := range entries {
		titles = append(titles, e.Title)
		if len(titles) == n {
			break
		}
	}
	return titles
}

// recordImplementedGoals persists this run's completed task objectives so
// future research cycles do not re-propose landed work.
func recordImplementedGoals(run *SuperpowersRun) {
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		return
	}
	for _, task := range run.Tasks {
		if task.Status != "done" && task.Status != "completed" {
			continue
		}
		title := task.Title
		if len(title) > 120 {
			title = title[:120]
		}
		store.Record("goap:implemented", title, task.Objective)
	}
	_ = store.Save()
}

// superpowersPlanAlreadyImplemented reports whether every task objective in
// the plan is already recorded as goap:implemented in the knowledge store —
// the signature of a stale carryover plan that must not be resumed.
func superpowersPlanAlreadyImplemented(activePlan string) bool {
	tasks, err := ParseSuperpowersPlan(activePlan)
	if err != nil || len(tasks) == 0 {
		return false
	}
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		return false
	}
	for _, task := range tasks {
		if !store.Known(task.Objective) {
			return false
		}
	}
	return true
}

// completeGoapProgramMilestone marks the active program milestone done — but
// only when the applied run demonstrably executed it. PrioritizeGoapGoals
// stamps "programID:index" into ChainState when it queues a milestone;
// completing on any successful apply would let a cycle that drifted onto
// unrelated goals silently check off milestone work it never did.
func completeGoapProgramMilestone(bb *Blackboard, run *SuperpowersRun) {
	ref, _ := bb.ChainState["goap_fusion_program_milestone"].(string)
	parts := strings.SplitN(strings.TrimSpace(ref), ":", 2)
	if len(parts) != 2 {
		return
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		return
	}
	// Locate the milestone so we can verify the run against its file anchors
	// before checking it off.
	var milestone *research.Milestone
	for _, p := range ps.Programs {
		if p.ID == parts[0] && idx >= 0 && idx < len(p.Milestones) {
			milestone = &p.Milestones[idx]
			break
		}
	}
	if milestone == nil || !runExecutedMilestone(run, milestone.Goal) {
		return
	}
	if ps.MarkDone(parts[0], idx, run.ID) {
		_ = ps.Save()
		setGoapState(bb, "program_milestone_done", ref)
	}
}

// runExecutedMilestone reports whether the applied run actually did the
// milestone's work: either its changed files intersect the milestone's file
// anchors, or a done task in the run worked on an anchor file. A milestone
// naming no Go-file anchors falls back to trusting the successful apply.
func runExecutedMilestone(run *SuperpowersRun, milestoneGoal string) bool {
	anchors := extractGoFilePaths(milestoneGoal)
	if len(anchors) == 0 {
		return true
	}
	anchorSet := map[string]bool{}
	for _, a := range anchors {
		anchorSet[a] = true
	}
	for _, f := range run.ChangedFiles {
		if anchorSet[f] {
			return true
		}
	}
	for _, task := range run.Tasks {
		if task.Status != "done" && task.Status != "completed" {
			continue
		}
		for _, f := range task.Files {
			if anchorSet[f] {
				return true
			}
		}
	}
	return false
}

// loadReviewModeRound / saveReviewModeRound persist the review-mode rotation
// counter across scheduled cycles (same agent-scope blackboard pattern as the
// grill round), so commits/structure/failures modes each get regular turns.
func loadReviewModeRound(bb *Blackboard) int {
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_review_mode_round"); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(e.Value)); err == nil {
				return n
			}
		}
	}
	if s, ok := bb.ChainState["goap_fusion_review_mode_round"].(string); ok {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 0
}

func saveReviewModeRound(bb *Blackboard, round int) {
	setGoapState(bb, "review_mode_round", strconv.Itoa(round))
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_review_mode_round", strconv.Itoa(round), "claude review mode rotation counter", "text")
	}
}

// goapScopeGrepFn is the injectable git-grep used to scope fileless goals;
// tests stub it. Production greps HEAD content in the (bare) main repo.
var goapScopeGrepFn = func(keyword string) []string {
	out, err := exec.Command("git", "-C", goapFusionRepo, "grep", "-l", "-i", keyword, "HEAD", "--", "*.go").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(l, "HEAD:"))
		if l != "" && !strings.HasSuffix(l, "_test.go") {
			files = append(files, l)
		}
	}
	return files
}

var goapScopeKeywordRe = regexp.MustCompile(`[A-Za-z][A-Za-z-]{5,}`)

// scopeGoapGoalLine deterministically scopes a goal line that names no Go
// files: the goal's most specific keywords are git-grepped and the top
// matches appended as "(files: …)" so the plan builder can task the goal
// instead of dropping it to the legacy fallback.
func scopeGoapGoalLine(line string) string {
	if len(extractGoFilePaths(line)) > 0 {
		return line
	}
	var keywords []string
	for _, kw := range goapScopeKeywordRe.FindAllString(strings.ToLower(line), -1) {
		keywords = append(keywords, kw)
		for _, part := range strings.Split(kw, "-") {
			if len(part) >= 6 && part != kw {
				keywords = append(keywords, part)
			}
		}
	}
	counts := map[string]int{}
	var hits []string
	for _, kw := range keywords {
		if goapScopeStopwords[kw] {
			continue
		}
		for _, f := range goapScopeGrepFn(kw) {
			if counts[f] == 0 {
				hits = append(hits, f)
			}
			counts[f]++
		}
		if len(hits) >= 6 {
			break
		}
	}
	if len(hits) == 0 {
		return line
	}
	sort.SliceStable(hits, func(i, j int) bool { return counts[hits[i]] > counts[hits[j]] })
	if len(hits) > 3 {
		hits = hits[:3]
	}
	return fmt.Sprintf("%s (files: %s)", line, strings.Join(hits, ", "))
}

var goapScopeStopwords = map[string]bool{
	"implement": true, "implementation": true, "improve": true, "ensure": true,
	"extend": true, "verify": true, "coverage": true, "research": true,
	"notebooklm": true, "platform": true, "domain": true, "domains": true,
	"internal": true, "engine": true, "should": true, "between": true,
	"across": true, "adding": true, "change": true, "changes": true,
}
