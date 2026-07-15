package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
)

// TestMain isolates BT_AGENT_HOME to a throwaway temp directory for every test
// in this package (the internal/a2a pattern). Without it, the bt_evolve tool
// tests batch-wrote the real ~/.go-bt-evolve *-godev evolution archives, the
// DLQ tool tests touched the real dead_letter_queue.json, and the blackboard
// tool tests wrote real blackboard files on every gate run — observed live on
// 2026-07-15 (the 10:46:51 archive burst). Per-test t.Setenv overrides still
// win over this package-wide default.
func TestMain(m *testing.M) {
	origHome, hadHome := os.LookupEnv("BT_AGENT_HOME")
	dir, err := os.MkdirTemp("", "bt-agent-test-home-")
	if err != nil {
		panic("cmd/bt-agent TestMain: MkdirTemp: " + err.Error())
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
// isolation above ever regresses: no test in cmd/bt-agent may resolve platform
// state under the real ~/.go-bt-evolve.
func TestPackageTestsIsolateHomeDir(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	realDataDir := filepath.Join(realHome, ".go-bt-evolve")

	for name, got := range map[string]string{
		"HomeDir":           agent.HomeDir(),
		"SchedulerJobsFile": agent.SchedulerJobsFile(),
		"DLQFile":           agent.DLQFile(),
	} {
		if got == realDataDir || strings.HasPrefix(got, realDataDir+string(os.PathSeparator)) {
			t.Fatalf("agent.%s() = %q resolves under the real %q; cmd/bt-agent tests must run with an isolated BT_AGENT_HOME (TestMain)", name, got, realDataDir)
		}
	}
}
