package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCodexDelegationWritesArtifactOptIn proves the selected platform backend
// executes a coding tool, rather than merely echoing a prompt. It spends Codex
// quota only when explicitly enabled, and operates in a disposable repository.
func TestCodexDelegationWritesArtifactOptIn(t *testing.T) {
	if os.Getenv("BT_SUPERPOWERS_CODEX_WRITE_SMOKE") != "1" {
		t.Skip("set BT_SUPERPOWERS_CODEX_WRITE_SMOKE=1 for a real Codex coding delegation")
	}
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")
	t.Setenv("BT_SUPERPOWERS_CODEX_SANDBOX", "workspace-write")
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "auto")
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	result := newImplementationDelegatingRunner().RunClaude(ctx, dir,
		"Use a tool to create bt-smoke.txt containing exactly BT_CODEX_WRITE_OK. Do not change other files. Then reply exactly DONE and nothing else.")
	if result.Err != nil {
		t.Fatalf("selected Codex backend failed: %v\n%s", result.Err, result.Output)
	}
	if strings.TrimSpace(result.Output) != "DONE" {
		t.Fatalf("expected final answer only, got %q", result.Output)
	}
	data, err := os.ReadFile(filepath.Join(dir, "bt-smoke.txt"))
	if err != nil || strings.TrimSpace(string(data)) != "BT_CODEX_WRITE_OK" {
		t.Fatalf("coding artifact not verified: data=%q err=%v", data, err)
	}
	t.Log("selected Codex backend produced final answer DONE and verified file BT_CODEX_WRITE_OK")
}
