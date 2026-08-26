package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
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

	// SaveFeedback takes mu.RLock itself, so it must not be called under the lock.
	if err := kg.SaveFeedback(path); err != nil {
		return err
	}

	kg.mu.Lock()
	kg.feedbackPersist.dirty = false
	kg.feedbackPersist.lastFlush = time.Now()
	kg.feedbackPersist.writeCount++
	kg.mu.Unlock()
	return nil
}

// SaveFeedback serializes the runtime-feedback fields (Fitness, RunCount,
// EvolvedCount, LastOutcome, LastDuration, RecentRuns) and the uses_tool and
// evolved_from edges to a JSON file. Static tree metadata is not written. The
// write is atomic: it lands in a temp file that is renamed into place, so a
// crash mid-write can never leave a truncated snapshot.
func (kg *KnowledgeGraph) SaveFeedback(path string) error {
	kg.mu.RLock()
	snap := feedbackSnapshot{
		Trees: make(map[string]treeFeedback, len(kg.Trees)),
	}
	for id, tree := range kg.Trees {
		snap.Trees[id] = treeFeedback{
			Fitness:           tree.Fitness,
			RunCount:          tree.RunCount,
			EvolvedCount:      tree.EvolvedCount,
			LastOutcome:       tree.LastOutcome,
			LastDuration:      tree.LastDuration,
			RecentRuns:        tree.RecentRuns,
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
	kg.mu.RUnlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".feedback-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// LoadFeedback restores runtime feedback from a JSON snapshot into the already
// registered trees, merging by ID so static metadata is preserved. An
// unregistered tree ID is resurrected as a new TreeMeta: evolved trees are
// only ever added to kg.Trees at runtime via RegisterEvolved, so after a
// daemon restart LoadFeedback runs before any evolution pass has
// re-registered them, even though their tree file and feedback metadata both
// still exist on disk. A missing file is a no-op (returns nil).
func (kg *KnowledgeGraph) LoadFeedback(path string) error {
	data, err := os.ReadFile(path)
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
	return nil
}
