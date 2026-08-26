package engine

import (
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"
)

// maxGoalDrivenTasks bounds how many prioritized goals one cycle turns into
// implementation tasks. Each task is a full RED→GREEN Claude execution, so
// three tasks is already a substantially bigger cycle than the legacy
// single-task template while staying inside the batch timeout.
// goapProgramMaxMilestoneAttempts bounds how many cycles the loop attempts a
// single program milestone before marking it blocked and moving on — the
// escape hatch for a fabricated/unbuildable milestone the implementation
// agent keeps declining (2026-07-05: a TDAD research-echo milestone declined 10x).
const goapProgramMaxMilestoneAttempts = 3

const maxGoalDrivenTasks = 3

// goFilePathRe matches repo-relative Go file paths in goal text, with or
// without backticks, tolerating a trailing :line suffix.
var goFilePathRe = regexp.MustCompile(`(?:internal|cmd)/[A-Za-z0-9_./-]+\.go`)

// goalLineRe matches prioritized goal lines emitted by the gap analysis.
var goalLineRe = regexp.MustCompile(`^\[(P\d)\]\s*(.+)$`)

// buildGoalDrivenImplementationPlan turns the cycle's prioritized research
// goals into a multi-task implementation plan sized to the goals themselves:
// one task per file-scoped goal (capped), file scope and test packages
// derived from the paths the goal names, and a complete-change objective
// instead of the legacy smallest-safe clamp. Goals that name no concrete Go
// files fall back to the legacy deterministic single-task template, so a
// vague research cycle degrades to exactly the old behavior.
func buildGoalDrivenImplementationPlan(task string) string {
	goals := extractPrioritizedGoals(task)
	var sections []string
	var fileScoped []string
	for _, goal := range goals {
		// File scope comes from the goal text and grep "(files: …)" scoping
		// ONLY. The transient annotations are stripped first: the graphify
		// REUSE-EXISTING suffix carries advisory .go hit paths (lexical
		// matches, sometimes pure boilerplate noise) that must never expand —
		// or, for a formerly pathless goal, entirely define — the task's
		// modify scope; likewise a failure note quoting a path must not
		// re-scope the retry.
		files := extractGoFilePaths(stripGoapGoalTransientNotes(goal))
		if len(files) == 0 {
			continue
		}
		fileScoped = append(fileScoped, goal)
		// A goal whose previous attempt failed the commit gate carries the
		// recorded failure into its task text (parse-safe single line), so
		// the retry fixes what actually failed instead of resubmitting the
		// same rejected code.
		goalForSection := goal
		if note := goapGoalFailureNote(goal); note != "" {
			goalForSection = goal + " " + note
		}
		sections = append(sections, buildGoalTaskSection(len(sections)+1, goalForSection, files))
		if len(sections) == maxGoalDrivenTasks {
			break
		}
	}
	if len(sections) == 0 {
		return buildDeterministicImplementationPlan(task)
	}
	deterministic := "# Superpowers Implementation Plan\n\n> Use RED/GREEN/REFACTOR. Preserve explicit feature paths; do not amputate functionality.\n\n" +
		strings.Join(sections, "\n")

	// Brainstorm expansion: the deterministic builder makes exactly one task
	// per goal, so a substantial goal that deserves several coherent tasks is
	// clamped to one bounded RED→GREEN pass. When the plan does not already
	// fill the task budget, ask an LLM to DECOMPOSE the goals into a deeper
	// multi-task plan (the missing "brainstorming" the superpowers workflow
	// never applied here). The expansion is used only if it round-trips
	// through ParseSuperpowersPlan and yields MORE tasks than the mechanical
	// plan — otherwise the deterministic plan stands. Fully offline by
	// default (goalPlanBrainstormFn is nil until wired in production).
	if goalPlanBrainstormFn != nil && len(sections) < maxGoalDrivenTasks && len(fileScoped) > 0 {
		if expanded := brainstormExpandPlan(task, fileScoped, deterministic); expanded != "" {
			return expanded
		}
	}
	return deterministic
}

