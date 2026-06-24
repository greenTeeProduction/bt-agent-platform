package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// findBBTool returns the attached blackboard tool with the given name.
func findBBTool(t *testing.T, bb *Blackboard, name string) *bbTool {
	t.Helper()
	for _, tool := range bb.ChainTools {
		if bt, ok := tool.(*bbTool); ok && bt.Name() == name {
			return bt
		}
	}
	t.Fatalf("tool %q not attached", name)
	return nil
}

// TestBBRecentTool_NewestFirst verifies the bb_recent agent tool returns the
// most recently written entries first — the context an error-recovery node
// needs — even when key order would otherwise hide the newest write.
func TestBBRecentTool_NewestFirst(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_recent_tool", "", "demo")
	bb := &Blackboard{BB: h, RunID: h.RunID}

	PrepareBlackboard(bb)

	// Accumulate subtask results; "err/10" sorts before "err/2" lexically.
	for _, k := range []string{"err/1", "err/2", "err/10", "err/3"} {
		if err := h.Set(k, "log-"+k, "", "text"); err != nil {
			t.Fatal(err)
		}
	}
	// Re-write err/3 last so it is unambiguously the most recent entry.
	if err := h.Set("err/3", "log-err/3-latest", "", "text"); err != nil {
		t.Fatal(err)
	}

	recent := findBBTool(t, bb, "bb_recent")
	out := recent.Call("err/")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "err/3") {
		t.Fatalf("bb_recent should list the newest write first, got:\n%s", out)
	}

	// The plain key-sorted bb_list lists err/1 first, so the two tools are
	// observably different and recovery nodes get a recency view.
	list := findBBTool(t, bb, "bb_list")
	listOut := list.Call("err/")
	listLines := strings.Split(strings.TrimSpace(listOut), "\n")
	if len(listLines) == 0 || !strings.Contains(listLines[0], "err/1") {
		t.Fatalf("bb_list should be key-sorted (err/1 first), got:\n%s", listOut)
	}
}

// TestBBSessionRecentTool verifies the session variant is attached and reports
// the most recent cross-step entry first.
func TestBBSessionRecentTool(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_x", "sess_recent", "demo")
	bb := &Blackboard{BB: h, RunID: h.RunID}

	PrepareBlackboard(bb)

	if err := h.SetSession("steps/a", "first", "", "text"); err != nil {
		t.Fatal(err)
	}
	if err := h.SetSession("steps/b", "second", "", "text"); err != nil {
		t.Fatal(err)
	}

	tool := findBBTool(t, bb, "bb_session_recent")
	out := tool.Call("steps/")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "steps/b") {
		t.Fatalf("bb_session_recent should list steps/b (newest) first, got:\n%s", out)
	}
}
