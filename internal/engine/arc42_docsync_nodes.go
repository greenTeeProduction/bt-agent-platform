package engine

// Registered BT nodes for the per-section arc42 + README documentation sync
// (spec 2026-07-16-arc42-docs-consolidation-design.md): SyncArc42Section01..12
// and SyncReadme, composed by the internal/domains "arc42:docsync" tree. The
// superpowers pipeline calls the same shared engine directly
// (syncArc42SectionsAndReadme) — these nodes exist so any tree / MCP caller
// can run the sync too. Every node succeeds regardless of sync outcome
// (non-fatal contract); the outcome string carries what happened.

import (
	"context"
	"fmt"
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

// Test seams (package-var injection-hook convention).
var (
	arc42SectionSyncFn = syncArc42Section
	arc42ReadmeSyncFn  = syncReadme
	arc42GitDiffFn     = func(ctx context.Context, workDir string) []string {
		res := defaultSuperpowersCommandRunner.Run(ctx, workDir, "git", "diff", "--name-only", "HEAD~1..HEAD")
		var files []string
		for line := range strings.SplitSeq(res.Output, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				files = append(files, line)
			}
		}
		return files
	}
)

// docChangeContextFromBlackboard builds the change context for tree-invoked
// sync nodes: chain state when the caller provided it, the last commit's
// file list otherwise.
func docChangeContextFromBlackboard(bb *Blackboard) docChangeContext {
	chg := docChangeContext{WorkDir: goModuleRoot()}
	if bb != nil {
		if s, ok := bb.ChainState["change_summary"].(string); ok && s != "" {
			chg.Summary = s
		} else {
			chg.Summary = bb.Task
		}
		switch v := bb.ChainState["changed_files"].(type) {
		case []string:
			chg.ChangedFiles = v
		case string:
			for f := range strings.SplitSeq(v, ",") {
				if f = strings.TrimSpace(f); f != "" {
					chg.ChangedFiles = append(chg.ChangedFiles, f)
				}
			}
		}
	}
	if len(chg.ChangedFiles) == 0 {
		chg.ChangedFiles = arc42GitDiffFn(context.Background(), chg.WorkDir)
	}
	return chg
}

func registerArc42SyncNode(name string, run func(chg docChangeContext) (bool, string)) {
	RegisterAction(name, func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		changed, note := run(docChangeContextFromBlackboard(bb))
		if bb.Result != "" {
			bb.Result += "\n"
		}
		bb.Result += note
		if changed {
			bb.Outcome = "arc42_sync_updated"
		} else {
			bb.Outcome = "arc42_sync_no_change"
			// A no-op sync is the healthy steady state — refine so the
			// notification throttle can suppress repeats (same convention as
			// SeedProgramFromArc42Goals's program-active skip).
			bb.OutcomeRefinement = "no_change"
		}
		return 1
	})
}

func init() {
	for _, sec := range arc42Sections {
		registerArc42SyncNode(fmt.Sprintf("SyncArc42Section%02d", sec.Num), func(chg docChangeContext) (bool, string) {
			return arc42SectionSyncFn(context.Background(), defaultSuperpowersClaudeRunner, defaultSuperpowersCommandRunner, chg, sec)
		})
	}
	registerArc42SyncNode("SyncReadme", func(chg docChangeContext) (bool, string) {
		return arc42ReadmeSyncFn(context.Background(), defaultSuperpowersClaudeRunner, defaultSuperpowersCommandRunner, chg)
	})
}
