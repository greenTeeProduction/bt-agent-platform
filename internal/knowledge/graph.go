// Package knowledge manages the behavior tree knowledge graph — a semantic index
// of all 38+ trees with capabilities, keywords, and cross-tree relationships.
//
// It powers tree discovery (find the best tree for a task), auto-creation (build
// new trees when no match exists), and the tree factory (crossover breeding from
// two parent trees). The knowledge graph supports 7 categories (core, domain,
// finance, research, startup, thinktank, evolution) with weighted capability edges
// between related trees.
//
// Key types:
//   - KnowledgeGraph — the in-memory graph with Query, Discover, Summary, AutoCreate
//   - TreeMeta — metadata for each tree: name, category, capabilities, keywords
//   - Relationship — typed edges between trees (derived_from, similar_to, composed_of)
//
// MCP tools exposed: bt_kg_discover, bt_kg_query, bt_kg_auto_create, bt_kg_summary,
// bt_kg_list, bt_kg_explain, bt_kg_analytics, bt_factory_create.
package knowledge

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TreeMeta describes a behavior tree in the knowledge graph.
type TreeMeta struct {
	ID          string  `json:"id"`          // unique identifier (e.g., "finance:pitch_agent")
	Name        string  `json:"name"`        // human-readable name
	Category    string  `json:"category"`    // finance, domain, research, startup, thinktank, evolution, core
	Description string  `json:"description"` // what it does
	NodeCount   int     `json:"node_count"`  // total nodes
	Fitness     float64 `json:"fitness"`     // runtime-success EMA (0-100), maintained only by genuine executions

	// StructuralFitness is the evolved structural quality of a winning QD/island
	// elite, written back by evolution passes (RecordRun "evolved"). It is kept
	// separate from Fitness so an evolution pass can never overwrite the
	// runtime-success EMA that real executions measure. Selection blends the two,
	// gated by RunCount, so an unproven tree surfaces on structural merit while a
	// well-run tree is judged on its measured runtime success (see
	// blendedSelectionFitness).
	StructuralFitness float64 `json:"structural_fitness,omitempty"`

	// Capabilities — what tasks this tree handles
	Capabilities []Capability `json:"capabilities"`

	// Keywords that trigger this tree
	Keywords []string `json:"keywords"`

	// Relationships to other trees
	Relations []Relation `json:"relations,omitempty"`

	// Dependencies — other trees this tree uses or extends
	DependsOn []string `json:"depends_on,omitempty"`

	// Tags for discovery
	Tags []string `json:"tags,omitempty"`

	// Runtime feedback (updated by RecordRun)
	RunCount     int           `json:"run_count"`     // genuine executions only (drives cold-start confidence weighting)
	EvolvedCount int           `json:"evolved_count"` // synthetic archive write-backs (QD/island elites), counted separately from RunCount
	LastOutcome  string        `json:"last_outcome"`
	LastDuration time.Duration `json:"last_duration"`

	// RecentRuns is a bounded window (maxRunHistory) of this tree's most recent
	// genuine executions, maintained by RecordRun so a registered domain fitness
	// function (see RegisterDomainFitness) has real history to score.
	RecentRuns []RunSummary `json:"recent_runs,omitempty"`

	// Embedding vector for semantic discovery
	Embedding Embedding `json:"embedding,omitempty"`
}

// RunSummary carries the per-run data a registered domain fitness function
// needs to score a tree's recent run history (see RegisterDomainFitness).
type RunSummary struct {
	Outcome string
	Quality float64
}

// maxRunHistory bounds TreeMeta.RecentRuns: old runs age out once a tree's
// history exceeds this many entries, so the window stays representative of
// recent behavior without growing unbounded over a tree's lifetime.
const maxRunHistory = 20

// Capability describes what a tree can do.
type Capability struct {
	Action   string  `json:"action"`             // what it does (e.g., "analyze_financials", "review_code")
	Domain   string  `json:"domain"`             // domain area (e.g., "finance", "engineering", "strategy")
	Strength float64 `json:"strength,omitempty"` // 0-1 how good it is at this (from benchmarks)
}

