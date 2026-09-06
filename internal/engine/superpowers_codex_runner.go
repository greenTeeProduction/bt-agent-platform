package engine

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CodexRunner executes a one-shot OpenAI Codex CLI delegation. It mirrors
// ClaudeRunner so the shared delegatingRunner (superpowers_provider.go) can
// route a single abstract "delegate this prompt" call to either provider.
type CodexRunner interface {
	RunCodex(ctx context.Context, repoDir string, prompt string) CommandResult
}

// execCodexRunner invokes the codex CLI via `codex exec`. Verified against
// codex-cli 0.153.4 (see `codex exec --help`): exec is the non-interactive
// subcommand, `--sandbox` selects the sandbox policy (read-only |
// workspace-write | danger-full-access), `--ephemeral` skips persisting the
// session to disk, and `--color never` disables ANSI codes so logs stay clean.
type execCodexRunner struct {
	Bin string
	// Sandbox is the codex --sandbox mode. Empty → resolved from
	// BT_SUPERPOWERS_CODEX_SANDBOX (default workspace-write so the
	// implementation phases can write; review runners pass "read-only").
	Sandbox string
	// ForceReadOnly pins --sandbox read-only regardless of the env/field
	// override — review paths depend on this so a configuration mistake can
	// never widen a review run into a write-capable sandbox.
	ForceReadOnly bool
}

// buildCodexArgs assembles the `codex exec` argument list (exposed for tests —
// RunCodex cannot be observed without executing the binary). The final response
// is requested via `--output-last-message <outputFile>` so RunCodex can return
// the agent's last message verbatim instead of the combined stdout+stderr
// transcript — codex echoes the prompt and interleaves session diagnostics into
// the console stream, which corrupts any consumer that parses Output as the
// answer. The prompt is always the final positional argument.
func (r execCodexRunner) buildCodexArgs(prompt string, outputFile string) []string {
	sandbox := r.Sandbox
	if r.ForceReadOnly {
		sandbox = "read-only"
	}
	if sandbox == "" {
		sandbox = getenvDefault("BT_SUPERPOWERS_CODEX_SANDBOX", "workspace-write")
	}
	args := []string{"exec"}
	if model := resolvedSuperpowersCodexModel(); model != "" {
		args = append(args, "-m", model)
	}
	return append(args,
		"--sandbox", sandbox,
		"--ephemeral",
		"--color", "never",
		"--output-last-message", outputFile,
		prompt,
	)
}

func (r execCodexRunner) RunCodex(ctx context.Context, repoDir string, prompt string) CommandResult {
	bin := r.Bin
	if bin == "" {
		bin = getenvDefault("BT_SUPERPOWERS_CODEX_BIN", "/mnt/ssd/npm-global/bin/codex")
	}

	// The final response is written to a dedicated tempfile; CombinedOutput
	// only feeds the diagnostics path (below). The path is reserved via
	// CreateTemp and then left absent so codex creates it itself (verified
	// against 0.153.4) — that keeps a success-with-no-file distinguishable
	// from an empty file, and mirrors the real CLI, which leaves the file
	// unwritten on error.
	outputFile, err := os.CreateTemp("", "codex-last-message-*.txt")
	if err != nil {
		return CommandResult{
			Command: fmt.Sprintf("%s exec <prompt>", bin),
			Dir:     repoDir,
			Err:     fmt.Errorf("codex output tempfile: %w", err),
		}
	}
	outputPath := outputFile.Name()
	if err := outputFile.Close(); err != nil {
		os.Remove(outputPath)
		return CommandResult{
			Command: fmt.Sprintf("%s exec <prompt>", bin),
			Dir:     repoDir,
			Err:     fmt.Errorf("codex output tempfile close: %w", err),
		}
	}
	if err := os.Remove(outputPath); err != nil {
		return CommandResult{
			Command: fmt.Sprintf("%s exec <prompt>", bin),
			Dir:     repoDir,
			Err:     fmt.Errorf("codex output tempfile reset: %w", err),
		}
	}
	defer os.Remove(outputPath)

	args := r.buildCodexArgs(prompt, outputPath)
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = repoDir
	// Codex shells out to git/go; make the npm-global codex install and the
	// absolute Go toolchain reachable regardless of the ambient PATH.
	cmd.Env = append(os.Environ(), "PATH=/mnt/ssd/npm-global/bin:/usr/local/go/bin:"+os.Getenv("HOME")+"/go/bin:"+os.Getenv("PATH"))
	// The codex CLI is a Node wrapper that spawns its own child group; a bare
	// CommandContext kill would reap only the wrapper and leave model/agent
	// children orphaned past a timeout. Kill the whole process group and bound
	// pipe cleanup with a finite WaitDelay.
	bindToolCommandCancellation(cmd)
	diag, runErr := cmd.CombinedOutput()

	result := CommandResult{
		Command:  fmt.Sprintf("%s %s <prompt>", bin, strings.Join(args[:len(args)-3], " ")),
		Dir:      repoDir,
		Duration: time.Since(start),
	}

	if runErr != nil {
		// On failure codex leaves --output-last-message unwritten (verified by
		// probe); the console transcript is the diagnostic material.
		result.Output = string(diag)
		result.Err = runErr
		return result
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		result.Err = fmt.Errorf("codex succeeded but final response missing: %w", err)
		return result
	}
	out := strings.TrimRight(string(data), "\r\n")
	if out == "" {
		result.Err = fmt.Errorf("codex succeeded but --output-last-message file is empty")
		return result
	}
	result.Output = out
	return result
}

// defaultSuperpowersCodexModel is passed as -m to the codex CLI when
// BT_SUPERPOWERS_CODEX_MODEL is unset. Like the Claude default, the exact
// model ID is pinned (not an alias) so a CLI/account-side alias move cannot
// silently reroute autonomous cycles onto a different model and quota pool.
// `gpt-6-astra` is the current frontier model the fleet's ChatGPT-account
// login accepts (verified by smoke run); set BT_SUPERPOWERS_CODEX_MODEL to
// "auto" (or "default"/"none") to drop the flag and inherit the account
// default instead.
const defaultSuperpowersCodexModel = "gpt-6-astra"

// resolvedSuperpowersCodexModel returns the model for codex runs.
// BT_SUPERPOWERS_CODEX_MODEL semantics mirror the Claude model env:
// unset/empty → defaultSuperpowersCodexModel; "auto"/"default"/"none" → ""
// (no -m flag, account default); anything else → used verbatim.
func resolvedSuperpowersCodexModel() string {
	model := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_CODEX_MODEL"))
	if strings.EqualFold(model, "auto") || strings.EqualFold(model, "default") || strings.EqualFold(model, "none") {
		return ""
	}
	return cmp.Or(model, defaultSuperpowersCodexModel)
}
