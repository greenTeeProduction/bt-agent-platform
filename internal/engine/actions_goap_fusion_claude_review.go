package engine

// Claude Code review fallback for the GOAP fusion runner: when NotebookLM is
// unavailable (daily quota exhausted or any other failure), the ResearchRouter
// selector in GoapFusionLoopTree falls through to RunClaudeCodeReviewResearch,
// which reviews the daemon's recent auto-approved commits and emits findings
// in the same GOAL/GAP/FILES/TESTS contract the downstream pipeline consumes.
// Spec: docs/superpowers/specs/2026-07-02-goap-fusion-claude-review-fallback-design.md

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	btcore "github.com/rvitorper/go-bt/core"
)

// isGoapNotebookLMQuotaError reports whether a NotebookLM CLI failure is the
// daily-quota kind (RESOURCE_EXHAUSTED / error code 8). It is strictly a
// subset of isGoapNotebookLMFailure: quota-looking text inside a successful
// answer is not a quota error.
func isGoapNotebookLMQuotaError(out string) bool {
	if !isGoapNotebookLMFailure(out) {
		return false
	}
	lower := strings.ToLower(out)
	// Only gRPC code 8 / RESOURCE_EXHAUSTED is a daily-quota signal. nlm
	// prefixes EVERY rejected RPC with "Google rejected the query" — including
	// code 3 INVALID_ARGUMENT — so matching that phrase stamped the 24h quota
	// cache from non-quota failures and blacked out research for a full day.
	return strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "error code 8")
}

// nextNlmQuotaReset returns when the NotebookLM daily quota next resets:
// midnight America/Los_Angeles (Google API daily quotas reset at midnight
// Pacific). If the tz database is unavailable, fall back to now+12h.
func nextNlmQuotaReset(now time.Time) time.Time {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return now.Add(12 * time.Hour)
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
}

// Quota state must survive across scheduled runs (same rationale and
// mechanism as the grill state in actions_goap_fusion.go): agent-scope
// blackboard first, ChainState fallback.

func saveNlmQuotaExhausted(bb *Blackboard, now time.Time) {
	until := nextNlmQuotaReset(now).Format(time.RFC3339)
	setGoapState(bb, "nlm_quota_until", until)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_nlm_quota_until", until,
			"NotebookLM daily quota exhausted until this RFC3339 timestamp", "text")
	}
}

func nlmQuotaExhaustedUntil(bb *Blackboard) (time.Time, bool) {
	var raw string
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_nlm_quota_until"); err == nil {
			raw = strings.TrimSpace(e.Value)
		}
	}
	if raw == "" {
		raw, _ = bb.ChainState["goap_fusion_nlm_quota_until"].(string)
	}
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return until, true
}

func nlmQuotaExhausted(bb *Blackboard) bool {
	until, ok := nlmQuotaExhaustedUntil(bb)
	return ok && until.After(time.Now())
}

// Last-reviewed SHA tracks how far the Claude review fallback has covered the
// daemon's commit history, so consecutive fallback cycles review disjoint
// ranges instead of the same commits.

func saveLastReviewedSHA(bb *Blackboard, sha string) {
	setGoapState(bb, "last_reviewed_sha", sha)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_last_reviewed_sha", sha,
			"HEAD SHA covered by the last Claude Code review fallback", "text")
	}
}

func loadLastReviewedSHA(bb *Blackboard) string {
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_last_reviewed_sha"); err == nil {
			return strings.TrimSpace(e.Value)
		}
	}
	s, _ := bb.ChainState["goap_fusion_last_reviewed_sha"].(string)
	return strings.TrimSpace(s)
}

// goapReviewContext is what the Claude review fallback will look at: either a
// concrete commit range ("commits") or, when nothing new was committed, the
// graphify structure report ("graphify").
type goapReviewContext struct {
	mode      string
	rangeDesc string
	body      string
}

const (
	goapReviewDiffLimit   = 12000
	goapReviewStatLimit   = 4000
	goapReviewReportLimit = 8000
)

