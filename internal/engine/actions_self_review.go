package engine

// actions_self_review.go — the proactive self-review agent (self-fixing
// fleet, spec docs/superpowers/specs/2026-07-17-self-fixing-fleet-design.md
// §3 Part B). Where the error-handler escalation (Part A,
// error_handler_claude.go) reacts to a tree FAILURE, this agent runs on a
// schedule and looks BACKWARD at the autonomous commits landed since the
// last review: it gathers the commit range, runs a READ-ONLY Claude Code
// review over the diffs, and seeds a self-fix:self-review:<sig> code-fix
// program per CONFIRMED defect via the shared seedCodeFixProgram primitive
// so the goap-fusion loop implements the fix through its own TDD → verify →
// auto-apply pipeline. This file never edits seedCodeFixProgram's guards
// (cooldown/cap/kill-switch) — it only calls them.
//
// STRUCTURE NOTE: this is a single composite action (RunSelfReview), not the
// spec's literal four separate stages. See internal/domains/self_review.go
// for why: a multi-action Sequence can't return a healthy SUCCESS for "no
// new commits" without an early failing child tripping the whole Sequence —
// which would bubble to the ClaudeErrorHandler wrapper every domain tree
// gets from AllDomainTrees() and spuriously propose a "recovery" for a
// routine no-op.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// selfReviewDirOverride redirects the self-review state dir in tests (same
// var-override pattern as selfFixDirOverride).
var selfReviewDirOverride string

