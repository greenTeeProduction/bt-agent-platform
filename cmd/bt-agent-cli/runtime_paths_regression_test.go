package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIUsesLoadedDefinitionsBeforeOpeningRegistry(t *testing.T) {
	for _, source := range []string{"json", "dotenv"} {
		t.Run(source, func(t *testing.T) {
			base := t.TempDir()
			defs := filepath.Join(base, "state", "custom-definitions")
			path := filepath.Join(base, "config")
			data, setting := `{"agent_defs_dir":"`+defs+`"}`, "BT_CONFIG_FILE="
			if source == "dotenv" {
				data, setting = "BT_AGENT_DEFS_DIR="+defs+"\n", "BT_DOTENV_FILE="
			}
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-test.run=^TestCLIPathProbeHelper$")
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "BT_") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			cmd.Env = append(cmd.Env, setting+path, "BT_TEST_CLI_PATH_PROBE=1")
			cmd.Dir = base
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("CLI path probe failed: %v\n%s", err, output)
			}
			if info, err := os.Stat(defs); err != nil || !info.IsDir() {
				t.Fatalf("CLI ignored configured definition root: %v", err)
			}
			if _, err := os.Stat(filepath.Join(base, "state", "agents")); !os.IsNotExist(err) {
				t.Fatal("CLI also opened an implicit registry")
			}
		})
	}
}

func TestCLIPathProbeHelper(t *testing.T) {
	if os.Getenv("BT_TEST_CLI_PATH_PROBE") == "" {
		return
	}
	os.Args = []string{"bt-agent-cli", "list"}
	main()
	os.Exit(0)
}
