package gardener

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// These are orchestration unit tests: they exercise proposal, gate, persistence
// and rollback decisions with a structural admission result. No LLM is mocked,
// no model output is generated and no benchmark evidence is claimed. Real-model
// qualification lives in benchmark/live_governance_test.go; integration tests
// construct NewGardener directly, which retains the live validator.
func newOrchestrationTestGardener(t *testing.T, cfg Config) *Gardener {
	t.Helper()
	g := NewGardener(cfg)
	g.candidateAcceptance = func(base, candidate *evolution.SerializableNode, _ benchmark.Suite, _ llm.LLM) bool {
		if !evolution.PreservesGovernance(base, candidate) {
			return false
		}
		_, err := engine.BuildAndValidate(candidate, &engine.Blackboard{})
		return err == nil
	}
	return g
}