// Relation describes a connection to another tree.
type Relation struct {
	Target string `json:"target"` // tree ID
	Type   string `json:"type"`   // specializes, composes, replaces, extends, depends_on
}

// KnowledgeGraph maps all behavior trees and their relationships.
// Protected by mu for concurrent read/write from scheduler and MCP tools.
type KnowledgeGraph struct {
	mu       sync.RWMutex
	Trees    map[string]*TreeMeta `json:"trees"`
	Edges    []Edge               `json:"edges"`
	Synonyms map[string]string    `json:"synonyms"` // capability → tree mapping

	// ExpectedDomains is the injectable set of domain tree IDs that CoverageGaps
	// audits against. The daemon populates it at startup from
	// domains.AllDomainTrees() keys (injection avoids the analytics→domains import
	// cycle), keeping CoverageGaps registry-accurate rather than tied to a stale
	// hardcoded slice. Defaults to defaultExpectedDomains when left unset.
	ExpectedDomains []string `json:"expected_domains,omitempty"`

	// feedbackPersist holds debounced-persistence state, guarded by mu. It is
	// unexported so it never lands in the serialized graph.
	feedbackPersist feedbackPersistState

	// domainFitness holds per-tree domain-aware fitness functions registered
	// via RegisterDomainFitness, guarded by mu. Unexported so it never lands in
	// the serialized graph — callers re-register on every process start.
	domainFitness map[string]func(runs []RunSummary) float64
}

// RegisterDomainFitness registers a domain-aware fitness function for treeID.
// Once registered, RecordRun drives that tree's Fitness from fn's output
// (scaled to the 0-100 Fitness range) over the tree's bounded RecentRuns
// window, instead of the generic runtime-success EMA. Trees with no
// registered function keep the existing EMA behavior unchanged.
func (kg *KnowledgeGraph) RegisterDomainFitness(treeID string, fn func(runs []RunSummary) float64) {
	kg.mu.Lock()
	defer kg.mu.Unlock()
	if kg.domainFitness == nil {
		kg.domainFitness = make(map[string]func(runs []RunSummary) float64)
	}
	kg.domainFitness[treeID] = fn
}

// defaultExpectedDomains is the fallback expected-domain set used when the
// daemon has not injected the live registry keys into ExpectedDomains. Kept as a
// safety net so CoverageGaps still reports something meaningful outside the
// daemon; production wiring overrides it from domains.AllDomainTrees().
var defaultExpectedDomains = []string{
	"domain:security_audit", "domain:crash_investigator",
	"domain:data_pipeline", "domain:meeting_notes",
	"domain:refactoring", "domain:devops_ci",
	"domain:trading_signal", "domain:game_ai",
}

// Edge is a directed relationship between two trees.
type Edge struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Type   string  `json:"type"`
	Weight float64 `json:"weight"` // 0-1 relationship strength
}

// NewKnowledgeGraph creates an empty graph.
func NewKnowledgeGraph() *KnowledgeGraph {
	return &KnowledgeGraph{
		Trees:    make(map[string]*TreeMeta),
		Synonyms: make(map[string]string),
	}
}

// Register adds a tree to the knowledge graph.
func (kg *KnowledgeGraph) Register(tree *TreeMeta) {
	kg.mu.Lock()
	defer kg.mu.Unlock()
	kg.Trees[tree.ID] = tree

	// Index keywords as synonyms → tree
	for _, kw := range tree.Keywords {
		kg.Synonyms[strings.ToLower(kw)] = tree.ID
	}
	// Index capabilities as synonyms
	for _, cap := range tree.Capabilities {
		kg.Synonyms[strings.ToLower(cap.Action)] = tree.ID
	}
}

// Connect adds a relationship between two trees. Deduplicates existing edges.
func (kg *KnowledgeGraph) Connect(from, to, relType string) {
	kg.mu.Lock()
	defer kg.mu.Unlock()
	// Check for duplicates
	for _, e := range kg.Edges {
		if e.From == from && e.To == to && e.Type == relType {
			return // already exists
		}
	}
	kg.Edges = append(kg.Edges, Edge{
		From:   from,
		To:     to,
		Type:   relType,
		Weight: 1.0,
	})
}

