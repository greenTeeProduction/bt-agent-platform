package knowledge

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// TraceStore — Record, Get, LastFailure, ExplainLastFailure
// =============================================================================

func TestTraceStore_NewStore(t *testing.T) {
	ts := NewTraceStore(50)
	if ts == nil {
		t.Fatal("NewTraceStore returned nil")
	}
	if ts.maxSize != 50 {
		t.Errorf("expected maxSize=50, got %d", ts.maxSize)
	}
	if len(ts.traces) != 0 {
		t.Errorf("expected empty traces, got %d", len(ts.traces))
	}
}

func TestTraceStore_Record(t *testing.T) {
	ts := NewTraceStore(10)
	n := time.Now()

	trace := DecisionTrace{
		RunID:     "run-001",
		TreeID:    "tree:test",
		Task:      "test task",
		Steps:     []TraceStep{{NodeName: "root", NodeType: "Sequence", Status: "success"}},
		Outcome:   "success",
		StartedAt: n,
		EndedAt:   n.Add(1 * time.Second),
	}
	ts.Record(trace)

	if len(ts.traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(ts.traces))
	}
	if ts.traces[0].RunID != "run-001" {
		t.Errorf("expected RunID 'run-001', got %q", ts.traces[0].RunID)
	}
}

func TestTraceStore_RecordEviction(t *testing.T) {
	ts := NewTraceStore(3)

	// Add 5 traces — only last 3 should remain
	for i := 0; i < 5; i++ {
		ts.Record(DecisionTrace{
			RunID:   "run-" + strconv.Itoa(i),
			TreeID:  "tree:evict",
			Outcome: "success",
		})
	}

	if len(ts.traces) != 3 {
		t.Fatalf("expected 3 traces after eviction, got %d", len(ts.traces))
	}
	if ts.traces[0].RunID != "run-2" {
		t.Errorf("expected first trace 'run-2', got %q", ts.traces[0].RunID)
	}
	if ts.traces[2].RunID != "run-4" {
		t.Errorf("expected last trace 'run-4', got %q", ts.traces[2].RunID)
	}
}

func TestTraceStore_GetByTreeID(t *testing.T) {
	ts := NewTraceStore(20)

	// Add traces for two different trees
	for i := 0; i < 5; i++ {
		ts.Record(DecisionTrace{
			RunID:   "a-" + strconv.Itoa(i),
			TreeID:  "tree:a",
			Outcome: "success",
		})
		ts.Record(DecisionTrace{
			RunID:   "b-" + strconv.Itoa(i),
			TreeID:  "tree:b",
			Outcome: "failure",
		})
	}

	// Get all traces for tree:a (limit=10)
	result := ts.Get("tree:a", 10)
	if len(result) != 5 {
		t.Fatalf("expected 5 traces for tree:a, got %d", len(result))
	}
	for _, r := range result {
		if r.TreeID != "tree:a" {
			t.Errorf("expected TreeID 'tree:a', got %q", r.TreeID)
		}
	}
}

func TestTraceStore_GetLimited(t *testing.T) {
	ts := NewTraceStore(20)

	for i := 0; i < 10; i++ {
		ts.Record(DecisionTrace{
			RunID:   "r-" + strconv.Itoa(i),
			TreeID:  "tree:lim",
			Outcome: "success",
		})
	}

	// Get only 3 most recent
	result := ts.Get("tree:lim", 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].RunID != "r-9" {
		t.Errorf("expected most recent 'r-9' first, got %q", result[0].RunID)
	}
}

func TestTraceStore_GetNoMatch(t *testing.T) {
	ts := NewTraceStore(10)
	ts.Record(DecisionTrace{RunID: "r1", TreeID: "tree:abc", Outcome: "success"})

	result := ts.Get("tree:nonexistent", 10)
	if len(result) != 0 {
		t.Errorf("expected 0 results for nonexistent tree, got %d", len(result))
	}
}

func TestTraceStore_GetEmpty(t *testing.T) {
	ts := NewTraceStore(10)
	result := ts.Get("tree:empty", 5)
	if len(result) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(result))
	}
}

func TestTraceStore_LastFailure_Found(t *testing.T) {
	ts := NewTraceStore(20)
	ts.Record(DecisionTrace{RunID: "r1", TreeID: "tree:fail", Outcome: "success"})
	ts.Record(DecisionTrace{RunID: "r2", TreeID: "tree:fail", Outcome: "chain_success"})
	ts.Record(DecisionTrace{RunID: "r3", TreeID: "tree:fail", Outcome: "failure"})

	trace := ts.LastFailure("tree:fail")
	if trace == nil {
		t.Fatal("expected to find last failure")
	}
	if trace.RunID != "r3" {
		t.Errorf("expected last failure 'r3', got %q", trace.RunID)
	}
	if trace.Outcome != "failure" {
		t.Errorf("expected outcome 'failure', got %q", trace.Outcome)
	}
}

