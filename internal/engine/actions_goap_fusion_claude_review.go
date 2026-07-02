package engine

// Claude Code review fallback for the GOAP fusion runner: when NotebookLM is
// unavailable (daily quota exhausted or any other failure), the ResearchRouter
// selector in GoapFusionLoopTree falls through to RunClaudeCodeReviewResearch,
// which reviews the daemon's recent auto-approved commits and emits findings
// in the same GOAL/GAP/FILES/TESTS contract the downstream pipeline consumes.
// Spec: docs/superpowers/specs/2026-07-02-goap-fusion-claude-review-fallback-design.md

import (
	"context"
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
	return strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "error code 8") ||
		strings.Contains(lower, "google rejected the query")
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
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gatherReviewContext picks the review target. Priority: commits after the
// last reviewed SHA; else commits from the last 24h; else the graphify report.
func gatherReviewContext(repoDir, lastSHA, graphReportPath string) goapReviewContext {
	const gitTimeout = 30 * time.Second

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
		mode:      "graphify",
		rangeDesc: "codebase structure (no unreviewed commits)",
		body:      truncateGoap(string(report), goapReviewReportLimit),
	}
}

func buildClaudeReviewPrompt(task string, rc goapReviewContext) string {
	var focus string
	if rc.mode == "commits" {
		focus = fmt.Sprintf(`Review the following recent commits to this repository (%s). They were
implemented by an automated pipeline and auto-committed WITHOUT human review.
Hunt for: bugs, regressions, missing or weak tests, convention violations,
and half-finished work.

%s`, rc.rangeDesc, rc.body)
	} else {
		focus = fmt.Sprintf(`There are no unreviewed commits. Instead, review the codebase structure
report below (%s) and identify the single highest-impact structural fix.

%s`, rc.rangeDesc, rc.body)
	}

	return fmt.Sprintf(`You are the code-review fallback of an autonomous GOAP fusion improvement cycle
(NotebookLM research is unavailable). You may Read/Glob/Grep files and run
read-only git commands to verify what you see. Do not edit any files — a later
pipeline stage implements fixes.

Task context: %s

%s

Return EXACTLY this format:
GOAL: <one specific code change the next automated Superpowers/Claude run should implement>
GAP: <why the current go-bt-evolve codebase needs it — cite the commit or file you reviewed>
FILES: <likely files or packages to inspect/change>
TESTS: <specific Go tests/build commands to verify it>
FINDINGS: <bullet list of everything else you found, most severe first>

Rules:
- Prefer fixing a concrete defect you actually found over generic improvements.
- The goal must be small enough for one scheduled coding run.
- If the reviewed code is clean, say so in FINDINGS and put the best
  code-level next step in GOAL.`, task, focus)
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
}

func defaultGoapReviewDeps() goapReviewDeps {
	return goapReviewDeps{
		runner: execClaudeRunner{
			AllowedTools: getenvDefault("BT_GOAP_REVIEW_ALLOWED_TOOLS", goapReviewAllowedTools),
		},
		repoDir:      goapFusionRepo,
		synthesesDir: goapFusionSynthesesDir,
		graphReport:  goapFusionGraphReport,
		timeout:      15 * time.Minute,
	}
}

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
	rc := gatherReviewContext(deps.repoDir, loadLastReviewedSHA(bb), deps.graphReport)
	prompt := buildClaudeReviewPrompt(bb.Task, rc)

	runCtx, cancel := context.WithTimeout(context.Background(), deps.timeout)
	defer cancel()
	result := deps.runner.RunClaude(runCtx, deps.repoDir, prompt)

	combined := result.Output
	if result.Err != nil {
		combined += " " + result.Err.Error()
	}
	if result.Err != nil || strings.TrimSpace(result.Output) == "" {
		if isClaudeRateLimit(combined) {
			bb.Result = fmt.Sprintf("## Claude Review Fallback Rate-Limited\n\n```\n%s\n```", truncateGoap(combined, 2000))
			bb.Outcome = "goap_fusion_claude_review_rate_limited"
			return -1
		}
		bb.Result = fmt.Sprintf("## Claude Review Fallback Failed\n\n```\n%s\n```", truncateGoap(combined, 2000))
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}

	answer := strings.TrimSpace(result.Output)
	goal, gap := extractGoapNotebookLMRecommendation(answer)
	if goal == "" {
		goal = firstNonEmptyGoapLine(answer)
	}
	if goal == "" {
		bb.Result = "## Claude Review Fallback Failed\n\nClaude returned no parseable recommendation."
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}
	if gap == "" {
		gap = "Claude Code review produced a recommendation; see raw findings."
	}

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

## Recommendation
GOAL: %s
GAP: %s

## Raw Claude Review Findings
%s
`, ts, truncateGoap(skipReason, 1500), rc.rangeDesc, rc.mode, goal, gap, answer)
	if err := writeString(path, report); err != nil {
		bb.Result = fmt.Sprintf("## Claude Review Fallback Failed\n\nCould not write `%s`: %v", path, err)
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}

	setGoapState(bb, "notebooklm_research", report)
	setGoapState(bb, "notebooklm_goal", goal)
	setGoapState(bb, "notebooklm_gap", gap)
	setGoapState(bb, "notebooklm_research_path", path)
	setGoapState(bb, "research_source", "claude_code_review")

	if head, err := runGoapGit(deps.repoDir, 10*time.Second, "rev-parse", "HEAD"); err == nil && head != "" {
		saveLastReviewedSHA(bb, head)
	}

	bb.Result = fmt.Sprintf("## Claude Code Review Fallback Complete\n\nReviewed: %s (%s)\n\nPath: `%s`\n\nGOAL: %s\n\nGAP: %s",
		rc.rangeDesc, rc.mode, path, goal, gap)
	return 1
}