// EvolvedFitnessImproves reports whether fitness would beat the fitness
// already stored for evolvedID, without mutating the graph. Callers gate an
// expensive disk write on this before attempting it, then call
// RegisterEvolved to commit the bookkeeping only once that write has
// actually succeeded — keeping the knowledge-graph metadata and the tree
// file on disk from ever diverging. An unregistered evolvedID always
// improves, matching RegisterEvolved's own first-registration behavior.
func (kg *KnowledgeGraph) EvolvedFitnessImproves(evolvedID string, fitness float64) bool {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	meta, exists := kg.Trees[evolvedID]
	if !exists {
		return true
	}
	return fitness > meta.StructuralFitness
}

// RegisterEvolved registers (or updates) the winner tree a production
// genetic-evolution tool bred under a derived "<baseID>-evolved" id,
// inheriting the base tree's category/capabilities/keywords so discovery
// treats it like any other tree, connecting it back to the base tree via an
// "evolved_from" edge so DiscoverRelated surfaces it, and stamping its
// structural fitness the same clamped, monotone way RecordRun's "evolved"
// outcome does — without touching the base tree's own fitness. Safe to call
// even when the base tree is unregistered; the evolved tree is still
// created, just without inherited capabilities.
//
// The bookkeeping write-back (NodeCount, EvolvedCount, StructuralFitness) is
// gated on the new fitness actually beating the fitness already stored for
// evolvedID — a weaker later winner leaves the stronger elite's metadata
// untouched. RegisterEvolved reports whether it updated the bookkeeping so
// callers can skip persisting the corresponding tree file when it did not.
// The "evolved_from" edge is still connected either way since the
// relationship between base and evolved tree holds regardless of which
// winner is currently stored.
func (kg *KnowledgeGraph) RegisterEvolved(baseID, evolvedID string, nodeCount int, fitness float64) bool {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	meta, exists := kg.Trees[evolvedID]
	if !exists {
		meta = &TreeMeta{ID: evolvedID, Name: evolvedID, Category: "evolution"}
		if base, ok := kg.Trees[baseID]; ok {
			meta.Category = base.Category
			meta.Capabilities = base.Capabilities
			meta.Keywords = base.Keywords
		}
		kg.Trees[evolvedID] = meta
		for _, kw := range meta.Keywords {
			kg.Synonyms[strings.ToLower(kw)] = meta.ID
		}
		for _, cap := range meta.Capabilities {
			kg.Synonyms[strings.ToLower(cap.Action)] = meta.ID
		}
	}

	kg.connectLocked(baseID, evolvedID, "evolved_from")

	if exists && fitness <= meta.StructuralFitness {
		return false
	}

	meta.NodeCount = nodeCount
	meta.EvolvedCount++
	meta.StructuralFitness = evolvedFitness(meta.StructuralFitness, fitness)
	return true
}

// Discover finds the best tree for a given task description.
// Returns the tree ID and a confidence score (0-1).
//
// The read lock is not held across discoverWithEmbeddings' network
// round-trip to Ollama: a slow or hung embedding backend must not starve
// concurrent Register/Connect/RegisterEvolved writers, which need the write
// lock. discoverWithEmbeddings and stringMatch each take their own RLock
// only around the map access they need.
func (kg *KnowledgeGraph) Discover(task string) (treeID string, confidence float64) {
	kg.mu.RLock()
	hasEmb := kg.hasEmbeddings()
	kg.mu.RUnlock()

	// Phase 1: embedding similarity (if embeddings are available)
	if hasEmb {
		if id, conf := kg.discoverWithEmbeddings(task); id != "" {
			return id, conf
		}
	}

	// Phase 2: keyword + capability matching (fallback)
	return kg.stringMatch(task)
}

