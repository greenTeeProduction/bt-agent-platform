package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeDir_DefaultUsesDotGoBtEvolve(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", "")
	t.Setenv("BT_AGENT_DEFS_DIR", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home")
	}

	got := HomeDir()
	want := filepath.Join(home, ".go-bt-evolve")
	if got != want {
		t.Errorf("HomeDir() = %q, want %q", got, want)
	}
}

func TestHomeDir_BTAgentHomeOverride(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", "/tmp/bt-agent-test-home")
	t.Setenv("BT_AGENT_DEFS_DIR", "")

	if got := HomeDir(); got != "/tmp/bt-agent-test-home" {
		t.Errorf("HomeDir() = %q, want override", got)
	}
}

func TestPathHelpers(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", filepath.Join(t.TempDir(), "agent-home"))

	if got, want := RegistryDir(), filepath.Join(HomeDir(), "agents"); got != want {
		t.Errorf("RegistryDir() = %q, want %q", got, want)
	}
	if got, want := TemplatesDir(), filepath.Join(HomeDir(), "agents", "templates"); got != want {
		t.Errorf("TemplatesDir() = %q, want %q", got, want)
	}
	if got, want := WorkflowsDir(), filepath.Join(HomeDir(), "agents", "workflows"); got != want {
		t.Errorf("WorkflowsDir() = %q, want %q", got, want)
	}
}
