package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/config"
)

func TestAssistantOwnersUseConfiguredDefinitionsAndLogs(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	for _, key := range []string{"BT_AGENT_HOME", "BT_HOME", "BT_AGENT_DEFS_DIR", "BT_LOG_DIR", "BT_CONFIG_FILE", "BT_DOTENV_FILE"} {
		t.Setenv(key, "")
	}
	defs, logs := filepath.Join(base, "state", "definitions"), filepath.Join(base, "diagnostics")
	path := filepath.Join(base, "config.json")
	if err := os.WriteFile(path, []byte(`{"agent_defs_dir":"`+defs+`","log_dir":"`+logs+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BT_CONFIG_FILE", path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	restore, err := cfg.ConfigureRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	output, errors := &bytes.Buffer{}, &bytes.Buffer{}
	a := newApp(strings.NewReader(""), output, errors)
	if a.registry == nil {
		t.Fatalf("registry initialization failed: %s", errors)
	}
	if _, err := a.registry.Create(agent.Definition{Name: "path-probe", Tree: "agent:ResearchAgent"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(defs)
	if err != nil || len(entries) == 0 {
		t.Fatal("assistant ignored configured definitions")
	}
	if code := a.run([]string{"logs", "path-probe"}); code != 0 || !strings.Contains(output.String(), filepath.Join(logs, "bt.log")) {
		t.Fatalf("assistant reported wrong log owner: %d %s %s", code, output, errors)
	}
}
