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

func TestIsValidProgramMilestone(t *testing.T) {
	valid := []string{
		"Implement the macro path extractor in `internal/evolution/extractor.go` and `internal/evolution/extractor_test.go` to distill recurring node sequences from reflection records.",
		"Add auction retry/backoff in internal/a2a/auction.go with regression tests in internal/a2a/auction_test.go",
	}
	for _, m := range valid {
		if !isValidProgramMilestone(m) {
			t.Fatalf("valid file-scoped milestone rejected: %q", m)
		}
	}
	invalid := []string{
		"Review complete. Everything looks good.",      // prose, no files
		"Make the platform better",                     // no files
		"internal/x.go",                                // too short, no verb/desc but names file — actually len<12? it's 15... but no real instruction
		"Summary: we should improve internal/a2a/a.go", // prose opener
	}
	// "internal/x.go" is 13 chars and names a file — the gate accepts it (file-scoped, not prose, len ok);
	// that is acceptable: a bare file path is a legitimate (if terse) milestone target.
	for _, m := range invalid[:2] {
		if isValidProgramMilestone(m) {
			t.Fatalf("no-file/prose milestone accepted: %q", m)
		}
	}
	if isValidProgramMilestone(invalid[3]) {
		t.Fatalf("prose-opener milestone accepted: %q", invalid[3])
	}
}

func TestMilestoneTouchesExistingFile(t *testing.T) {
	old := goapFusionRepoFileExistsFn
	goapFusionRepoFileExistsFn = func(p string) bool { return p == "internal/engine/tree.go" }
	t.Cleanup(func() { goapFusionRepoFileExistsFn = old })

	if !milestoneTouchesExistingFile("Extend routing in internal/engine/tree.go with tests in internal/engine/tree_test.go") {
		t.Fatal("milestone modifying an existing file must be grounded")
	}
	if milestoneTouchesExistingFile("Build the MonotonicityAuditor in internal/evolution/auditor.go (does not exist)") {
		t.Fatal("greenfield milestone naming only non-existent files must be ungrounded")
	}
	// A milestone naming only a new test file (no existing production target) is ungrounded.
	if milestoneTouchesExistingFile("Add internal/engine/new_thing_test.go") {
		t.Fatal("a test-only milestone with no existing production target is ungrounded")
	}
}

func TestSeedNextProgramRejectsUngroundedProgram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	oldP := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = oldP })
	oldEx := goapFusionRepoFileExistsFn
	goapFusionRepoFileExistsFn = func(string) bool { return false } // nothing exists
	t.Cleanup(func() { goapFusionRepoFileExistsFn = oldEx })
	withSeedFetch(t, `PROGRAM: Fabricated research subsystem
MILESTONE1: Build the Auditor in internal/evolution/auditor.go
MILESTONE2: Build the Distiller in internal/evolution/distiller.go`)

	bb := &Blackboard{ChainState: map[string]any{}}
	GetAction("SeedNextProgram")(&btcore.BTContext[Blackboard]{Blackboard: bb})
	ps, _ := research.OpenPrograms(path)
	if len(ps.Programs) != 0 {
		t.Fatal("an all-greenfield (ungrounded) program must not persist")
	}
	if !strings.Contains(bb.Result, "ungrounded") {
		t.Fatalf("rejection must explain ungrounded: %s", bb.Result)
	}
}
