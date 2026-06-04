package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// TreeExecutor defines the interface for building and running behavior trees.
// This extracts the two most-connected functions (BuildTree: 111 edges,
// RunTask: 73 edges) behind an interface boundary so callers can inject
// alternative implementations for testing or different execution strategies.
type TreeExecutor interface {
	// BuildTree converts a SerializableNode tree definition into a runnable go-bt Command.
	// Invalid trees produce a failing command instead of silently executing an unsafe structure.
	BuildTree(serTree *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard]

	// RunTask executes a tree to completion with a 1000-tick safety limit.
	// Returns the Blackboard result after execution.
	RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string
}

// DefaultExecutor is the standard TreeExecutor implementation used in production.
// It delegates to the package-level BuildTree and RunTask functions.
type DefaultExecutor struct{}

// BuildTree delegates to the package-level BuildTree.
func (e *DefaultExecutor) BuildTree(serTree *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	return BuildTree(serTree, bb)
}

// RunTask delegates to the package-level RunTask.
func (e *DefaultExecutor) RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string {
	return RunTask(bb, tree)
}

// globalExecutor is the singleton TreeExecutor used by package-level convenience functions.
// Tests can override it via SetExecutor.
var globalExecutor TreeExecutor = &DefaultExecutor{}

// SetExecutor replaces the global TreeExecutor. Used for test injection.
// Callers outside tests should not call this.
func SetExecutor(e TreeExecutor) {
	globalExecutor = e
}

// GetExecutor returns the current global TreeExecutor.
func GetExecutor() TreeExecutor {
	return globalExecutor
}