func TestTraceStore_LastFailure_ChainFailed(t *testing.T) {
	ts := NewTraceStore(20)
	ts.Record(DecisionTrace{RunID: "r1", TreeID: "tree:cf", Outcome: "success"})
	ts.Record(DecisionTrace{RunID: "r2", TreeID: "tree:cf", Outcome: "chain_failed"})

	trace := ts.LastFailure("tree:cf")
	if trace == nil {
		t.Fatal("expected to find chain_failed as failure")
	}
	if trace.RunID != "r2" {
		t.Errorf("expected 'r2', got %q", trace.RunID)
	}
}

func TestTraceStore_LastFailure_ChainPanic(t *testing.T) {
	ts := NewTraceStore(20)
	ts.Record(DecisionTrace{RunID: "r1", TreeID: "tree:cp", Outcome: "chain_panic"})

	trace := ts.LastFailure("tree:cp")
	if trace == nil {
		t.Fatal("expected chain_panic as failure")
	}
	if trace.RunID != "r1" {
		t.Errorf("expected 'r1', got %q", trace.RunID)
	}
}

func TestTraceStore_LastFailure_NotFound(t *testing.T) {
	ts := NewTraceStore(10)
	ts.Record(DecisionTrace{RunID: "r1", TreeID: "tree:ok", Outcome: "success"})

	trace := ts.LastFailure("tree:ok")
	if trace != nil {
		t.Errorf("expected nil for all-success tree, got %+v", trace)
	}
}

func TestTraceStore_LastFailure_EmptyStore(t *testing.T) {
	ts := NewTraceStore(10)
	trace := ts.LastFailure("tree:anything")
	if trace != nil {
		t.Errorf("expected nil for empty store, got %+v", trace)
	}
}

func TestTraceStore_LastFailure_NoMatch(t *testing.T) {
	ts := NewTraceStore(10)
	ts.Record(DecisionTrace{RunID: "r1", TreeID: "tree:a", Outcome: "failure"})

	trace := ts.LastFailure("tree:b")
	if trace != nil {
		t.Errorf("expected nil for non-matching tree, got %+v", trace)
	}
}

func TestGlobalTraceStore(t *testing.T) {
	if GlobalTraceStore == nil {
		t.Fatal("GlobalTraceStore should be non-nil")
	}
	if GlobalTraceStore.maxSize != 100 {
		t.Errorf("expected maxSize=100, got %d", GlobalTraceStore.maxSize)
	}
}

func TestExplainLastFailure_TraceFound(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:explain", Name: "Explain Me", Category: "test"})

	// Record a failure trace in the global store
	GlobalTraceStore.Record(DecisionTrace{
		RunID:  "explain-1",
		TreeID: "tree:explain",
		Task:   "do the thing",
		Steps: []TraceStep{
			{NodeName: "root", NodeType: "Sequence", Status: "success"},
			{NodeName: "step2", NodeType: "Action", Status: "failure", Error: "something broke"},
		},
		Outcome:   "failure",
		StartedAt: time.Now().Add(-5 * time.Second),
		EndedAt:   time.Now(),
	})
	GlobalTraceStore.Record(DecisionTrace{
		RunID:   "explain-2",
		TreeID:  "tree:explain",
		Task:    "do another thing",
		Outcome: "success",
	})

	report := kg.ExplainLastFailure("tree:explain")
	if !strings.Contains(report, "tree:explain") {
		t.Errorf("report should mention tree ID, got: %s", report)
	}
	if !strings.Contains(report, "do the thing") {
		t.Errorf("report should contain the failed task, got: %s", report)
	}
	if !strings.Contains(report, "failure") {
		t.Errorf("report should mention failure outcome, got: %s", report)
	}
	if !strings.Contains(report, "something broke") {
		t.Errorf("report should include error message, got: %s", report)
	}
	if strings.Contains(report, "do another thing") {
		t.Errorf("report should NOT contain the successful task, got: %s", report)
	}
	t.Logf("ExplainLastFailure output:\n%s", report)
}

func TestExplainLastFailure_NoTrace(t *testing.T) {
	kg := NewKnowledgeGraph()
	report := kg.ExplainLastFailure("tree:unknown")
	if !strings.Contains(report, "no failure traces found") {
		t.Errorf("expected 'no failure traces found', got: %s", report)
	}
}
