package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

func withSeedFetch(t *testing.T, answer string) {
	t.Helper()
	old := seedProgramFetchFn
	seedProgramFetchFn = func(prompt string) string { return answer }
	t.Cleanup(func() { seedProgramFetchFn = old })
}

func TestNeedsFreshProgramCondition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	old := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = old })

	cond := GetCondition("NeedsFreshProgram")
	if !cond(&Blackboard{}) {
		t.Fatal("empty store must need a fresh program")
	}
	ps, _ := research.OpenPrograms(path)
	ps.Add("Active", "test", []string{"m1 in internal/a2a/a.go"})
	_ = ps.Save()
	if cond(&Blackboard{}) {
		t.Fatal("active program must suppress seeding")
	}
	ps.MarkDone(ps.Programs[0].ID, 0, "run-x")
	_ = ps.Save()
	if !cond(&Blackboard{}) {
		t.Fatal("completed programs must re-enable seeding")
	}
}

func TestSeedNextProgramPersistsValidProposal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	oldP := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = oldP })
	withSeedFetch(t, `PROGRAM: Knowledge-graph freshness pipeline
MILESTONE1: Add incremental graph updates in internal/knowledge/graph.go with tests
MILESTONE2: Wire freshness checks into internal/domains/trees.go gating
MILESTONE3: Expose graph staleness in internal/dashboard/agents.go`)

	bb := &Blackboard{Task: "improve", ChainState: map[string]any{}}
	fn := GetAction("SeedNextProgram")
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d: %s", got, bb.Result)
	}
	ps, _ := research.OpenPrograms(path)
	p := ps.Active()
	if p == nil || p.Source != "auto-seed" || len(p.Milestones) != 3 {
		t.Fatalf("proposal must persist as the active program: %+v", p)
	}
	if !strings.Contains(bb.Result, "Backlog Seeded") || !strings.Contains(bb.Result, "PROGRAM-CONTINUE") {
		t.Fatalf("result must announce the seed and carry the continue marker: %s", bb.Result)
	}
}

func TestSeedNextProgramRejectsUnscopedMilestones(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	oldP := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = oldP })
	withSeedFetch(t, `PROGRAM: Vague ambitions
MILESTONE1: Make everything better
MILESTONE2: Improve quality across the board`)

	bb := &Blackboard{ChainState: map[string]any{}}
	fn := GetAction("SeedNextProgram")
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("rejection must not fail the cycle, got %d", got)
	}
	ps, _ := research.OpenPrograms(path)
	if len(ps.Programs) != 0 {
		t.Fatal("unscoped proposals must not persist")
	}
	if !strings.Contains(bb.Result, "Rejected Proposal") {
		t.Fatalf("result must explain the rejection: %s", bb.Result)
	}
}

func TestSeedNextProgramSkipsWhenProgramActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	oldP := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = oldP })
	ps, _ := research.OpenPrograms(path)
	ps.Add("Active", "test", []string{"m1 in internal/a2a/a.go"})
	_ = ps.Save()
	withSeedFetch(t, "PROGRAM: should never be fetched\nMILESTONE1: x in internal/a2a/b.go")

	bb := &Blackboard{ChainState: map[string]any{}}
	fn := GetAction("SeedNextProgram")
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("got %d", got)
	}
	re, _ := research.OpenPrograms(path)
	if len(re.Programs) != 1 {
		t.Fatal("no pile-up: active program must suppress seeding")
	}
}

// The seeding stage must execute when BUILT AS A TREE, not only when the
// action is invoked directly: the first shipped version wrapped the sequence
// in an AlwaysSucceed node, which this engine builds as a LEAF that ignores
// children — the stage "succeeded" every cycle without ever running.
func TestBacklogReplenishSubtreeActuallyExecutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	oldP := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = oldP })
	withSeedFetch(t, `PROGRAM: Subtree proof
MILESTONE1: Add coverage in internal/knowledge/graph.go
MILESTONE2: Wire checks into internal/domains/trees.go`)

	subtree := &evolution.SerializableNode{Type: "Selector", Name: "BacklogReplenish", Children: []evolution.SerializableNode{
		{Type: "Sequence", Name: "SeedWhenIdle", Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "NeedsFreshProgram"},
			{Type: "Action", Name: "SeedNextProgram"},
		}},
		{Type: "AlwaysSucceed", Name: "SeedSkipped"},
	}}
	bb := &Blackboard{Task: "improve", ChainState: map[string]any{}}
	cmd := BuildTree(subtree, bb)
	if cmd == nil {
		t.Fatal("BuildTree returned nil")
	}
	if out := RunTask(bb, cmd); out == "" && bb.Outcome == "" {
		t.Log("tick complete")
	}
	ps, _ := research.OpenPrograms(path)
	if ps.Active() == nil {
		t.Fatal("built-and-ticked subtree must actually seed the program store")
	}
}

func TestChooseProgramProposalFallsThroughWhenNlmHasNoProgram(t *testing.T) {
	claudeProgram := "PROGRAM: Claude program\nMILESTONE1: Add x in internal/a2a/a.go\nMILESTONE2: Wire y in internal/engine/b.go"

	// nlm succeeded but returned prose with no PROGRAM block → must use Claude.
	called := false
	got := chooseProgramProposal(`{"answer":"Here are some thoughts, all is well."}`, func() string { called = true; return claudeProgram })
	if !called || extractGoapProgram(got) == nil {
		t.Fatalf("prose nlm answer must fall through to Claude; called=%v got=%q", called, got)
	}

	// nlm returned a real PROGRAM → Claude must NOT be called.
	called = false
	nlmProg := `{"answer":"PROGRAM: Nlm program\nMILESTONE1: Add z in internal/knowledge/g.go\nMILESTONE2: Wire w in internal/domains/t.go"}`
	got = chooseProgramProposal(nlmProg, func() string { called = true; return "" })
	if called || extractGoapProgram(got) == nil {
		t.Fatalf("a usable nlm program must be accepted without Claude; called=%v", called)
	}

	// nlm failed → Claude.
	called = false
	chooseProgramProposal("Error: RESOURCE_EXHAUSTED", func() string { called = true; return claudeProgram })
	if !called {
		t.Fatal("nlm failure must fall through to Claude")
	}
}
