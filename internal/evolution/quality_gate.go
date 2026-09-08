package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nico/go-bt-evolve/internal/util"
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
	MinComposite      float64 // 30.0 — reject declines below this floor (composite is 0-100)
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
		MinComposite:      30.0,
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

	// Fitness floor — below the floor even small declines are rejected
	// (stricter than the 20% regression tolerance). Improvements below the
	// floor pass: weak trees must be allowed to climb out.
	if postComposite < q.MinComposite && postComposite < preComposite {
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

// Probe returns the gate's verdict on a pre→post composite pair WITHOUT
// recording anything: no failure-streak increment, no streak reset, no
// disabling. It is the read-only counterpart to Validate/ValidateFor, for
// speculative changes a caller may discard — LocalSearcher.RefineGated's
// parameter tuning, for one. A tuning attempt that does not pan out is not a
// regression of the live tree, so it must not push the tree toward
// fail-closed.
//
// The verdict order differs deliberately from Validate: rollback outranks the
// composite floor when the baseline itself was healthy. A tree at or above
// MinComposite that drops more than MaxRegressionRate has a known-good state
// worth restoring (GateRollback), whereas a tree already below the floor has no
// such baseline and is simply refused (GateRejected).
//
// The floor is absolute, unlike Validate's — Probe refuses any post score below
// MinComposite regardless of direction, improvements included. Validate can
// afford the "let weak trees climb out" escape hatch because it gates the
// structural loop, whose alternative to a sub-floor improvement is keeping an
// even weaker tree. Probe's caller (RefineGated) already returns early unless
// post > pre, so a direction-only floor is unreachable by construction and the
// gate check degenerates to an unconditional accept. Judging the tuned score
// against the absolute floor is what makes the gate mean anything there: a
// refinement that improves but leaves the tree below the health floor is
// speculative tuning the caller can simply discard, not a tree worth committing
// to disk.
func (q *QualityGate) Probe(preComposite, postComposite float64) GateResult {
	// Regression from a healthy baseline — there is a known-good state to
	// roll back to.
	if preComposite >= q.MinComposite && postComposite < preComposite*(1-q.MaxRegressionRate) {
		return GateRollback
	}

	// Fitness floor — a post score under the floor is refused whether it
	// climbed there or fell there.
	if postComposite < q.MinComposite {
		return GateRejected
	}

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

// snapshotIndex tracks the ordered revision history for one tree's snapshots,
// oldest first, so a regression discovered several cycles after it was
// introduced can still roll back past just the immediately-preceding cycle.
// Fitness records each revision's composite score when known (via
// SnapshotTreeWithFitness), letting RestoreTreeBeforeRegressionStreak walk
// back past a multi-cycle regression streak to the last known-good peak.
type snapshotIndex struct {
	Revisions []int           `json:"revisions"`
	Fitness   map[int]float64 `json:"fitness,omitempty"`
}

func snapshotIndexPath(treeName, snapshotDir string) string {
	return filepath.Join(snapshotDir, fmt.Sprintf("snapshot_%s.index.json", treeName))
}

func snapshotRevisionPath(treeName, snapshotDir string, revision int) string {
	return filepath.Join(snapshotDir, fmt.Sprintf("snapshot_%s_%d.json", treeName, revision))
}

func loadSnapshotIndex(treeName, snapshotDir string) (snapshotIndex, error) {
	data, err := os.ReadFile(snapshotIndexPath(treeName, snapshotDir))
	if err != nil {
		if os.IsNotExist(err) {
			return snapshotIndex{}, nil
		}
		return snapshotIndex{}, fmt.Errorf("read snapshot index: %w", err)
	}

	var idx snapshotIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return snapshotIndex{}, fmt.Errorf("unmarshal snapshot index: %w", err)
	}
	return idx, nil
}

func saveSnapshotIndex(treeName, snapshotDir string, idx snapshotIndex) error {
	// Snapshots stay deliberately tighter than the 0644/0755 default: the
	// revision files and this index are restricted to the owner.
	return util.SaveJSONAtomicMode(snapshotIndexPath(treeName, snapshotDir), idx, 0o600, 0o700)
}

// SnapshotTree saves a copy of the tree as a new revision in the snapshot
// directory, atomically. Revisions accumulate — earlier snapshots for the
// same treeName are never overwritten — so ListRevisions and
// RestoreTreeRevision can recover any prior cycle's state, not just the one
// immediately before the latest.
func SnapshotTree(tree *SerializableNode, treeName, snapshotDir string) (string, error) {
	return snapshotTree(tree, treeName, snapshotDir, nil)
}

// SnapshotTreeWithFitness is SnapshotTree plus the revision's composite
// fitness score, which RestoreTreeBeforeRegressionStreak needs to identify
// where a regression streak began.
func SnapshotTreeWithFitness(tree *SerializableNode, treeName, snapshotDir string, fitness float64) (string, error) {
	return snapshotTree(tree, treeName, snapshotDir, &fitness)
}

func snapshotTree(tree *SerializableNode, treeName, snapshotDir string, fitness *float64) (string, error) {
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}

	idx, err := loadSnapshotIndex(treeName, snapshotDir)
	if err != nil {
		return "", err
	}

	revision := 1
	if n := len(idx.Revisions); n > 0 {
		revision = idx.Revisions[n-1] + 1
	}

	path := snapshotRevisionPath(treeName, snapshotDir, revision)
	if err := util.SaveJSONAtomicMode(path, tree, 0o600, 0o700); err != nil {
		return "", err
	}

	idx.Revisions = append(idx.Revisions, revision)
	if fitness != nil {
		if idx.Fitness == nil {
			idx.Fitness = make(map[int]float64)
		}
		idx.Fitness[revision] = *fitness
	}
	if err := saveSnapshotIndex(treeName, snapshotDir, idx); err != nil {
		return "", err
	}

	return path, nil
}

