// Package gardener implements the 24/7 tree evolution daemon that continuously
// improves all registered behavior trees through benchmark-validated mutations.
//
// It runs 5-minute evolution cycles across 25 trees, using Stockfish-style move
// ordering to rank mutation candidates. Each candidate is validated against
// domain-specific benchmark suites before application. The gardener tracks
// per-tree metrics (cycles, mutations applied, composite fitness) and persists
// cycle results to a shared log.
//
// Key types:
//   - Gardener — orchestrates evolution cycles with configurable interval
//   - Registry — manages the set of active trees being evolved
//   - MetricsTracker — per-tree cycle counts, mutation history, fitness scores
//   - Config — cycle interval, mutation cap, benchmark validation, real-LLM flag
//
// Evolution guarantees: idempotency guards (no duplicate nodes), retry cap (15),
// node cap (20x original), neutral mutation acceptance (score >= 0 passes).
package gardener

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/finance"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"
	"github.com/nico/go-bt-evolve/internal/research"
)

// TreeEntry is a named tree in the registry with its evolution state.
type TreeEntry struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Tree        *evolution.SerializableNode `json:"-"`
	FilePath    string                     `json:"file_path"`
	Active      bool                       `json:"active"`
}

// Registry manages all known behavior trees.
type Registry struct {
	mu      sync.RWMutex
	entries []TreeEntry
	dir     string
}

// NewRegistry creates a registry and loads all known trees.
func NewRegistry(storageDir string) *Registry {
	r := &Registry{dir: storageDir}
	r.loadAll()
	return r
}

// loadAll loads default, domain, and any persisted trees from disk.
func (r *Registry) loadAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = nil

	// Default trees (built-in)
	r.addBuiltin("default", "General-purpose BT agent", evolution.DefaultTree())
	r.addBuiltin("godev", "Go software developer BT", evolution.GoDeveloperTree())

	// Finance trees
	for name, tree := range finance.AllFinanceTrees() {
		r.addBuiltin("finance_"+name, finance.AgentDescriptions[name], tree)
	}

	// Research trees
	for name, tree := range research.ResearchTrees() {
		r.addBuiltin("research_"+name, research.Descriptions[name], tree)
	}

	// Domain trees
	for name, tree := range domains.AllDomainTrees() {
		r.addBuiltin("domain_"+name, domains.Descriptions[name], tree)
	}

	// Load persisted trees from disk (tree-<name>.json files only)
	entries, _ := os.ReadDir(r.dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Only load files matching tree-*.json pattern, skip reflections, transpositions
		if !strings.HasPrefix(name, "tree-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(r.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var tree evolution.SerializableNode
		if json.Unmarshal(data, &tree) != nil {
			continue
		}
		// Check if already added as builtin
		already := false
		for _, entry := range r.entries {
			if entry.FilePath == path {
				already = true
				break
			}
		}
		if !already {
			treeName := name[:len(name)-5] // strip .json
			r.entries = append(r.entries, TreeEntry{
				Name:        treeName,
				Description: "Persisted tree",
				Tree:        &tree,
				FilePath:    path,
				Active:      true,
			})
		}
	}
}

func (r *Registry) addBuiltin(name, desc string, tree *evolution.SerializableNode) {
	path := filepath.Join(r.dir, "tree-"+name+".json")
	r.entries = append(r.entries, TreeEntry{
		Name:        name,
		Description: desc,
		Tree:        tree,
		FilePath:    path,
		Active:      true,
	})
}

// List returns all registered trees.
func (r *Registry) List() []TreeEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]TreeEntry, len(r.entries))
	copy(result, r.entries)
	return result
}

// Count returns the number of registered trees.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// SaveTree persists a tree to its file path.
func (r *Registry) SaveTree(entry TreeEntry) error {
	data, err := json.MarshalIndent(entry.Tree, "", "  ")
	if err != nil {
		return err
	}
	tmp := entry.FilePath + ".tmp"
	os.WriteFile(tmp, data, 0644)
	return os.Rename(tmp, entry.FilePath)
}

// --- Metrics ---

