package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// graphifyFallbackDir is the conventional user-local install directory searched
// for the graphify tool when it is not resolvable on PATH.
//
// graphify is a uv-managed tool shimmed into ~/.local/bin. A cold boot restores
// the systemd-user default PATH (which drops ~/.local/bin), which on 2026-07-13
// made the scheduled GOAP fusion cycle's graphify guard the one preflight that
// broke on reboot — every landing cycle dead-lettered — while its sibling
// guards (VerifyScheduledGoapFusionRuntime/Toolchain/NotebookLMTool, all of
// which resolve their tool by absolute path) kept working. Pinning the fallback
// dir makes graphify resolution PATH-robust like those siblings. A package var
// so tests can override it.
var graphifyFallbackDir = defaultGraphifyFallbackDir()

func defaultGraphifyFallbackDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "bin")
	}
	return ""
}

// resolveGraphifyBin resolves the external graphify tool to an executable path,
// preferring a PATH lookup and falling back to the conventional user-local
// install dir. It returns an error only when graphify is genuinely absent, so
// the guards' loud fail-fast contract is preserved: this makes graphify
// PATH-robust without ever silently proceeding from a stale report.
func resolveGraphifyBin() (string, error) {
	if p, err := exec.LookPath(goapFusionGraphifyTool); err == nil {
		return p, nil
	}
	if graphifyFallbackDir != "" {
		cand := filepath.Join(graphifyFallbackDir, goapFusionGraphifyTool)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return cand, nil
		}
	}
	return "", fmt.Errorf("graphify tool %q not resolvable on PATH and not an executable in %q", goapFusionGraphifyTool, graphifyFallbackDir)
}
