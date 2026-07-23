package agentexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
)

// isolateRunDepsEnv points every path NewRunDeps touches (config file,
// dotenv, and the shared ~/.go-bt-evolve / ~/.go-bt-reflections roots) at a
// fresh temp dir, so these characterization tests never read or write the
// developer's real BT_CONFIG_FILE (which may hold a live API key).
func isolateRunDepsEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("BT_AGENT_HOME", "")
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
