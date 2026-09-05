package engine

import (
	"context"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestPsGrepPatternMatchesRealServiceNames pins the process-liveness pattern:
// the old code built "[b]agent" (bt- prefix stripped, [b] re-prefixed), which
// can never match a "bt-agent" process — so RestartDeadAgents judged every
// service dead on every tick and restarted the entire bt fleet
// unconditionally. Observed live 2026-07-02: every pre-commit hook test run
// (which smoke-executes the agent_monitor tree) restarted bt-agent,
// bt-gardener, and bt-dashboard, killing in-flight goap-fusion cycles.
func TestPsGrepPatternMatchesRealServiceNames(t *testing.T) {
	cases := map[string]string{
		"bt-agent":     "[b]t-agent",
		"bt-gardener":  "[b]t-gardener",
		"bt-dashboard": "[b]t-dashboard",
	}
	for name, want := range cases {
		if got := psGrepPattern(name); got != want {
			t.Errorf("psGrepPattern(%q) = %q, want %q", name, got, want)
		}
		// The bracket trick must still match the process name itself.
		plain := strings.ReplaceAll(strings.ReplaceAll(psGrepPattern(name), "[", ""), "]", "")
		if plain != name {
			t.Errorf("pattern %q does not reduce to process name %q", psGrepPattern(name), name)
		}
	}
}

// TestRestartDeadAgentsInertUnderGoTest pins that the action never touches
// systemctl from inside a test binary: a test run must observe, not manage,
// the live fleet.
func TestRestartDeadAgentsInertUnderGoTest(t *testing.T) {
	fn := GetAction("RestartDeadAgents")
	if fn == nil {
		t.Fatal("RestartDeadAgents not registered")
	}
	bb := &Blackboard{Task: "restart dead agents"}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("RestartDeadAgents under test = %d, want 1 (report-only)", code)
	}
	if strings.Contains(bb.Result, "→ RESTARTED") || strings.Contains(bb.Result, "RESTART FAILED") {
		t.Fatalf("test binary must never manage services; report:\n%s", bb.Result)
	}
}
