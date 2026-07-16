package domains

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Arc42DocsyncTree runs the per-section arc42 + README documentation sync:
// one SyncArc42SectionNN node per section file, then SyncReadme. Every node
// succeeds regardless of sync outcome (the engine's non-fatal contract), so
// the tree is a plain sequence — a Claude failure in §3 never blocks §4.
// The superpowers pipeline runs the same engine directly; this tree exposes
// it to schedules and MCP callers.
func Arc42DocsyncTree() *evolution.SerializableNode {
	titles := []string{
		"Introduction and Goals", "Architecture Constraints", "Context and Scope",
		"Solution Strategy", "Building Block View", "Runtime View",
		"Deployment View", "Crosscutting Concepts", "Architecture Decisions",
		"Quality Requirements", "Risks and Technical Debt", "Glossary",
	}
	children := make([]evolution.SerializableNode, 0, 13)
	for i, title := range titles {
		children = append(children, act(
			fmt.Sprintf("SyncArc42Section%02d", i+1),
			fmt.Sprintf("Update arc42 §%d (%s) if the last change affects it — bounded Claude pass, guideline-constrained", i+1, title)))
	}
	children = append(children, act("SyncReadme",
		"Update README.md counts/links/claims if the last change made them stale"))
	root := seq("Arc42Docsync_Main",
		"Per-section arc42 + README documentation sync (sections are the source of truth; the monolith is retired)",
		children...)
	return &root
}
