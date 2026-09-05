package gardener

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestReviewFinalGateRejectsReorderedCandidate(t *testing.T) {
	for _, mutations := range []int{0, 2} {
		t.Run(string(rune('0'+mutations)), func(t *testing.T) {
			root := t.TempDir()
			stats := filepath.Join(root, "stats.json")
			seedSelectorStats(t, stats)
			refs, err := evolution.NewStore(filepath.Join(root, "refs"))
			if err != nil {
				t.Fatal(err)
			}
			metrics, err := NewMetricsTracker(filepath.Join(root, "metrics"))
			if err != nil {
				t.Fatal(err)
			}
			tree := selectorOrderingTree()
			original := cloneTreeForGardener(tree)
			path := filepath.Join(root, "tree.json")
			reg := &Registry{dir: root, entries: []TreeEntry{{Name: "review-final-gate", Tree: tree, FilePath: path, Active: true}}}
			gate := DefaultValidationGateConfig()
			gate.EvidencePath = filepath.Join(root, "absent-evidence.json")
			g := NewGardener(Config{Registry: reg, MetricsTracker: metrics, RefStore: refs, MaxMutations: mutations, EvolveWithoutReflections: true, SelectorStatsPath: stats, ValidationGate: gate})
			g.evolveTreeV2(reg.List()[0], EvolveV2Config{CascadeCfg: evaluator.CascadeConfig{QuickThreshold: 0}, SelectorOrdering: true})
			if !reflect.DeepEqual(tree, original) {
				t.Error("rejected candidate changed live tree")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("rejected candidate persisted: %v", err)
			}
		})
	}
}