// selfReviewDir is the state directory (~/.go-bt-evolve/self_review),
// overridable in tests. Empty only when the home dir is unresolvable.
func selfReviewDir() string {
	if selfReviewDirOverride != "" {
		return selfReviewDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-bt-evolve", "self_review")
}

func selfReviewStatePath(dir string) string { return filepath.Join(dir, "state.json") }

// selfReviewState is the durable record of how far the self-review agent has
// covered the autonomous commit history. Advanced ONLY after a review cycle
// has finished seeding — see runSelfReview.
type selfReviewState struct {
	LastReviewedSHA string `json:"last_reviewed_sha"`
}

// loadSelfReviewState reads state.json from dir; a missing file reads as the
// zero value (first run — reviews from the beginning of history).
func loadSelfReviewState(dir string) selfReviewState {
	var s selfReviewState
	readErrorHandlerJSON(selfReviewStatePath(dir), &s)
	return s
}

// saveSelfReviewState persists state.json atomically (tmp+rename per
// ADR-003), reusing the same generic JSON helper self_fix_seed.go and
// error_handler_store.go use — one scheduled agent owns this file, so no
// cross-process lock is needed here.
func saveSelfReviewState(dir string, s selfReviewState) error {
	return writeErrorHandlerJSON(selfReviewStatePath(dir), s)
}

// selfReviewAutonomousPrefix identifies an autonomous (unreviewed-by-a-human)
// commit subject. "superpowers: apply" is overwhelmingly the dominant shape
// in this repo's history (superpowers: apply verified run <ts>-<hash>); other
// prefixes (fix/feat/refactor/docs/...) are hand-authored or reviewed work
// and out of scope for this agent.
const selfReviewAutonomousPrefix = "superpowers: apply"

// selfReviewDiffLimit reuses the goap review fallback's diff truncation
// budget (goapReviewDiffLimit) so the Claude prompt stays bounded.

// selfReviewDeps are the RunSelfReview action's injectable dependencies
// (mirrors goapReviewDeps / defaultGoapReviewDeps) so tests can fake the
// Claude runner and the git scan without touching the real repo or state.
type selfReviewDeps struct {
	runner   ClaudeRunner
	repoDir  string
	stateDir string
	timeout  time.Duration
	now      func() time.Time
	// commitScanner isolates ALL git from unit tests: returns the
	// autonomous-commit log and diff body for lastSHA..HEAD (or an initial
	// window on first run) plus the current HEAD sha. Real impl is
	// scanSelfReviewCommits; tests fake it.
	commitScanner func(repoDir, lastSHA string) (commitLog string, diff string, head string, err error)
}

func defaultSelfReviewDeps() selfReviewDeps {
	return selfReviewDeps{
		runner: execClaudeRunner{
			AllowedTools: getenvDefault("BT_SELF_REVIEW_ALLOWED_TOOLS", goapReviewAllowedTools),
		},
		repoDir:       goapFusionRepo,
		stateDir:      selfReviewDir(),
		timeout:       15 * time.Minute,
		now:           time.Now,
		commitScanner: scanSelfReviewCommits,
	}
}

// selfReviewDepsOverride lets tests swap the whole deps struct (same pattern
// used elsewhere in this package for full-dependency test overrides). Nil in
// production.
var selfReviewDepsOverride *selfReviewDeps

func init() {
	RegisterAction("RunSelfReview", func(ctx *btcore.BTContext[Blackboard]) int {
		deps := defaultSelfReviewDeps()
		if selfReviewDepsOverride != nil {
			deps = *selfReviewDepsOverride
		}
		return runSelfReview(ctx.Blackboard, deps)
	})
}

// scanSelfReviewCommits is the real commitScanner: it uses runGoapGit (the
// GOAP review fallback's read-only git helper) with a 30s timeout to gather
// the autonomous commits since lastSHA and their diff. When lastSHA is empty
// or no longer an ancestor of HEAD (rebased away), it falls back to the last
// 24 hours instead of failing.
func scanSelfReviewCommits(repoDir, lastSHA string) (commitLog, diff, head string, err error) {
	const gitTimeout = 30 * time.Second

	rawHead, herr := runGoapGit(repoDir, gitTimeout, "rev-parse", "HEAD")
	if herr != nil {
		return "", "", "", fmt.Errorf("rev-parse HEAD: %w", herr)
	}
	head = strings.TrimSpace(rawHead)

	rangeSpec := "--since=24 hours ago"
	if lastSHA != "" {
		if _, aerr := runGoapGit(repoDir, gitTimeout, "merge-base", "--is-ancestor", lastSHA, "HEAD"); aerr == nil {
			rangeSpec = lastSHA + "..HEAD"
		}
	}

	rawLog, lerr := runGoapGit(repoDir, gitTimeout, "log", "--oneline", "--no-merges", rangeSpec)
	if lerr != nil {
		return "", "", head, fmt.Errorf("git log %s: %w", rangeSpec, lerr)
	}

	filtered := filterAutonomousCommits(rawLog)
	if filtered == "" {
		// No autonomous commits in range: the caller treats an empty
		// commitLog as the healthy up-to-date skip, so no diff is needed.
		return "", "", head, nil
	}

	rawDiff, _ := runGoapGit(repoDir, gitTimeout, "diff", rangeSpec)
	return filtered, truncateGoap(rawDiff, goapReviewDiffLimit), head, nil
}

// filterAutonomousCommits keeps only `git log --oneline` lines whose commit
// subject starts with selfReviewAutonomousPrefix.
func filterAutonomousCommits(oneline string) string {
	lines := strings.Split(oneline, "\n")
	var kept []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, " ", 2)
		if len(parts) < 2 {
			continue
		}
		if strings.HasPrefix(parts[1], selfReviewAutonomousPrefix) {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

// selfReviewFinding is one CONFIRMED source-code defect Claude reports from
// the review. Signature is the stable dedup key seedCodeFixProgram's
// per-signature cooldown ledger keys on.
type selfReviewFinding struct {
	Title     string   `json:"title"`
	Milestone string   `json:"milestone"`
	Files     []string `json:"files"`
	Severity  string   `json:"severity"`
	Signature string   `json:"signature"`
}

// buildSelfReviewPrompt frames the read-only review: modeled on
// buildClaudeReviewPrompt's "commits" branch, but with a different return
// contract — CONFIRMED defects only, capped at 3, returned as a single JSON
// array rather than the GOAL/GAP/FILES text format the goap loop consumes.
func buildSelfReviewPrompt(task, rangeDesc, commitLog, diff string) string {
	return fmt.Sprintf(`You are the proactive self-review agent for an autonomous Go behavior-tree
platform. Review the following commits (%s), all auto-committed WITHOUT
human review by an automated pipeline. You may Read/Glob/Grep files and run
read-only git commands (git log, git show, git diff, git status) to verify
what you see. Do not edit any files — a later pipeline stage implements
fixes.

Task context: %s

### Commits (%s)
%s

### Diff
%s

Only report CONFIRMED source-code defects: verify each one against the
actual code with Read/Grep/git before reporting it. Skip transient
failures, configuration issues, rate-limit noise, and pure style nits — this
review exists to catch real bugs the autonomous pipeline introduced, not to
nitpick.

Return AT MOST THREE findings, most severe first, as a single JSON array and
NOTHING ELSE besides the array itself (prose around it, or wrapping it in a
`+"```json"+` fence, is fine — but the array must contain the complete finding
objects):
[{"title":"<short title>","milestone":"<file-scoped TDD instruction naming the exact file(s) and the RED→GREEN fix>","files":["path/to/x.go"],"severity":"high|med|low","signature":"<stable short slug for dedup>"}]

If the review is clean (no confirmed defects), return an empty array: []`,
		rangeDesc, task, rangeDesc, commitLog, diff)
}

// extractSelfReviewFindingsArray scans output for the first balanced JSON
// array that decodes as []selfReviewFinding, tolerant of prose or ```json
// fences wrapping it. Bounded: each candidate '[' is tried with
// json.Decoder, which consumes only the bytes of exactly one JSON value —
// no unbounded manual bracket-counting across the whole string. Mirrors
// parseErrorHandlerProposal's technique (error_handler_claude.go).
func extractSelfReviewFindingsArray(output string) ([]selfReviewFinding, bool) {
	rest := output
	for {
		idx := strings.Index(rest, "[")
		if idx < 0 {
			return nil, false
		}
		rest = rest[idx:]
		var arr []selfReviewFinding
		if err := json.NewDecoder(strings.NewReader(rest)).Decode(&arr); err == nil {
			return arr, true
		}
		rest = rest[1:]
	}
}

// validateSelfReviewFinding gates a finding before it seeds a program:
// non-empty trimmed title/milestone/signature, at least one non-empty files
// entry, and — same lenience as validateCodeFix (error_handler_claude.go) —
// the milestone must reference at least one of the files (full path or
// basename), a soft check that only rejects when it names NONE of them.
func validateSelfReviewFinding(f selfReviewFinding) bool {
	title := strings.TrimSpace(f.Title)
	milestone := strings.TrimSpace(f.Milestone)
	sig := strings.TrimSpace(f.Signature)
	if title == "" || milestone == "" || sig == "" {
		return false
	}
	var files []string
	for _, file := range f.Files {
		file = strings.TrimSpace(file)
		if file != "" {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if strings.Contains(milestone, file) || strings.Contains(milestone, filepath.Base(file)) {
			return true
		}
	}
	return false
}

// parseSelfReviewFindings extracts and validates the findings array from
// Claude's review output. Invalid findings are dropped (counted, not
// returned); an unparseable or missing array (or an explicit empty array —
// a clean review) both yield (nil, 0).
func parseSelfReviewFindings(output string) ([]selfReviewFinding, int) {
	raw, ok := extractSelfReviewFindingsArray(output)
	if !ok {
		return nil, 0
	}
	var valid []selfReviewFinding
	dropped := 0
	for _, f := range raw {
		if validateSelfReviewFinding(f) {
			valid = append(valid, f)
		} else {
			dropped++
		}
	}
	return valid, dropped
}

// runSelfReview is the RunSelfReview action body. Every skip path (scan
// failure, up-to-date, rate-limited, review failure) returns 1 (success) —
// never -1 — because this tree has no ResearchRouter fallback to fail into;
// a -1 here would fail the enclosing Sequence and spuriously trip the
// ClaudeErrorHandler wrapper AllDomainTrees() puts on every domain tree over
// what is, in every one of those cases, a routine or infrastructure-only
// skip. The last-reviewed SHA advances ONLY after the seed loop below
// completes, so a crash mid-review re-reviews the same range next run
// instead of silently skipping it.
func runSelfReview(bb *Blackboard, deps selfReviewDeps) int {
	now := deps.now
	if now == nil {
		now = time.Now
	}

	state := loadSelfReviewState(deps.stateDir)
	lastSHA := state.LastReviewedSHA

	commitLog, diff, head, err := deps.commitScanner(deps.repoDir, lastSHA)
	if err != nil {
		bb.Outcome = "self_review_scan_failed"
		bb.Result = fmt.Sprintf("## Self-Review Scan Failed\n\nCould not gather the autonomous commit range: %v", err)
		return 1
	}

	rangeDesc := lastSHA + "..HEAD"
	if lastSHA == "" {
		rangeDesc = "beginning..HEAD"
	}

	if strings.TrimSpace(commitLog) == "" {
		sinceDesc := lastSHA
		if sinceDesc == "" {
			sinceDesc = "the beginning"
		}
		bb.Outcome = "self_review_up_to_date"
		bb.OutcomeRefinement = "no_change"
		bb.QualityScore = 0.5
		bb.QualityAuthoritative = true
		bb.Result = fmt.Sprintf("## Self-Review Skipped\n\nNo new autonomous commits since %s.", sinceDesc)
		return 1
	}

	// Rate-limit backoff guard — BEFORE invoking Claude. Return 1 (not -1):
	// this healthy skip must not fail the sequence. Do NOT advance the SHA:
	// this range has not actually been reviewed yet.
	if claudeBackoffActive(bb, now()) {
		until, _ := loadClaudeBackoffState(bb)
		bb.Outcome = "self_review_rate_limited"
		bb.Result = fmt.Sprintf("## Self-Review Skipped\n\nBackoff active until %s: a previous tick hit the Claude rate limit, skipping without invoking Claude.", until.Format(time.RFC3339))
		return 1
	}

	prompt := buildSelfReviewPrompt(bb.Task, rangeDesc, commitLog, diff)
	runCtx, cancel := context.WithTimeout(context.Background(), deps.timeout)
	defer cancel()
	result := deps.runner.RunClaude(runCtx, deps.repoDir, prompt)

	combined := result.Output
	if result.Err != nil {
		combined += " " + result.Err.Error()
	}
	if result.Err != nil || strings.TrimSpace(result.Output) == "" {
		if isClaudeRateLimit(combined) {
			saveClaudeBackoffState(bb, claudeBackoffDeadline(combined, now(), goapClaudeBackoffWindow))
			bb.Outcome = "self_review_rate_limited"
			bb.Result = fmt.Sprintf("## Self-Review Rate-Limited\n\n```\n%s\n```", truncateGoap(combined, 2000))
			return 1
		}
		// Review failure: operator-visible skip, SHA NOT advanced so the next
		// run re-reviews this same range instead of silently skipping it.
		bb.Outcome = "self_review_failed"
		bb.Result = fmt.Sprintf("## Self-Review Failed\n\n```\n%s\n```", truncateGoap(combined, 2000))
		return 1
	}

	findings, dropped := parseSelfReviewFindings(result.Output)

	seededCount := 0
	var lines []string
	for _, f := range findings {
		seeded, reason := seedCodeFixProgram(f.Signature, f.Title, f.Milestone, "self-fix:self-review:"+f.Signature)
		Info("self-review: seed result", "sig", f.Signature, "seeded", seeded, "reason", reason)
		if seeded {
			seededCount++
			lines = append(lines, fmt.Sprintf("- SEEDED %q (sig=%s, severity=%s)", f.Title, f.Signature, f.Severity))
		} else {
			lines = append(lines, fmt.Sprintf("- skipped %q (sig=%s): %s", f.Title, f.Signature, reason))
		}
	}

	// Advance state ONLY after the seed loop completes — even when every
	// finding was cooldown/cap-skipped (they were still reviewed), and even
	// on a clean verdict (those commits were reviewed and are clean).
	if err := saveSelfReviewState(deps.stateDir, selfReviewState{LastReviewedSHA: head}); err != nil {
		Error("self-review: failed to persist last-reviewed SHA; next run will re-review this range", "err", err)
	}

	if len(findings) == 0 {
		bb.Outcome = "self_review_clean"
	} else {
		bb.Outcome = "self_review_seeded"
	}
	summary := "(no findings)"
	if len(lines) > 0 {
		summary = strings.Join(lines, "\n")
	}
	bb.Result = fmt.Sprintf("## Self-Review Complete\n\nReviewed: %s\n\nConfirmed findings: %d (dropped %d invalid)\nPrograms seeded: %d\n\n%s",
		rangeDesc, len(findings), dropped, seededCount, summary)
	return 1
}
