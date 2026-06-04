package metrics

import (
	"strconv"
	"time"
)

// BT node and block execution metrics (Prometheus-exported via writePrometheusMetrics).

var (
	nodeTicksTotal    = NewLabeledCounter()
	nodeErrorsTotal   = NewLabeledCounter()
	nodeDurationHist  = NewLabeledHistogram([]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 30000, 120000, 300000})
	blockOpsTotal     = NewLabeledCounter()
	blockErrorsTotal  = NewLabeledCounter()
	blockDurationHist = NewLabeledHistogram([]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000, 30000})
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

// ObserveBlockCompose records compose timing.
func ObserveBlockCompose(blockCount int, err error, start time.Time) {
	status := "ok"
	if err != nil {
		status = "error"
	}
	RecordBlockOp("compose", strconv.Itoa(blockCount), status, time.Since(start).Milliseconds())
}