func runGoapGit(repoDir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoDir}, args...)...)
	// -C names the target repo explicitly; drop repo-location vars that git
	// exports inside hooks (they would silently redirect these commands at
	// whatever repo triggered the hook).
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") || strings.HasPrefix(kv, "GIT_PREFIX=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gatherReviewContext picks the review target for this cycle's mode.
//
// The old selection reviewed commits whenever unreviewed commits existed and
// fell back to structure review only when there were none — but the loop
// itself commits every cycle, so structure mode was dead code and every
// research finding was a commit-review-sized nibble. Modes now rotate per
// cycle: commits → structure → failures, so architecture-scale and
// failure-pattern findings surface regularly regardless of commit traffic.
func gatherReviewContext(repoDir, lastSHA, graphReportPath string, round int) goapReviewContext {
	const gitTimeout = 30 * time.Second

	switch round % 3 {
	case 1:
		report, _ := os.ReadFile(graphReportPath)
		return goapReviewContext{
			mode:      "structure",
			rangeDesc: "codebase structure (scheduled structural review)",
			body:      truncateGoap(string(report), goapReviewReportLimit),
		}
	case 2:
		if body := gatherFailureReviewBody(); body != "" {
			return goapReviewContext{
				mode:      "failures",
				rangeDesc: "recent failure records (dead-letter queue)",
				body:      body,
			}
		}
		// No failure records — fall through to commits.
	}

	logArgs := []string{"log", "--stat", "--since=24 hours ago"}
	diffArgs := []string{"log", "-p", "--since=24 hours ago"}
	rangeDesc := "commits from the last 24 hours"
	if lastSHA != "" {
		if _, err := runGoapGit(repoDir, gitTimeout, "merge-base", "--is-ancestor", lastSHA, "HEAD"); err == nil {
			spec := lastSHA + "..HEAD"
			logArgs = []string{"log", "--stat", spec}
			diffArgs = []string{"diff", spec}
			rangeDesc = spec
		}
	}

	stat, statErr := runGoapGit(repoDir, gitTimeout, logArgs...)
	if statErr == nil && strings.TrimSpace(stat) != "" {
		diff, _ := runGoapGit(repoDir, gitTimeout, diffArgs...)
		body := fmt.Sprintf("### Commits (%s)\n%s\n\n### Diff\n%s",
			rangeDesc, truncateGoap(stat, goapReviewStatLimit), truncateGoap(diff, goapReviewDiffLimit))
		return goapReviewContext{mode: "commits", rangeDesc: rangeDesc, body: body}
	}

	report, _ := os.ReadFile(graphReportPath)
	return goapReviewContext{
		mode:      "structure",
		rangeDesc: "codebase structure (no unreviewed commits)",
		body:      truncateGoap(string(report), goapReviewReportLimit),
	}
}

// goapDeadLetterPath is the scheduler's failure record (test seam).
var goapDeadLetterPath = "/home/nico/.go-bt-evolve/dead_letter_queue.json"

// gatherFailureReviewBody renders the newest dead-letter entries for the
// failures review mode; empty when there is nothing to review.
func gatherFailureReviewBody() string {
	b, err := os.ReadFile(goapDeadLetterPath)
	if err != nil {
		return ""
	}
	var entries []map[string]any
	if err := json.Unmarshal(b, &entries); err != nil || len(entries) == 0 {
		return ""
	}
	if len(entries) > 15 {
		entries = entries[len(entries)-15:]
	}
	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("- agent=%v failed_at=%v error=%v", e["agent"], e["failed_at"], e["error"]))
	}
	return "### Recent dead-letter failures (newest last)\n" + strings.Join(lines, "\n")
}

