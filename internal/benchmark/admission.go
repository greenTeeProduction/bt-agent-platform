package benchmark

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// Live benchmarks execute real model operations. Capabilities requiring external
// services or writes need a dedicated isolated task fixture; lacking one is an
// explicit unsupported result, never an action stub that pretends success.
func benchmarkAdmission(kind, name string) error {
	if kind == "HumanApprovalGate" || kind == "PersistentMemSequence" || kind == "ClaudeErrorHandler" || kind == "PlannerNode" {
		return unsupportedNode(kind, name)
	}
	if kind == "Action" {
		switch name {
		case "AnalyzeTask", "AssignComplexity", "GeneratePlan", "ExecutePlan", "ExecLLMCall", "ExecRefine", "ValidateInput", "ValidateOutput", "MarkSuccessful", "SelfCorrect", "EscalateToDeepSeek":
			return nil
		}
		return unsupportedNode(kind, name)
	}
	if kind == "ChainAction" {
		chain, _, _ := strings.Cut(name, ":")
		switch chain {
		case "llm_call", "conversation", "structured_output", "map_reduce", "refine":
			return nil
		}
		return unsupportedNode(kind, name)
	}
	return nil
}

func unsupportedNode(kind, name string) error {
	return &reliability.ExecutionStoppedError{Outcome: "aborted", Err: &unsupportedCapability{kind: kind, name: name}}
}

type unsupportedCapability struct{ kind, name string }

func (e *unsupportedCapability) Error() string {
	return fmt.Sprintf("live benchmark needs an isolated capability fixture for %s %q", e.kind, e.name)
}

func benchmarkOutcome(bb *engine.Blackboard) string {
	var missing *unsupportedCapability
	if errors.As(bb.ExecutionError(), &missing) {
		return "benchmark_unsupported"
	}
	return bb.Outcome
}
