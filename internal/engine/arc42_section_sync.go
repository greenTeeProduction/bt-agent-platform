package engine

// arc42 per-section documentation sync (spec
// 2026-07-16-arc42-docs-consolidation-design.md): every landing that changes
// production Go code updates the affected arc42 SECTION FILES (the monolith
// is retired) through one bounded Claude pass per section, each prompt
// carrying that section's conformance checklist from docs/arc42/GUIDELINES.md.
// All passes are non-fatal by design — the hard gate remains the doc-drift
// verification check.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const arc42GuidelinesRelPath = "docs/arc42/GUIDELINES.md"

// arc42SectionSyncTimeout bounds each per-section Claude pass; sections are
// narrower than the retired whole-document pass (10m), so 5m each.
const arc42SectionSyncTimeout = 5 * time.Minute

// arc42GuidelineFor returns the "## Section N — …" block for sec from the
// worktree's GUIDELINES.md, empty when the file or block is missing (degrade,
// don't wedge — the sync still runs, just without the checklist).
func arc42GuidelineFor(workDir string, sec Arc42Section) string {
	data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(arc42GuidelinesRelPath)))
	if err != nil {
		return ""
	}
	body := string(data)
	marker := fmt.Sprintf("## Section %d ", sec.Num)
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	rest := body[start:]
	if end := strings.Index(rest[1:], "\n## Section "); end >= 0 {
		rest = rest[:end+1]
	}
	return strings.TrimSpace(rest)
}

// docChangeContext describes the change a documentation pass must reflect.
// Decoupled from SuperpowersRun so the SyncArc42SectionNN tree nodes can
// build one from the blackboard / git instead.
type docChangeContext struct {
	ChangedFiles []string
	Summary      string
	WorkDir      string
	ArtifactDir  string // optional; empty disables artifact capture
}

// syncArc42Section runs one bounded, non-fatal Claude pass that updates a
// single arc42 section file when — and only when — the described change
// affects that section. Mirrors the retired whole-document syncArc42Docs.
func syncArc42Section(ctx context.Context, claude ClaudeRunner, runner CommandRunner, chg docChangeContext, sec Arc42Section) (changed bool, note string) {
	relPath := arc42DocsDir + "/" + sec.File
	if _, err := os.Stat(filepath.Join(chg.WorkDir, filepath.FromSlash(relPath))); err != nil {
		return false, fmt.Sprintf("§%d skipped: %s not present", sec.Num, sec.File)
	}
	guideline := arc42GuidelineFor(chg.WorkDir, sec)

	prompt := fmt.Sprintf(`You are the arc42 documentation maintainer for this repository.
The architecture documentation is split into per-section files under %s/.

A change was just made in this working tree:

Summary:
%s

Changed files:
- %s

Your section: arc42 §%d (%s), file %s.

Decide whether this change affects §%d. If it does NOT, change nothing and
say so. If it does, update ONLY %s:
- Follow this section's conformance checklist:
%s
- Content that belongs in a DIFFERENT arc42 section gets a cross-reference
  (e.g. "→ ADR-NNN", "→ §5.2"), never a paragraph here.
- Keep the required headings and keep the generated footer as the LAST line.
- Prefer precise small edits over rewrites; verify claims against the actual
  code with Read/Grep before writing them.

Rules: edit no file other than %s.`,
		arc42DocsDir,
		truncateGoap(chg.Summary, 2000),
		strings.Join(chg.ChangedFiles, "\n- "),
		sec.Num, sec.Title, sec.File,
		sec.Num, relPath,
		guideline,
		relPath)

	cctx, cancel := context.WithTimeout(ctx, arc42SectionSyncTimeout)
	defer cancel()
	res := claude.RunClaude(cctx, chg.WorkDir, prompt)
	if chg.ArtifactDir != "" {
		_ = os.MkdirAll(filepath.Join(chg.ArtifactDir, "verification"), 0o755)
		_ = os.WriteFile(filepath.Join(chg.ArtifactDir, "verification", fmt.Sprintf("arc42-sync-%02d.txt", sec.Num)), []byte(res.Output), 0o644)
	}
	if res.Err != nil {
		return false, fmt.Sprintf("§%d failed (non-fatal): %s", sec.Num, truncateGoap(res.Err.Error(), 200))
	}

	status := runner.Run(ctx, chg.WorkDir, "git", "status", "--short", "--", relPath)
	if strings.TrimSpace(status.Output) == "" {
		return false, fmt.Sprintf("§%d: no impact", sec.Num)
	}
	return true, fmt.Sprintf("§%d: updated %s", sec.Num, sec.File)
}

// syncReadme keeps the hand-written README.md honest after a change: counts,
// links, and feature claims only — same bounded non-fatal contract as the
// per-section passes.
func syncReadme(ctx context.Context, claude ClaudeRunner, runner CommandRunner, chg docChangeContext) (changed bool, note string) {
	if _, err := os.Stat(filepath.Join(chg.WorkDir, "README.md")); err != nil {
		return false, "README skipped: not present"
	}

	prompt := fmt.Sprintf(`You are the README maintainer for this repository.

A change was just made in this working tree:

Summary:
%s

Changed files:
- %s

Check whether README.md is now stale because of this change: hardcoded
counts (trees, tools, packages, test files, ADR counts), links to files
under docs/, and feature claims. If nothing is stale, change nothing and say
so. Otherwise update ONLY README.md with precise small edits; verify every
count and path against the actual repository with Read/Grep/Glob first.

Rules: edit no file other than README.md.`,
		truncateGoap(chg.Summary, 2000),
		strings.Join(chg.ChangedFiles, "\n- "))

	cctx, cancel := context.WithTimeout(ctx, arc42SectionSyncTimeout)
	defer cancel()
	res := claude.RunClaude(cctx, chg.WorkDir, prompt)
	if chg.ArtifactDir != "" {
		_ = os.MkdirAll(filepath.Join(chg.ArtifactDir, "verification"), 0o755)
		_ = os.WriteFile(filepath.Join(chg.ArtifactDir, "verification", "readme-sync.txt"), []byte(res.Output), 0o644)
	}
	if res.Err != nil {
		return false, "README failed (non-fatal): " + truncateGoap(res.Err.Error(), 200)
	}

	status := runner.Run(ctx, chg.WorkDir, "git", "status", "--short", "--", "README.md")
	if strings.TrimSpace(status.Output) == "" {
		return false, "README: no impact"
	}
	return true, "README: updated"
}

