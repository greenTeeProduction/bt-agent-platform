package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestResolvedSuperpowersCodexModelDefaultsToAstra(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "")

	if got := resolvedSuperpowersCodexModel(); got != "gpt-6-astra" {
		t.Fatalf("resolvedSuperpowersCodexModel() = %q, want gpt-6-astra", got)
	}
}

func TestResolvedSuperpowersCodexModelAllowsExplicitAuto(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "auto")

	if got := resolvedSuperpowersCodexModel(); got != "" {
		t.Fatalf("resolvedSuperpowersCodexModel() = %q, want empty auto/default model", got)
	}
}

func TestExecCodexRunnerBuildsReadOnlyArgs(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "")
	t.Setenv("BT_SUPERPOWERS_CODEX_SANDBOX", "")

	args := captureRunnerCodexArgs(t, execCodexRunner{Sandbox: "read-only", ForceReadOnly: true})
	joined := strings.Join(args, "\n")
	if len(args) < 2 || args[0] != "exec" {
		t.Fatalf("codex args = %q, want exec subcommand first", args)
	}
	for _, want := range []string{"--sandbox", "read-only", "--ephemeral", "--color", "never", "--output-last-message"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("codex args = %q, missing %q", args, want)
		}
	}
	if !strings.Contains(joined, "-m\ngpt-6-astra") {
		t.Fatalf("codex args = %q, want default -m gpt-6-astra", args)
	}
	if args[len(args)-1] != "hello" {
		t.Fatalf("codex args = %q, want prompt last", args)
	}
}

func TestExecCodexRunnerBuildsWriteArgs(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "")
	t.Setenv("BT_SUPERPOWERS_CODEX_SANDBOX", "")

	args := captureRunnerCodexArgs(t, execCodexRunner{})
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--sandbox\nworkspace-write") {
		t.Fatalf("codex args = %q, want default --sandbox workspace-write", args)
	}
}

func TestExecCodexRunnerOmitsModelWhenAuto(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "auto")
	t.Setenv("BT_SUPERPOWERS_CODEX_SANDBOX", "")

	args := captureRunnerCodexArgs(t, execCodexRunner{})
	for _, a := range args {
		if a == "-m" {
			t.Fatalf("codex args = %q, did not expect -m when model set to auto", args)
		}
	}
}

func TestExecCodexRunnerSandboxEnvOverride(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "")
	t.Setenv("BT_SUPERPOWERS_CODEX_SANDBOX", "danger-full-access")

	args := captureRunnerCodexArgs(t, execCodexRunner{})
	if !strings.Contains(strings.Join(args, "\n"), "--sandbox\ndanger-full-access") {
		t.Fatalf("codex args = %q, want env --sandbox danger-full-access override", args)
	}
}

func TestExecCodexRunnerForceReadOnlyPinsSandbox(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "")
	// The env override must never widen a read-only review run.
	t.Setenv("BT_SUPERPOWERS_CODEX_SANDBOX", "danger-full-access")

	args := captureRunnerCodexArgs(t, execCodexRunner{Sandbox: "read-only", ForceReadOnly: true})
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--sandbox\nread-only") {
		t.Fatalf("codex args = %q, ForceReadOnly must pin --sandbox read-only", args)
	}
	if strings.Contains(joined, "danger-full-access") {
		t.Fatalf("codex args = %q, env override leaked into a read-only run", args)
	}
}

func TestRunCodexFailurePropagatesError(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	script := "#!/bin/sh\necho 'boom' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	r := execCodexRunner{Bin: bin}
	res := r.RunCodex(context.Background(), dir, "hello")
	if res.Err == nil {
		t.Fatalf("RunCodex = nil err, want failure\noutput: %s", res.Output)
	}
	if !strings.Contains(res.Output, "boom") {
		t.Fatalf("RunCodex output = %q, want captured stderr 'boom'", res.Output)
	}
}

