package agentexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestStartupReflectionsSurviveConfigurationRemoval(t *testing.T) {
	base := isolateRunDepsEnv(t)
	t.Setenv("BT_REFLECTIONS_DIR", "")
	root := filepath.Join(base, "configured-reflections")
	defs, history := filepath.Join(base, "configured-definitions"), filepath.Join(base, "configured-history")
	t.Setenv("BT_HISTORY_DIR", "")
	path := filepath.Join(base, "config.json")
	if err := os.WriteFile(path, []byte(`{"reflections_dir":"`+root+`","agent_defs_dir":"`+defs+`","history_dir":"`+history+`"}`), 0600); err != nil {
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
	previous := generatedTreeDir
	generatedTreeDir = ""
	t.Cleanup(func() { generatedTreeDir = previous })
	tree := &evolution.SerializableNode{Type: "Action", Name: "onceStep"}
	if _, err := evolution.SaveNamedTree(root, "startup-root-probe", tree); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, err := ReflectionsPath(); err != nil || got != root {
		t.Fatalf("startup root changed after config removal: %q %v", got, err)
	}
	if got := ResolveGeneratedTree("startup-root-probe"); got == nil || got.Name != tree.Name {
		t.Fatal("persisted generated tree stopped resolving after config removal")
	}
	deps, err := NewRunDeps()
	if err != nil {
		t.Fatal(err)
	}
	if deps.RefStore.Dir() != root || deps.TreeStore.Dir() != root {
		t.Fatal("run deps redirected reflection owners after config removal")
	}
	if _, err := deps.Registry.Create(agent.Definition{Name: "pinned-owner-probe", Tree: "agent:ResearchAgent"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.History.Record(agent.RunRecord{AgentName: "pinned-owner-probe", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(defs); err != nil || len(entries) == 0 {
		t.Fatal("run deps redirected definitions after config removal")
	}
	if _, err := os.Stat(filepath.Join(history, "pinned-owner-probe.jsonl")); err != nil {
		t.Fatal("run deps redirected history after config removal")
	}
}

func TestNewRunDepsUsesLoadedDefinitionAndHistoryPaths(t *testing.T) {
	base := isolateRunDepsEnv(t)
	t.Setenv("BT_AGENT_HOME", "")
	t.Setenv("BT_HISTORY_DIR", "")
	defs, history := filepath.Join(base, "state", "custom-definitions"), filepath.Join(base, "custom-history")
	path := filepath.Join(base, "config.json")
	if err := os.WriteFile(path, []byte(`{"agent_defs_dir":"`+defs+`","history_dir":"`+history+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BT_CONFIG_FILE", path)
	deps, err := NewRunDeps()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Registry.Create(agent.Definition{Name: "path-probe", Tree: "agent:ResearchAgent"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.History.Record(agent.RunRecord{AgentName: "path-probe", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(defs)
	if err != nil || len(entries) == 0 {
		t.Fatalf("configured definitions missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(history, "path-probe.jsonl")); err != nil {
		t.Fatalf("configured history missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "state", "agents")); !os.IsNotExist(err) {
		t.Fatal("runner also created default registry despite configured directory")
	}
}

// isolateRunDepsEnv points every path NewRunDeps touches (config file,
// dotenv, and the shared ~/.go-bt-evolve / ~/.go-bt-reflections roots) at a
// fresh temp dir, so these characterization tests never read or write the
// developer's real BT_CONFIG_FILE (which may hold a live API key).
func isolateRunDepsEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv("BT_AGENT_HOME", tmp)
	t.Setenv("BT_REFLECTIONS_DIR", filepath.Join(tmp, "reflections"))
	t.Setenv("BT_HOME", "")
	t.Setenv("BT_AGENT_DEFS_DIR", "")
	t.Setenv("BT_CONFIG_FILE", "")
	t.Setenv("BT_DOTENV_FILE", "")
	return tmp
}

func TestNewRunDeps_PopulatesAllFields(t *testing.T) {
	isolateRunDepsEnv(t)

	deps, err := NewRunDeps()
	if err != nil {
		t.Fatalf("NewRunDeps() error = %v", err)
	}
	if deps == nil {
		t.Fatal("NewRunDeps() returned nil deps with nil error")
	}

	fields := map[string]bool{
		"Registry":           deps.Registry == nil,
		"History":            deps.History == nil,
		"LLM":                deps.LLM == nil,
		"RefStore":           deps.RefStore == nil,
		"TreeStore":          deps.TreeStore == nil,
		"ResolveTree":        deps.ResolveTree == nil,
		"ResolveTreeForUser": deps.ResolveTreeForUser == nil,
	}
	for name, isNil := range fields {
		if isNil {
			t.Errorf("RunDeps.%s is nil, want populated", name)
		}
	}
}

func TestNewRunDeps_SharedReflectionsRoot(t *testing.T) {
	isolateRunDepsEnv(t)

	deps, err := NewRunDeps()
	if err != nil {
		t.Fatalf("NewRunDeps() error = %v", err)
	}

	wantRoot, err := ReflectionsPath()
	if err != nil {
		t.Fatalf("ReflectionsPath() error = %v", err)
	}

	if got := deps.RefStore.Dir(); got != wantRoot {
		t.Errorf("RefStore.Dir() = %q, want %q", got, wantRoot)
	}
	if got := deps.TreeStore.Dir(); got != wantRoot {
		t.Errorf("TreeStore.Dir() = %q, want %q", got, wantRoot)
	}
	if info, err := os.Stat(wantRoot); err != nil || !info.IsDir() {
		t.Errorf("reflections root %q not created as a directory: stat err = %v", wantRoot, err)
	}
}

func TestNewRunDeps_ResolveTree_MatchesDomainsResolver(t *testing.T) {
	isolateRunDepsEnv(t)

	deps, err := NewRunDeps()
	if err != nil {
		t.Fatalf("NewRunDeps() error = %v", err)
	}

	tests := []struct {
		name string
		id   string
	}{
		{name: "empty id", id: ""},
		{name: "known builtin id", id: "godev"},
		{name: "unknown id falls back to default tree", id: "no-such-tree-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deps.ResolveTree(tt.id)
			want := domains.ResolveTreeID(tt.id)
			if (got == nil) != (want == nil) {
				t.Errorf("deps.ResolveTree(%q) nil-ness = %v, domains.ResolveTreeID(%q) nil-ness = %v; wrapper diverged from domains.ResolveTreeID",
					tt.id, got == nil, tt.id, want == nil)
			}
		})
	}
}

func TestNewRunDeps_ResolveTreeForUser_MatchesDomainsResolver(t *testing.T) {
	isolateRunDepsEnv(t)

	deps, err := NewRunDeps()
	if err != nil {
		t.Fatalf("NewRunDeps() error = %v", err)
	}

	tests := []struct {
		name string
		user string
		id   string
	}{
		{name: "empty user falls back to unscoped resolution", user: "", id: "godev"},
		{name: "named user, empty id", user: "alice", id: ""},
		{name: "named user, unknown id", user: "alice", id: "no-such-tree-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deps.ResolveTreeForUser(tt.user, tt.id)
			want := domains.ResolveTreeIDForUser(tt.user, tt.id)
			if (got == nil) != (want == nil) {
				t.Errorf("deps.ResolveTreeForUser(%q, %q) nil-ness = %v, domains.ResolveTreeIDForUser(...) nil-ness = %v; wrapper diverged from domains.ResolveTreeIDForUser",
					tt.user, tt.id, got == nil, want == nil)
			}
		})
	}
}

func TestNewRunDeps_ConfigLoadFailure_FallsBackToZeroConfig(t *testing.T) {
	tmp := isolateRunDepsEnv(t)

	// An unreadable BT_CONFIG_FILE makes config.Load() return an error;
	// NewRunDeps must tolerate it (falls back to &config.Config{}) rather
	// than failing the whole dependency build.
	badConfig := filepath.Join(tmp, "missing-dir", "config.json")
	t.Setenv("BT_CONFIG_FILE", badConfig)

	deps, err := NewRunDeps()
	if err != nil {
		t.Fatalf("NewRunDeps() error = %v, want nil (config load failure should be tolerated)", err)
	}
	if deps.LLM == nil {
		t.Error("RunDeps.LLM is nil after config load failure, want a zero-config default provider")
	}
}

func TestConfiguredReflectionRootReachesStoresAndResolver(t *testing.T) {
	for _, source := range []string{"json", "dotenv", "environment"} {
		t.Run(source, func(t *testing.T) {
			base := isolateRunDepsEnv(t)
			root := filepath.Join(base, "configured-reflections")
			t.Setenv("BT_REFLECTIONS_DIR", "")
			switch source {
			case "json":
				file := filepath.Join(base, "config.json")
				if err := os.WriteFile(file, []byte(`{"reflections_dir":"`+root+`"}`), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("BT_CONFIG_FILE", file)
			case "dotenv":
				if err := os.WriteFile(filepath.Join(base, ".env"), []byte("BT_REFLECTIONS_DIR="+root+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "environment":
				// The explicit environment wins over loaded configuration.
				if err := os.WriteFile(filepath.Join(base, ".env"), []byte("BT_REFLECTIONS_DIR="+filepath.Join(base, "wrong-root")+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("BT_REFLECTIONS_DIR", root)
			}
			deps, err := NewRunDeps()
			if err != nil {
				t.Fatal(err)
			}
			if deps.RefStore.Dir() != root || deps.TreeStore.Dir() != root {
				t.Fatalf("stores bypassed configured root: %q / %q", deps.RefStore.Dir(), deps.TreeStore.Dir())
			}
			const id = "configured-root-probe"
			if _, err := evolution.SaveNamedTree(root, id, &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "ConfiguredRootProbe"}); err != nil {
				t.Fatal(err)
			}
			got := ResolveGeneratedTree(id)
			if got == nil || got.Name != "ConfiguredRootProbe" {
				t.Fatal("resolver did not read the configured store")
			}
		})
	}
}

func TestReflectionsPathPreservesLegacyDefault(t *testing.T) {
	isolateRunDepsEnv(t)
	t.Setenv("BT_REFLECTIONS_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReflectionsPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".go-bt-reflections"); got != want {
		t.Fatalf("legacy root=%q want=%q", got, want)
	}
}