// arc42ClassifierTimeout bounds the cheap which-sections-changed call.
const arc42ClassifierTimeout = 2 * time.Minute

// classifyAffectedArc42Sections asks one cheap Claude call which sections a
// change plausibly affects, so a landing runs 1-3 section passes instead of
// 13. ok=false (invalid JSON, error, empty, out-of-range) means the caller
// must degrade to running everything — never skip silently.
func classifyAffectedArc42Sections(ctx context.Context, claude ClaudeRunner, chg docChangeContext) (sections []int, readme bool, ok bool) {
	prompt := fmt.Sprintf(`Given this change to the repository, which arc42 sections (1-12) plausibly
need a documentation update, and does README.md?

Summary: %s
Changed files:
- %s

Sections: 1 intro/goals, 2 constraints, 3 context, 4 strategy, 5 building
blocks, 6 runtime, 7 deployment, 8 crosscutting concepts, 9 decisions (any
architecturally significant change), 10 quality, 11 risks/debt, 12 glossary.

Answer with ONLY a JSON object, no prose: {"sections":[5,9],"readme":false}`,
		truncateGoap(chg.Summary, 1000),
		strings.Join(chg.ChangedFiles, "\n- "))

	cctx, cancel := context.WithTimeout(ctx, arc42ClassifierTimeout)
	defer cancel()
	res := claude.RunClaude(cctx, chg.WorkDir, prompt)
	if res.Err != nil {
		return nil, false, false
	}
	start := strings.Index(res.Output, "{")
	end := strings.LastIndex(res.Output, "}")
	if start < 0 || end <= start {
		return nil, false, false
	}
	var parsed struct {
		Sections []int `json:"sections"`
		Readme   bool  `json:"readme"`
	}
	if err := json.Unmarshal([]byte(res.Output[start:end+1]), &parsed); err != nil {
		return nil, false, false
	}
	seen := map[int]bool{}
	for _, n := range parsed.Sections {
		if n >= 1 && n <= 12 && !seen[n] {
			seen[n] = true
			sections = append(sections, n)
		}
	}
	if len(sections) == 0 && !parsed.Readme {
		return nil, false, false
	}
	sort.Ints(sections)
	return sections, parsed.Readme, true
}

// syncArc42SectionsAndReadme is the pipeline entry point replacing the
// retired whole-monolith syncArc42Docs: classifier-prefiltered per-section
// passes + README, all non-fatal, notes aggregated for run.Arc42Sync.
func syncArc42SectionsAndReadme(ctx context.Context, claude ClaudeRunner, runner CommandRunner, run *SuperpowersRun) (changed bool, note string) {
	if run == nil || run.Mode == SuperpowersModeDryRun || run.WorktreePath == "" || run.WorktreePath == run.RepoDir {
		return false, ""
	}
	prodFiles := nonTestGoFiles(run.ChangedFiles)
	if len(prodFiles) == 0 {
		return false, "skipped: no production Go changes"
	}

	var objectives []string
	for _, task := range run.Tasks {
		if task.Status == "done" {
			objectives = append(objectives, "- "+task.Objective)
		}
	}
	diffStat := runner.Run(ctx, run.WorktreePath, "git", "diff", "--stat", "HEAD", "--", ".", ":(exclude)graphify-out/**")
	chg := docChangeContext{
		ChangedFiles: prodFiles,
		Summary:      "Objectives completed:\n" + strings.Join(objectives, "\n") + "\n\nDiff stat:\n" + truncateGoap(diffStat.Output, 3000),
		WorkDir:      run.WorktreePath,
		ArtifactDir:  run.ArtifactDir,
	}

	selected, readme, ok := classifyAffectedArc42Sections(ctx, claude, chg)
	if !ok {
		selected = nil
		for _, sec := range arc42Sections {
			selected = append(selected, sec.Num)
		}
		readme = true
	}

	var notes []string
	if !ok {
		notes = append(notes, "classifier unavailable — ran all sections")
	}
	for _, num := range selected {
		sec := arc42Sections[num-1]
		secChanged, secNote := arc42SectionSyncFn(ctx, claude, runner, chg, sec)
		notes = append(notes, secNote)
		if secChanged {
			changed = true
			run.ChangedFiles = mergeChangedFiles(run.ChangedFiles, []string{arc42DocsDir + "/" + sec.File})
		}
	}
	if readme {
		rChanged, rNote := arc42ReadmeSyncFn(ctx, claude, runner, chg)
		notes = append(notes, rNote)
		if rChanged {
			changed = true
			run.ChangedFiles = mergeChangedFiles(run.ChangedFiles, []string{"README.md"})
		}
	}
	return changed, strings.Join(notes, "; ")
}
