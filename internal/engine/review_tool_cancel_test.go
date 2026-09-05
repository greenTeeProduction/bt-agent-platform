package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewToolHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "side-effect")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	executeAgentTool("shell_exec", fmt.Sprintf("touch %q", path), &Blackboard{TraceContext: ctx, ChainTools: []any{ShellExec()}})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled tool performed side effect")
	}
}
func TestReviewRealToolRunningCancellation(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	effect := filepath.Join(dir, "effect")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan string, 1)
	go func() {
		done <- executeAgentTool("shell_exec", fmt.Sprintf("touch %q; sleep 2; touch %q", started, effect), &Blackboard{TraceContext: ctx, ChainTools: []any{newShellExecTool()}})
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(started); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("tool ignored running cancellation")
		<-done
	}
	if _, err := os.Stat(effect); !os.IsNotExist(err) {
		t.Error("cancelled shell descendant performed side effect")
	}
}
