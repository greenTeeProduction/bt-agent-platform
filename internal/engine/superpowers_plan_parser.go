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
