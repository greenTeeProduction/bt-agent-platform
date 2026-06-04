package engine

import (
	"strings"

	"github.com/nico/go-bt-evolve/internal/reflection"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerReliabilityNodes()
}

func registerReliabilityNodes() {
	RegisterCondition("CircuitAllows", func(b *Blackboard) bool {
		name, _ := b.ChainState["active_cb"].(string)
		if name == "" {
			return true
		}
		// Allow when no breaker exists yet or circuit is closed/half-open test
		cb := circuitBreakerFor(name, 3, 0)
		return cb.Allow()
	})

	RegisterCondition("CircuitOpen", func(b *Blackboard) bool {
		cat, _ := b.ChainState["last_error_category"].(string)
		return cat == reliability.ErrCatResourceExhausted.String() ||
			strings.Contains(strings.ToLower(b.Result), "circuit open")
	})

	RegisterCondition("LastErrorIsTimeout", func(b *Blackboard) bool {
		cat, _ := b.ChainState["last_error_category"].(string)
		return cat == reliability.ErrCatTimeout.String()
	})

	RegisterCondition("LastErrorIsTransient", func(b *Blackboard) bool {
		if b.ChainState == nil {
			return false
		}
		cat, _ := b.ChainState["last_error_category"].(string)
		return cat == reliability.ErrCatNetwork.String() ||
			cat == reliability.ErrCatTimeout.String() ||
			cat == reliability.ErrCatLLM.String()
	})

	RegisterCondition("LastErrorIsValidation", func(b *Blackboard) bool {
		cat, _ := b.ChainState["last_error_category"].(string)
		return cat == reliability.ErrCatValidation.String() ||
			cat == reliability.ErrCatAuth.String()
	})

	RegisterAction("SetActiveCircuit", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = make(map[string]any)
		}
		// Name from node is not available here; callers set via ChainState before tick
		if name, ok := b.ChainState["pending_cb"].(string); ok {
			b.ChainState["active_cb"] = name
			delete(b.ChainState, "pending_cb")
		}
		return 1
	})

	RegisterAction("HandleTimeoutError", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		b.Outcome = string(reflection.Partial)
		if b.Result == "" {
			b.Result = "Execution timed out; partial result preserved. Retry with a narrower task or increase timeout."
		} else {
			b.Result = "Timeout after partial progress: " + b.Result
		}
		recordNodeFailure(b, "HandleTimeoutError", reliability.NewCategorizedError(reliability.ErrCatTimeout, nil))
		return 1 // graceful degrade — selector continues
	})

	RegisterAction("HandleTransientError", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		b.Outcome = string(reflection.Partial)
		if b.Result == "" {
			b.Result = "Transient failure (network/LLM). Will retry on next tick or escalation path."
		}
		return 1
	})

	RegisterAction("HandleValidationError", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		b.Outcome = "failure"
		if b.Result == "" {
			b.Result = "Validation failed — fix inputs before retrying."
		}
		return -1
	})

	RegisterAction("HandleCircuitOpen", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		b.Outcome = string(reflection.Partial)
		if b.Result == "" {
			b.Result = "Circuit breaker open — too many recent failures. Cooling down before retry."
		}
		return 1
	})

	RegisterAction("ClearNodeError", func(ctx *btcore.BTContext[Blackboard]) int {
		recordNodeSuccess(ctx.Blackboard, "ClearNodeError")
		return 1
	})
}