// extractPrioritizedGoals returns goal lines in priority order (P0 first).
func extractPrioritizedGoals(task string) []string {
	byPriority := map[string][]string{}
	var priorities []string
	for line := range strings.SplitSeq(task, "\n") {
		m := goalLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		if _, seen := byPriority[m[1]]; !seen {
			priorities = append(priorities, m[1])
		}
		byPriority[m[1]] = append(byPriority[m[1]], strings.TrimSpace(m[2]))
	}
	slices.Sort(priorities)
	var goals []string
	for _, p := range priorities {
		goals = append(goals, byPriority[p]...)
	}
	return goals
}

func extractGoFilePaths(text string) []string {
	seen := map[string]bool{}
	var files []string
	for _, m := range goFilePathRe.FindAllString(text, -1) {
		if !seen[m] {
			seen[m] = true
			files = append(files, m)
		}
	}
	return files
}

func buildGoalTaskSection(index int, goal string, files []string) string {
	var modify, tests []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			tests = append(tests, f)
		} else {
			modify = append(modify, f)
		}
	}
	// TDD needs a test target: when the goal names none, point at the
	// conventional sibling test file of the first modified file.
	if len(tests) == 0 && len(modify) > 0 {
		first := modify[0]
		tests = append(tests, strings.TrimSuffix(first, ".go")+"_test.go")
	}

	pkgs := goPackagesOf(files)
	risk := "medium"
	if len(pkgs) > 2 || len(files) > 6 {
		risk = "high"
	}
	testCmd := fmt.Sprintf("/usr/local/go/bin/go test %s -short -count=1 -timeout 300s", strings.Join(pkgs, " "))

	title := goal
	if len(title) > 90 {
		title = title[:90] + "…"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### Task %d: %s\n\n", index, title)
	fmt.Fprintf(&b, "**Objective:** Implement the complete, verified change for this goal: %s\n\n", goal)
	b.WriteString("**Files:**\n")
	for _, f := range modify {
		fmt.Fprintf(&b, "- Modify: %s\n", f)
	}
	for _, f := range tests {
		fmt.Fprintf(&b, "- Test: %s\n", f)
	}
	b.WriteString("\n**Step 1: Write failing test (RED)**\nAdd or extend regression tests for every behavior this goal changes.\n\n")
	fmt.Fprintf(&b, "**Step 2: Run RED**\nRun: %s\nExpected: FAIL for the intended missing behavior before implementation.\n\n", testCmd)
	b.WriteString("**Step 3: Implement the full change**\nImplement the complete behavior this goal requires — multi-file edits within the listed packages are in scope; do not stop at a partial scaffold or an adjacent helper.\n\n")
	fmt.Fprintf(&b, "**Step 4: Run GREEN**\nRun: %s\nExpected: PASS.\n\n", testCmd)
	fmt.Fprintf(&b, "**Risk:** %s\n", risk)
	return b.String()
}

func goPackagesOf(files []string) []string {
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		dir := "./" + path.Dir(f)
		if !seen[dir] {
			seen[dir] = true
			pkgs = append(pkgs, dir)
		}
	}
	return pkgs
}

// superpowersLintBin locates the hook's linter; empty disables the lint
// verification check (test seam; resolved once at init).
var superpowersLintBin = func() string {
	if p, err := exec.LookPath("golangci-lint"); err == nil {
		return p
	}
	return ""
}()

// changedPackagesLintCommand mirrors the pre-commit hook's golangci-lint
// gate over the run's changed packages, so lint failures surface during
// verification (with evidence) instead of only at the final hook-gated
// landing commit as an opaque applied_uncommitted (run 20260704T050842:
// tests and build passed, then the hook rejected a prealloc issue and the
// whole verified run was wasted).
func changedPackagesLintCommand(changedFiles []string) string {
	if superpowersLintBin == "" {
		return ""
	}
	pkgs := goPackagesOf(changedFiles)
	if len(pkgs) == 0 {
		return ""
	}
	for i, p := range pkgs {
		pkgs[i] = p + "/..."
	}
	return fmt.Sprintf("PATH=/usr/local/go/bin:$PATH %s run %s", superpowersLintBin, strings.Join(pkgs, " "))
}

// changedPackagesTestCommand builds the verification command that scales
// with the run's actual blast radius: every Go package the run touched gets
// its full test suite, not just the fixed contract set. Returns "" when the
// run changed no Go files (the check is skipped).
func changedPackagesTestCommand(changedFiles []string) string {
	pkgs := goPackagesOf(changedFiles)
	if len(pkgs) == 0 {
		return ""
	}
	return fmt.Sprintf("/usr/local/go/bin/go test %s -short -count=1 -timeout 300s", strings.Join(pkgs, " "))
}

