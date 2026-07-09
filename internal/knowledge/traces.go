package knowledge

import (
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// DecisionTrace captures the full execution path through a behavior tree.
type DecisionTrace struct {
	RunID     string      `json:"run_id"`
	TreeID    string      `json:"tree_id"`
	Task      string      `json:"task"`
	Steps     []TraceStep `json:"steps"`
	Outcome   string      `json:"outcome"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at"`
}

// TraceStep is a single node execution in the tree.
type TraceStep struct {
	NodeName   string `json:"node_name"`
	NodeType   string `json:"node_type"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	LLMOutput  string `json:"llm_output,omitempty"`
	LLMPrompt  string `json:"llm_prompt,omitempty"`
	Error      string `json:"error,omitempty"`
	// ParentName attributes a child step to its enclosing composite node (e.g.
	// the Selector that tried this child). It lets the Selector-ordering bridge
	// bucket each child outcome under the right Selector without reconstructing
	// the tree topology. Empty for root or unattributed steps.
	ParentName string `json:"parent_name,omitempty"`
}

// TraceStore holds a rolling buffer of recent traces.
type TraceStore struct {
	mu      sync.RWMutex
	traces  []DecisionTrace
	maxSize int
}

// NewTraceStore creates a trace store with a max capacity.
func NewTraceStore(maxSize int) *TraceStore {
	return &TraceStore{
		traces:  make([]DecisionTrace, 0, maxSize),
		maxSize: maxSize,
	}
}

// Record appends a trace, evicting oldest if at capacity.
func (ts *TraceStore) Record(trace DecisionTrace) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.traces) >= ts.maxSize {
		ts.traces = ts.traces[1:]
	}
	ts.traces = append(ts.traces, trace)
}

// Get returns the most recent N traces for a tree.
func (ts *TraceStore) Get(treeID string, limit int) []DecisionTrace {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	result := make([]DecisionTrace, 0, limit)
	for i := len(ts.traces) - 1; i >= 0 && len(result) < limit; i-- {
		if ts.traces[i].TreeID == treeID {
			result = append(result, ts.traces[i])
		}
	}
	return result
}

// LastFailure returns the most recent failed trace for a tree.
func (ts *TraceStore) LastFailure(treeID string) *DecisionTrace {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	for i := len(ts.traces) - 1; i >= 0; i-- {
		if ts.traces[i].TreeID == treeID && ts.traces[i].Outcome != "success" && ts.traces[i].Outcome != "chain_success" {
			t := ts.traces[i]
			return &t
		}
	}
	return nil
}

// GlobalTraceStore is the singleton trace store.
var GlobalTraceStore = NewTraceStore(100)

// RecordSelectorOutcomes bridges executed decision traces into the durable
// Selector-ordering telemetry store (evolution.SelectorOptimizer, MILESTONE
// 1–2). It walks the trace's pre-order step list, notes every Selector node,
// and for each step attributed to a Selector via ParentName records that
// child's success/failure outcome under its Selector. The accumulated per-child
// counts are then persisted to path — SaveSelectorStats sums onto whatever is
// already on disk, so counts from successive traces accumulate rather than
// clobber earlier runs. A trace with no Selector-attributed child steps is a
// no-op (no file write).
func RecordSelectorOutcomes(trace DecisionTrace, path string) error {
	selectors := make(map[string]bool)
	for _, step := range trace.Steps {
		if step.NodeType == "Selector" {
			selectors[step.NodeName] = true
		}
	}
	if len(selectors) == 0 {
		return nil
	}

	opt := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	recorded := false
	for _, step := range trace.Steps {
		if step.ParentName == "" || !selectors[step.ParentName] {
			continue
		}
		opt.Record(step.ParentName, evolution.NodeExecutionRecord{
			NodeName: step.NodeName,
			Outcome:  step.Status,
		})
		recorded = true
	}
	if !recorded {
		return nil
	}
	return opt.SaveSelectorStats(path)
}

// ExplainLastFailure returns a human-readable explanation of why a tree last failed.
func (kg *KnowledgeGraph) ExplainLastFailure(treeID string) string {
	trace := GlobalTraceStore.LastFailure(treeID)
	if trace == nil {
		return "no failure traces found for " + treeID
	}

	s := "Tree: " + trace.TreeID + "\n"
	s += "Task: " + trace.Task + "\n"
	s += "Outcome: " + trace.Outcome + "\n"
	s += "Duration: " + trace.EndedAt.Sub(trace.StartedAt).String() + "\n"
	s += "Path:\n"

	for _, step := range trace.Steps {
		icon := "\u2713" // ✓
		if step.Status != "success" && step.Status != "chain_success" {
			icon = "\u2717" // ✗
		}
		s += "  " + icon + " " + step.NodeName + " (" + step.NodeType + ") "
		s += "[" + step.Status + "]"
		if step.Error != "" {
			s += " ERROR: " + step.Error
		}
		s += "\n"
	}
	return s
}
