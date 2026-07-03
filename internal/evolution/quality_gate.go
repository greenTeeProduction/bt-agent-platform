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
	failCounts        map[string]int
	mu                sync.Mutex
}

// globalGateKey is the failure-streak key used by the legacy tree-agnostic
// Validate/IsDisabled/FailCount methods.
const globalGateKey = ""

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
//
// Failure streaks recorded here are tree-agnostic (global key); prefer
// ValidateFor when gating multiple trees through one gate instance.
func (q *QualityGate) Validate(preComposite, postComposite float64) GateResult {
	return q.ValidateFor(globalGateKey, preComposite, postComposite)
}

// ValidateFor is Validate with a per-tree failure streak: consecutive failures
// on one tree disable the gate for that tree only, never fleet-wide.
func (q *QualityGate) ValidateFor(treeKey string, preComposite, postComposite float64) GateResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.failCounts == nil {
		q.failCounts = make(map[string]int)
	}

	// Fitness floor — reject if composite falls below minimum
	if postComposite < q.MinComposite {
		q.failCounts[treeKey]++
		return GateRejected
	}

	// Regression threshold — rollback if fitness drops by more than MaxRegressionRate.
	// Only triggers when preComposite > 0 (new trees have Composite=0 and can't regress).
	if preComposite > 0 && postComposite < preComposite*(1-q.MaxRegressionRate) {
		q.failCounts[treeKey]++
		return GateRollback
	}

	// Passed — reset fail counter
	q.failCounts[treeKey] = 0
	return GateAccepted
}

// IsDisabled returns true if consecutive failures have exceeded the threshold.
//
// NOTE (A2 fail-closed semantics): once disabled, nothing in production
// re-enables the gate for the process lifetime (no ResetFailCount caller).
// Disabled means evolution is PAUSED for affected trees until process restart —
// the gardener skips/rolls back all mutations for trees whose gate is disabled,
// it never applies them ungated. Deliberate, to avoid flapping.
func (q *QualityGate) IsDisabled() bool {
	return q.IsDisabledFor(globalGateKey)
}

// IsDisabledFor reports whether treeKey's consecutive failures exceeded the
// threshold. A global-key streak (legacy Validate) acts as a kill switch that
// disables every tree; per-tree streaks disable only their own tree.
// See IsDisabled's A2 note for disable semantics.
func (q *QualityGate) IsDisabledFor(treeKey string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.ConsecutiveFails <= 0 {
		return false
	}
	return q.failCounts[treeKey] >= q.ConsecutiveFails ||
		q.failCounts[globalGateKey] >= q.ConsecutiveFails
}

// FailCount returns the current consecutive failure count.
func (q *QualityGate) FailCount() int {
	return q.FailCountFor(globalGateKey)
}

// FailCountFor returns treeKey's current consecutive failure count.
func (q *QualityGate) FailCountFor(treeKey string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.failCounts[treeKey]
}

// ResetFailCount resets the consecutive failure counter.
func (q *QualityGate) ResetFailCount() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failCounts = nil
}

// SnapshotTree saves a copy of the tree to the snapshot directory atomically.
func SnapshotTree(tree *SerializableNode, treeName, snapshotDir string) (string, error) {
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}

	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal snapshot: %w", err)
	}

	path := filepath.Join(snapshotDir, fmt.Sprintf("snapshot_%s.json", treeName))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
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
