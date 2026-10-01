package knowledge

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

// feedbackSnapshot is the serializable subset of the knowledge graph that
// captures runtime feedback only — the fields RecordRun mutates. Static tree
// metadata (Name, Category, Description, Capabilities, …) is deliberately
// excluded so a Load merges into already-registered trees without clobbering it.
type feedbackSnapshot struct {
	Trees map[string]treeFeedback `json:"trees"`
	// ToolEdges holds both uses_tool and evolved_from edges. The field and JSON
	// key keep their original name for backward compatibility with feedback
	// files written before evolved_from edges were captured.
	ToolEdges []Edge `json:"tool_edges"`
}

// treeFeedback is the per-tree runtime feedback restored on Load.
type treeFeedback struct {
	Fitness      float64       `json:"fitness"`
	RunCount     int           `json:"run_count"`
	EvolvedCount int           `json:"evolved_count"`
	LastOutcome  string        `json:"last_outcome"`
	LastDuration time.Duration `json:"last_duration"`
	// RecentRuns mirrors TreeMeta.RecentRuns so a registered domain fitness
	// function (see RegisterDomainFitness) sees the full run-history window
	// immediately after a restart, not just runs recorded since the restart.
	RecentRuns []RunSummary `json:"recent_runs,omitempty"`
	// StructuralFitness and NodeCount mirror the bookkeeping RegisterEvolved
	// writes onto an evolved tree's metadata, and Category mirrors the value
	// (often inherited from the base tree) at registration time — none of
	// which a fresh Register call after a restart can reconstruct on its own.
	StructuralFitness float64 `json:"structural_fitness,omitzero"`
	NodeCount         int     `json:"node_count,omitzero"`
	Category          string  `json:"category,omitempty"`
}

// feedbackPersistState is the debounce bookkeeping wrapped around SaveFeedback.
// It records where to write, whether there is unwritten feedback (dirty), when
// the last write landed, the minimum spacing between writes, and a write count
// for tests. All fields are guarded by KnowledgeGraph.mu.
type feedbackPersistState struct {
	path        string
	dirty       bool
	lastFlush   time.Time
	minInterval time.Duration
	writeCount  int
	baseline    map[string]treeFeedback
	pending     map[string]*feedbackDelta
}

// A bounded delta retains all EMA updates but only the last history window.
// Baselines identify observations already loaded/committed by this process.
type feedbackDelta struct {
	runs      int
	emaWeight float64
	emaOffset float64
	recent    []RunSummary
}

func treeFeedbackFor(tree *TreeMeta) treeFeedback {
	return treeFeedback{Fitness: tree.Fitness, RunCount: tree.RunCount,
		EvolvedCount: tree.EvolvedCount, LastOutcome: tree.LastOutcome,
		LastDuration: tree.LastDuration, RecentRuns: slices.Clone(tree.RecentRuns),
		StructuralFitness: tree.StructuralFitness, NodeCount: tree.NodeCount, Category: tree.Category}
}

// Caller holds kg.mu, before the first local mutation since load/save.
func (kg *KnowledgeGraph) rememberFeedbackBaselineLocked(id string, tree *TreeMeta) {
	if kg.feedbackPersist.baseline == nil {
		kg.feedbackPersist.baseline = make(map[string]treeFeedback)
	}
	if _, ok := kg.feedbackPersist.baseline[id]; !ok {
		kg.feedbackPersist.baseline[id] = treeFeedbackFor(tree)
	}
}

func (kg *KnowledgeGraph) recordFeedbackDeltaLocked(rec RunRecord) {
	if rec.Outcome == "evolved" {
		return // EvolvedCount deltas are computed against the baseline.
	}
	if kg.feedbackPersist.pending == nil {
		kg.feedbackPersist.pending = make(map[string]*feedbackDelta)
	}
	delta := kg.feedbackPersist.pending[rec.TreeID]
	if delta == nil {
		delta = &feedbackDelta{emaWeight: 1}
		kg.feedbackPersist.pending[rec.TreeID] = delta
	}
	delta.runs++
	delta.emaWeight *= 0.9
	delta.emaOffset = 0.9*delta.emaOffset + 10*outcomeScore(rec.Outcome)
	delta.recent = append(delta.recent, RunSummary{Outcome: rec.Outcome, Quality: rec.Quality})
	if len(delta.recent) > maxRunHistory {
		delta.recent = slices.Clone(delta.recent[len(delta.recent)-maxRunHistory:])
	}
}

