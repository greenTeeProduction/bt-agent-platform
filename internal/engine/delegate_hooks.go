package engine

// DelegateToTreeFn runs a task through another behavior tree by id (wired from cmd/bt-agent).
var DelegateToTreeFn func(treeID string, bb *Blackboard) (string, error)

// AgentMemoryBaseDir is the root directory for per-agent memory stores (set from main).
var AgentMemoryBaseDir string
