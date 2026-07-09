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
	Trees     map[string]treeFeedback `json:"trees"`
	ToolEdges []Edge                  `json:"tool_edges"`
}

// treeFeedback is the per-tree runtime feedback restored on Load.
type treeFeedback struct {
	Fitness      float64       `json:"fitness"`
	RunCount     int           `json:"run_count"`
	EvolvedCount int           `json:"evolved_count"`
	LastOutcome  string        `json:"last_outcome"`
	LastDuration time.Duration `json:"last_duration"`
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
// EvolvedCount, LastOutcome, LastDuration) and the uses_tool edges to a JSON
// file. Static tree
// metadata is not written. The write is atomic: it lands in a temp file that is
// renamed into place, so a crash mid-write can never leave a truncated snapshot.
func (kg *KnowledgeGraph) SaveFeedback(path string) error {
	kg.mu.RLock()
	snap := feedbackSnapshot{
		Trees: make(map[string]treeFeedback, len(kg.Trees)),
	}
	for id, tree := range kg.Trees {
		snap.Trees[id] = treeFeedback{
			Fitness:      tree.Fitness,
			RunCount:     tree.RunCount,
			EvolvedCount: tree.EvolvedCount,
			LastOutcome:  tree.LastOutcome,
			LastDuration: tree.LastDuration,
		}
	}
	for _, e := range kg.Edges {
		if e.Type == "uses_tool" {
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
// registered trees, merging by ID so static metadata is preserved. Feedback for
// unregistered tree IDs is skipped. A missing file is a no-op (returns nil).
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
			continue // unknown tree — don't resurrect it, only merge feedback
		}
		tree.Fitness = fb.Fitness
		tree.RunCount = fb.RunCount
		tree.EvolvedCount = fb.EvolvedCount
		tree.LastOutcome = fb.LastOutcome
		tree.LastDuration = fb.LastDuration
	}
	for _, e := range snap.ToolEdges {
		// Only restore edges whose source tree is registered.
		if _, ok := kg.Trees[e.From]; !ok {
			continue
		}
		kg.connectLocked(e.From, e.To, e.Type)
	}
	return nil
}