func buildClaudeReviewPrompt(task string, rc goapReviewContext) string {
	var focus string
	switch rc.mode {
	case "commits":
		focus = fmt.Sprintf(`Review the following recent commits to this repository (%s). They were
implemented by an automated pipeline and auto-committed WITHOUT human review.
Hunt for: bugs, regressions, missing or weak tests, convention violations,
and half-finished work.

%s`, rc.rangeDesc, rc.body)
	case "failures":
		focus = fmt.Sprintf(`Review the recent failure records below (%s). Identify the highest-impact
change that addresses a RECURRING failure pattern — not a one-off — and
verify the pattern against the code before proposing it.

%s`, rc.rangeDesc, rc.body)
	default:
		focus = fmt.Sprintf(`Review the codebase structure report below (%s) and identify the
highest-impact structural improvements — architecture-level changes are in
scope, not just local fixes.

Priority guidance: prefer goals that expand PLATFORM capabilities —
internal/gardener, internal/evolution, internal/a2a, internal/domains,
internal/dashboard, internal/knowledge — over further changes to the
self-improvement pipeline's own files (actions_goap_fusion*,
actions_superpowers*, superpowers_*), which have received heavy recent
attention. Only propose pipeline work for a defect you can demonstrate.

%s`, rc.rangeDesc, rc.body)
	}

	return fmt.Sprintf(`You are the code-review fallback of an autonomous GOAP fusion improvement cycle
(NotebookLM research is unavailable). You may Read/Glob/Grep files and run
read-only git commands to verify what you see. Do not edit any files — a later
pipeline stage implements fixes.

Task context: %s

%s
%s
%s%s
Return EXACTLY this format, with up to THREE ranked targets:
GOAL1: <the highest-impact concrete code change the next automated Superpowers/Claude run should implement>
GAP1: <why the codebase needs it — cite the commit, file, or failure record you reviewed>
FILES1: <repo-relative Go files/packages to change>
GOAL2/GAP2/FILES2 and GOAL3/GAP3/FILES3: <optional further independent targets>
TESTS: <specific Go tests/build commands to verify them>
FINDINGS: <bullet list of everything else you found, most severe first>

If the single highest-impact change is too large even for one multi-task run, return INSTEAD a program:
PROGRAM: <title of the multi-cycle change>
MILESTONE1..MILESTONE5: <self-contained milestones, each naming the repo-relative Go files it touches>

Rules:
- Prefer fixing a concrete defect you actually found over generic improvements.
- Each goal must be scoped to the named files/packages; multi-file and multi-package changes are welcome.
- Prefer one coherent larger change over several trivial ones.
- Each GAPn must name the arc42 quality goal (e.g. Q1/Q2/Q3) the goal advances.
- If the reviewed code is clean, say so in FINDINGS and put the best
  code-level next step in GOAL1.`,
		task, focus, arc42GoalsPromptBlock(), implementedGoalsPromptBlock(), graphifyComponentsPromptBlock(task))
}

// goapReviewAllowedTools keeps the review run read-only: the review must not
// edit code — the implementation phase does that. One command prefix per
// Bash() rule (see defaultSuperpowersAllowedTools).
const goapReviewAllowedTools = "Read,Glob,Grep," +
	"Bash(git log:*),Bash(git show:*),Bash(git diff:*),Bash(git status:*)"

type goapReviewDeps struct {
	runner       ClaudeRunner
	repoDir      string
	synthesesDir string
	graphReport  string
	timeout      time.Duration
	now          func() time.Time
}

func defaultGoapReviewDeps() goapReviewDeps {
	return goapReviewDeps{
		runner: newReadOnlyDelegatingRunner(
			getenvDefault("BT_GOAP_REVIEW_ALLOWED_TOOLS", goapReviewAllowedTools),
		),
		repoDir:      goapFusionRepo,
		synthesesDir: goapFusionSynthesesDir,
		graphReport:  goapFusionGraphReport,
		timeout:      15 * time.Minute,
		now:          time.Now,
	}
}

// goapClaudeBackoffWindow is how long a rate-limited review closes Claude
// attempts when the CLI output carries no parseable reset hint (a
// "resets <time>"/epoch hint takes precedence — see claudeBackoffDeadline).
// One hour skips at least the next doomed tick while the half-open expiry in
// claudeBackoffActive guarantees the window can never wedge permanently.
const goapClaudeBackoffWindow = time.Hour

func init() {
	RegisterAction("RunClaudeCodeReviewResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		return runClaudeCodeReviewResearch(ctx.Blackboard, defaultGoapReviewDeps())
	})
}

