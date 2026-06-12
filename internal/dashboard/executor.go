package dashboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// AgentExecutor runs tasks through BT agents via Hermes CLI.
type AgentExecutor struct {
	Timeout time.Duration
}

func NewAgentExecutor() *AgentExecutor {
	return &AgentExecutor{Timeout: 5 * time.Minute}
}

// RunTask executes a task through the specified BT agent tree using Hermes CLI.
// Returns the agent's output and outcome (success/failure).
func (e *AgentExecutor) RunTask(_, task, treeID string) (output string, outcome string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	defer cancel()

	// Build the Hermes command: hermes chat -q "delegate task to tree"
	// We use the bt-agent platform's tree delegation pattern
	prompt := fmt.Sprintf(
		`Use bt_delegate_to_tree to run this task through the %s tree. Task: %s. After completion, report: what was done, what was the outcome, and any relevant output. Be thorough.`,
		treeID, task,
	)

	hermesPath := "hermes"
	// Find hermes in common locations
	if _, err := os.Stat(hermesPath); os.IsNotExist(err) {
		if _, err := os.Stat("/usr/local/bin/hermes"); err == nil {
			hermesPath = "/usr/local/bin/hermes"
		}
	}

	cmd := exec.CommandContext(ctx,
		hermesPath, "chat",
		"-q", prompt,
		"--yolo",
		"-m", "deepseek-v4-flash",
	)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	cmd.Env = append(os.Environ(), "HOME="+home)

	outBytes, err := cmd.CombinedOutput()
	output = strings.TrimSpace(string(outBytes))

	if ctx.Err() == context.DeadlineExceeded {
		return output, "timeout", fmt.Errorf("task timed out after %v", e.Timeout)
	}
	if err != nil {
		outcome = "failed"
		// Still return output — it may contain useful error info
		return output, outcome, nil
	}

	// Determine outcome from output
	lower := strings.ToLower(output)
	if strings.Contains(lower, "success") || strings.Contains(lower, "completed") || strings.Contains(lower, "done") {
		outcome = "success"
	} else if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		outcome = "failed"
	} else {
		outcome = "completed" // ran but couldn't determine
	}

	return output, outcome, nil
}

// TaskAgentMap maps task roles to real BT agent names.
var TaskAgentMap = map[string]string{
	"CEO":       "hermes-researcher",
	"CTO":       "hermes-code-reviewer",
	"PM":        "bt-implementer",
	"Engineer":  "bt-implementer",
	"Marketing": "hermes-researcher",
	"Sales":     "hermes-researcher",
}

// PickTreeForTask selects the best BT tree for a task based on its content.
func PickTreeForTask(task Task) string {
	lower := strings.ToLower(task.Title + " " + task.Description)
	switch {
	case strings.Contains(lower, "bug") || strings.Contains(lower, "review") || strings.Contains(lower, "code"):
		return "domain:code_review"
	case strings.Contains(lower, "build") || strings.Contains(lower, "deploy") || strings.Contains(lower, "ci"):
		return "domain:devops_ci"
	case strings.Contains(lower, "security") || strings.Contains(lower, "vuln"):
		return "domain:security_audit"
	case strings.Contains(lower, "research") || strings.Contains(lower, "analyze") || strings.Contains(lower, "investigate"):
		return "research:deep_research"
	case strings.Contains(lower, "test") || strings.Contains(lower, "benchmark"):
		return "domain:devops_ci"
	case strings.Contains(lower, "refactor") || strings.Contains(lower, "improve"):
		return "domain:refactoring"
	default:
		return "godev"
	}
}

// ResolveAgentName returns the BT agent name for a task's assignee.
func ResolveAgentName(assignee string) string {
	if name, ok := TaskAgentMap[assignee]; ok {
		return name
	}
	return assignee // use assignee directly if no mapping
}
