package main

import "testing"

// Regression: `bt-agent-cli test` (no agent name) must print a usage error,
// not panic with index-out-of-range. cmdTest/cmdLogs/cmdDelete share the
// requireNameArg seam so the guard is testable without os.Exit.
func TestRequireNameArg(t *testing.T) {
	if _, err := requireNameArg([]string{"bt-agent-cli", "test"}); err == nil {
		t.Fatal("missing agent name must return an error, not panic")
	}

	name, err := requireNameArg([]string{"bt-agent-cli", "test", "myagent"})
	if err != nil {
		t.Fatalf("unexpected error with name present: %v", err)
	}
	if name != "myagent" {
		t.Errorf("name = %q, want %q", name, "myagent")
	}
}