// CycleMetrics records the outcome of one evolution cycle for a single tree.
type CycleMetrics struct {
	TreeName     string  `json:"tree_name"`
	Cycle        int     `json:"cycle"`
	Timestamp    int64   `json:"timestamp"`
	BaseFitness  float64 `json:"base_fitness"`
	NewFitness   float64 `json:"new_fitness"`
	Delta        float64 `json:"delta"`
	Mutations    int     `json:"mutations_applied"`
	NodesBefore  int     `json:"nodes_before"`
	NodesAfter   int     `json:"nodes_after"`
	Improved     bool    `json:"improved"`
	DurationMs   int64   `json:"duration_ms"`
	Rejections   int     `json:"rejections,omitempty"`   // quality gate rejections this cycle
	Rollbacks    int     `json:"rollbacks,omitempty"`     // quality gate rollbacks this cycle
}

// MetricsTracker records and analyzes evolution metrics over time.
type MetricsTracker struct {
	mu      sync.RWMutex
	history []CycleMetrics
	path    string
}

// NewMetricsTracker creates a tracker with persistent storage.
func NewMetricsTracker(dir string) (*MetricsTracker, error) {
	os.MkdirAll(dir, 0755)
	mt := &MetricsTracker{path: filepath.Join(dir, "gardener-metrics.json")}
	mt.load()
	return mt, nil
}

// Record adds a cycle metric.
func (mt *MetricsTracker) Record(m CycleMetrics) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.history = append(mt.history, m)
	if len(mt.history) > 10000 {
		mt.history = mt.history[len(mt.history)-5000:]
	}
}

// Save persists metrics to disk.
func (mt *MetricsTracker) Save() error {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	data, _ := json.MarshalIndent(mt.history, "", "  ")
	tmp := mt.path + ".tmp"
	os.WriteFile(tmp, data, 0644)
	return os.Rename(tmp, mt.path)
}

func (mt *MetricsTracker) load() {
	data, _ := os.ReadFile(mt.path)
	json.Unmarshal(data, &mt.history)
}

// CyclesForTree returns how many cycles a specific tree has been processed.
func (mt *MetricsTracker) CyclesForTree(name string) int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	count := 0
	for _, m := range mt.history {
		if m.TreeName == name {
			count++
		}
	}
	return count
}

// Summary returns aggregate metrics.
func (mt *MetricsTracker) Summary() map[string]interface{} {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	if len(mt.history) == 0 {
		return map[string]interface{}{"cycles": 0}
	}

	byTree := make(map[string][]CycleMetrics)
	for _, m := range mt.history {
		byTree[m.TreeName] = append(byTree[m.TreeName], m)
	}

	type treeStats struct {
		Cycles       int     `json:"cycles"`
		BestFitness  float64 `json:"best_fitness"`
		LastFitness  float64 `json:"last_fitness"`
		Improvements int     `json:"improvements"`
		TotalDelta   float64 `json:"total_delta"`
	}
	perTree := make(map[string]treeStats)
	totalCycles := 0
	totalImprovements := 0

	for name, cycles := range byTree {
		ts := treeStats{Cycles: len(cycles)}
		if len(cycles) > 0 {
			ts.LastFitness = cycles[len(cycles)-1].NewFitness
		}
		for _, c := range cycles {
			totalCycles++
			if c.Improved {
				ts.Improvements++
				totalImprovements++
				ts.TotalDelta += c.Delta
			}
			if c.NewFitness > ts.BestFitness {
				ts.BestFitness = c.NewFitness
			}
		}
		perTree[name] = ts
	}

	return map[string]interface{}{
		"total_cycles":       totalCycles,
		"total_improvements": totalImprovements,
		"improvement_rate":   fmt.Sprintf("%.1f%%", float64(totalImprovements)/float64(maxInt(totalCycles, 1))*100),
		"unique_trees":       len(byTree),
		"per_tree":           perTree,
	}
}

// --- Gardener ---

