package engine

import (
	"testing"
	"time"
)

func TestSuperpowersCommandTimeout_DefaultExceedsInnerTestTimeouts(t *testing.T) {
	// The verification suite wraps `go test -timeout 300s` (changed-packages) and
	// `-timeout 180s` (focused). The outer budget MUST exceed the largest inner
	// timeout (300s) with compile headroom, or cold-compiling internal/engine
	// deadline-exceeds the outer ctx before the test finishes → the observed
	// "verification focused-tests failed: context deadline exceeded" degrades.
	if got := superpowersCommandTimeoutSecs(); got <= 300 {
		t.Fatalf("default command timeout %ds must exceed the 300s inner changed-packages-tests timeout with headroom", got)
	}
	if got := superpowersCommandTimeoutSecs(); got != 600 {
		t.Fatalf("default command timeout = %ds, want 600", got)
	}
	ctx, cancel := superpowersCommandTimeout()
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok || time.Until(dl) < 500*time.Second {
		t.Fatalf("command context deadline too tight: %v", time.Until(dl))
	}
}

func TestSuperpowersCommandTimeout_EnvOverride(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_COMMAND_TIMEOUT_SECS", "900")
	if got := superpowersCommandTimeoutSecs(); got != 900 {
		t.Fatalf("env override = %ds, want 900", got)
	}
	t.Setenv("BT_SUPERPOWERS_COMMAND_TIMEOUT_SECS", "bogus")
	if got := superpowersCommandTimeoutSecs(); got != 600 {
		t.Fatalf("bogus env should fall back to 600, got %d", got)
	}
	t.Setenv("BT_SUPERPOWERS_COMMAND_TIMEOUT_SECS", "0")
	if got := superpowersCommandTimeoutSecs(); got != 600 {
		t.Fatalf("zero/invalid env should fall back to 600, got %d", got)
	}
}