// ConfigureFeedbackPersistence wires the debounced writer to a target path and a
// minimum interval between throttled writes. lastFlush is left at its zero value
// so the very first FlushFeedback passes the throttle window and lands on disk.
func (kg *KnowledgeGraph) ConfigureFeedbackPersistence(path string, minInterval time.Duration) {
	kg.mu.Lock()
	kg.feedbackPersist.path = path
	kg.feedbackPersist.minInterval = minInterval
	// Reset the throttle clock so a freshly-armed writer's first flush always
	// lands, even when re-arming a graph that already flushed under a prior
	// configuration (e.g. the process-global GlobalGraph across constructions).
	kg.feedbackPersist.lastFlush = time.Time{}
	kg.mu.Unlock()
}

// MarkFeedbackDirty flags that feedback has changed since the last write, so the
// next eligible FlushFeedback will persist it. Cheap to call on every RecordRun.
func (kg *KnowledgeGraph) MarkFeedbackDirty() {
	kg.mu.Lock()
	kg.feedbackPersist.dirty = true
	kg.mu.Unlock()
}

// FlushFeedback persists pending feedback via SaveFeedback, but only when the
// graph is dirty AND either force is set (shutdown) or the throttle interval has
// elapsed since the last write. A successful write clears the dirty flag, stamps
// the flush time, and bumps the write count. Bursty non-forced calls inside the
// throttle window are suppressed, leaving the dirty flag set for a later flush.
func (kg *KnowledgeGraph) FlushFeedback(force bool) error {
	kg.mu.Lock()
	fp := &kg.feedbackPersist
	if fp.path == "" || !fp.dirty {
		kg.mu.Unlock()
		return nil
	}
	if !force && time.Since(fp.lastFlush) < fp.minInterval {
		kg.mu.Unlock()
		return nil // throttled: keep dirty so a later flush captures this state
	}
	path := fp.path
	kg.mu.Unlock()

	// SaveFeedback takes mu.Lock (and the file's sidecar lock) itself, so it
	// must not be called under the lock.
	if err := kg.SaveFeedback(path); err != nil {
		return err
	}

	kg.mu.Lock()
	kg.feedbackPersist.lastFlush = time.Now()
	kg.feedbackPersist.writeCount++
	kg.mu.Unlock()
	return nil
}

