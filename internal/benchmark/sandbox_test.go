package benchmark

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// TestRunSuiteIsSandboxed verifies benchmark execution never invokes real
// action implementations. The gardener validates mutations by ticking trees
// through RunSuite; production trees contain actions that shell out (nlm, git,
// claude) and a benchmark run must not trigger any of them.
func TestRunSuiteIsSandboxed(t *testing.T) {
	executed := false
	engine.RegisterAction("BenchmarkSandboxProbe", func(_ *btcore.BTContext[engine.Blackboard]) int {
		executed = true
		return 1
	})

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BenchmarkSandboxProbe"},
		},
	}
	suite := Suite{
		Name:  "sandbox_probe",
		Tasks: []TaskCase{{Task: "run the probe", ShouldSucceed: true}},
	}

	RunSuite(tree, suite, DefaultMock())

	if executed {
		t.Fatal("RunSuite executed a real registered action — benchmark must be sandboxed")
	}
}
