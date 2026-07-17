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
// Evolution guarantees (RunCycleV2, the single pipeline since ADR-133
// Phase 6): evidence gate (no mutation without reflection records), bloat cap
// (20x original node count), clone-and-prescore candidate isolation, quality
// and validation gates with snapshot rollback.
package gardener

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// TreeEntry is a named tree in the registry with its evolution state.
type TreeEntry struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Tree        *evolution.SerializableNode `json:"-"`
	FilePath    string                      `json:"file_path"`
	Active      bool                        `json:"active"`
	// User marks a personal tree loaded from a user workspace (ADR-133
	// Phase 5). It is the workspace directory name (already sanitized by
	// persona.SanitizeUserID); empty for shared/builtin trees. Personal
	// trees are evaluated on strictly-matching reflections and evolve
	// against the user's own experience bank.
	User string `json:"user,omitempty"`
}

// Registry manages all known behavior trees.
type Registry struct {
	mu        sync.RWMutex
	entries   []TreeEntry
	dir       string
	usersRoot string
}

// NewRegistry creates a registry and loads all known trees.
func NewRegistry(storageDir string) *Registry {
	r := &Registry{dir: storageDir}
	r.loadAll()
	return r
}

// NewRegistryWithUsers creates a registry that additionally scans per-user
// personalization workspaces (<usersRoot>/<user>/trees/tree-*.json, ADR-133
// Phase 5) so personal trees join the 24/7 evolution loop. Snapshots and
// rollback work per tree as usual; SaveTree writes back into the user's own
// workspace.
func NewRegistryWithUsers(storageDir, usersRoot string) *Registry {
	r := &Registry{dir: storageDir, usersRoot: usersRoot}
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
	for name, tree := range evolution.AllFinanceTrees() {
		r.addBuiltin("finance_"+name, evolution.AgentDescriptions[name], tree)
	}

	// Research trees
	for name, tree := range evolution.ResearchTrees() {
		r.addBuiltin("research_"+name, evolution.Descriptions[name], tree)
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
		// Persisted evolved state takes precedence over the compiled-in
		// builtin: SaveTree wrote this file in an earlier cycle, and dropping
		// it on restart would reset evolution progress to zero.
		already := false
		for i := range r.entries {
			if r.entries[i].FilePath == path {
				r.entries[i].Tree = &tree
				already = true
				break
			}
		}
		if !already {
			// Strip both the "tree-" prefix and ".json" suffix so a persisted
			// tree that doesn't shadow a builtin still registers under the
			// bare name addBuiltin would have used (e.g. "tree-foo.json" ->
			// "foo", not "tree-foo") — SaveTree round-trips FilePath using
			// that same bare-name convention.
			treeName := strings.TrimSuffix(strings.TrimPrefix(name, "tree-"), ".json")
			r.entries = append(r.entries, TreeEntry{
				Name:        treeName,
				Description: "Persisted tree",
				Tree:        &tree,
				FilePath:    path,
				Active:      true,
			})
		}
	}

	r.loadUserTreesLocked()
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

// DeactivateAll sets Active=false on all entries in the registry.
// Returns the previous count of active entries.
func (r *Registry) DeactivateAll() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	active := 0
	for i := range r.entries {
		if r.entries[i].Active {
			active++
			r.entries[i].Active = false
		}
	}
	return active
}

// SaveTree persists a tree to its file path.
func (r *Registry) SaveTree(entry TreeEntry) error {
	data, err := json.MarshalIndent(entry.Tree, "", "  ")
	if err != nil {
		return err
	}
	tmp := entry.FilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write tree %q: %w", entry.FilePath, err)
	}
	return os.Rename(tmp, entry.FilePath)
}