// stringMatch is the keyword-based matching (original implementation).
func (kg *KnowledgeGraph) stringMatch(task string) (string, float64) {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	taskLower := strings.ToLower(task)

	// Phase 1: keyword match — deterministic and rank-based. Go randomizes map
	// iteration, so instead of returning the first arbitrary keyword hit at a
	// flat 0.8, collect every hit and pick the longest/most-specific matched
	// keyword. Ties on keyword length break by sorted tree ID, and the score
	// accumulates across every matched keyword that lands on the winning tree.
	type kwHit struct {
		keyword string
		treeID  string
	}
	var hits []kwHit
	for keyword, matchedID := range kg.Synonyms {
		if strings.Contains(taskLower, keyword) {
			if _, ok := kg.Trees[matchedID]; ok {
				hits = append(hits, kwHit{keyword: keyword, treeID: matchedID})
			}
		}
	}
	if len(hits) > 0 {
		sort.Slice(hits, func(i, j int) bool {
			if li, lj := len(hits[i].keyword), len(hits[j].keyword); li != lj {
				return li > lj // longest / most-specific keyword first
			}
			// Equal specificity: blend persisted fitness so the tie resolves
			// toward the fitter tree, mirroring the embedding path's
			// 0.7*sim + 0.3*(fitness/100) selection pressure (see embeddings.go).
			// Fitness is discounted by cold-start confidence so a single lucky
			// run cannot dominate a tree proven across many runs, and the tree's
			// evolved structural fitness is blended in (gated by RunCount) so an
			// unproven-but-archive-improved tree can still surface.
			ti, tj := kg.Trees[hits[i].treeID], kg.Trees[hits[j].treeID]
			if fi, fj := blendedSelectionFitness(ti.Fitness, ti.StructuralFitness, ti.RunCount),
				blendedSelectionFitness(tj.Fitness, tj.StructuralFitness, tj.RunCount); fi != fj {
				return fi > fj // more-trustworthy fitter tree wins the tie
			}
			return hits[i].treeID < hits[j].treeID // final deterministic fallback
		})
		winner := hits[0].treeID
		matched := 0
		for _, h := range hits {
			if h.treeID == winner {
				matched++
			}
		}
		conf := 0.8 + 0.1*float64(matched-1)
		if conf > 1.0 {
			conf = 1.0
		}
		return winner, conf
	}

	// Phase 2: capability overlap scoring. On an exact-score tie, blend persisted
	// fitness so the tie resolves toward the fitter tree — mirroring the embedding
	// path's 0.7*sim + 0.3*(fitness/100) selection pressure (see embeddings.go).
	// Fitness is discounted by cold-start confidence so a single lucky run cannot
	// dominate a tree proven across many runs, and the tree's evolved structural
	// fitness is blended in (gated by RunCount). Fitness only breaks ties (the raw
	// score still gates the 0.3 threshold and sets the returned confidence), and
	// sorted tree ID remains the final deterministic fallback so map iteration
	// order can never decide the winner.
	best := ""
	bestScore := 0.0
	bestFitness := 0.0
	for id, tree := range kg.Trees {
		score := kg.matchScore(taskLower, tree)
		if score <= 0 {
			continue
		}
		wfit := blendedSelectionFitness(tree.Fitness, tree.StructuralFitness, tree.RunCount)
		if best == "" || score > bestScore {
			best, bestScore, bestFitness = id, score, wfit
			continue
		}
		if score == bestScore &&
			(wfit > bestFitness || (wfit == bestFitness && id < best)) {
			best, bestScore, bestFitness = id, score, wfit
		}
	}

	if bestScore > 0.3 {
		return best, bestScore
	}

	return "", 0.0
}

// coldStartPrior is the shrinkage constant in the cold-start confidence
// multiplier: a tree needs roughly this many recorded runs before its fitness
// is trusted at about half strength. Larger → more skeptical of lucky trees.
const coldStartPrior = 10.0

// coldStartConfidence returns a multiplier in (0,1) that grows with the number
// of recorded runs, so a tree's fitness counts more the more times it has
// actually run. A tree with a single lucky run is discounted hard; one proven
// across many runs is trusted near-fully.
//
// The multiplier is (runCount+1)/(runCount+1+coldStartPrior). The +1 keeps it
// strictly positive and — crucially — equal across trees with equal run counts,
// so fitness still fully discriminates two equally-unproven trees (the m2/5
// fitness tie-breaks). Only a genuine gap in run counts applies a discount.
func coldStartConfidence(runCount int) float64 {
	rc := float64(runCount)
	if rc < 0 {
		rc = 0
	}
	return (rc + 1) / (rc + 1 + coldStartPrior)
}

