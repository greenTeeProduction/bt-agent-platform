package dashboard

import (
	"context"
	"fmt"
	"log/slog"
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
	// CBStore, when set, records each RunTaskResult call's outcome to the
	// shared agent circuit breaker store and persists it to
	// agent.CircuitBreakersFile() after every run — mirroring
	// internal/agent/scheduler.go's reportAgentOutcome/Save calls, so a
	// flaky agent invoked only through the dashboard still trips the
	// breaker the scheduler and A2A auction paths already honor.
	CBStore *agent.AgentCircuitBreakerStore
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
	var res *agent.RunResult
	var err error
	if e.Runner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
		defer cancel()
		opts := agent.RunOptions{
			InjectMemory:   true,
			EnforceQuality: true,
			RecordHistory:  true,
			DisplayName:    agentName,
		}
		_, _, res, err = agent.RunAgent(ctx, e.Runner, agentName, task, treeID, opts)
	} else {
		start := time.Now()
		var output, outcome string
		output, outcome, err = e.runViaHermes(task, treeID)
		if err != nil && outcome == "" {
			outcome = "failure"
		}
		res = &agent.RunResult{
			AgentName: agentName,
			Task:      task,
			Outcome:   outcome,
			Output:    output,
			Duration:  time.Since(start),
		}
	}
	e.recordCircuitBreakerOutcome(agentName, res)
	e.recordTaskMetric(agentName, res)
	e.recordBlockFitnessMetric(agentName, treeID, res)
	return res, err
}

// recordCircuitBreakerOutcome reports res's outcome to CBStore and persists
// it, mirroring internal/agent/scheduler.go's runJob: reportAgentOutcome
// followed by cbStore.Save(CircuitBreakersFile()) on every cycle.
func (e *AgentExecutor) recordCircuitBreakerOutcome(agentName string, res *agent.RunResult) {
	if e.CBStore == nil || res == nil {
		return
	}
	if res.Outcome == "success" {
		e.CBStore.RecordSuccess(agentName)
	} else {
		e.CBStore.RecordFailure(agentName)
	}
	if err := e.CBStore.Save(agent.CircuitBreakersFile()); err != nil {
		slog.Warn("dashboard: persist circuit breaker state failed", "path", agent.CircuitBreakersFile(), "err", err)
	}
}

// recordTaskMetric reports res's outcome and duration to the dashboard's
// global agent-task metrics (GetAgentMetrics), so every agent run through
// the dashboard — in-process or via the Hermes fallback — is reflected in
// the agent metrics panel and /metrics endpoint the same way
// internal/agent/scheduler.go's runJob already records its runs.
func (e *AgentExecutor) recordTaskMetric(agentName string, res *agent.RunResult) {
	if res == nil {
		return
	}
	RecordTask(agentName, res.Outcome == "success", uint64(res.Duration.Milliseconds()))
}

// recordBlockFitnessMetric reports a bt_block_fitness_score gauge for the
// tree treeID ran through, closing the milestone-3/3 gap: RecordBlockFitness
// was previously reachable only from the FitnessProbe BT action
// (internal/engine/ops_actions.go) and internal/blocks' own
// RecordTaskBlockFitness, never from the dashboard's own RunTaskResult
// dispatch path. internal/dashboard can't import internal/blocks to reuse
// its per-node block_id walk (blocks already imports dashboard), so this
// mirrors ops_actions.go's fitnessProbeAction: it scores the whole task run
// under treeID as a single block key rather than segmenting by tree node.
func (e *AgentExecutor) recordBlockFitnessMetric(agentName, treeID string, res *agent.RunResult) {
	if res == nil || treeID == "" {
		return
	}
	success := res.Outcome == "success"
	score := res.Quality * 100
	if score <= 0 {
		if success || strings.EqualFold(res.Outcome, "completed") {
			score = 75
		} else {
			score = 25
		}
	}
	if score > 100 {
		score = 100
	} else if score < 0 {
		score = 0
	}
	RecordBlockFitness(treeID, agentName, score)
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

// DiscoverTreeFn, when set, is consulted by PickTreeForTask ahead of the
// static keyword switch. It mirrors knowledge.KnowledgeGraph.Discover's
// signature so cmd/bt-dashboard can wire it directly to kg.Discover without
// an import cycle (internal/knowledge depends on internal/dashboard's
// sibling packages, not the other way around). A zero-value confidence (the
// ("", 0) contract Discover returns when unconfident) falls through to the
// static switch below instead of routing to an empty tree ID.
var DiscoverTreeFn func(task string) (treeID string, confidence float64)

// PickTreeForTask selects the best BT tree for a task based on its content.
func PickTreeForTask(task Task) string {
	lower := strings.ToLower(task.Title + " " + task.Description)
	if isAuctionShapedTask(lower) {
		return "auction_demo"
	}
	if DiscoverTreeFn != nil {
		if treeID, confidence := DiscoverTreeFn(task.Title + " " + task.Description); confidence > 0 && treeID != "" {
			return treeID
		}
	}
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
