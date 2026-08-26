package knowledge

import (
	"slices"
	"strings"
	"sync"
	"time"
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

// ChildTick mirrors engine.Blackboard's terminal child-tick record (Parent,
// Child, Status) without importing internal/engine, so this package stays
// free of a dependency on the engine's node-execution machinery.
type ChildTick struct {
	Parent string
	Child  string
	Status string
}

// StepsFromChildTicks converts a run's terminal child ticks (from
// engine.Blackboard.ChildTicks()) into TraceSteps, giving DecisionTrace.Steps
// a real execution path to render. Ticks with an empty Child are skipped —
// they carry no node identity worth showing.
func StepsFromChildTicks(ticks []ChildTick) []TraceStep {
	steps := make([]TraceStep, 0, len(ticks))
	for _, tick := range ticks {
		if tick.Child == "" {
			continue
		}
		steps = append(steps, TraceStep{
			NodeName:   tick.Child,
			ParentName: tick.Parent,
			Status:     tick.Status,
		})
	}
	return steps
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
	for _, t := range slices.Backward(ts.traces) {
		if t.TreeID == treeID && t.Outcome != "success" && t.Outcome != "chain_success" {

			return &t
		}
	}
	return nil
}

// GlobalTraceStore is the singleton trace store.
var GlobalTraceStore = NewTraceStore(100)

// ExplainLastFailure returns a human-readable explanation of why a tree last failed.
func (kg *KnowledgeGraph) ExplainLastFailure(treeID string) string {
	trace := GlobalTraceStore.LastFailure(treeID)
	if trace == nil {
		return "no failure traces found for " + treeID
	}

	var s strings.Builder
	s.WriteString("Tree: " + trace.TreeID + "\n")
	s.WriteString("Task: " + trace.Task + "\n")
	s.WriteString("Outcome: " + trace.Outcome + "\n")
	s.WriteString("Duration: " + trace.EndedAt.Sub(trace.StartedAt).String() + "\n")
	s.WriteString("Path:\n")

	for _, step := range trace.Steps {
		icon := "\u2713" // ✓
		if step.Status != "success" && step.Status != "chain_success" {
			icon = "\u2717" // ✗
		}
		s.WriteString("  " + icon + " " + step.NodeName + " (" + step.NodeType + ") ")
		s.WriteString("[" + step.Status + "]")
		if step.Error != "" {
			s.WriteString(" ERROR: " + step.Error)
		}
		s.WriteString("\n")
	}
	return s.String()
}