// coldStartWeightedFitness discounts a tree's fitness by its cold-start
// confidence so a single lucky run cannot dominate selection. Shared by
// stringMatch (discovery tie-break) and selectParents (breeding weight), so
// both callers apply the same RunCount-aware selection pressure.
func coldStartWeightedFitness(fitness float64, runCount int) float64 {
	return fitness * coldStartConfidence(runCount)
}

// structuralGate is the weight given to a tree's evolved structural fitness in
// selection. It is the complement of cold-start confidence: at RunCount 0 an
// unproven tree leans almost entirely on its structural (archive-measured)
// fitness, and the gate decays toward 0 as genuine runs accumulate and the
// runtime-success EMA takes over. This is what keeps a structurally-elite but
// runtime-failing tree from dominating forever — real runs steadily reclaim the
// selection signal from the frozen structural score.
func structuralGate(runCount int) float64 {
	return 1 - coldStartConfidence(runCount)
}

// blendedSelectionFitness combines a tree's runtime-success EMA (discounted by
// cold-start confidence) with its evolved structural fitness (gated by the
// complementary weight), so selection blends the two rather than letting an
// evolution pass overwrite the runtime EMA. Shared by stringMatch (discovery
// tie-break) and templateSelectionWeight (breeding weight) so both callers apply
// the same RunCount-aware structural blend. When a tree has no structural
// fitness (the common case) this reduces exactly to coldStartWeightedFitness.
func blendedSelectionFitness(fitness, structural float64, runCount int) float64 {
	return coldStartWeightedFitness(fitness, runCount) + structuralGate(runCount)*structural
}

// matchScore computes how well a tree matches a task.
func (kg *KnowledgeGraph) matchScore(task string, tree *TreeMeta) float64 {
	score := 0.0

	// Keyword matches
	for _, kw := range tree.Keywords {
		if strings.Contains(task, strings.ToLower(kw)) {
			score += 0.2
		}
	}

	// Capability matches
	for _, cap := range tree.Capabilities {
		if strings.Contains(task, strings.ToLower(cap.Action)) {
			score += 0.15 * cap.Strength
		}
		if strings.Contains(task, strings.ToLower(cap.Domain)) {
			score += 0.1 * cap.Strength
		}
	}

	// Category match
	if strings.Contains(task, tree.Category) {
		score += 0.1
	}

	return score
}

// ListByCategory returns all trees in a category.
func (kg *KnowledgeGraph) ListByCategory(category string) []*TreeMeta {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	var result []*TreeMeta
	for _, tree := range kg.Trees {
		if tree.Category == category {
			result = append(result, tree)
		}
	}
	return result
}

// Query returns trees matching a capability.
func (kg *KnowledgeGraph) Query(capability string) []*TreeMeta {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	var result []*TreeMeta
	capLower := strings.ToLower(capability)
	for _, tree := range kg.Trees {
		for _, cap := range tree.Capabilities {
			if strings.Contains(strings.ToLower(cap.Action), capLower) ||
				strings.Contains(strings.ToLower(cap.Domain), capLower) {
				result = append(result, tree)
				break
			}
		}
	}
	return result
}

// Summary returns a human-readable graph summary.
func (kg *KnowledgeGraph) Summary() string {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	categories := make(map[string]int)
	for _, t := range kg.Trees {
		categories[t.Category]++
	}

	s := "Knowledge Graph: "
	first := true
	for cat, count := range categories {
		if !first {
			s += ", "
		}
		s += cat + "(" + strconv.Itoa(count) + ")"
		first = false
	}
	s += " | " + strconv.Itoa(len(kg.Trees)) + " trees, " + strconv.Itoa(len(kg.Edges)) + " edges"
	return s
}

// DiscoverRelated returns trees connected to the given tree via edges.
func (kg *KnowledgeGraph) DiscoverRelated(treeID string) []string {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	seen := map[string]bool{treeID: true}
	results := []string{}

	for _, edge := range kg.Edges {
		if edge.From == treeID && !seen[edge.To] {
			results = append(results, edge.To)
			seen[edge.To] = true
		}
		if edge.To == treeID && !seen[edge.From] {
			results = append(results, edge.From)
			seen[edge.From] = true
		}
	}
	return results
}
