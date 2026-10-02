package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardVersionDoesNotLoadCorruptTaskState(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tasks.json"), []byte("{invalid task state"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestDashboardVersionProbeHelper$", "version")
	cmd.Env = append(os.Environ(), "BT_AGENT_HOME="+root, "BT_TEST_DASHBOARD_VERSION_PROBE=1", "BT_CONFIG_FILE="+filepath.Join(root, "missing-config.json"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version probe loaded state or failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "bt-dashboard revision=") {
		t.Fatalf("version identity missing: %s", output)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("version probe created runtime state")
	}
}

func TestDashboardVersionProbeHelper(t *testing.T) {
	if os.Getenv("BT_TEST_DASHBOARD_VERSION_PROBE") == "" {
		return
	}
	main()
	os.Exit(0)
}
