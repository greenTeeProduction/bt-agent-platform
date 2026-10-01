// Legacy compiled-GOAP guards remain readable. Unconditional effect assertions
// fail explicitly; regenerated trees use GoapStep observations instead.
package engine

import (
	"encoding/json"
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
// Legacy effect assertions are recognized and rejected. A GoapStep must
// observe an execution result before the world state can advance.
func compiledGoapActionFor(name string) ActionFunc {
	if !isCompiledGoapAction(name) {
		return nil
	}
	return func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Outcome = "failure"
		ctx.Blackboard.Result = "Legacy GOAP effect assertion is unverified; regenerate with an observed GoapStep gate"
		return -1
	}
}

// goapWorldStateFrom reads the GOAP world state off the blackboard,
// tolerating both the typed form (set by SetupGoapTools / GoapStep)
// and the plain-map form that survives a JSON roundtrip.
func goapWorldStateFrom(b *Blackboard) goap.WorldState {
	if b == nil || b.ChainState == nil {
		return nil
	}
	switch v := b.ChainState[goapWorldStateChainKey].(type) {
	case goap.WorldState:
		return v
	case map[string]any:
		return goap.WorldState(v)
	default:
		return nil
	}
}

// parseGoapPairs decodes "k=v,k2=v2" into typed values: booleans, numbers,
// else strings. Malformed or duplicate keys reject the complete guard.
func parseGoapPairs(spec string) map[string]any {
	pairs := make(map[string]any)
	for frag := range strings.SplitSeq(spec, ",") {
		frag = strings.TrimSpace(frag)
		if frag == "" {
			continue
		}
		kv := strings.SplitN(frag, "=", 2)
		if len(kv) != 2 {
			return nil
		}
		key := strings.TrimSpace(kv[0])
		if _, duplicate := pairs[key]; key == "" || duplicate {
			return nil
		}
		pairs[key] = parseGoapValue(strings.TrimSpace(kv[1]))
	}
	return pairs
}

func parseGoapValue(raw string) any {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err == nil && json.Valid([]byte(raw)) {
		if number, ok := value.(json.Number); ok && goap.ValuesEqual(number, number) {
			return number
		}
	}
	return raw
}

// goapValuesEqual compares typed scalar facts with exact numeric equivalence.
func goapValuesEqual(a, b any) bool { return goap.ValuesEqual(a, b) }
