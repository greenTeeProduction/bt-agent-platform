package dashboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
)

func TestSecurityDashboardStorage(t *testing.T) {
	for _, name := range []string{"../outside", `a\b`, "bad name", "/absolute"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("BT_AGENT_HOME", base)
			outside := filepath.Join(base, "outside.yaml")
			if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := CreateAgent(AgentYAMLConfig{Name: name, Tree: "unused"}); err == nil {
				t.Errorf("create accepted %q", name)
			}
			if err := DeleteAgent(name); err == nil {
				t.Errorf("delete accepted %q", name)
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != "unchanged" {
				t.Errorf("outside changed: %q %v", b, err)
			}
		})
	}
	for _, ext := range []string{".yaml", ".yml"} {
		t.Run(ext, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("BT_AGENT_HOME", base)
			if err := os.MkdirAll(agent.RegistryDir(), 0755); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "outside"+ext)
			if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(agent.RegistryDir(), "linked"+ext)); err != nil {
				t.Fatal(err)
			}
			if ext == ".yaml" {
				if err := CreateAgent(AgentYAMLConfig{Name: "linked", Tree: "unused"}); err == nil {
					t.Error("created through symlink")
				}
			}
			if err := DeleteAgent("linked"); err == nil {
				t.Error("fallback accepted symlink")
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != "unchanged" {
				t.Errorf("outside changed: %q %v", b, err)
			}
		})
	}
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	if err := CreateAgent(AgentYAMLConfig{Name: "valid-name", Tree: "unused"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAgent("valid-name"); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityDashboardLegacyYMLDelete(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	if err := os.MkdirAll(agent.RegistryDir(), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agent.RegistryDir(), "legacy-agent.yml")
	if err := os.WriteFile(path, []byte("name: legacy-agent\ntree: unused\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAgent("legacy-agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy definition still exists: %v", err)
	}
}