// SaveFeedback commits the runtime-feedback fields (Fitness, RunCount,
// EvolvedCount, LastOutcome, LastDuration, RecentRuns) and the uses_tool and
// evolved_from edges to a JSON file as one locked read-merge-write
// transaction. Static tree metadata is still never written.
//
// feedback.json has several concurrent writers: the daemon's scheduler, the
// dashboard, and the bt-agent MCP siblings each hold an independent
// KnowledgeGraph over the same file and each flush it. Serializing only this
// process's in-memory view and renaming it into place made the file
// untearable but did not serialize the transaction — everything a sibling
// committed between this process's load and its save was silently dropped.
// The whole cycle therefore runs inside reliability.UpdateSharedJSON, which
// holds the shared sidecar flock across the read, the merge and the atomic
// write, so what lands is the merge of both writers rather than whichever one
// renamed last (see mergeFeedback for the per-field rules).
func (kg *KnowledgeGraph) SaveFeedback(path string) error {
	// Hold the graph lock through commit and acknowledgement, so a concurrent
	// RecordRun can never be cleared by a flush that did not persist it. The
	// shared helper bounds lock acquisition to 30 seconds.
	kg.mu.Lock()
	defer kg.mu.Unlock()
	var merged feedbackSnapshot
	err := reliability.UpdateSharedJSON(path, func(onDisk []byte) (any, error) {
		committed, err := readCommittedFeedback(path, onDisk)
		if err != nil {
			return nil, err
		}
		mem := kg.snapshotFeedbackLocked()
		merged = mergeFeedback(committed, mem)
		for id, base := range kg.feedbackPersist.baseline {
			local, ok := mem.Trees[id]
			if !ok {
				continue
			}
			runs := local.RunCount - base.RunCount
			evolved := local.EvolvedCount - base.EvolvedCount
			if runs < 0 || evolved < 0 {
				return nil, fmt.Errorf("feedback counters regressed for %s", id)
			}
			parent := base
			if disk, ok := committed.Trees[id]; ok {
				parent = mergeTreeFeedback(disk, base)
				// Preserve an explicit local score edit only while its loaded
				// value is still current on disk; stale score edits never win.
				if runs == 0 && local.Fitness != base.Fitness && disk.Fitness == base.Fitness {
					parent.Fitness = local.Fitness
				}
			}
			parent.RunCount += runs
			parent.EvolvedCount += evolved
			if delta := kg.feedbackPersist.pending[id]; delta != nil && delta.runs == runs {
				parent.RecentRuns = append(slices.Clone(parent.RecentRuns), delta.recent...)
				if len(parent.RecentRuns) > maxRunHistory {
					parent.RecentRuns = slices.Clone(parent.RecentRuns[len(parent.RecentRuns)-maxRunHistory:])
				}
				if fn := kg.domainFitness[id]; fn != nil {
					parent.Fitness = fn(parent.RecentRuns) * 100
				} else {
					parent.Fitness = delta.emaWeight*parent.Fitness + delta.emaOffset
				}
			}
			if runs > 0 || evolved > 0 {
				parent.LastOutcome, parent.LastDuration = local.LastOutcome, local.LastDuration
			}
			if local.StructuralFitness > parent.StructuralFitness {
				parent.StructuralFitness, parent.NodeCount = local.StructuralFitness, local.NodeCount
			}
			merged.Trees[id] = parent
		}
		return merged, nil
	})
	if err != nil {
		return err // retain baseline, pending deltas and dirty state for retry
	}
	kg.applyFeedbackLocked(merged)
	// Save acknowledges the winning committed structure, unlike Load's gap-only
	// compatibility policy. Otherwise a stale process could admit a weaker tree.
	for id, fb := range merged.Trees {
		kg.Trees[id].StructuralFitness = fb.StructuralFitness
		kg.Trees[id].NodeCount = fb.NodeCount
	}
	kg.feedbackPersist.baseline = kg.snapshotFeedbackLocked().Trees
	kg.feedbackPersist.pending = nil
	kg.feedbackPersist.dirty = false
	return nil
}

// snapshotFeedback captures this process's view of the persisted fields.
// The caller holds kg.mu. Clone RecentRuns to detach committed/baseline windows
// from future RecordRun appends.
func (kg *KnowledgeGraph) snapshotFeedbackLocked() feedbackSnapshot {
	snap := feedbackSnapshot{Trees: make(map[string]treeFeedback, len(kg.Trees))}
	for id, tree := range kg.Trees {
		snap.Trees[id] = treeFeedback{
			Fitness:           tree.Fitness,
			RunCount:          tree.RunCount,
			EvolvedCount:      tree.EvolvedCount,
			LastOutcome:       tree.LastOutcome,
			LastDuration:      tree.LastDuration,
			RecentRuns:        slices.Clone(tree.RecentRuns),
			StructuralFitness: tree.StructuralFitness,
			NodeCount:         tree.NodeCount,
			Category:          tree.Category,
		}
	}
	for _, e := range kg.Edges {
		if e.Type == "uses_tool" || e.Type == "evolved_from" {
			snap.ToolEdges = append(snap.ToolEdges, e)
		}
	}
	return snap
}

