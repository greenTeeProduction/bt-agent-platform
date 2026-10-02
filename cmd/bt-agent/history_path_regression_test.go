package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInvalidHistoryStopsBeforeRuntimeInitialization(t *testing.T) {
	base := t.TempDir()
	history := filepath.Join(base, "history-is-file")
	if err := os.WriteFile(history, []byte("preserve history fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "config.json")
	data := `{"agent_defs_dir":"` + filepath.Join(base, "state", "definitions") + `","history_dir":"` + history + `"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestAgentInvalidHistoryProbeHelper$")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "BT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "BT_CONFIG_FILE="+path, "BT_TEST_HISTORY_PROBE=1")
	cmd.Dir = base
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "fatal: history store:") {
		t.Fatalf("invalid history was not rejected before startup: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(base, "state")); !os.IsNotExist(err) {
		t.Fatal("failed history startup opened other runtime owners")
	}
	if data, err := os.ReadFile(history); err != nil || string(data) != "preserve history fixture" {
		t.Fatal("history fixture was modified")
	}
}

func TestAgentInvalidHistoryProbeHelper(t *testing.T) {
	if os.Getenv("BT_TEST_HISTORY_PROBE") == "" {
		return
	}
	if os.Getenv("BT_TEST_MISSING_HOME_PROBE") == "1" {
		// TestMain installs an isolated BT home for every child as well.
		// Remove that fixture-only override for the missing-default-root probe.
		os.Unsetenv("BT_AGENT_HOME")
	}
	main()
	t.Fatal("invalid history startup returned successfully")
}

func TestAgentMissingDefaultHomeDoesNotWriteWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestAgentInvalidHistoryProbeHelper$")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "BT_") && !strings.HasPrefix(value, "HOME=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "BT_TEST_HISTORY_PROBE=1", "BT_TEST_MISSING_HOME_PROBE=1")
	cmd.Dir = base
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "fatal: configuration: platform home:") {
		t.Fatalf("missing default home was not rejected: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatal("missing default home created working-directory state")
	}
}