func TestRunCodexCancellationPropagates(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already dead before the call

	start := time.Now()
	res := execCodexRunner{Bin: bin}.RunCodex(ctx, dir, "hello")
	if res.Err == nil {
		t.Fatalf("RunCodex = nil err on cancelled context, want cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancelled RunCodex blocked for %s, want immediate return", elapsed)
	}
}

func TestRunCodexRespectsBinEnv(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	capture := filepath.Join(dir, "ran.txt")
	script := fmt.Sprintf("#!/bin/sh\ntouch %s\n%s\n", strconv.Quote(capture), codexOutputLastMessageSh)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("BT_SUPERPOWERS_CODEX_BIN", bin)

	res := execCodexRunner{}.RunCodex(context.Background(), dir, "hello")
	if res.Err != nil {
		t.Fatalf("RunCodex returned error: %v", res.Err)
	}
	if _, err := os.Stat(capture); err != nil {
		t.Fatalf("env bin not invoked: %v", err)
	}
}

func TestRunCodexReturnsLastMessageOnSuccess(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	// The real CLI echoes the prompt and interleaves session diagnostics into
	// the console stream; the runner must return ONLY the --output-last-message
	// content, never the transcript.
	script := "#!/bin/sh\n" +
		"echo 'NOISY-STDOUT-DIAGNOSTIC'\n" +
		"echo 'prompt echo: hello' >&2\n" +
		codexOutputLastMessageSh
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	res := execCodexRunner{Bin: bin}.RunCodex(context.Background(), dir, "hello")
	if res.Err != nil {
		t.Fatalf("RunCodex err = %v, want nil\noutput: %q", res.Err, res.Output)
	}
	if res.Output != "FAKE_CODEX_LAST_MESSAGE" {
		t.Fatalf("RunCodex output = %q, want exactly FAKE_CODEX_LAST_MESSAGE (no echoed prompt/diagnostics)", res.Output)
	}
	if strings.Contains(res.Output, "NOISY-STDOUT") || strings.Contains(res.Output, "prompt echo") {
		t.Fatalf("RunCodex output leaked transcript: %q", res.Output)
	}
}

func TestRunCodexMissingLastMessageFails(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	// Exits 0 but never writes the output file (the codex-on-error shape) —
	// must surface as an error, not a silent empty success.
	script := "#!/bin/sh\necho 'looks fine'\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	res := execCodexRunner{Bin: bin}.RunCodex(context.Background(), dir, "hello")
	if res.Err == nil {
		t.Fatalf("RunCodex err = nil, want missing-output error\noutput: %q", res.Output)
	}
	if !strings.Contains(res.Err.Error(), "missing") {
		t.Fatalf("RunCodex err = %q, want it to mention missing output", res.Err)
	}
}

func TestRunCodexEmptyLastMessageFails(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n" +
		"_out=\"\"\n" +
		"while [ \"$#\" -gt 1 ]; do\n" +
		"\tif [ \"$1\" = \"--output-last-message\" ]; then _out=\"$2\"; break; fi\n" +
		"\tshift\n" +
		"done\n" +
		"if [ -n \"$_out\" ]; then : > \"$_out\"; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	res := execCodexRunner{Bin: bin}.RunCodex(context.Background(), dir, "hello")
	if res.Err == nil {
		t.Fatalf("RunCodex err = nil, want empty-output error")
	}
	if !strings.Contains(res.Err.Error(), "empty") {
		t.Fatalf("RunCodex err = %q, want it to mention empty output", res.Err)
	}
}

