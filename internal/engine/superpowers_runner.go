package engine

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CommandResult struct {
	Command  string
	Dir      string
	Output   string
	Err      error
	Duration time.Duration
}

type CommandRunner interface {
	Run(ctx context.Context, dir string, name string, args ...string) CommandResult
}

type ClaudeRunner interface {
	RunClaude(ctx context.Context, repoDir string, prompt string) CommandResult
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, dir string, name string, args ...string) CommandResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return CommandResult{
		Command:  strings.TrimSpace(name + " " + strings.Join(args, " ")),
		Dir:      dir,
		Output:   string(out),
		Err:      err,
		Duration: time.Since(start),
	}
}

// defaultSuperpowersAllowedTools is the claude CLI --allowedTools default for
// non-interactive (--print) runs, where denied tools cannot prompt. Claude Code
// permission syntax allows ONE command prefix per Bash() rule ("Bash(go test:*)");
// a colon-joined multi-command list would parse as a single unmatched prefix and
// silently deny every shell command. The prompts instruct Claude to run go
// test/build via the absolute Go path, so both bare and absolute forms are listed.
// The apply step commits via superpowers_apply.go, so no git write commands here.
const defaultSuperpowersAllowedTools = "Bash(git diff:*),Bash(git status:*),Bash(git log:*),Bash(gofmt:*)," +
	"Bash(go test:*),Bash(go build:*),Bash(go vet:*)," +
	"Bash(/usr/local/go/bin/go test:*),Bash(/usr/local/go/bin/go build:*),Bash(/usr/local/go/bin/go vet:*)," +
	"Read,Write,Edit,Glob,Grep"

type execClaudeRunner struct {
	Bin string
	// AllowedTools, when non-empty, replaces the env/default --allowedTools
	// list. Used by the GOAP review fallback to run Claude read-only.
	AllowedTools string
	// ForceReadOnly pins the explicit --allowedTools list even when
	// BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS=true — the skip-permissions
	// branch would otherwise discard the caller's read-only tool set and run
	// Claude unrestricted. Set by callers whose security contract depends on
	// the tool list (the error handler's Read,Glob,Grep proposal run).
	ForceReadOnly bool
}

// buildClaudeArgs assembles the claude CLI argument list (exposed for tests —
// RunClaude cannot be observed without executing the binary).
func (r execClaudeRunner) buildClaudeArgs(prompt string) []string {
	allowed := r.AllowedTools
	if allowed == "" {
		allowed = getenvDefault("BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS", defaultSuperpowersAllowedTools)
	}
	args := []string{"--print", "--allowedTools", allowed, "-p", prompt}
	if !r.ForceReadOnly && strings.EqualFold(os.Getenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS"), "true") {
		args = []string{"--print", "--dangerously-skip-permissions", "-p", prompt}
	}
	args = withSuperpowersClaudeEffort(args, resolvedSuperpowersClaudeEffort())
	return withSuperpowersClaudeModel(args, resolvedSuperpowersClaudeModel())
}

func (r execClaudeRunner) RunClaude(ctx context.Context, repoDir string, prompt string) CommandResult {
	bin := r.Bin
	if bin == "" {
		bin = getenvDefault("BT_SUPERPOWERS_CLAUDE_BIN", "/home/nico/.local/bin/claude")
	}
	args := r.buildClaudeArgs(prompt)
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "PATH=/usr/local/go/bin:"+os.Getenv("HOME")+"/go/bin:"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return CommandResult{
		Command:  fmt.Sprintf("%s %s <prompt>", bin, strings.Join(args[:len(args)-1], " ")),
		Dir:      repoDir,
		Output:   string(out),
		Err:      err,
		Duration: time.Since(start),
	}
}

// defaultSuperpowersClaudeModel is passed as --model to the claude CLI when
// BT_SUPERPOWERS_CLAUDE_MODEL is unset. NOTE: this pins the model explicitly —
// deployments that relied on the CLI's own configured default must set the
// env var to "auto" (or "default"/"none") to omit the flag. The bare "opus"
// alias tracks whatever the CLI currently considers latest-Opus; the fleet
// pins the exact ID so a CLI-side alias move can never silently reroute
// autonomous cycles onto a different model (and a different quota pool).
const defaultSuperpowersClaudeModel = "claude-opus-5"

