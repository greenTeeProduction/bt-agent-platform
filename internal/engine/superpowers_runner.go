package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
}

func (r execClaudeRunner) RunClaude(ctx context.Context, repoDir string, prompt string) CommandResult {
	bin := r.Bin
	if bin == "" {
		bin = getenvDefault("BT_SUPERPOWERS_CLAUDE_BIN", "/home/nico/.local/bin/claude")
	}
	model := resolvedSuperpowersClaudeModel()
	allowed := r.AllowedTools
	if allowed == "" {
		allowed = getenvDefault("BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS", defaultSuperpowersAllowedTools)
	}
	args := []string{"--print", "--allowedTools", allowed, "-p", prompt}
	if strings.EqualFold(os.Getenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS"), "true") {
		args = []string{"--print", "--dangerously-skip-permissions", "-p", prompt}
	}
	args = withSuperpowersClaudeModel(args, model)
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
// env var to "auto" (or "default"/"none") to omit the flag.
const defaultSuperpowersClaudeModel = "opus"

// resolvedSuperpowersClaudeModel returns the model for superpowers and
// GOAP-fusion claude runs. BT_SUPERPOWERS_CLAUDE_MODEL semantics:
// unset/empty → defaultSuperpowersClaudeModel; "auto"/"default"/"none" →
// "" (no --model flag, CLI default); anything else → used verbatim.
func resolvedSuperpowersClaudeModel() string {
	model := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_CLAUDE_MODEL"))
	if strings.EqualFold(model, "auto") || strings.EqualFold(model, "default") || strings.EqualFold(model, "none") {
		return ""
	}
	if model != "" {
		return model
	}
	return defaultSuperpowersClaudeModel
}

func withSuperpowersClaudeModel(args []string, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return args
	}
	return append([]string{"--model", model}, args...)
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var defaultSuperpowersCommandRunner CommandRunner = execCommandRunner{}
var defaultSuperpowersClaudeRunner ClaudeRunner = execClaudeRunner{}

func superpowersCommandTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 180*time.Second)
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