// goalPlanBrainstormFn is the LLM plan-expansion seam: given the framing task,
// the file-scoped goals, and the deterministic fallback plan, it returns a
// richer multi-task plan in the same "### Task N:" format, or "" to decline.
// nil by default (offline/tests); wired to a Claude-backed impl in production.
var goalPlanBrainstormFn func(task string, goals []string, deterministicPlan string) string

// brainstormExpandPlan runs the expansion and accepts it only if it parses and
// produces strictly MORE tasks than the deterministic plan (bounded by
// maxGoalDrivenTasks), so a worse or malformed brainstorm can never degrade a
// working plan.
func brainstormExpandPlan(task string, goals []string, deterministicPlan string) string {
	detTasks, _ := ParseSuperpowersPlan(deterministicPlan)
	out := goalPlanBrainstormFn(task, goals, deterministicPlan)
	if strings.TrimSpace(out) == "" {
		return ""
	}
	tasks, err := ParseSuperpowersPlan(out)
	if err != nil || len(tasks) <= len(detTasks) {
		return ""
	}
	if len(tasks) > maxGoalDrivenTasks {
		return ""
	}
	// Every expanded task must still name Go files and be actionable — no
	// regressing to vague prose tasks. Beyond non-empty, every Files entry must
	// match the repo-relative Go-path form (goFilePathRe): a doc/config file
	// gives the executor no real Go target to edit and test against. And every
	// go-test Run command must carry -short: a full-package run drags in
	// LLM-gated and flaky tests, so a -short-less command yields false RED
	// passes or aborted GREEN cycles. Any violation rejects the whole
	// expansion, keeping the deterministic plan.
	for _, t := range tasks {
		if len(t.Files) == 0 {
			return ""
		}
		for _, f := range t.Files {
			if !goFilePathRe.MatchString(f) {
				return ""
			}
		}
		for _, cmd := range t.Tests {
			if strings.Contains(cmd, "go test") && !strings.Contains(cmd, "-short") {
				return ""
			}
		}
	}
	return out
}

// buildGoalBrainstormPrompt frames the decomposition request.
func buildGoalBrainstormPrompt(task string, goals []string, deterministicPlan string) string {
	return fmt.Sprintf(`You are planning one autonomous coding cycle for the go-bt-evolve
platform (Go, packages under internal/). Below are prioritized, file-scoped
goals and a MINIMAL fallback plan of one task per goal.

Goals:
- %s

Fallback plan (one task per goal):
%s

Produce a BETTER implementation plan that goes DEEPER: decompose the
substantial goal(s) into a coherent sequence of up to %d self-contained
TDD tasks that together build a complete, well-tested change — e.g. split a
capability into "types + validation", "core logic", and "wiring + tests".
Each task must be independently landable via RED→GREEN.

Return ONLY the plan in EXACTLY this format (no prose before or after):
### Task 1: <title>

**Objective:** <complete change for this task>

**Files:**
- Modify: <repo-relative .go file>
- Test: <repo-relative _test.go file>

**Step 1: Write failing test (RED)**
...
**Step 2: Run RED**
Run: /usr/local/go/bin/go test <packages> -short -count=1 -timeout 300s
**Step 3: Implement the full change**
...
**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test <packages> -short -count=1 -timeout 300s

**Risk:** <low|medium|high>

### Task 2: ...

Rules: every task MUST name at least one repo-relative .go file; produce
MORE tasks than the fallback only when the goals genuinely warrant it, else
return the fallback plan unchanged.`,
		strings.Join(goals, "\n- "), truncateFusion(deterministicPlan, 2000), maxGoalDrivenTasks)
}

// changedPackagesLintFixCommand is the machine-remediation twin of
// changedPackagesLintCommand: same packages, with --fix applied so linters
// that ship applicable fixes (staticcheck's QF class, gofmt-style issues)
// repair the worktree in place.
func changedPackagesLintFixCommand(changedFiles []string) string {
	cmd := changedPackagesLintCommand(changedFiles)
	if cmd == "" {
		return ""
	}
	return strings.Replace(cmd, " run ", " run --fix ", 1)
}