// Config for the tree gardener agent.
type Config struct {
	Registry       *Registry
	MetricsTracker *MetricsTracker
	RefStore       *reflection.Store
	TT             *evaluator.TranspositionTable
	Interval       time.Duration // how often to wake up
	MaxMutations   int           // max mutations per cycle per tree
	UseRealLLM     bool          // use real Ollama for benchmark validation (slow but accurate)
	Gate           *evolution.QualityGate // quality gate for regression detection
	SnapshotDir    string                 // directory for pre-mutation snapshots
}

// Gardener is the 24/7 tree evolution agent.
type Gardener struct {
	cfg Config
}

// NewGardener creates a tree gardener.
func NewGardener(cfg Config) *Gardener {
	return &Gardener{cfg: cfg}
}

// RunCycle executes one full evolution cycle over all trees.
func (g *Gardener) RunCycle() ([]CycleMetrics, error) {
	entries := g.cfg.Registry.List()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	var results []CycleMetrics

	for _, entry := range entries {
		if !entry.Active {
			continue
		}

		start := time.Now()
		metrics := g.evolveTree(entry)
		metrics.DurationMs = time.Since(start).Milliseconds()
		results = append(results, metrics)

		g.cfg.MetricsTracker.Record(metrics)
	}

	g.cfg.MetricsTracker.Save()
	return results, nil
}

// evolveTree runs one evolution pass on a single tree.
func (g *Gardener) evolveTree(entry TreeEntry) CycleMetrics {
	tree := entry.Tree
	if tree == nil {
		return CycleMetrics{TreeName: entry.Name, Improved: false}
	}

	records, _ := g.cfg.RefStore.LoadAll()
	baseFitness := evaluator.EvaluateTree(tree, records)
	nodesBefore := evolution.CountNodes(tree)
	rejections := 0
	rollbacks := 0

	// Only cap at extreme bloat (20x original) — trees should be allowed to grow
	baseNodes := baseNodeCount(entry.Name)
	if nodesBefore > baseNodes*20 {
		return CycleMetrics{
			TreeName: entry.Name, Improved: false,
			BaseFitness: baseFitness.Composite, NewFitness: baseFitness.Composite,
			NodesBefore: nodesBefore, NodesAfter: nodesBefore,
		}
	}

	candidates := evaluator.OrderMutations(tree, records, baseFitness)

	// Validate mutations via benchmark before applying
	suite := benchmark.SuiteForTree(entry.Name)
	var llm llm.LLM
	if g.cfg.UseRealLLM {
		llm = benchmark.DefaultLLM()
	} else {
		llm = benchmark.DefaultMock()
	}

	// Snapshot tree before mutations for potential rollback.
	// Only snapshot if we have a quality gate and snapshot dir configured.
	var snapshotTaken bool
	if g.cfg.Gate != nil && g.cfg.SnapshotDir != "" {
		if _, err := evolution.SnapshotTree(tree, entry.Name, g.cfg.SnapshotDir); err == nil {
			snapshotTaken = true
		}
	}

	applied := 0
	for i := 0; i < len(candidates) && applied < g.cfg.MaxMutations; i++ {
		// Idempotency guards
		if candidates[i].Op.Operation == "add_before" && hasNodeNamed(tree, "CheckConfidence") { continue }
		if candidates[i].Op.Operation == "wrap_retry" && isNodeWrapped(tree, candidates[i].Op.Target) { continue }
		if candidates[i].Op.Operation == "increase_retries" && getRetryCount(tree, candidates[i].Op.Target) >= 15 { continue }
		if candidates[i].Op.Operation == "add_fallback" && hasChildNamed(tree, candidates[i].Op.Target, "DefaultFallback") { continue }

		if candidates[i].Score < 0.2 { break }

		// Only apply if benchmark says it helps (quick 2-task validation for speed)
		score := benchmark.QuickValidate(tree, suite, llm, []evolution.MutationOp{candidates[i].Op})
		if score < 0 {
			continue // skip mutations that regress benchmark results
		}

		if evolution.ApplyMutations(tree, []evolution.MutationOp{candidates[i].Op}) > 0 {
			applied++
		}
	}

	// Quality gate: validate that mutations didn't cause regression.
	// Runs after all mutations are applied to check the combined effect.
	if applied > 0 && g.cfg.Gate != nil {
		postFitness := evaluator.EvaluateTree(tree, records)
		result := g.cfg.Gate.Validate(baseFitness.Composite, postFitness.Composite)

		switch result {
		case evolution.GateRejected:
			// Revert all mutations — restore from snapshot
			rejections = applied
			applied = 0
			if snapshotTaken {
				if restored, err := evolution.RestoreTree(entry.Name, g.cfg.SnapshotDir); err == nil {
					*entry.Tree = *restored
					tree = entry.Tree
				}
			}
		case evolution.GateRollback:
			// Regression detected — rollback to pre-mutation snapshot
			rollbacks = applied
			applied = 0
			if snapshotTaken {
				if restored, err := evolution.RestoreTree(entry.Name, g.cfg.SnapshotDir); err == nil {
					*entry.Tree = *restored
					tree = entry.Tree
				}
			}
		case evolution.GateAccepted:
			// Passed — persist as normal
		}
	}

	if applied > 0 {
		g.cfg.Registry.SaveTree(TreeEntry{Name: entry.Name, Tree: tree, FilePath: entry.FilePath})
		// Sync to tree.json so bt-agent picks up mutations on restart
		treeJSONPath := filepath.Join(g.cfg.Registry.dir, "tree.json")
		data, _ := json.MarshalIndent(tree, "", "  ")
		tmp := treeJSONPath + ".tmp"
		os.WriteFile(tmp, data, 0644)
		os.Rename(tmp, treeJSONPath)
	}

	newFitness := evaluator.EvaluateTree(tree, records)
	nodesAfter := evolution.CountNodes(tree)

	return CycleMetrics{
		TreeName: entry.Name, Timestamp: time.Now().Unix(),
		BaseFitness: baseFitness.Composite, NewFitness: newFitness.Composite,
		Delta: newFitness.Composite - baseFitness.Composite,
		Mutations: applied, NodesBefore: nodesBefore, NodesAfter: nodesAfter,
		Improved: applied > 0,
		Rejections: rejections, Rollbacks: rollbacks,
	}
}

