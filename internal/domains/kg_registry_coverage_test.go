package domains

import (
	"sort"
	"testing"

	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// TestKnowledgeGraphRegistersAllDomainTrees guards against domain trees going
// invisible to the knowledge graph. knowledge.RecordRun no-ops silently when
// kg.Trees[rec.TreeID] is missing, so any tree returned by AllDomainTrees()
// that isn't registered here has its run outcomes silently dropped —
// invisible to ComputeAnalytics, RegisterDomainFitness, and gardener
// prioritization.
func TestKnowledgeGraphRegistersAllDomainTrees(t *testing.T) {
	kg := knowledge.BuildKnowledgeGraph()
	trees := AllDomainTrees()

	var missing []string
	for name := range trees {
		id := "domain:" + name
		if _, ok := kg.Trees[id]; !ok {
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("knowledge graph missing %d of %d domain trees from AllDomainTrees(): %v",
			len(missing), len(trees), missing)
	}
}

// resolverSpecialCaseTreeIDs mirrors the bare (non-"domain:"-prefixed)
// special-case ID branches in resolveTreeIDWithResolver
// (internal/domains/tree_resolver.go). godev/hermes_evolve/stockfish_evolve/
// stockfish_loop are deliberately excluded: they are already registered under
// their own (non-"domain:") IDs in knowledge.BuildKnowledgeGraph(). This list
// must be kept in sync with tree_resolver.go's special cases by hand — its
// purpose is to catch the next resolver ID that's added without a matching
// registry.go registration.
var resolverSpecialCaseTreeIDs = []string{
	"vault_manager",
	"kanban:task_creator",
	"kanban:refiner",
	"kanban:qa",
	"kanban:monitor",
	"kanban:workflow",
	"kanban:autopilot",
	"notebooklm",
	"notebooklm-consumer",
	"notebooklm-bridge",
	"hermes_obsidian",
	"superpowers_pipeline",
	"fusion",
}

// TestKnowledgeGraphRegistersResolverSpecialCaseTrees guards against the bare
// (non-"domain:"-prefixed) tree IDs that resolveTreeIDWithResolver
// special-cases going invisible to the knowledge graph. Unlike the
// AllDomainTrees()-based test above, these IDs are never returned by
// AllDomainTrees(), so knowledge.RecordRun silently no-ops for every run of
// these trees (e.g. notebooklm-bridge's 4h cron) unless each ID is also
// registered in knowledge.BuildKnowledgeGraph().
func TestKnowledgeGraphRegistersResolverSpecialCaseTrees(t *testing.T) {
	kg := knowledge.BuildKnowledgeGraph()

	var missing []string
	for _, id := range resolverSpecialCaseTreeIDs {
		if _, ok := kg.Trees[id]; !ok {
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("knowledge graph missing %d of %d resolver special-case tree IDs: %v",
			len(missing), len(resolverSpecialCaseTreeIDs), missing)
	}
}
