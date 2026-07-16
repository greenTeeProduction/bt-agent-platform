// Compiled-GOAP leaf nodes (ADR-133 Phase 3). The plan→BT compiler
// (goap.CompilePlanToTree) emits precondition guards and effect writes as
// name-parameterized leaves, following the same name-encoding convention as
// ChainAction's "llm_call:<prompt>":
//
//	Condition "GoapStateMatches:has_analysis=true,task_type=build"
//	Action    "ApplyGoapEffects:has_resources=true"
//
// Both operate on ChainState["goap_world_state"], the same world-state map
// the dynamic GOAP nodes (PlanGoapActions/ExecuteGoapStep) maintain, so a
// compiled plan path and its replan fallback share one source of truth.
package engine

import (
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/goap"
	btcore "github.com/rvitorper/go-bt/core"
)

const (
	goapStateCondPrefix    = "GoapStateMatches:"
	goapEffectsActPrefix   = "ApplyGoapEffects:"
	goapWorldStateChainKey = "goap_world_state"
)

// isCompiledGoapCondition reports whether name is a parameterized world-state
// guard emitted by the plan compiler.
func isCompiledGoapCondition(name string) bool {
	return strings.HasPrefix(name, goapStateCondPrefix) && name != goapStateCondPrefix
}

// isCompiledGoapAction reports whether name is a parameterized effect write
// emitted by the plan compiler.
func isCompiledGoapAction(name string) bool {
	return strings.HasPrefix(name, goapEffectsActPrefix) && name != goapEffectsActPrefix
}

// compiledGoapConditionFor returns the guard implementation for a
// GoapStateMatches name, or nil when the name is not a compiled guard.
// The guard is satisfied when every encoded key=value pair matches the
// blackboard's GOAP world state; a missing key fails the guard.
func compiledGoapConditionFor(name string) ConditionFunc {
	if !isCompiledGoapCondition(name) {
		return nil
	}
	want := parseGoapPairs(strings.TrimPrefix(name, goapStateCondPrefix))
	return func(b *Blackboard) bool {
		if len(want) == 0 {
			return false // malformed spec must not silently pass
		}
		ws := goapWorldStateFrom(b)
		if ws == nil {
			return false
		}
		for k, v := range want {
			have, ok := ws[k]
			if !ok || !goapValuesEqual(have, v) {
				return false
			}
		}
		return true
	}
}

// compiledGoapActionFor returns the effect-write implementation for an
// ApplyGoapEffects name, or nil when the name is not a compiled effect node.
// Effects are merged into ChainState["goap_world_state"] (created on first
// write), mirroring what ExecuteGoapStep does after a dynamic step.
func compiledGoapActionFor(name string) ActionFunc {
	if !isCompiledGoapAction(name) {
		return nil
	}
	effects := parseGoapPairs(strings.TrimPrefix(name, goapEffectsActPrefix))
	return func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if len(effects) == 0 {
			return -1 // malformed spec: fail loudly instead of no-op success
		}
		if b.ChainState == nil {
			b.ChainState = make(map[string]interface{})
		}
		ws := goapWorldStateFrom(b)
		if ws == nil {
			ws = make(goap.WorldState)
		}
		for k, v := range effects {
			ws[k] = v
		}
		b.ChainState[goapWorldStateChainKey] = ws
		return 1
	}
}

// goapWorldStateFrom reads the GOAP world state off the blackboard,
// tolerating both the typed form (set by SetupGoapTools / ApplyGoapEffects)
// and the plain-map form that survives a JSON roundtrip.
func goapWorldStateFrom(b *Blackboard) goap.WorldState {
	if b == nil || b.ChainState == nil {
		return nil
	}
	switch v := b.ChainState[goapWorldStateChainKey].(type) {
	case goap.WorldState:
		return v
	case map[string]interface{}:
		return goap.WorldState(v)
	default:
		return nil
	}
}

// parseGoapPairs decodes "k=v,k2=v2" into typed values: booleans, numbers,
// else strings. Malformed fragments (no "=") are skipped.
func parseGoapPairs(spec string) map[string]interface{} {
	pairs := make(map[string]interface{})
	for _, frag := range strings.Split(spec, ",") {
		frag = strings.TrimSpace(frag)
		if frag == "" {
			continue
		}
		kv := strings.SplitN(frag, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		if key == "" {
			continue
		}
		pairs[key] = parseGoapValue(strings.TrimSpace(kv[1]))
	}
	return pairs
}

func parseGoapValue(raw string) interface{} {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	return raw
}

// goapValuesEqual compares world-state values with numeric tolerance:
// ints and float64s that JSON/parsing produce for the same number compare
// equal, everything else falls back to ==.
func goapValuesEqual(a, b interface{}) bool {
	if af, aok := asFloat(a); aok {
		if bf, bok := asFloat(b); bok {
			return af == bf
		}
		return false
	}
	return a == b
}

func asFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
