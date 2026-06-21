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

type execClaudeRunner struct {
	Bin string
}

func (r execClaudeRunner) RunClaude(ctx context.Context, repoDir string, prompt string) CommandResult {
	bin := r.Bin
	if bin == "" {
		bin = getenvDefault("BT_SUPERPOWERS_CLAUDE_BIN", "/home/nico/.local/bin/claude")
	}
	model := getenvDefault("BT_SUPERPOWERS_CLAUDE_MODEL", "opus")
	allowed := getenvDefault("BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS", "Bash(git diff:git status:gofmt:go test:go build:go vet:*),Read,Write,Edit,Glob,Grep")
	args := []string{"--print", "--model", model, "--allowedTools", allowed, "-p", prompt}
	if strings.EqualFold(os.Getenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS"), "true") {
		args = []string{"--print", "--dangerously-skip-permissions", "-p", prompt}
	}
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