// defaultSuperpowersClaudeEffort is passed as --effort to the claude CLI when
// BT_SUPERPOWERS_CLAUDE_EFFORT is unset. Autonomous cycles are long-horizon
// agentic work where correctness beats token spend, so the fleet runs the top
// tier. Valid levels: low | medium | high | xhigh | max.
const defaultSuperpowersClaudeEffort = "max"

// resolvedSuperpowersClaudeModel returns the model for superpowers and
// GOAP-fusion claude runs. BT_SUPERPOWERS_CLAUDE_MODEL semantics:
// unset/empty → defaultSuperpowersClaudeModel; "auto"/"default"/"none" →
// "" (no --model flag, CLI default); anything else → used verbatim.
func resolvedSuperpowersClaudeModel() string {
	model := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_CLAUDE_MODEL"))
	if strings.EqualFold(model, "auto") || strings.EqualFold(model, "default") || strings.EqualFold(model, "none") {
		return ""
	}
	return cmp.Or(model, defaultSuperpowersClaudeModel)
}

func withSuperpowersClaudeModel(args []string, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return args
	}
	return append([]string{"--model", model}, args...)
}

// resolvedSuperpowersClaudeEffort mirrors resolvedSuperpowersClaudeModel's
// semantics for --effort: unset/empty → defaultSuperpowersClaudeEffort;
// "auto"/"default"/"none" → "" (no --effort flag, CLI/settings default);
// anything else → used verbatim.
func resolvedSuperpowersClaudeEffort() string {
	effort := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_CLAUDE_EFFORT"))
	if strings.EqualFold(effort, "auto") || strings.EqualFold(effort, "default") || strings.EqualFold(effort, "none") {
		return ""
	}
	return cmp.Or(effort, defaultSuperpowersClaudeEffort)
}

func withSuperpowersClaudeEffort(args []string, effort string) []string {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return args
	}
	return append([]string{"--effort", effort}, args...)
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var defaultSuperpowersCommandRunner CommandRunner = execCommandRunner{}
var defaultSuperpowersClaudeRunner ClaudeRunner = execClaudeRunner{}

// superpowersCommandTimeoutSecs is the wall-clock budget for a single wrapped
// command (build/test/lint/git). Default 600s. The OLD 180s budget was <= the
// inner go-test timeouts it wraps — verification runs `go test ... -timeout 300s`
// (changed-packages) and `-timeout 180s` (focused) — so it left ZERO headroom to
// cold-compile internal/engine (which changes almost every goap-fusion cycle),
// surfacing as "verification focused-tests failed: context deadline exceeded"
// degrades. 600s clears the 300s inner timeout plus compile headroom; genuinely
// slow tests still hit their own inner -timeout (a distinct, correct signal).
// Quick preflight/git checks are unaffected — they finish in milliseconds; the
// only effect there is a longer ceiling before a truly hung command is killed.
// Override with BT_SUPERPOWERS_COMMAND_TIMEOUT_SECS.
func superpowersCommandTimeoutSecs() int {
	if v := os.Getenv("BT_SUPERPOWERS_COMMAND_TIMEOUT_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 600
}

func superpowersCommandTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(superpowersCommandTimeoutSecs())*time.Second)
}

func recordCommandArtifact(path string, result CommandResult) error {
	status := "PASS"
	if result.Err != nil {
		status = "FAIL: " + result.Err.Error()
	}
	content := fmt.Sprintf("Command: %s\nDir: %s\nDuration: %s\nStatus: %s\n\n%s", result.Command, result.Dir, result.Duration, status, result.Output)
	_, err := writeArtifactOnce(path, []byte(content))
	if err != nil && strings.Contains(err.Error(), "file exists") {
		return nil
	}
	if err == nil {
		// writeArtifactOnce is intentionally idempotent; verification outputs should refresh.
		_ = err
	}
	return nil
}
