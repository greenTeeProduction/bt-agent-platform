// Live-run registry: mutable runs register here so action nodes and MCP
// callers can enqueue MutationOps against them by RunID
// (spec: docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md).
package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// maxMutationsPerRun caps enqueued ops per run so a self-growing tree cannot
// mutate without bound. The 1000-tick cap and tree timeout still apply.
const maxMutationsPerRun = 100

// LiveRunInfo names the run for registry listing and persistence.
type LiveRunInfo struct{ Agent, TreeID string }

// MutationRecord is one journal entry: an op that was applied or rejected.
type MutationRecord struct {
	OpID       string     `json:"op_id"`
	Op         MutationOp `json:"op"`
	Status     string     `json:"status"` // "applied" | "rejected"
	Error      string     `json:"error,omitempty"`
	Generation int        `json:"generation"`
	At         time.Time  `json:"at"`
}

// LiveRunStatus is the externally visible summary of a registered run.
type LiveRunStatus struct {
	RunID      string    `json:"run_id"`
	Agent      string    `json:"agent"`
	TreeID     string    `json:"tree_id"`
	Generation int       `json:"generation"`
	Pending    int       `json:"pending"`
	JournalLen int       `json:"journal_len"`
	StartedAt  time.Time `json:"started_at"`
}

type queuedOp struct {
	id string
	op MutationOp
}

// liveRun is one mutable run's mutation state. The tree fields (cur, capture)
// are owned by the run goroutine and only touched at tick boundaries; the
// queue, journal, and counters are mutex-guarded because MCP handlers read
// and write them from other goroutines.
type liveRun struct {
	runID     string
	info      LiveRunInfo
	startedAt time.Time

	mu         sync.Mutex
	queue      []queuedOp
	journal    []MutationRecord
	generation int
	opSeq      int
	total      int // lifetime enqueued count, enforces maxMutationsPerRun

	// Run-goroutine-owned (no lock): current serializable tree and the
	// source-node → inner-command capture of its latest build.
	cur     *evolution.SerializableNode
	capture map[*evolution.SerializableNode]btcore.Command[Blackboard]
}

var liveRuns sync.Map // runID → *liveRun

// registerLiveRun creates and registers the run's mutation state and attaches
// it to bb. A missing RunID gets a generated one so registry lookup works.
func registerLiveRun(bb *Blackboard, info LiveRunInfo) *liveRun {
	if bb.RunID == "" {
		bb.RunID = blackboard.NewRunID()
	}
	lr := &liveRun{runID: bb.RunID, info: info, startedAt: time.Now()}
	liveRuns.Store(lr.runID, lr)
	bb.liveRun = lr
	return lr
}

func deregisterLiveRun(runID string) { liveRuns.Delete(runID) }

func (lr *liveRun) enqueue(op MutationOp) (string, error) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.total >= maxMutationsPerRun {
		return "", fmt.Errorf("run %s: mutation cap (%d) reached", lr.runID, maxMutationsPerRun)
	}
	lr.total++
	lr.opSeq++
	id := fmt.Sprintf("%s-op%d", lr.runID, lr.opSeq)
	lr.queue = append(lr.queue, queuedOp{id: id, op: op})
	return id, nil
}

func (lr *liveRun) drain() []queuedOp {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	ops := lr.queue
	lr.queue = nil
	return ops
}

func (lr *liveRun) record(rec MutationRecord) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.journal = append(lr.journal, rec)
}

func (lr *liveRun) status() LiveRunStatus {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return LiveRunStatus{
		RunID: lr.runID, Agent: lr.info.Agent, TreeID: lr.info.TreeID,
		Generation: lr.generation, Pending: len(lr.queue),
		JournalLen: len(lr.journal), StartedAt: lr.startedAt,
	}
}

// ListLiveRuns returns a snapshot of all registered mutable runs.
func ListLiveRuns() []LiveRunStatus {
	var out []LiveRunStatus
	liveRuns.Range(func(_, v any) bool {
		out = append(out, v.(*liveRun).status())
		return true
	})
	return out
}

// EnqueueLiveMutation queues op against a registered run by RunID.
func EnqueueLiveMutation(runID string, op MutationOp) (string, error) {
	v, ok := liveRuns.Load(runID)
	if !ok {
		return "", fmt.Errorf("no live mutable run %q", runID)
	}
	return v.(*liveRun).enqueue(op)
}

// LiveMutationJournal returns a copy of the run's applied/rejected records.
func LiveMutationJournal(runID string) ([]MutationRecord, error) {
	v, ok := liveRuns.Load(runID)
	if !ok {
		return nil, fmt.Errorf("no live mutable run %q", runID)
	}
	lr := v.(*liveRun)
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return append([]MutationRecord(nil), lr.journal...), nil
}

// EnqueueMutation queues a mutation against this run's own tree, effective at
// the next tick boundary. Callable from action implementations during a tick.
func (bb *Blackboard) EnqueueMutation(op MutationOp) (string, error) {
	if bb.liveRun == nil {
		return "", fmt.Errorf("this run is not mutable (not started via RunTaskMutable)")
	}
	return bb.liveRun.enqueue(op)
}