// RollbackTree restores name's tree from its milestone-1 pre-mutation
// snapshot in snapshotDir (evolution.RestoreTree) and durably persists the
// restored state via SaveTree, recovering a bad mutation without rerunning a
// full evolution cycle.
func (r *Registry) RollbackTree(name, snapshotDir string) error {
	restored, err := evolution.RestoreTree(name, snapshotDir)
	if err != nil {
		return fmt.Errorf("rollback tree %q: %w", name, err)
	}

	r.mu.Lock()
	idx := -1
	for i := range r.entries {
		if r.entries[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		r.mu.Unlock()
		return fmt.Errorf("rollback tree %q: not found in registry", name)
	}
	r.entries[idx].Tree = restored
	entry := r.entries[idx]
	r.mu.Unlock()

	return r.SaveTree(entry)
}

// --- Metrics ---

// CycleMetrics records the outcome of one evolution cycle for a single tree.
type CycleMetrics struct {
	TreeName    string  `json:"tree_name"`
	Cycle       int     `json:"cycle"`
	Timestamp   int64   `json:"timestamp"`
	BaseFitness float64 `json:"base_fitness"`
	NewFitness  float64 `json:"new_fitness"`
	Delta       float64 `json:"delta"`
	Mutations   int     `json:"mutations_applied"`
	NodesBefore int     `json:"nodes_before"`
	NodesAfter  int     `json:"nodes_after"`
	Improved    bool    `json:"improved"`
	DurationMs  int64   `json:"duration_ms"`
	Rejections  int     `json:"rejections,omitempty"` // quality gate rejections this cycle
	Rollbacks   int     `json:"rollbacks,omitempty"`  // quality gate rollbacks this cycle
	// SkippedNoEvidence marks a tree that carried no reflection records, so
	// the evidence gate skipped mutation (no run-derived fitness gradient).
	SkippedNoEvidence bool `json:"skipped_no_evidence,omitempty"`
	// CrisisIntervention marks a cycle where the crisis detector intervened
	// (emergency mutation-budget boost) for this tree.
	CrisisIntervention bool `json:"crisis_intervention,omitempty"`
	// CrisisIntervened mirrors CrisisIntervention for external observability
	// consumers (metrics/dashboard, Q3 Reliability milestone 1).
	CrisisIntervened bool `json:"crisis_intervened,omitempty"`
	// MutationBudget is the per-cycle mutation budget actually used, boosted
	// above the configured MaxMutations when CrisisIntervened is true.
	MutationBudget int `json:"mutation_budget,omitempty"`
	// DeepSearchUsed marks a cycle that exercised evaluator.IterativeDeepening
	// (Q2 Evolvability milestone 2), gated on a configured
	// Config.TranspositionTablePath.
	DeepSearchUsed bool `json:"deep_search_used,omitempty"`
	// DeepSearchDepth is the deepest ply evaluator.IterativeDeepening reached
	// this cycle. Zero when DeepSearchUsed is false.
	DeepSearchDepth int `json:"deep_search_depth,omitempty"`
	// TTHitRate is the transposition-table probe hit rate
	// (DeepeningResult.TTProbeHits / TTProbes) for this cycle's deep search.
	// Zero when DeepSearchUsed is false.
	TTHitRate float64 `json:"tt_hit_rate,omitempty"`
	// SaveFailed marks a cycle where persisting the evolved tree via
	// Registry.SaveTree failed (Q3 Reliability milestone 3) — the in-memory
	// mutation was applied, but it is not durably saved, so it must not be
	// treated as a successfully persisted result.
	SaveFailed bool `json:"save_failed,omitempty"`
}

// MetricsTracker records and analyzes evolution metrics over time.
type MetricsTracker struct {
	mu      sync.RWMutex
	history []CycleMetrics
	path    string
}

// NewMetricsTracker creates a tracker with persistent storage.
func NewMetricsTracker(dir string) (*MetricsTracker, error) {
	_ = os.MkdirAll(dir, 0755)
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

// metricsDocument is the on-disk shape of gardener-metrics.json: the full
// cycle history plus aggregates that dashboards can read without replaying it.
type metricsDocument struct {
	LastRun                  int64          `json:"last_run"`
	TotalCrisisInterventions int            `json:"total_crisis_interventions"`
	TotalCycles              int            `json:"total_cycles"`
	ActiveTrees              int            `json:"active_trees"`
	BestFitness              float64        `json:"best_fitness"`
	TotalImprovements        int            `json:"total_improvements"`
	TotalDeepSearchCycles    int            `json:"total_deep_search_cycles"`
	AvgTTHitRate             float64        `json:"avg_tt_hit_rate"`
	TotalRollbacks           int            `json:"total_rollbacks"`
	History                  []CycleMetrics `json:"history"`
}

// Save persists metrics to disk.
func (mt *MetricsTracker) Save() error {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	doc := metricsDocument{
		LastRun: time.Now().Unix(),
		History: mt.history,
	}
	trees := make(map[string]bool)
	var ttHitRateSum float64
	for _, m := range mt.history {
		if m.CrisisIntervention {
			doc.TotalCrisisInterventions++
		}
		doc.TotalCycles++
		trees[m.TreeName] = true
		if m.Improved {
			doc.TotalImprovements++
		}
		if m.NewFitness > doc.BestFitness {
			doc.BestFitness = m.NewFitness
		}
		if m.DeepSearchUsed {
			doc.TotalDeepSearchCycles++
			ttHitRateSum += m.TTHitRate
		}
		doc.TotalRollbacks += m.Rollbacks
	}
	doc.ActiveTrees = len(trees)
	if doc.TotalDeepSearchCycles > 0 {
		doc.AvgTTHitRate = math.Round(ttHitRateSum/float64(doc.TotalDeepSearchCycles)*10000) / 10000
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := mt.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write metrics %q: %w", mt.path, err)
	}
	return os.Rename(tmp, mt.path)
}

func (mt *MetricsTracker) load() {
	data, err := os.ReadFile(mt.path)
	if err != nil {
		return
	}
	var doc metricsDocument
	if json.Unmarshal(data, &doc) == nil && doc.History != nil {
		mt.history = doc.History
		return
	}
	// Legacy format: a bare CycleMetrics array without aggregates.
	_ = json.Unmarshal(data, &mt.history)
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
	RefStore       *evolution.Store
	Interval       time.Duration             // how often to wake up
	MaxMutations   int                       // max mutations per cycle per tree
	UseRealLLM     bool                      // use real Ollama for benchmark validation (slow but accurate)
	Gate           *evolution.QualityGate    // quality gate for regression detection
	CrisisDetector *evolution.CrisisDetector // crisis detection & diversity injection
	SnapshotDir    string                    // directory for pre-mutation snapshots
	ValidationGate ValidationGateConfig      // validation gate for decentralized coordination (Gap 5)
	// EvolveWithoutReflections, when false (the default), skips mutation for
	// trees that carry no reflection records: fitness has no run-derived
	// gradient there, so mutating them only burns benchmark compute — 6,973
	// cycles over 2 days improved 0/50 trees, 44 of which never run and hold
	// no reflections. Set true to force blind evolution of every tree.
	EvolveWithoutReflections bool
	// ExperienceBank, when non-nil, records every accepted mutation so its
	// context can be retrieved and reused across cycles and binaries. Nil
	// degrades to the historical no-recording behavior.
	ExperienceBank *evolution.ExperienceBank
	// UserExperienceRoot, when set (usually agent.UsersDir()), gives personal
	// trees per-user experience banks at <root>/<user>/experience (ADR-133
	// Phase 5); empty means every tree shares ExperienceBank.
	UserExperienceRoot string
	// SelectorStatsPath, when set, is the durable Selector telemetry file
	// (written by SelectorOptimizer.SaveSelectorStats) that seeds the
	// learned-ordering pass in evolveTreeV2 when EvolveV2Config.SelectorOrdering
	// is enabled. Empty disables the pass regardless of the flag.
	SelectorStatsPath string
	// MetaValidator, when non-nil, is consulted inside evolveTreeV2's
	// per-candidate acceptance loop after the fitness/quality/SLO gates have
	// already accepted a candidate: a MetaReject decision rejects the
	// candidate regardless of how well it scored on fitness alone, catching
	// structurally broken mutations (empty selectors, unbounded retries,
	// expert-antipattern hits) those gates never inspect. Nil disables the
	// check, preserving the historical fitness-only acceptance behavior.
	MetaValidator *evolution.MetaValidator
	// TranspositionTablePath, when set, is the directory for the Stockfish-style
	// transposition table (evaluator.TranspositionTable) that caches (tree,task)
	// evaluations across cycles. The gardener persists it after every tree in
	// RunCycleV2, alongside MetricsTracker.Save(), so cached evaluations survive
	// gardener restarts instead of only the standalone bt-evaluator binary
	// persisting them (Q2 Evolvability milestone 1). Empty disables the table.
	TranspositionTablePath string
	// KnowledgeGraph, when non-nil, seeds RunCycleV2's per-cycle tree ordering
	// from ComputeAnalytics(): trees flagged as Bottlenecks (low success rate,
	// enough runs to trust) go first, SelectionPressure trees (proven but
	// underbred) go next, and everything else keeps the historical flat
	// alphabetical order — so the daemon's limited per-cycle mutation budget
	// goes to trees that need attention instead of round-robining blindly.
	// Nil preserves the historical alphabetical-only ordering.
	KnowledgeGraph *knowledge.KnowledgeGraph
}

// Gardener is the 24/7 tree evolution agent.
type Gardener struct {
	cfg Config

	// Lazily opened per-user experience banks (see bankFor).
	userBanksMu sync.Mutex
	userBanks   map[string]*evolution.ExperienceBank

	// Lazily opened transposition table (see transpositionTable in evolve_v2.go).
	ttMu sync.Mutex
	tt   *evaluator.TranspositionTable
}

// NewGardener creates a tree gardener.
func NewGardener(cfg Config) *Gardener {
	return &Gardener{cfg: cfg}
}

// The v1 RunCycle/evolveTree pipeline was retired in ADR-133 Phase 6.
// RunCycleV2 (evolve_v2.go) is the single evolution pipeline; the v1 safety
// rails it lacked — evidence gate, bloat cap, crisis detection — were ported
// into evolveTreeV2.

// baseNodeCount returns the expected baseline node count for a tree, used by
// the bloat cap in evolveTreeV2.
func baseNodeCount(name string) int {
	switch {
	case strings.HasPrefix(name, "domain_"):
		return 30
	case strings.HasPrefix(name, "finance_"):
		if strings.Contains(name, "pitch") {
			return 39
		}
		return 27
	case strings.HasPrefix(name, "research_"):
		if strings.Contains(name, "deep") {
			return 54
		}
		return 18
	case name == "godev":
		return 30
	case name == "default":
		return 22
	default:
		return 25
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// CollectAgentSLOs reads all registered SLO metrics from the engine and returns
// a flat map of key metrics per agent. The gardener calls this after each
// evolution cycle to collect and export SLO data.
//
// Keys follow the pattern: <agentName>/<metric>, e.g.:
//
//	"default/success_rate", "default/recovery_rate", "default/avg_latency"
func CollectAgentSLOs() map[string]float64 {
	all := engine.AllSLOMetrics()
	if len(all) == 0 {
		return nil
	}

	result := make(map[string]float64, len(all)*3)
	for key, m := range all {
		prefix := key + "/"
		result[prefix+"success_rate"] = m.SuccessRate()
		result[prefix+"recovery_rate"] = m.RecoveryRate()
		result[prefix+"avg_latency"] = m.AvgLatencyMs()

		fmt.Printf("[gardener] SLO %s\n", m.Summary())
	}
	return result
}
