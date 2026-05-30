package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// GateResult is the outcome of a quality gate validation.
type GateResult int

const (
	GateAccepted GateResult = iota
	GateRejected
	GateRollback
)

func (g GateResult) String() string {
	switch g {
	case GateAccepted:
		return "accepted"
	case GateRejected:
		return "rejected"
	case GateRollback:
		return "rollback"
	default:
		return "unknown"
	}
}

// QualityGate validates mutations against regression thresholds.
// Implements the EvoRepair-inspired pattern: every mutation must pass
// a quality gate; regression triggers automatic rollback.
type QualityGate struct {
	MinComposite      float64 // 0.3 — reject below this floor
	MaxRegressionRate float64 // 0.2 — rollback if fitness drops >20%
	ConsecutiveFails  int     // 5 — auto-disable after N consecutive regressions
	SnapshotDir       string  // backup tree.json before mutation
	failCount         int
	mu                sync.Mutex
}

// NewQualityGate creates a quality gate with sensible defaults.
func NewQualityGate(snapshotDir string) *QualityGate {
	return &QualityGate{
		MinComposite:      0.3,
		MaxRegressionRate: 0.2,
		ConsecutiveFails:  5,
		SnapshotDir:       snapshotDir,
	}
}

// Validate checks pre- and post-mutation composite fitness and returns whether to
// accept, reject, or rollback. Takes float64 composite scores to avoid circular
// imports with the evaluator package.
func (q *QualityGate) Validate(preComposite, postComposite float64) GateResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Fitness floor — reject if composite falls below minimum
	if postComposite < q.MinComposite {
		q.failCount++
		return GateRejected
	}

	// Regression threshold — rollback if fitness drops by more than MaxRegressionRate.
	// Only triggers when preComposite > 0 (new trees have Composite=0 and can't regress).
	if preComposite > 0 && postComposite < preComposite*(1-q.MaxRegressionRate) {
		q.failCount++
		return GateRollback
	}

	// Passed — reset fail counter
	q.failCount = 0
	return GateAccepted
}

// IsDisabled returns true if consecutive failures have exceeded the threshold.
func (q *QualityGate) IsDisabled() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ConsecutiveFails > 0 && q.failCount >= q.ConsecutiveFails
}

// FailCount returns the current consecutive failure count.
func (q *QualityGate) FailCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.failCount
}

// ResetFailCount resets the consecutive failure counter.
func (q *QualityGate) ResetFailCount() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failCount = 0
}

// SnapshotTree saves a copy of the tree to the snapshot directory atomically.
func SnapshotTree(tree *SerializableNode, treeName, snapshotDir string) (string, error) {
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}

	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal snapshot: %w", err)
	}

	path := filepath.Join(snapshotDir, fmt.Sprintf("snapshot_%s.json", treeName))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return "", fmt.Errorf("write snapshot: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("rename snapshot: %w", err)
	}

	return path, nil
}

// RestoreTree loads a snapshot from disk and returns the tree.
func RestoreTree(treeName, snapshotDir string) (*SerializableNode, error) {
	path := filepath.Join(snapshotDir, fmt.Sprintf("snapshot_%s.json", treeName))

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	var tree SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}

	return &tree, nil
}

// CloneTree creates a deep copy of a SerializableNode via JSON round-trip.
func CloneTree(tree *SerializableNode) *SerializableNode {
	if tree == nil {
		return nil
	}
	data, err := json.Marshal(tree)
	if err != nil {
		return nil
	}
	var clone SerializableNode
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil
	}
	return &clone
}
