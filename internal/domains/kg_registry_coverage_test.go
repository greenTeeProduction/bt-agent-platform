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
