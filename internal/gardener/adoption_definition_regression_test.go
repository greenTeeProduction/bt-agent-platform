package gardener

import (
	"sync"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestAdoptionFixturesBuild(t *testing.T) {
	for name, tree := range map[string]*evolution.SerializableNode{"ordinary": gateDisabledTestTree(), "ordering": dtOrderingTree(), "elite": eliteSeedTree()} {
		if _, err := engine.BuildAndValidate(tree, &engine.Blackboard{Sandbox: true, ChainState: make(map[string]any)}); err != nil {
			t.Errorf("%s fixture: %v", name, err)
		}
	}
}

var fixtureLeavesOnce sync.Once

func registerGardenerFixtureLeaves() {
	fixtureLeavesOnce.Do(func() {
		if engine.GetAction("Step") == nil {
			engine.RegisterAction("Step", func(*btcore.BTContext[engine.Blackboard]) int { return 1 })
		}
		for _, name := range []string{"CondA", "CondB", "CondC"} {
			if engine.GetCondition(name) == nil {
				engine.RegisterCondition(name, func(*engine.Blackboard) bool { return true })
			}
		}
	})
}
