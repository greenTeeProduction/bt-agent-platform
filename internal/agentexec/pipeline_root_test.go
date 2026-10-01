package agentexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
)

func TestLoadPipelineRootedNamesAndSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)
	root := agent.WorkflowsDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	valid := []byte("name: owned-workflow\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(root, "nested", "owned.yaml"), valid, 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside.yaml")
	if err := os.WriteFile(outside, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("nested", "owned.yaml"), filepath.Join(root, "inside.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nested/owned", "nested/owned.yaml", "inside"} {
		pipeline, err := LoadPipeline(name)
		if err != nil || pipeline.Name != "owned-workflow" {
			t.Errorf("load owned %q: name=%q err=%v", name, pipeline.Name, err)
		}
	}
	for _, name := range []string{"", "../../outside", outside, "escape", "nested/../../../outside"} {
		if _, err := LoadPipeline(name); err == nil {
			t.Errorf("escaped workflow %q was read", name)
		}
	}

	// Operators may relocate the entire configured workflow root; containment
	// applies within the opened directory, not to its operator-owned location.
	relocated := filepath.Join(home, "relocated-workflows")
	if err := os.Rename(root, relocated); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relocated, root); err != nil {
		t.Fatal(err)
	}
	if pipeline, err := LoadPipeline("nested/owned"); err != nil || pipeline.Name != "owned-workflow" {
		t.Errorf("relocated configured root: name=%q err=%v", pipeline.Name, err)
	}
}
