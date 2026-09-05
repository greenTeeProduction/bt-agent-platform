package notebooklmauth

import (
	"context"
	_ "embed"
	"os/exec"
	"time"
)

// CLIPath and pythonPath refer to the same upgraded uv installation. Do not
// resolve nlm through a daemon/cron PATH that may still contain an old CLI.
const CLIPath = "/home/nico/.local/share/uv/tools/notebooklm-mcp-cli/bin/nlm"
const pythonPath = "/home/nico/.local/share/uv/tools/notebooklm-mcp-cli/bin/python3"

//go:embed helper.py
var helper string

// Command runs the installed CLI with browser fallback and credential writes
// disabled. Authentication restoration is exclusively owned by Ensure.
func Command(ctx context.Context, args ...string) *exec.Cmd {
	return helperCommand(ctx, "cli", args...)
}

func helperCommand(ctx context.Context, mode string, args ...string) *exec.Cmd {
	argv := append([]string{"-B", "-c", helper, mode}, args...)
	// The executable is pinned and -c receives only the build-time embedded helper.
	// User arguments follow the script as argv data; no shell or dynamic code is used.
	cmd := exec.CommandContext(ctx, pythonPath, argv...) // #nosec G204 -- trusted embedded adapter; argv is not interpreted as code.
	cmd.WaitDelay = time.Second
	return cmd
}
