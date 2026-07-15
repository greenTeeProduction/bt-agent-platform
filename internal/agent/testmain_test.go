package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain isolates BT_AGENT_HOME to a throwaway temp directory for every test
// in this package (the internal/a2a pattern). Without it, any test that
// forgets a per-test t.Setenv silently uses the real ~/.go-bt-evolve —
// runner_tracing_test.go's RunOnce polluted the real selector-stats/
// trace-test-agent.json on every gate run (re-observed 2026-07-15 after the
// 2026-07-10 cleanup). Per-test t.Setenv overrides still win.
func TestMain(m *testing.M) {
	origHome, hadHome := os.LookupEnv("BT_AGENT_HOME")
	dir, err := os.MkdirTemp("", "agent-test-home-")
	if err != nil {
		panic("internal/agent TestMain: MkdirTemp: " + err.Error())
	}
	os.Setenv("BT_AGENT_HOME", dir)

	code := m.Run()

	os.RemoveAll(dir)
	if hadHome {
		os.Setenv("BT_AGENT_HOME", origHome)
	} else {
		os.Unsetenv("BT_AGENT_HOME")
	}
	os.Exit(code)
}

// TestPackageTestsIsolateHomeDir fails the whole package loudly if the
// isolation above ever regresses: no test in internal/agent may resolve
// platform state under the real ~/.go-bt-evolve.
func TestPackageTestsIsolateHomeDir(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	realDataDir := filepath.Join(realHome, ".go-bt-evolve")

	for name, got := range map[string]string{
		"HomeDir":           HomeDir(),
		"SchedulerJobsFile": SchedulerJobsFile(),
		"SelectorStatsFile": SelectorStatsFile("trace-test-agent"),
	} {
		if got == realDataDir || strings.HasPrefix(got, realDataDir+string(os.PathSeparator)) {
			t.Fatalf("%s resolves to %q under the real %q; internal/agent tests must run with an isolated BT_AGENT_HOME (TestMain)", name, got, realDataDir)
		}
	}
}
