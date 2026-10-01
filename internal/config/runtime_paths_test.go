package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/util"
)

func TestLoadedRuntimePathsReachAllPlatformOwners(t *testing.T) {
	for _, source := range []string{"json", "dotenv", "environment"} {
		t.Run(source, func(t *testing.T) {
			base := t.TempDir()
			t.Chdir(base)
			for _, key := range []string{"BT_AGENT_HOME", "BT_HOME", "BT_AGENT_DEFS_DIR", "BT_HISTORY_DIR", "BT_LOG_DIR", "BT_REFLECTIONS_DIR", "BT_CONFIG_FILE", "BT_DOTENV_FILE"} {
				t.Setenv(key, "")
			}
			defs := filepath.Join(base, "state", "custom-definitions")
			history := filepath.Join(base, "history-evidence")
			logs := filepath.Join(base, "diagnostics")
			reflections := filepath.Join(base, "reflections")
			switch source {
			case "json":
				data, err := json.Marshal(map[string]string{"agent_defs_dir": defs, "history_dir": history, "log_dir": logs, "reflections_dir": reflections})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(base, "config.json")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("BT_CONFIG_FILE", path)
			case "dotenv":
				path := filepath.Join(base, "paths.env")
				data := "BT_AGENT_DEFS_DIR=" + defs + "\nBT_HISTORY_DIR=" + history + "\nBT_LOG_DIR=" + logs + "\nBT_REFLECTIONS_DIR=" + reflections + "\n"
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("BT_DOTENV_FILE", path)
			case "environment":
				t.Setenv("BT_AGENT_DEFS_DIR", defs)
				t.Setenv("BT_HISTORY_DIR", history)
				t.Setenv("BT_LOG_DIR", logs)
				t.Setenv("BT_REFLECTIONS_DIR", reflections)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			restore, err := cfg.ConfigureRuntimePaths()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restore)
			for name, paths := range map[string][2]string{
				"home":      {agent.HomeDir(), filepath.Dir(defs)},
				"registry":  {agent.RegistryDir(), defs},
				"tasks":     {agent.TasksFile(), filepath.Join(filepath.Dir(defs), "tasks.json")},
				"HITL root": {util.RuntimePlatformHome(), cfg.Paths.HomeDir},
				"history":   {agent.HistoryDir(), cfg.Paths.HistoryDir},
				"logs":      {util.PlatformLogDir(), cfg.Paths.LogDir},
			} {
				if paths[0] != paths[1] {
					t.Fatalf("%s path=%q want=%q", name, paths[0], paths[1])
				}
			}
			if cfg.Paths.ReflectionsDir != reflections {
				t.Fatalf("reflection path=%q want=%q", cfg.Paths.ReflectionsDir, reflections)
			}
			// Loading a second configuration must not redirect active owners.
			other := config.DefaultConfig()
			other.AgentDefsDir = filepath.Join(base, "other", "agents")
			other.ResolvePaths()
			if agent.HomeDir() != filepath.Dir(defs) {
				t.Fatal("resolving a config redirected live state")
			}
			// An explicit state home overrides only the fallback state root;
			// independent configured definition/history/log paths remain intact.
			preferred := filepath.Join(base, "preferred-home")
			t.Setenv("BT_AGENT_HOME", preferred)
			if agent.HomeDir() != preferred || agent.RegistryDir() != defs || agent.HistoryDir() != history || agent.LogsDir() != logs {
				t.Fatal("explicit home lost independent path overrides")
			}
		})
	}
}
