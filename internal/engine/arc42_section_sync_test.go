package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArc42Guidelines(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "arc42"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "arc42", "GUIDELINES.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestArc42GuidelineForSlicesOneSection(t *testing.T) {
	dir := t.TempDir()
	writeArc42Guidelines(t, dir, "# arc42 Section Guidelines\n\n## Section 1 — Introduction and Goals\n- rule one\n\n## Section 2 — Architecture Constraints\n- rule two\n")
	got := arc42GuidelineFor(dir, arc42Sections[0])
	if !strings.Contains(got, "rule one") {
		t.Errorf("section 1 slice missing its rule: %q", got)
	}
	if strings.Contains(got, "rule two") {
		t.Errorf("section 1 slice leaked section 2 content: %q", got)
	}
}

func TestArc42GuidelineForMissingFileDegrades(t *testing.T) {
	if got := arc42GuidelineFor(t.TempDir(), arc42Sections[0]); got != "" {
		t.Errorf("want empty guideline for missing file, got %q", got)
	}
}

type arc42SyncFakeClaude struct {
	prompts []string
	result  CommandResult
}

func (f *arc42SyncFakeClaude) RunClaude(_ context.Context, _ string, prompt string) CommandResult {
	f.prompts = append(f.prompts, prompt)
	return f.result
}

type arc42SyncFakeRunner struct {
	statusOutput string
	cmds         []string
}

func (f *arc42SyncFakeRunner) Run(_ context.Context, _ string, name string, args ...string) CommandResult {
	f.cmds = append(f.cmds, name+" "+strings.Join(args, " "))
	return CommandResult{Output: f.statusOutput}
}

func writeArc42SectionFile(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "arc42"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "arc42", file), []byte("# stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncArc42SectionSkipsMissingFile(t *testing.T) {
	claude := &arc42SyncFakeClaude{}
	changed, note := syncArc42Section(context.Background(), claude, &arc42SyncFakeRunner{},
		docChangeContext{WorkDir: t.TempDir(), ChangedFiles: []string{"internal/engine/tree.go"}}, arc42Sections[0])
	if changed || !strings.Contains(note, "skipped") {
		t.Errorf("want skip, got changed=%v note=%q", changed, note)
	}
	if len(claude.prompts) != 0 {
		t.Error("claude must not run for a missing section file")
	}
}

func TestSyncArc42SectionUpdatesAndEmbedsGuideline(t *testing.T) {
	dir := t.TempDir()
	writeArc42SectionFile(t, dir, arc42Sections[4].File) // section 5
	writeArc42Guidelines(t, dir, "## Section 5 — Building Block View\n- static decomposition only\n")
	claude := &arc42SyncFakeClaude{result: CommandResult{Output: "edited"}}
	runner := &arc42SyncFakeRunner{statusOutput: " M docs/arc42/05-building-blocks.md\n"}
	chg := docChangeContext{WorkDir: dir, ChangedFiles: []string{"internal/evolution/island.go"}, Summary: "island persistence"}
	changed, note := syncArc42Section(context.Background(), claude, runner, chg, arc42Sections[4])
	if !changed || !strings.Contains(note, arc42Sections[4].File) {
		t.Errorf("want updated note naming the file, got changed=%v note=%q", changed, note)
	}
	if len(claude.prompts) != 1 || !strings.Contains(claude.prompts[0], "static decomposition only") {
		t.Fatalf("guideline block missing from prompt")
	}
	if !strings.Contains(claude.prompts[0], "island persistence") || !strings.Contains(claude.prompts[0], "internal/evolution/island.go") {
		t.Error("change context missing from prompt")
	}
	if !strings.Contains(strings.Join(runner.cmds, "|"), "git status --short -- docs/arc42/05-building-blocks.md") {
		t.Errorf("change detection must be scoped to the section file: %v", runner.cmds)
	}
}

func TestSyncArc42SectionClaudeFailureIsNonFatal(t *testing.T) {
	dir := t.TempDir()
	writeArc42SectionFile(t, dir, arc42Sections[0].File)
	claude := &arc42SyncFakeClaude{result: CommandResult{Err: fmt.Errorf("boom")}}
	changed, note := syncArc42Section(context.Background(), claude, &arc42SyncFakeRunner{},
		docChangeContext{WorkDir: dir}, arc42Sections[0])
	if changed || !strings.Contains(note, "non-fatal") {
		t.Errorf("claude failure must be non-fatal: changed=%v note=%q", changed, note)
	}
}

func TestSyncArc42SectionNoImpact(t *testing.T) {
	dir := t.TempDir()
	writeArc42SectionFile(t, dir, arc42Sections[0].File)
	claude := &arc42SyncFakeClaude{result: CommandResult{Output: "no changes needed"}}
	changed, note := syncArc42Section(context.Background(), claude, &arc42SyncFakeRunner{statusOutput: "  \n"},
		docChangeContext{WorkDir: dir}, arc42Sections[0])
	if changed || !strings.Contains(note, "no impact") {
		t.Errorf("want no-impact, got changed=%v note=%q", changed, note)
	}
}

func TestSyncReadmeUpdates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# BT Agent Platform\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claude := &arc42SyncFakeClaude{result: CommandResult{Output: "edited"}}
	runner := &arc42SyncFakeRunner{statusOutput: " M README.md\n"}
	changed, note := syncReadme(context.Background(), claude, runner,
		docChangeContext{WorkDir: dir, ChangedFiles: []string{"cmd/bt-agent/tools.go"}, Summary: "new tool"})
	if !changed || !strings.Contains(note, "README") {
		t.Errorf("want README update, got changed=%v note=%q", changed, note)
	}
	if len(claude.prompts) != 1 || !strings.Contains(claude.prompts[0], "README.md") {
		t.Fatal("prompt must target README.md")
	}
}

func TestSyncReadmeSkipsMissingFile(t *testing.T) {
	claude := &arc42SyncFakeClaude{}
	changed, note := syncReadme(context.Background(), claude, &arc42SyncFakeRunner{},
		docChangeContext{WorkDir: t.TempDir()})
	if changed || !strings.Contains(note, "skipped") || len(claude.prompts) != 0 {
		t.Errorf("want skip without claude call, got changed=%v note=%q calls=%d", changed, note, len(claude.prompts))
	}
}
