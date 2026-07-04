package engine

// arc42 documentation sync: every goap-fusion run that lands production Go
// changes also updates docs/arc42/go-bt-evolve-arc42.md in the SAME run
// worktree, between implementation and verification — so the architecture
// documentation lands in the same verified, hook-gated commit as the change
// that made it necessary, instead of drifting for weeks (the doc predated
// the entire goap/superpowers/research-store architecture when this stage
// was introduced).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const arc42DocRelPath = "docs/arc42/go-bt-evolve-arc42.md"

// arc42SyncTimeout bounds the docs Claude pass; docs sync is best-effort and
// must never dominate the cycle budget.
const arc42SyncTimeout = 10 * time.Minute

// arc42AllowedTools keeps the docs pass read-mostly: it may edit only via
// Write/Edit (the prompt restricts it to the arc42 file) and inspect the
// change with read-only git.
const arc42AllowedTools = "Read,Glob,Grep,Edit,Write," +
	"Bash(git diff:*),Bash(git log:*),Bash(git status:*)"

// syncArc42Docs runs the documentation pass in the run worktree. It is
// deliberately non-fatal: it reports what happened (for the finish report)
// and never fails the run — a missed docs update must not discard verified
// implementation work.
func syncArc42Docs(ctx context.Context, claude ClaudeRunner, runner CommandRunner, run *SuperpowersRun) (changed bool, note string) {
	if run == nil || run.Mode == SuperpowersModeDryRun || run.WorktreePath == "" || run.WorktreePath == run.RepoDir {
		return false, ""
	}
	prodFiles := nonTestGoFiles(run.ChangedFiles)
	if len(prodFiles) == 0 {
		return false, "skipped: no production Go changes"
	}
	if _, err := os.Stat(filepath.Join(run.WorktreePath, arc42DocRelPath)); err != nil {
		return false, "skipped: arc42 document not present in worktree"
	}

	var objectives []string
	for _, task := range run.Tasks {
		if task.Status == "done" {
			objectives = append(objectives, "- "+task.Objective)
		}
	}
	diffStat := runner.Run(ctx, run.WorktreePath, "git", "diff", "--stat", "HEAD", "--", ".", ":(exclude)graphify-out/**")

	prompt := fmt.Sprintf(`You are the arc42 documentation maintainer for this repository.

An automated run just implemented the following in this worktree (uncommitted):

Objectives completed:
%s

Changed production files:
- %s

Diff stat:
%s

Update ONLY %s so the architecture documentation reflects these changes:
- Edit only the arc42 sections genuinely affected (building blocks, runtime
  view, crosscutting concepts, architecture decisions, glossary, risks).
- If the change is architecturally significant, add a dated one-paragraph
  entry to "arc42 Section 9 — Architecture Decisions".
- Keep the existing structure, headings, anchors, and table of contents
  exactly as they are; never delete sections.
- Prefer precise small edits over rewrites; do not restate the diff.
- If nothing in the document is affected, change nothing and say so.

Rules: edit no file other than %s. Verify claims against the actual code
with Read/Grep before writing them.`,
		strings.Join(objectives, "\n"),
		strings.Join(prodFiles, "\n- "),
		truncateGoap(diffStat.Output, 3000),
		arc42DocRelPath, arc42DocRelPath)

	cctx, cancel := context.WithTimeout(ctx, arc42SyncTimeout)
	defer cancel()
	res := claude.RunClaude(cctx, run.WorktreePath, prompt)
	_ = os.MkdirAll(filepath.Join(run.ArtifactDir, "verification"), 0o755)
	_ = os.WriteFile(filepath.Join(run.ArtifactDir, "verification", "arc42-sync.txt"), []byte(res.Output), 0o644)
	if res.Err != nil {
		return false, "failed (non-fatal): " + truncateGoap(res.Err.Error(), 200)
	}

	status := runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--", arc42DocRelPath)
	if strings.TrimSpace(status.Output) == "" {
		return false, "no documentation impact"
	}
	run.ChangedFiles = mergeChangedFiles(run.ChangedFiles, []string{arc42DocRelPath})
	return true, "updated " + arc42DocRelPath
}
