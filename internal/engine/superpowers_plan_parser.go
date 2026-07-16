package engine

import (
	"fmt"
	"regexp"
	"strings"
)

func ParseSuperpowersPlan(markdown string) ([]SuperpowersTask, error) {
	sections := regexp.MustCompile(`(?m)^### Task `).Split(markdown, -1)
	if len(sections) <= 1 {
		return nil, fmt.Errorf("plan has no ### Task sections")
	}
	headings := regexp.MustCompile(`(?m)^### Task `).FindAllStringIndex(markdown, -1)
	_ = headings
	parts := strings.Split(markdown, "### Task ")
	var tasks []SuperpowersTask
	for i, raw := range parts[1:] {
		raw = strings.TrimSpace(raw)
		lines := strings.SplitN(raw, "\n", 2)
		title := strings.TrimSpace(lines[0])
		body := ""
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		task := SuperpowersTask{Index: i + 1, Title: title, Body: raw, Status: "pending", Risk: "medium"}
		task.Objective = extractMarkdownField(body, "Objective")
		task.Files = extractBulletValues(body, []string{"Modify:", "Create:", "Test:"})
		task.Tests = extractRunCommands(body)
		hardenSuperpowersTaskForRace(&task)
		if risk := extractMarkdownField(body, "Risk"); risk != "" {
			task.Risk = strings.ToLower(risk)
		}
		if err := validateSuperpowersTask(task); err != nil {
			return nil, fmt.Errorf("task %d %q invalid: %w", task.Index, task.Title, err)
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func extractMarkdownField(body, field string) string {
	prefix := "**" + field + ":**"
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func extractBulletValues(body string, prefixes []string) []string {
	seen := map[string]bool{}
	var vals []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		line = strings.TrimSpace(line)
		for _, prefix := range prefixes {
			if strings.HasPrefix(line, prefix) {
				v := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				v = strings.Trim(v, "`")
				if v != "" && !seen[v] {
					seen[v] = true
					vals = append(vals, v)
				}
			}
		}
	}
	return vals
}

func extractRunCommands(body string) []string {
	var cmds []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Run:") {
			cmd := strings.TrimSpace(strings.TrimPrefix(line, "Run:"))
			cmd = strings.Trim(cmd, "`")
			if cmd != "" {
				cmds = append(cmds, cmd)
			}
		}
	}
	return cmds
}

func validateSuperpowersTask(task SuperpowersTask) error {
	if strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("missing title")
	}
	if strings.TrimSpace(task.Objective) == "" {
		return fmt.Errorf("missing Objective")
	}
	if len(task.Files) == 0 {
		return fmt.Errorf("missing Files entries")
	}
	if len(task.Tests) == 0 {
		return fmt.Errorf("missing Run commands")
	}
	body := strings.ToLower(task.Body)
	if !strings.Contains(body, "red") || !strings.Contains(body, "green") {
		return fmt.Errorf("missing RED/GREEN TDD language")
	}
	for _, cmd := range task.Tests {
		lower := strings.ToLower(cmd)
		if strings.Contains(lower, "rm -rf /") || strings.Contains(lower, "sudo ") {
			return fmt.Errorf("dangerous command rejected: %s", cmd)
		}
	}
	return nil
}

// superpowersRaceMarkerRe matches task text that names shared-state
// concurrency work. Word boundaries keep this codebase's ubiquitous
// "trace"/"tracing" (OTel) from matching the embedded "race". Over-matching
// is safe — a needlessly hardened test is just slower; under-matching is the
// vacuous-RED hole this exists to close.
var superpowersRaceMarkerRe = regexp.MustCompile(`(?i)\b(races?|racy|concurren(t|tly|cy)|(rw)?mutex(es)?|goroutines?)\b`)

// superpowersTaskMentionsRace reports whether a task's text names shared-state
// concurrency work.
func superpowersTaskMentionsRace(task SuperpowersTask) bool {
	return superpowersRaceMarkerRe.MatchString(task.Title + "\n" + task.Objective + "\n" + task.Body)
}

// goTestVerbRe locates the `go test` verb in a plan command (any go binary
// path prefix).
var goTestVerbRe = regexp.MustCompile(`\bgo test\b`)

// raceHardenedGoTestCommand injects -race directly after the `go test` verb.
// Commands that already carry -race, and non-go-test commands, pass through
// unchanged.
func raceHardenedGoTestCommand(cmd string) string {
	if strings.Contains(cmd, "-race") {
		return cmd
	}
	loc := goTestVerbRe.FindStringIndex(cmd)
	if loc == nil {
		return cmd
	}
	return cmd[:loc[1]] + " -race" + cmd[loc[1]:]
}

// hardenSuperpowersTaskForRace runs a race-flavored task's go-test commands
// under the race detector. A data-race regression test cannot fail without
// it: on 2026-07-16 two race-guard milestones were closed on red-pass
// evidence while the guard did not yet exist, because their RED commands ran
// race-blind and passed vacuously. Applied at plan parse — the single point
// tasks are materialized — so RED, GREEN, review, and resumed plans all see
// the same hardened commands.
func hardenSuperpowersTaskForRace(task *SuperpowersTask) {
	if !superpowersTaskMentionsRace(*task) {
		return
	}
	for i, cmd := range task.Tests {
		task.Tests[i] = raceHardenedGoTestCommand(cmd)
	}
}