// ListRevisions returns treeName's snapshot revisions in the given
// snapshotDir, ordered oldest to newest. An empty result with a nil error
// means the tree has never been snapshotted.
func ListRevisions(treeName, snapshotDir string) ([]int, error) {
	idx, err := loadSnapshotIndex(treeName, snapshotDir)
	if err != nil {
		return nil, err
	}
	return idx.Revisions, nil
}

// RestoreTreeRevision loads a specific snapshot revision from disk and
// returns the tree, letting callers roll back past just the
// immediately-preceding cycle when a regression is discovered late.
func RestoreTreeRevision(treeName, snapshotDir string, revision int) (*SerializableNode, error) {
	data, err := os.ReadFile(snapshotRevisionPath(treeName, snapshotDir, revision))
	if err != nil {
		return nil, fmt.Errorf("read snapshot revision: %w", err)
	}

	var tree SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}

	return &tree, nil
}

// RestoreTree loads the most recent snapshot revision from disk and returns
// the tree.
func RestoreTree(treeName, snapshotDir string) (*SerializableNode, error) {
	idx, err := loadSnapshotIndex(treeName, snapshotDir)
	if err != nil {
		return nil, err
	}
	if len(idx.Revisions) == 0 {
		return nil, fmt.Errorf("no snapshot found for tree %q in %s", treeName, snapshotDir)
	}

	return RestoreTreeRevision(treeName, snapshotDir, idx.Revisions[len(idx.Revisions)-1])
}

// RestoreTreeBeforeRegressionStreak walks back past a multi-cycle regression
// streak to the last known-good (peak) snapshot, instead of restoring only
// the single most-recent revision (which RestoreTree does, and which may
// itself be mid-streak — already regressed relative to an earlier peak).
//
// Starting at the newest revision, it steps backward for as long as each
// revision's fitness is strictly lower than the one before it — i.e. for as
// long as it's still inside an unbroken decline — and returns the first
// revision where that's no longer true. Revisions snapshotted via plain
// SnapshotTree (no recorded fitness) compare as 0, so an all-zero history
// degrades to returning the latest revision, matching RestoreTree.
func RestoreTreeBeforeRegressionStreak(treeName, snapshotDir string) (*SerializableNode, error) {
	idx, err := loadSnapshotIndex(treeName, snapshotDir)
	if err != nil {
		return nil, err
	}
	if len(idx.Revisions) == 0 {
		return nil, fmt.Errorf("no snapshot found for tree %q in %s", treeName, snapshotDir)
	}

	i := len(idx.Revisions) - 1
	for i > 0 && idx.Fitness[idx.Revisions[i]] < idx.Fitness[idx.Revisions[i-1]] {
		i--
	}

	return RestoreTreeRevision(treeName, snapshotDir, idx.Revisions[i])
}
