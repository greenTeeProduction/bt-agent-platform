package engine

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// maxGoalDrivenTasks bounds how many prioritized goals one cycle turns into
// implementation tasks. Each task is a full RED→GREEN Claude execution, so
// three tasks is already a substantially bigger cycle than the legacy
// single-task template while staying inside the batch timeout.
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
	for _, goal := range goals {
		files := extractGoFilePaths(goal)
		if len(files) == 0 {
			continue
		}
		sections = append(sections, buildGoalTaskSection(len(sections)+1, goal, files))
		if len(sections) == maxGoalDrivenTasks {
			break
		}
	}
	if len(sections) == 0 {
		return buildDeterministicImplementationPlan(task)
	}
	return "# Superpowers Implementation Plan\n\n> Use RED/GREEN/REFACTOR. Preserve explicit feature paths; do not amputate functionality.\n\n" +
		strings.Join(sections, "\n")
}

// extractPrioritizedGoals returns goal lines in priority order (P0 first).
func extractPrioritizedGoals(task string) []string {
	byPriority := map[string][]string{}
	var priorities []string
	for _, line := range strings.Split(task, "\n") {
		m := goalLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		if _, seen := byPriority[m[1]]; !seen {
			priorities = append(priorities, m[1])
		}
		byPriority[m[1]] = append(byPriority[m[1]], strings.TrimSpace(m[2]))
	}
	sort.Strings(priorities)
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
	testCmd := fmt.Sprintf("/usr/local/go/bin/go test %s -count=1 -timeout 300s", strings.Join(pkgs, " "))

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

// changedPackagesTestCommand builds the verification command that scales
// with the run's actual blast radius: every Go package the run touched gets
// its full test suite, not just the fixed contract set. Returns "" when the
// run changed no Go files (the check is skipped).
func changedPackagesTestCommand(changedFiles []string) string {
	pkgs := goPackagesOf(changedFiles)
	if len(pkgs) == 0 {
		return ""
	}
	return fmt.Sprintf("/usr/local/go/bin/go test %s -count=1 -timeout 300s", strings.Join(pkgs, " "))
}