// readCommittedFeedback decodes the snapshot a sibling left on disk. No bytes
// is a cold start, not a failure.
//
// Bytes that are not a snapshot are moved aside to `<path>.corrupt` and
// treated as a cold start. Failing the save instead would mean this process
// never persists feedback again while the unreadable file stays; overwriting
// in place would destroy the only copy of whatever was committed there.
// Quarantining keeps the evidence and lets the graph heal on the next write.
func readCommittedFeedback(path string, onDisk []byte) (feedbackSnapshot, error) {
	var snap feedbackSnapshot
	if len(onDisk) == 0 {
		return snap, nil
	}
	if err := json.Unmarshal(onDisk, &snap); err != nil {
		if renameErr := os.Rename(path, path+".corrupt"); renameErr != nil {
			return feedbackSnapshot{}, fmt.Errorf("quarantine corrupt feedback %s: %w", path, renameErr)
		}
		return feedbackSnapshot{}, nil
	}
	return snap, nil
}

// mergeFeedback folds the committed snapshot into this process's view and
// returns what to write.
//
// Trees union by ID. A tree only this process knows and a tree only a sibling
// knows are both real evidence that the other side cannot reconstruct —
// dropping a sibling's loses that tree's whole fitness and run history, and
// for an evolved tree permanently, since nothing else rebuilds it from disk.
// Nothing in the graph ever deletes a tree or an edge, so a union can only
// add back what a stale writer would have erased. Edges union the same way,
// keyed like connectLocked (from/to/type); disk order leads so the committed
// file keeps a stable shape across saves.
func mergeFeedback(disk, mem feedbackSnapshot) feedbackSnapshot {
	merged := feedbackSnapshot{Trees: make(map[string]treeFeedback, len(disk.Trees)+len(mem.Trees))}
	maps.Copy(merged.Trees, disk.Trees)
	for id, fb := range mem.Trees {
		if committed, both := disk.Trees[id]; both {
			fb = mergeTreeFeedback(committed, fb)
		}
		merged.Trees[id] = fb
	}

	type edgeKey struct{ from, to, relType string }
	seen := make(map[edgeKey]struct{}, len(disk.ToolEdges)+len(mem.ToolEdges))
	for _, edges := range [][]Edge{disk.ToolEdges, mem.ToolEdges} {
		for _, e := range edges {
			key := edgeKey{e.From, e.To, e.Type}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			merged.ToolEdges = append(merged.ToolEdges, e)
		}
	}
	return merged
}

// mergeTreeFeedback reconciles untracked legacy snapshots and selects the
// committed baseline before SaveFeedback adds this writer's pending deltas.
// Legacy aggregate counters use max because their overlap cannot be inferred;
// genuine observations are tracked separately and summed exactly once.
// Runtime evidence comes from the larger run count, with disk winning ties.
// Structural fitness is monotone and carries its associated node count.
// Static discovery category fills only empty/unknown registration gaps.
func mergeTreeFeedback(disk, mem treeFeedback) treeFeedback {
	merged := mem
	if disk.RunCount >= mem.RunCount {
		merged.Fitness = disk.Fitness
		merged.LastOutcome = disk.LastOutcome
		merged.LastDuration = disk.LastDuration
		merged.RecentRuns = disk.RecentRuns
	}
	merged.RunCount = max(mem.RunCount, disk.RunCount)
	merged.EvolvedCount = max(mem.EvolvedCount, disk.EvolvedCount)

	// A writer can hold a tally without the window behind it — a tree
	// registered fresh this process, or a feedback file written before
	// recent_runs existed. An empty window is the absence of evidence rather
	// than evidence of no runs, so it never displaces a real one.
	if len(merged.RecentRuns) == 0 {
		if len(disk.RecentRuns) > 0 {
			merged.RecentRuns = disk.RecentRuns
		} else {
			merged.RecentRuns = mem.RecentRuns
		}
	}

	if disk.StructuralFitness > merged.StructuralFitness {
		merged.StructuralFitness = disk.StructuralFitness
		merged.NodeCount = disk.NodeCount
	}
	if merged.NodeCount == 0 {
		merged.NodeCount = disk.NodeCount
	}
	if (merged.Category == "" || merged.Category == "unknown") && disk.Category != "" {
		merged.Category = disk.Category
	}
	return merged
}

