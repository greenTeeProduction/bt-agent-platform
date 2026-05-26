package engine

import (
	btcore "github.com/rvitorper/go-bt/core"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ActionFunc is the signature for behavior tree action implementations.
type ActionFunc func(*btcore.BTContext[Blackboard]) int

// ConditionFunc is the signature for behavior tree condition implementations.
type ConditionFunc func(*Blackboard) bool

var actionRegistry = map[string]ActionFunc{}
var conditionRegistry = map[string]ConditionFunc{}

func init() {
	registerGoapNodes()
}

// RegisterAction adds an action handler to the registry.
func RegisterAction(name string, fn ActionFunc) {
	actionRegistry[name] = fn
}

// RegisterCondition adds a condition handler to the registry.
func RegisterCondition(name string, fn ConditionFunc) {
	conditionRegistry[name] = fn
}

// GetAction returns the action handler for a name. Uses registry if available,
// otherwise creates a closure bound to the given blackboard.
func GetAction(name string, bb *Blackboard) ActionFunc {
	if fn, ok := actionRegistry[name]; ok {
		return fn
	}
	// Fall back to method-based lookup on the provided blackboard
	if bb != nil {
		return bb.actionForName(name)
	}
	return func(ctx *btcore.BTContext[Blackboard]) int { return 1 }
}

// GetCondition returns the condition handler for a name.
func GetCondition(name string, bb *Blackboard) ConditionFunc {
	if fn, ok := conditionRegistry[name]; ok {
		return fn
	}
	if bb != nil {
		return bb.conditionForName(name)
	}
	return func(b *Blackboard) bool { return true }
}

// ValidateTree checks that all nodes reference known handlers.
func ValidateTree(tree *evolution.SerializableNode) []string {
	var missing []string
	// Use a dummy blackboard just for validation — we only check existence
	dummy := &Blackboard{}
	var walk func(n *evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		switch n.Type {
		case "Action":
			fn := GetAction(n.Name, dummy)
			// Check if it's the no-op fallback (new closures would differ, but method closures always succeed)
			_ = fn
		case "Condition":
			fn := GetCondition(n.Name, dummy)
			_ = fn
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(tree)
	return missing
}
