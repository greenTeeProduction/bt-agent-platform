package dashboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// AgentExecutor runs tasks in-process when Runner is set, else falls back to Hermes CLI.
type AgentExecutor struct {
	Runner  *agent.RunDeps
	Timeout time.Duration
}

func NewAgentExecutor() *AgentExecutor {
	return &AgentExecutor{Timeout: 5 * time.Minute}
}

// RunTask executes a task through the specified BT agent tree.
func (e *AgentExecutor) RunTask(agentName, task, treeID string) (output string, outcome string, err error) {
	res, err := e.RunTaskResult(agentName, task, treeID)
	if res == nil {
		return "", "failure", err
	}
	return res.Output, res.Outcome, err
}

// RunTaskResult executes a task and returns the full RunResult (includes blackboard run_id).
func (e *AgentExecutor) RunTaskResult(agentName, task, treeID string) (*agent.RunResult, error) {
	if e.Runner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
		defer cancel()
		opts := agent.RunOptions{
			InjectMemory:   true,
			EnforceQuality: true,
			RecordHistory:  true,
			DisplayName:    agentName,
		}
		_, _, res, err := agent.RunAgent(ctx, e.Runner, agentName, task, treeID, opts)
		return res, err
	}
	output, outcome, err := e.runViaHermes(task, treeID)
	if err != nil && outcome == "" {
		outcome = "failure"
	}
	return &agent.RunResult{
		AgentName: agentName,
		Task:      task,
		Outcome:   outcome,
		Output:    output,
	}, err
}

func (e *AgentExecutor) runViaHermes(task, treeID string) (output string, outcome string, err error) {
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

// auctionKeywordPattern matches any of engine.AuctionTaskKeywords as a whole
// word, so e.g. a task mentioning "auctions" in passing (plural, or otherwise
// not the bare keyword) doesn't falsely trigger auction routing the way a
// plain substring check would.
var auctionKeywordPattern = regexp.MustCompile(
	`\b(` + strings.Join(quoteKeywords(engine.AuctionTaskKeywords), "|") + `)\b`,
)

func quoteKeywords(keywords []string) []string {
	quoted := make([]string, len(keywords))
	for i, kw := range keywords {
		quoted[i] = regexp.QuoteMeta(kw)
	}
	return quoted
}

// isAuctionShapedTask reports whether lower (already-lowercased task text)
// signals auction/delegation intent, using the same keyword set as the
// auction_demo tree's IsAuctionTask condition (engine.AuctionTaskKeywords).
// Matching is whole-word so incidental mentions (e.g. "unrelated to
// auctions") don't falsely trigger routing.
func isAuctionShapedTask(lower string) bool {
	return auctionKeywordPattern.MatchString(lower)
}

// PickTreeForTask selects the best BT tree for a task based on its content.
func PickTreeForTask(task Task) string {
	lower := strings.ToLower(task.Title + " " + task.Description)
	switch {
	case isAuctionShapedTask(lower):
		return "auction_demo"
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