// runClaudeCodeReviewResearch is the ResearchRouter fallback: Claude Code
// reviews the daemon's recent commits (or graphify hotspots) and its findings
// feed the pipeline through the exact ChainState keys the NotebookLM research
// action produces, so downstream phases need no changes.
func runClaudeCodeReviewResearch(bb *Blackboard, deps goapReviewDeps) int {
	now := deps.now
	if now == nil {
		now = time.Now
	}
	// Resolve the configured delegation provider once for this run's backoff /
	// rate-limit accounting. An invalid BT_SUPERPOWERS_PROVIDER is rejected here
	// (not silently defaulted) so a misconfiguration surfaces instead of quietly
	// reviewing through the wrong provider.
	provider, err := resolvedSuperpowersProvider()
	if err != nil {
		bb.Result = fmt.Sprintf("## Claude Review Fallback Failed\n\n%v", err)
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}
	// Consume the durable rate-limit backoff before doing ANY work: a skipped
	// tick must not spend a 15-minute run against a quota known to be
	// closed, and it must not consume a review-mode rotation slot either. The
	// -1 lets the ResearchRouter fall through to its non-fatal ResearchOptional
	// skip in milliseconds. The backoff state is namespaced by provider — a
	// Codex rate limit never closes Claude and vice versa.
	if delegationBackoffActive(bb, provider, now()) {
		until, _ := loadDelegationBackoffState(bb, provider)
		bb.Result = fmt.Sprintf("## Claude Review Fallback Skipped\n\nBackoff active until %s: a previous tick hit the %s rate limit, skipping without invoking it.", until.Format(time.RFC3339), provider)
		bb.Outcome = "goap_fusion_claude_review_rate_limited"
		return -1
	}

	round := loadReviewModeRound(bb)
	rc := gatherReviewContext(deps.repoDir, loadLastReviewedSHA(bb), deps.graphReport, round)
	saveReviewModeRound(bb, round+1)
	prompt := buildClaudeReviewPrompt(bb.Task, rc)

	runCtx, cancel := context.WithTimeout(context.Background(), deps.timeout)
	defer cancel()
	result := deps.runner.RunClaude(runCtx, deps.repoDir, prompt)

	combined := result.Output
	if result.Err != nil {
		combined += " " + result.Err.Error()
	}
	if result.Err != nil || strings.TrimSpace(result.Output) == "" {
		if isDelegationRateLimit(provider, combined) {
			// Record the backoff — the CLI-reported reset when the output names
			// one, the provider's window otherwise — so the NEXT tick short-circuits
			// at the entry guard instead of burning another 15-minute doomed run.
			saveDelegationBackoffState(bb, provider, delegationBackoffDeadline(provider, combined, now(), delegationReviewBackoffWindow(provider)))
			bb.Result = fmt.Sprintf("## Claude Review Fallback Rate-Limited\n\n```\n%s\n```", truncateGoap(combined, 2000))
			bb.Outcome = "goap_fusion_claude_review_rate_limited"
			return -1
		}
		bb.Result = fmt.Sprintf("## Claude Review Fallback Failed\n\n```\n%s\n```", truncateGoap(combined, 2000))
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}

	answer := strings.TrimSpace(result.Output)
	program := extractGoapProgram(answer)
	goals := extractGoapResearchGoals(answer)
	if len(goals) == 0 && program == nil {
		if first := fallbackGoapGoal(answer); first != "" {
			goals = []goapResearchGoal{{Goal: first, Gap: "Claude Code review produced a recommendation; see raw findings."}}
		}
	}
	if len(goals) == 0 && program == nil {
		bb.Result = "## Claude Review Fallback Failed\n\nClaude returned no parseable recommendation."
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}
	if program != nil {
		persistGoapProgram(bb, program, "claude_review:"+rc.mode)
	}
	appendGoapResearchGoals(bb, goals)
	goalSummary := strings.Join(goapResearchGoalLines(bb), "\n- ")

	skipReason, _ := bb.ChainState["goap_fusion_notebooklm_skip_reason"].(string)
	if skipReason == "" {
		skipReason = "NotebookLM research step failed or was skipped"
	}

	ts := time.Now().Format("2006-01-02T150405")
	path := filepath.Join(deps.synthesesDir, fmt.Sprintf("goap-fusion-claude-review-%s.md", ts))
	report := fmt.Sprintf(`# GOAP Fusion Claude Code Review — %s

## Source
claude_code_review (fallback; NotebookLM unavailable)

## Why NotebookLM Was Skipped
%s

## Reviewed
%s (%s mode)

## Recommendations
- %s

## Raw Claude Review Findings
%s
`, ts, truncateGoap(skipReason, 1500), rc.rangeDesc, rc.mode, goalSummary, answer)
	if err := writeString(path, report); err != nil {
		bb.Result = fmt.Sprintf("## Claude Review Fallback Failed\n\nCould not write `%s`: %v", path, err)
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}

	setGoapState(bb, "notebooklm_research", report)
	setGoapState(bb, "notebooklm_research_path", path)
	setGoapState(bb, "research_source", "claude_code_review")

	// Only advance the last-reviewed SHA when this cycle actually reviewed
	// commits. A structure/failures cycle never looked at the commit range, so
	// advancing here would make the next commit cycle skip commits this cycle
	// never reviewed.
	if rc.mode == "commits" {
		if head, err := runGoapGit(deps.repoDir, 10*time.Second, "rev-parse", "HEAD"); err == nil && head != "" {
			saveLastReviewedSHA(bb, head)
		}
	}

	bb.Result = fmt.Sprintf("## Claude Code Review Fallback Complete\n\nReviewed: %s (%s)\n\nPath: `%s`\n\nGoals:\n- %s",
		rc.rangeDesc, rc.mode, path, goalSummary)
	return 1
}
