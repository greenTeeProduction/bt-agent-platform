package engine

// Doc-drift sync: the trees own the documentation. Every superpowers run
// checks the worktree's drift-validated docs (API_REFERENCE.md package
// listing, GETTING_STARTED binaries, TUTORIAL/TROUBLESHOOTING command refs,
// ADR catalog — whatever scripts/check-doc-drift.sh enforces) and, when the
// run's changes introduced drift, writes the missing documentation via a
// bounded Claude pass in the SAME worktree — so docs land in the same
// hook-gated commit as the code that made them necessary.
//
// The quality gate is NOT softened: buildSuperpowersVerificationChecks adds
// a hard "doc-drift" verification check, so a run that leaves documentation
// inconsistent fails verification and never reaches the landing commit.
//
// Regression context 2026-07-09: an external landing added internal/persona
// undocumented; the pre-commit drift check then failed EVERY autonomous
// commit for 16 hours (cycle commits always touch docs/arc42 via the sync
// stage, which arms the hook's docs-changed trigger) while nothing in the
// pipeline could write the missing documentation. With this stage the first
// cycle after such a pull heals the drift itself.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const docDriftScriptRelPath = "scripts/check-doc-drift.sh"

// docDriftSyncTimeout bounds the docs Claude pass, mirroring the per-section
// arc42SectionSyncTimeout family.
const docDriftSyncTimeout = 10 * time.Minute

// superpowersDocDriftFn runs the WORKTREE's own drift checker (its ROOT
// resolves to the script's repo, so invoking the worktree copy validates the
// worktree — the shared pre-commit hook, by contrast, validates the main
// repo's materialized docs). Var for test override.
var superpowersDocDriftFn = func(ctx context.Context, dir string) (output string, ok bool) {
	res := runShellCommand(ctx, defaultSuperpowersCommandRunner, dir, "bash "+docDriftScriptRelPath)
	return res.Output, res.Err == nil
}

// syncDriftDocs checks and, when needed, repairs documentation drift in the
// run worktree. Like syncArc42Docs it never fails the run itself — the hard
// enforcement is the doc-drift verification check that follows — but its
// note surfaces unfixed drift in the run report.
func syncDriftDocs(ctx context.Context, claude ClaudeRunner, runner CommandRunner, run *SuperpowersRun) (changed bool, note string) {
	if run == nil || run.Mode == SuperpowersModeDryRun || run.WorktreePath == "" || run.WorktreePath == run.RepoDir {
		return false, ""
	}
	if _, err := os.Stat(filepath.Join(run.WorktreePath, docDriftScriptRelPath)); err != nil {
		return false, ""
	}

	report, ok := superpowersDocDriftFn(ctx, run.WorktreePath)
	if ok {
		return false, "docs in sync"
	}

	prompt := fmt.Sprintf(`You are the documentation maintainer for this repository.

The documentation drift checker (%s) FAILED in this worktree:

%s

Fix ONLY the reported drift by updating the documentation under docs/ that
the checker validates (for example: add missing packages to the
API_REFERENCE.md index table AND give each a "## Package: <name>" section
matching the existing format; add missing binaries to GETTING_STARTED.md;
fix stale command references; index unlisted ADRs).

Rules:
- Edit ONLY files under docs/. Never edit code to satisfy the checker and
  never edit the checker script itself — the gate must stay strict.
- Ground every documented symbol in the actual code: Read/Grep the package
  before describing it.
- Match the surrounding format exactly (index rows, section layout).
- When done, re-run: bash %s — and iterate until it exits 0.`,
		docDriftScriptRelPath, truncateGoap(report, 3000), docDriftScriptRelPath)

	cctx, cancel := context.WithTimeout(ctx, docDriftSyncTimeout)
	defer cancel()
	res := claude.RunClaude(cctx, run.WorktreePath, prompt)
	_ = os.MkdirAll(filepath.Join(run.ArtifactDir, "verification"), 0o755)
	_ = os.WriteFile(filepath.Join(run.ArtifactDir, "verification", "doc-drift-sync.txt"), []byte(res.Output), 0o644)
	if res.Err != nil {
		return false, "docs pass failed, drift still present (verification will fail): " + truncateGoap(res.Err.Error(), 200)
	}

	if _, ok := superpowersDocDriftFn(ctx, run.WorktreePath); !ok {
		return false, "drift still present after docs pass (verification will fail)"
	}

	status := runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--", "docs/")
	var docFiles []string
	for line := range strings.SplitSeq(status.Output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 2 {
			docFiles = append(docFiles, fields[len(fields)-1])
		}
	}
	if len(docFiles) > 0 {
		run.ChangedFiles = mergeChangedFiles(run.ChangedFiles, docFiles)
	}
	return true, "repaired documentation drift (" + strings.Join(docFiles, ", ") + ")"
}
