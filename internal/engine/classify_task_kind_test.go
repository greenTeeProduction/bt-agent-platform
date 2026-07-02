package engine

import (
	"testing"
)

func runClassify(t *testing.T, task string) string {
	t.Helper()
	bb := newTestBlackboard()
	bb.Task = task
	fn := GetAction("ClassifyTaskKind")
	if fn == nil {
		t.Fatal("ClassifyTaskKind not registered")
	}
	if fn(newTestBTContext(bb)) != 1 {
		t.Fatal("ClassifyTaskKind must succeed")
	}
	kind, _ := bb.ChainState["task_kind"].(string)
	return kind
}

func TestClassifyTaskKind_Heuristics(t *testing.T) {
	if k := runClassify(t, "Fix the failing TestCMAESOptimizer regression"); k != "bug" {
		t.Fatalf("bug keywords: got %q", k)
	}
	if k := runClassify(t, "Add a new dashboard panel for node latency"); k != "creative" {
		t.Fatalf("creative keywords: got %q", k)
	}
	if k := runClassify(t, "gofmt the repo"); k != "direct" {
		t.Fatalf("short no-keyword task: got %q", k)
	}
}

func TestClassifyTaskKind_Idempotent(t *testing.T) {
	bb := newTestBlackboard()
	bb.Task = "Fix crash"
	bb.ChainState["task_kind"] = "creative" // pre-set (e.g. resumed run)
	fn := GetAction("ClassifyTaskKind")
	fn(newTestBTContext(bb))
	if bb.ChainState["task_kind"] != "creative" {
		t.Fatal("must not reclassify a resumed run")
	}
}
