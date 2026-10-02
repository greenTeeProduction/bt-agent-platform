package agentexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/dashboard"
)

func TestRunDepsReportsBlackboardInitializationFailure(t *testing.T) {
	base := isolateRunDepsEnv(t)
	if err := os.WriteFile(filepath.Join(base, "blackboard"), []byte("blocked fixture root"), 0600); err != nil {
		t.Fatal(err)
	}
	deps, err := NewRunDeps()
	if deps != nil || err == nil || !strings.Contains(err.Error(), "blackboard") {
		t.Fatalf("failed persistent owner accepted: deps=%p err=%v", deps, err)
	}
}

func TestLoadedDefinitionHomeOwnsBlackboardBeforeFirstRun(t *testing.T) {
	base := isolateRunDepsEnv(t)
	t.Setenv("BT_AGENT_HOME", "")
	home := filepath.Join(base, "loaded-owner")
	path := filepath.Join(base, "config.json")
	if err := os.WriteFile(path, []byte(`{"agent_defs_dir":"`+filepath.Join(home, "agents")+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BT_CONFIG_FILE", path)
	deps, err := NewRunDeps()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	manager, err := deps.BoardManager()
	if err != nil {
		t.Fatal(err)
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "configured-owner"}
	if err := manager.Set(scope, "proof", "loaded owner retained", "", "text"); err != nil {
		t.Fatal(err)
	}
	fresh, err := blackboard.NewPersistentManager(filepath.Join(home, "blackboard"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := fresh.Get(scope, "proof")
	if err != nil || entry.Value != "loaded owner retained" {
		t.Fatalf("configured blackboard root redirected: %+v %v", entry, err)
	}
}

func TestPipelineRejectsMissingOrFailedBlackboardOwnerBeforeSteps(t *testing.T) {
	base := isolateRunDepsEnv(t)
	if err := os.WriteFile(filepath.Join(base, "blackboard"), []byte("blocked fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, deps := range []*agent.RunDeps{nil, {}} {
		result, err := RunPipelineWithID(context.Background(), deps, dashboard.Pipeline{Name: "empty fixture"}, "fixture input", "fixture")
		if result != nil || err == nil {
			t.Fatalf("workflow ignored unavailable owner: %+v %v", result, err)
		}
	}
}
