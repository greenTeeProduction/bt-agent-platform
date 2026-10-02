package agentexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStandaloneRunnerMissingHomeDoesNotCreateRelativeState(t *testing.T) {
	base := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestStandaloneMissingHomeProbeHelper$")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "BT_") && !strings.HasPrefix(value, "HOME=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "BT_TEST_STANDALONE_MISSING_HOME=1", "BT_REFLECTIONS_DIR="+filepath.Join(base, "reflections"))
	cmd.Dir = base
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("standalone missing-home probe failed: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatal("missing platform home created cwd or reflection state")
	}
}

func TestStandaloneMissingHomeProbeHelper(t *testing.T) {
	if os.Getenv("BT_TEST_STANDALONE_MISSING_HOME") == "" {
		return
	}
	deps, err := NewRunDeps()
	if deps != nil || err == nil || !strings.Contains(err.Error(), "platform home:") {
		t.Fatalf("unresolved standalone root was accepted: %v", err)
	}
}
