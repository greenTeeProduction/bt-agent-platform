package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformHomePrecedence(t *testing.T) {
	base := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, preferred, legacy, defs, want string }{
		{"preferred", filepath.Join(base, "preferred"), filepath.Join(base, "legacy"), filepath.Join(base, "definitions", "agents"), filepath.Join(base, "preferred")},
		{"legacy", "", filepath.Join(base, "legacy"), filepath.Join(base, "definitions", "agents"), filepath.Join(base, "legacy")},
		{"definitions", "", "", filepath.Join(base, "definitions", "agents"), filepath.Join(base, "definitions")},
		{"default", "", "", "", filepath.Join(home, ".go-bt-evolve")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BT_AGENT_HOME", tc.preferred)
			t.Setenv("BT_HOME", tc.legacy)
			if got := PlatformHome(tc.defs); got != tc.want {
				t.Fatalf("root=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRuntimePathsPublishOwnedValuesAndEnvironmentOverrides(t *testing.T) {
	base := t.TempDir()
	for _, key := range []string{"BT_AGENT_HOME", "BT_HOME", "BT_AGENT_DEFS_DIR", "BT_HISTORY_DIR", "BT_LOG_DIR"} {
		t.Setenv(key, "")
	}
	paths := PlatformPaths{AgentDefsDir: filepath.Join(base, "configured", "definitions")}
	t.Cleanup(ConfigurePlatformPaths(paths))
	paths.AgentDefsDir = filepath.Join(base, "mutated", "definitions")
	if RuntimePlatformHome() != filepath.Join(base, "configured") {
		t.Fatal("configuration retained caller-owned mutable state")
	}
	if PlatformLogDir() != filepath.Join(base, "configured", "logs") || PlatformHistoryDir() != filepath.Join(base, "configured", "history") {
		t.Fatal("default owners do not share configured root")
	}
	envDefs := filepath.Join(base, "environment", "definitions")
	t.Setenv("BT_AGENT_DEFS_DIR", envDefs)
	t.Setenv("BT_HISTORY_DIR", filepath.Join(base, "history-env"))
	t.Setenv("BT_LOG_DIR", filepath.Join(base, "logs-env"))
	if RuntimePlatformHome() != filepath.Dir(envDefs) || PlatformAgentDefinitionsDir() != envDefs || PlatformHistoryDir() != filepath.Join(base, "history-env") || PlatformLogDir() != filepath.Join(base, "logs-env") {
		t.Fatal("environment failed to override loaded startup paths")
	}
}
