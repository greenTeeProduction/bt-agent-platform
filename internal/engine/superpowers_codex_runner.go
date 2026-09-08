package engine

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		"--", // Untrusted prompt must never be parsed as CLI options.
		prompt,
	)
}

func (r execCodexRunner) RunCodex(ctx context.Context, repoDir string, prompt string) CommandResult {
	bin := r.Bin
	if bin == "" {
		bin = getenvDefault("BT_SUPERPOWERS_CODEX_BIN", "/mnt/ssd/npm-global/bin/codex")
	}

	// Bin and its environment override are trusted operator configuration,
	// never model/request input. Refuse relative paths so repoDir/PATH cannot
	// substitute a repository-controlled executable. Symlinks are supported
	// for npm-global installs; operators must protect the installation and
	// its owning group (shared npm installs legitimately use 0775).
	if !filepath.IsAbs(bin) || filepath.Clean(bin) != bin {
		return CommandResult{Dir: repoDir, Err: fmt.Errorf("codex executable must be a clean absolute path")}
	}
	info, err := os.Stat(bin)
	if err != nil {
		return CommandResult{Dir: repoDir, Err: fmt.Errorf("codex executable: %w", err)}
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o002 != 0 || info.Mode().Perm()&0o111 == 0 {
		return CommandResult{Dir: repoDir, Err: fmt.Errorf("codex executable must be a regular executable not writable by others")}
	}
	// Keep the missing-vs-empty distinction without an unreserved name in a
	// shared temp directory. The directory is 0700 and the read is rooted so
	// even the child cannot redirect the final response outside it by symlink.
	outputDir, err := os.MkdirTemp("", "codex-last-message-*")
	if err != nil {
		return CommandResult{Dir: repoDir, Err: fmt.Errorf("codex output directory: %w", err)}
	}
	defer os.RemoveAll(outputDir)
	outputRoot, err := os.OpenRoot(outputDir)
	if err != nil {
		return CommandResult{Dir: repoDir, Err: fmt.Errorf("codex output root: %w", err)}
	}
	defer outputRoot.Close()
	outputPath := filepath.Join(outputDir, "response.txt")

	args := r.buildCodexArgs(prompt, outputPath)
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- validated absolute operator-configured CLI; no shell, prompt follows --.
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
		Command:  fmt.Sprintf("%s %s <prompt>", bin, strings.Join(args[:len(args)-4], " ")),
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

	data, err := outputRoot.ReadFile("response.txt")
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
// Set BT_SUPERPOWERS_CODEX_MODEL to an explicit model ID to override this
// default, or "auto" (or "default"/"none") to drop the flag and inherit the
// account default instead. Unavailable models fail without substitution.
const defaultSuperpowersCodexModel = "gpt-5.3-codex-spark"

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