// LoadFeedback restores runtime feedback from a JSON snapshot into the already
// registered trees, merging by ID so static metadata is preserved. An
// unregistered tree ID is resurrected as a new TreeMeta: evolved trees are
// only ever added to kg.Trees at runtime via RegisterEvolved, so after a
// daemon restart LoadFeedback runs before any evolution pass has
// re-registered them, even though their tree file and feedback metadata both
// still exist on disk. A missing file is a no-op (returns nil).
func (kg *KnowledgeGraph) LoadFeedback(path string) error {
	data, err := util.ReadPersistenceFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var snap feedbackSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	kg.mu.Lock()
	defer kg.mu.Unlock()
	for id := range snap.Trees {
		if kg.feedbackPersist.pending[id] != nil {
			return fmt.Errorf("cannot reload %s with uncommitted local observations", id)
		}
		if base, ok := kg.feedbackPersist.baseline[id]; ok {
			if tree := kg.Trees[id]; tree != nil && tree.EvolvedCount > base.EvolvedCount {
				return fmt.Errorf("cannot reload %s with uncommitted evolution observations", id)
			}
		}
	}
	kg.applyFeedbackLocked(snap)
	if kg.feedbackPersist.baseline == nil {
		kg.feedbackPersist.baseline = make(map[string]treeFeedback)
	}
	for id := range snap.Trees {
		kg.feedbackPersist.baseline[id] = treeFeedbackFor(kg.Trees[id])
	}
	return nil
}

func (kg *KnowledgeGraph) applyFeedbackLocked(snap feedbackSnapshot) {
	for id, fb := range snap.Trees {
		tree, ok := kg.Trees[id]
		if !ok {
			tree = &TreeMeta{ID: id, Name: id}
			kg.Trees[id] = tree
		}
		tree.Fitness = fb.Fitness
		tree.RunCount = fb.RunCount
		tree.EvolvedCount = fb.EvolvedCount
		tree.LastOutcome = fb.LastOutcome
		tree.LastDuration = fb.LastDuration
		tree.RecentRuns = fb.RecentRuns
		// Metadata fields fill gaps only — they must never clobber what a
		// live Register/RegisterEvolved already set (the snapshot-level
		// contract above: a Load merges into already-registered trees without
		// clobbering static metadata). The zero-value guards also make a
		// PRE-upgrade feedback file — written before these keys existed, so
		// they unmarshal to zero — a no-op instead of a wipe that the next
		// Save would persist forever. Save uses omitempty, so a zero on disk
		// is indistinguishable from absent anyway.
		if tree.StructuralFitness == 0 {
			tree.StructuralFitness = fb.StructuralFitness
		}
		if tree.NodeCount == 0 {
			tree.NodeCount = fb.NodeCount
		}
		// A real registered category ("domain", "finance", …) is never
		// clobbered; the empty string and the "unknown" pre-registration
		// placeholder are fillable gaps.
		if (tree.Category == "" || tree.Category == "unknown") && fb.Category != "" {
			tree.Category = fb.Category
		}
	}
	for _, e := range snap.ToolEdges {
		// Only restore edges whose source tree is registered.
		if _, ok := kg.Trees[e.From]; !ok {
			continue
		}
		kg.connectLocked(e.From, e.To, e.Type)
		// Resurrection repair: a restored evolved_from edge names the
		// registered base (From) of a tree this load may have resurrected as
		// a bare ID/Name shell (To). Inherit the base's discovery metadata
		// here — waiting for the next RegisterEvolved is not enough, since
		// production only calls it for a strictly better winner than the
		// strong StructuralFitness just restored.
		if e.Type == "evolved_from" {
			if evolved, ok := kg.Trees[e.To]; ok {
				kg.inheritBaseMetadataLocked(e.From, evolved)
			}
		}
	}
}
