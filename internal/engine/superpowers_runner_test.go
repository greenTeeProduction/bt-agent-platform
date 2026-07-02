package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestResolvedSuperpowersClaudeModelDefaultsToOpus(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CLAUDE_MODEL", "")

	if got := resolvedSuperpowersClaudeModel(); got != "opus" {
		t.Fatalf("resolvedSuperpowersClaudeModel() = %q, want opus", got)
	}
}

func TestResolvedSuperpowersClaudeModelAllowsExplicitAuto(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CLAUDE_MODEL", "auto")

	if got := resolvedSuperpowersClaudeModel(); got != "" {
		t.Fatalf("resolvedSuperpowersClaudeModel() = %q, want empty auto/default model", got)
	}
}

func TestExecClaudeRunnerPassesDefaultModel(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CLAUDE_MODEL", "")
	t.Setenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS", "")

	args := captureExecClaudeArgs(t)
	if len(args) < 2 || args[0] != "--model" || args[1] != "opus" {
		t.Fatalf("claude args = %q, want explicit default --model opus", args)
	}
}

func TestExecClaudeRunnerKeepsModelWhenSkippingPermissions(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CLAUDE_MODEL", "sonnet")
	t.Setenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS", "true")

	args := captureExecClaudeArgs(t)
	joined := strings.Join(args, "\n")
	if len(args) < 4 || args[0] != "--model" || args[1] != "sonnet" {
		t.Fatalf("claude args = %q, want explicit configured --model sonnet", args)
	}
	if !strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Fatalf("claude args = %q, want skip-permissions flag", args)
	}
	if strings.Contains(joined, "--allowedTools") {
		t.Fatalf("claude args = %q, did not expect allowedTools in skip-permissions mode", args)
	}
}

func TestExecClaudeRunnerAllowedToolsOverride(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS", "")
	t.Setenv("BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS", "")

	args := captureRunnerClaudeArgs(t, execClaudeRunner{AllowedTools: "Read,Grep"})
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--allowedTools\nRead,Grep") {
		t.Fatalf("claude args = %q, want --allowedTools Read,Grep override", args)
	}
	if strings.Contains(joined, "Write") {
		t.Fatalf("claude args = %q, override must replace the default tool list", args)
	}
}

func captureExecClaudeArgs(t *testing.T) []string {
	t.Helper()
	return captureRunnerClaudeArgs(t, execClaudeRunner{})
}

func captureRunnerClaudeArgs(t *testing.T, runner execClaudeRunner) []string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "args.txt")
	fakeClaude := filepath.Join(dir, "claude")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %s\n", strconv.Quote(capture))
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	runner.Bin = fakeClaude
	result := runner.RunClaude(context.Background(), dir, "hello")
	if result.Err != nil {
		t.Fatalf("RunClaude returned error: %v\noutput: %s", result.Err, result.Output)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}