func TestRunCodexInFlightCancellationKillsChildGroup(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	pidFile := filepath.Join(dir, "child.pid")
	// Spawn a long-lived grandchild (standing in for codex's Node child group),
	// record its PID, then block so the runner is still in-flight when the
	// context is cancelled. Cancellation must reap the whole process group, not
	// just the direct wrapper child.
	script := fmt.Sprintf("#!/bin/sh\nsleep 300 &\necho $! > %s\nwait\n", strconv.Quote(pidFile))
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan CommandResult, 1)
	go func() {
		done <- execCodexRunner{Bin: bin}.RunCodex(ctx, dir, "hello")
	}()

	// Wait until the grandchild has spawned.
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if p, aerr := strconv.Atoi(strings.TrimSpace(string(data))); aerr == nil && p > 0 {
				pid = p
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("grandchild never spawned")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()

	select {
	case res := <-done:
		if res.Err == nil {
			t.Fatalf("RunCodex = nil err after in-flight cancellation, want cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("RunCodex did not return after in-flight cancellation")
	}

	// The grandchild must be dead too — proof the process group was killed,
	// not just the wrapper.
	deadline = time.Now().Add(5 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatalf("kill(%d, 0): %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid %d still alive after cancellation", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// captureRunnerCodexArgs runs RunCodex against a fake executable that dumps
// its argv to a file (and satisfies the --output-last-message contract), so
// argument assembly can be asserted without the real codex binary. Mirrors
// captureRunnerClaudeArgs in superpowers_runner_test.go.
func captureRunnerCodexArgs(t *testing.T, runner execCodexRunner) []string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "args.txt")
	fakeCodex := filepath.Join(dir, "codex")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %s\n%s\n", strconv.Quote(capture), codexOutputLastMessageSh)
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	runner.Bin = fakeCodex
	result := runner.RunCodex(context.Background(), dir, "hello")
	if result.Err != nil {
		t.Fatalf("RunCodex returned error: %v\noutput: %s", result.Err, result.Output)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// codexOutputLastMessageSh is a POSIX sh snippet that locates the
// --output-last-message <path> argument and writes a sentinel to it, so fake
// codex binaries satisfy the runner's final-response contract without the real
// CLI. The runner returns the file content verbatim (trailing newline trimmed),
// so the sentinel is exactly what success-path tests assert against.
const codexOutputLastMessageSh = `
_out=""
while [ "$#" -gt 1 ]; do
	if [ "$1" = "--output-last-message" ]; then
		_out="$2"
		break
	fi
	shift
done
if [ -n "$_out" ]; then
	printf 'FAKE_CODEX_LAST_MESSAGE\n' > "$_out"
fi
`

// TestCodexRunnerRealSmokeOptIn exercises the REAL codex CLI end-to-end through
// the shared delegating runner so the platform's Codex delegation is proven
// against the installed binary without modifying the repository. It is OFF by
// default (it needs a valid codex login and spends quota); opt in with:
//
//	BT_SUPERPOWERS_CODEX_SMOKE=1 go test ./internal/engine -run TestCodexRunnerRealSmokeOptIn -v
//
// The provider is pinned to codex, the runner is read-only (--sandbox
// read-only), and the sentinel prompt instructs Codex to use no tools and edit
// nothing, so the smoke run cannot write to disk. The final response must match
// the sentinel EXACTLY — a verbatim --output-last-message read, not a
// transcript that merely contains the token.
func TestCodexRunnerRealSmokeOptIn(t *testing.T) {
	if os.Getenv("BT_SUPERPOWERS_CODEX_SMOKE") == "" {
		t.Skip("opt-in: set BT_SUPERPOWERS_CODEX_SMOKE=1 to exercise the real codex CLI")
	}
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")

	repo := t.TempDir()
	if out, err := runGoapGit(repo, 30*time.Second, "init"); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	const sentinel = "BT_CODEX_SMOKE_OK"
	r := newReadOnlyDelegatingRunner("Read")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res := r.RunClaude(ctx, repo,
		"Reply with exactly the token "+sentinel+" and nothing else. Do not run any tools, do not read or edit any files.")
	if res.Err != nil {
		t.Fatalf("codex smoke failed: %v\noutput: %s", res.Err, res.Output)
	}
	if res.Output != sentinel {
		t.Fatalf("codex smoke output = %q, want exactly %q", res.Output, sentinel)
	}
}
