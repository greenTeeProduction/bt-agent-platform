package dashboard

import (
	"strconv"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// BT node and block execution metrics (Prometheus-exported via writePrometheusMetrics).

// Wire the engine's metrics hooks — engine cannot import dashboard
// (cycle via startup), so recording is injected here.
func init() {
	engine.RecordNodeTickFn = RecordNodeTick
	engine.RecordBlockFitnessFn = RecordBlockFitness
}

var (
	nodeTicksTotal    = NewLabeledCounter()
	nodeErrorsTotal   = NewLabeledCounter()
	nodeDurationHist  = NewLabeledHistogram([]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 30000, 120000, 300000})
	blockOpsTotal     = NewLabeledCounter()
	blockErrorsTotal  = NewLabeledCounter()
	blockDurationHist = NewLabeledHistogram([]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000, 30000})
	blockFitnessGauge = NewLabeledGauge()
)

// RecordNodeTick records one behavior-tree node tick.
func RecordNodeTick(nodeType, nodeName, parentName, blockID, status string, durationMs int64) {
	labels := map[string]string{
		"type":   nodeType,
		"name":   sanitizeLabel(nodeName),
		"parent": sanitizeLabel(parentName),
		"status": status,
	}
	if blockID != "" {
		labels["block_id"] = sanitizeLabel(blockID)
	}
	nodeTicksTotal.Inc(labels)

	durLabels := map[string]string{"type": nodeType, "name": sanitizeLabel(nodeName)}
	if blockID != "" {
		durLabels["block_id"] = sanitizeLabel(blockID)
	}
	nodeDurationHist.Observe(float64(durationMs), durLabels)

	if status == "failure" {
		errLabels := map[string]string{"type": nodeType, "name": sanitizeLabel(nodeName)}
		if blockID != "" {
			errLabels["block_id"] = sanitizeLabel(blockID)
		}
		nodeErrorsTotal.Inc(errLabels)
	}
}

// RecordBlockOp records a blocks-package operation (expand, compose, resolve).
func RecordBlockOp(operation, blockID, status string, durationMs int64) {
	labels := map[string]string{
		"operation": operation,
		"block_id":  sanitizeLabel(blockID),
		"status":    status,
	}
	blockOpsTotal.Inc(labels)
	blockDurationHist.Observe(float64(durationMs), map[string]string{
		"operation": operation,
		"block_id":  sanitizeLabel(blockID),
	})
	if status == "error" {
		blockErrorsTotal.Inc(labels)
	}
}

func sanitizeLabel(s string) string {
	if s == "" {
		return "_"
	}
	if len(s) > 64 {
		return s[:64]
	}
	return s
}

// NodeMetricsSnapshot returns node tick counters for JSON/debug export.
func NodeMetricsSnapshot() map[string]uint64 {
	return nodeTicksTotal.Snapshot()
}

// BlockMetricsSnapshot returns block op counters for JSON/debug export.
func BlockMetricsSnapshot() map[string]uint64 {
	return blockOpsTotal.Snapshot()
}

// RecordBlockFitness sets bt_block_fitness_score for a block/agent pair (0-100).
func RecordBlockFitness(blockID, agent string, score float64) {
	if blockID == "" {
		return
	}
	if agent == "" {
		agent = "default"
	}
	blockFitnessGauge.Set(int64(score), map[string]string{
		"block_id": sanitizeLabel(blockID),
		"agent":    sanitizeLabel(agent),
	})
}

// BlockFitnessSnapshot returns current fitness gauge values.
func BlockFitnessSnapshot() map[string]int64 {
	return blockFitnessGauge.Snapshot()
}

// BlockFitnessRanking returns block_ids sorted by fitness descending.
func BlockFitnessRanking() []string {
	snap := blockFitnessGauge.Snapshot()
	type pair struct {
		id    string
		score int64
	}
	pairs := make([]pair, 0, len(snap))
	for key, val := range snap {
		labels := parseLabelKey(key)
		id := labels["block_id"]
		if id == "" || id == "_" {
			continue
		}
		pairs = append(pairs, pair{id: id, score: val})
	}
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].score > pairs[i].score {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	out := make([]string, len(pairs))
	for i, pr := range pairs {
		out[i] = pr.id
	}
	return out
}

// ObserveBlockCompose records compose timing.
func ObserveBlockCompose(blockCount int, err error, start time.Time) {
	status := "ok"
	if err != nil {
		status = "error"
	}
	RecordBlockOp("compose", strconv.Itoa(blockCount), status, time.Since(start).Milliseconds())
}
