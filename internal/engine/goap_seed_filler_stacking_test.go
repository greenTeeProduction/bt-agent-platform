package engine

import (
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

// Guard for the change that let the seeders past an active coverage program
// (research.ActiveExcludingFiller). Letting RESEARCH seed past filler is the
// point; letting the FILLER FALLBACK seed past filler is a pile-up: every cycle
// where only coverage is active would fail research (nlm has been down since
// ~07-31) and stack another coverage program, without bound.
func TestSeedNextProgram_DoesNotStackFillerOnFiller(t *testing.T) {
	dir := t.TempDir()
	prevPath := goapProgramsPath
	goapProgramsPath = filepath.Join(dir, "programs.json")
	t.Cleanup(func() { goapProgramsPath = prevPath })

	if err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
		ps.Add("Deterministic coverage backlog: characterization tests for internal/engine/a.go",
			research.CoverageFillerSource,
			[]string{
				"Add characterization tests pinning internal/engine/a.go in internal/engine/a_test.go",
				"Add characterization tests pinning internal/engine/b.go in internal/engine/b_test.go",
			})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Research is down — the live case.
	prevFetch := seedProgramFetchFn
	seedProgramFetchFn = func(string) string { return "" }
	t.Cleanup(func() { seedProgramFetchFn = prevFetch })

	// A repo listing that would happily yield more coverage milestones.
	prevList := goapFusionListRepoGoFilesFn
	goapFusionListRepoGoFilesFn = func() []string {
		return []string{"internal/engine/c.go", "internal/engine/d.go", "internal/engine/e.go"}
	}
	t.Cleanup(func() { goapFusionListRepoGoFilesFn = prevList })
	prevExists := goapFusionRepoFileStateFn
	goapFusionRepoFileStateFn = func(string) (bool, bool) { return true, true }
	t.Cleanup(func() { goapFusionRepoFileStateFn = prevExists })

	seed := GetAction("SeedNextProgram")
	bb := &Blackboard{ChainState: map[string]any{}}
	if got := seed(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("SeedNextProgram status = %d, want 1", got)
	}

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	filler := 0
	for _, p := range ps.Programs {
		if p.IsFiller() {
			filler++
		}
	}
	if filler != 1 {
		t.Fatalf("coverage programs = %d, want 1: research may seed past an active filler program, "+
			"but the filler FALLBACK must not stack another one on top — that pile-up is unbounded "+
			"while research is down", filler)
	}
}
