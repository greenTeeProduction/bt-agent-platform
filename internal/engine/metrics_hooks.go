package engine

// Metrics hooks — implemented by internal/dashboard (bt_nodes.go) and wired in
// its init(). Engine stays free of the dashboard import to avoid the cycle
// engine -> dashboard -> startup -> engine. Nil hooks mean "don't record".

// RecordNodeTickFn records one behavior-tree node tick (type, name, parent, block, status, duration ms).
var RecordNodeTickFn func(nodeType, nodeName, parentName, blockID, status string, durationMs int64)

// RecordBlockFitnessFn sets the fitness score for a block/agent pair.
var RecordBlockFitnessFn func(blockID, agent string, score float64)