func baseNodeCount(name string) int {
	switch {
	case strings.HasPrefix(name, "domain_"): return 30
	case strings.HasPrefix(name, "finance_"): 
		if strings.Contains(name, "pitch") { return 39 }
		return 27
	case strings.HasPrefix(name, "research_"):
		if strings.Contains(name, "deep") { return 54 }
		return 18
	case name == "godev": return 30
	case name == "default": return 22
	default: return 25
	}
}

func getRetryCount(tree *evolution.SerializableNode, name string) int {
	var find func(n *evolution.SerializableNode) int
	find = func(n *evolution.SerializableNode) int {
		if n.Name == name && n.Type == "Retry" { return n.MaxRetries }
		for i := range n.Children {
			if r := find(&n.Children[i]); r > 0 { return r }
		}
		return 0
	}
	return find(tree)
}

func hasNodeNamed(tree *evolution.SerializableNode, name string) bool {
	if tree.Name == name { return true }
	for i := range tree.Children {
		if hasNodeNamed(&tree.Children[i], name) { return true }
	}
	return false
}

// hasChildNamed checks if a node with the given name has a direct child with childName.
func hasChildNamed(tree *evolution.SerializableNode, parentName, childName string) bool {
	if tree.Name == parentName {
		for i := range tree.Children {
			if tree.Children[i].Name == childName { return true }
		}
	}
	for i := range tree.Children {
		if hasChildNamed(&tree.Children[i], parentName, childName) { return true }
	}
	return false
}

func isNodeWrapped(tree *evolution.SerializableNode, name string) bool {
	for i := range tree.Children {
		if tree.Children[i].Type == "Retry" && len(tree.Children[i].Children) > 0 && tree.Children[i].Children[0].Name == name {
			return true
		}
	}
	for i := range tree.Children {
		if isNodeWrapped(&tree.Children[i], name) { return true }
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
