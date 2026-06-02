// Package evaluator provides a Stockfish-adapted behavior tree evaluator
// with multi-dimensional fitness scoring (success rate, stability,
// coverage, speed, complexity). It uses transposition tables, killer
// move heuristics, history heuristics, alpha-beta pruning, iterative
// deepening, and late move reductions — all adapted from chess engines
// for behavior tree optimization.
package evaluator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reflection"
)

// --- Multi-dimensional Fitness (Stockfish: Evaluation Function) ---

// FitnessScore evaluates a behavior tree on multiple dimensions, like Stockfish
// evaluates a chess position on material + positional factors.
type FitnessScore struct {
	SuccessRate   float64 `json:"success_rate"`    // 0.0–1.0, most important (like material)
	AvgDurationMs int64   `json:"avg_duration_ms"` // lower is better (like tempo)
	NodeCount     int     `json:"node_count"`      // lower is better (like mobility)
	Stability     float64 `json:"stability"`       // 1/variance of success rate (like king safety)
	PathCoverage  float64 `json:"path_coverage"`   // fraction of strategy paths used (like development)
	Composite     float64 `json:"composite"`       // weighted sum, in centipawns-like scale
}

// EvaluateTree computes a multi-dimensional fitness score for a tree given its history.
func EvaluateTree(tree *evolution.SerializableNode, records []reflection.Record) FitnessScore {
	n := len(records)
	if n == 0 {
		return FitnessScore{
			NodeCount: evolution.CountNodes(tree),
			Composite: 0,
		}
	}

	// Success rate
	successes := 0
	var totalDuration int64
	for _, r := range records {
		if r.Outcome == reflection.Success {
			successes++
		}
		totalDuration += r.DurationMs
	}
	successRate := float64(successes) / float64(n)
	avgDuration := totalDuration / int64(n)

	// Stability: 1 - variance of binary outcomes (high variance = unstable)
	mean := successRate
	variance := 0.0
	for _, r := range records {
		v := 0.0
		if r.Outcome == reflection.Success {
			v = 1.0
		}
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(n)
	stability := 1.0 - variance // 1.0 = perfectly stable, 0.0 = maximally unstable

	// Path coverage: count unique strategy paths used
	pathCoverage := estimatePathCoverage(records)

	// Composite (weighted like Stockfish: material=success, positional=others)
	// Scale: 0–100 "centipawns" where 100 = perfect
	// Node count penalty reduced from 10 to 2 to avoid punishing structural improvements
	composite := successRate*50 +
		stability*15 +
		pathCoverage*15 +
		(1.0-minFloat64(float64(avgDuration)/120000.0, 1.0))*10 +
		(1.0-minFloat64(float64(evolution.CountNodes(tree))/100.0, 1.0))*2

	nodeCount := evolution.CountNodes(tree)

	return FitnessScore{
		SuccessRate:   successRate,
		AvgDurationMs: avgDuration,
		NodeCount:     nodeCount,
		Stability:     stability,
		PathCoverage:  pathCoverage,
		Composite:     composite,
	}
}

func estimatePathCoverage(records []reflection.Record) float64 {
	// Simple heuristic: if plans vary, coverage is higher
	if len(records) < 2 {
		return 0.5
	}
	uniquePlans := make(map[string]bool)
	for _, r := range records {
		// Use first 50 chars of plan as signature
		sig := ""
		if len(r.Plan) > 50 {
			sig = r.Plan[:50]
		} else {
			sig = r.Plan
		}
		uniquePlans[sig] = true
	}
	return float64(len(uniquePlans)) / float64(len(records))
}

// --- Transposition Table (Stockfish: TT) ---

// TranspositionEntry caches an evaluation result for a (tree_hash, task_signature) pair.
type TranspositionEntry struct {
	TreeHash    string  `json:"tree_hash"`
	TaskSig     string  `json:"task_sig"`
	Outcome     string  `json:"outcome"`
	Complexity  string  `json:"complexity"`
	DurationMs  int64   `json:"duration_ms"`
	SuccessRate float64 `json:"success_rate"`
}

// TranspositionTable is a persistent cache mapping (tree, task) → evaluation.
// Like Stockfish's TT, it avoids re-evaluating already-seen positions.
type TranspositionTable struct {
	mu      sync.RWMutex
	entries map[string]TranspositionEntry // key = tree_hash:task_sig
	path    string
	maxSize int
}

// NewTranspositionTable creates or loads a TT from disk.
func NewTranspositionTable(dir string, maxSize int) (*TranspositionTable, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	tt := &TranspositionTable{
		entries: make(map[string]TranspositionEntry),
		path:    filepath.Join(dir, "transposition.json"),
		maxSize: maxSize,
	}
	tt.load()
	return tt, nil
}

// Probe looks up a cached result. Returns false if not found.
func (tt *TranspositionTable) Probe(tree *evolution.SerializableNode, task string) (TranspositionEntry, bool) {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	key := makeKey(tree, task)
	entry, ok := tt.entries[key]
	return entry, ok
}

// Store saves a result in the TT.
func (tt *TranspositionTable) Store(tree *evolution.SerializableNode, task string, entry TranspositionEntry) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	key := makeKey(tree, task)
	entry.TreeHash = hashTree(tree)
	entry.TaskSig = hashTask(task)
	tt.entries[key] = entry

	// Evict oldest if over max
	if len(tt.entries) > tt.maxSize {
		for k := range tt.entries {
			delete(tt.entries, k)
			break
		}
	}
}

// Stats returns entry count.
func (tt *TranspositionTable) Stats() int {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	return len(tt.entries)
}

// Save persists the TT to disk.
func (tt *TranspositionTable) Save() error {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	data, err := json.MarshalIndent(tt.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := tt.path + ".tmp"
	os.WriteFile(tmp, data, 0644)
	return os.Rename(tmp, tt.path)
}

func (tt *TranspositionTable) load() {
	data, err := os.ReadFile(tt.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &tt.entries)
	if len(tt.entries) > tt.maxSize {
		// Trim
		count := 0
		for k := range tt.entries {
			delete(tt.entries, k)
			count++
			if len(tt.entries) <= tt.maxSize {
				break
			}
		}
	}
}

func makeKey(tree *evolution.SerializableNode, task string) string {
	return hashTree(tree) + ":" + hashTask(task)
}

func hashTree(tree *evolution.SerializableNode) string {
	data, _ := json.Marshal(tree)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:16]
}

func hashTask(task string) string {
	h := sha256.Sum256([]byte(task))
	return hex.EncodeToString(h[:])[:12]
}

// --- Move Ordering (Stockfish: killer moves, history heuristic) ---

// MutationCandidate is a proposed mutation with an ordering score.
type MutationCandidate struct {
	Op     evolution.MutationOp `json:"op"`
	Score  float64              `json:"score"` // higher = try first
	Reason string               `json:"reason"`
}

// OrderMutations ranks mutation candidates using Stockfish-like heuristics.
// Priority: 1) wrap_retry on frequently-failing nodes (killer move)
//  2. increase_retries on existing retries (history heuristic)
//  3. add_condition to catch failures early
//  4. prune dead/unreachable nodes
//  5. add_fallback for selectors with few children
func OrderMutations(tree *evolution.SerializableNode, records []reflection.Record, fitness FitnessScore) []MutationCandidate {
	var candidates []MutationCandidate

	// Find nodes that appear in failure plans — wrap them with retry (killer move)
	failureNodes := findFailureNodes(records)
	for _, nodeName := range failureNodes {
		candidates = append(candidates, MutationCandidate{
			Op:     evolution.MutationOp{Operation: "wrap_retry", Target: nodeName},
			Score:  0.9, // highest priority — killer move
			Reason: fmt.Sprintf("node %q appears in failure plans", nodeName),
		})
	}

	// Increase retries on existing Retry nodes (history heuristic)
	if hasNode(tree, "RetrySelfCorrect") {
		candidates = append(candidates, MutationCandidate{
			Op:     evolution.MutationOp{Operation: "increase_retries", Target: "RetrySelfCorrect"},
			Score:  0.7,
			Reason: "increasing retries on existing retry node",
		})
	}

	// Add pre-condition if failures are frequent (early cutoff)
	if fitness.SuccessRate < 0.7 {
		candidates = append(candidates, MutationCandidate{
			Op: evolution.MutationOp{Operation: "add_before", Target: "PreGate", Node: &evolution.SerializableNode{
				Type: "Condition", Name: "CheckConfidence", Description: "Skip if confidence too low",
			}},
			Score:  0.6,
			Reason: fmt.Sprintf("success rate %.0f%% — add early validation", fitness.SuccessRate*100),
		})
	}

	// Prune if node count > 40 (complexity penalty)
	if fitness.NodeCount > 40 {
		candidates = append(candidates, MutationCandidate{
			Op:     evolution.MutationOp{Operation: "prune_node", Target: "CachePath"},
			Score:  0.5,
			Reason: fmt.Sprintf("tree has %d nodes — consider pruning unused paths", fitness.NodeCount),
		})
	}

	// Add fallback to selectors with few children
	selCount := countSelectors(tree)
	if selCount > 0 && float64(selCount)/float64(fitness.NodeCount) < 0.15 {
		candidates = append(candidates, MutationCandidate{
			Op: evolution.MutationOp{Operation: "add_fallback", Target: "OutcomeSelector", Node: &evolution.SerializableNode{
				Type: "Action", Name: "DefaultFallback", Description: "Generic fallback action",
			}},
			Score:  0.4,
			Reason: "selectors have few children — add fallback",
		})
	}

	return candidates
}

// findFailureNodes extracts node names that appear in failure records.
func findFailureNodes(records []reflection.Record) []string {
	seen := make(map[string]bool)
	for _, r := range records {
		if r.Outcome != reflection.Failure {
			continue
		}
		for _, wi := range r.WhatToImprove {
			// Extract key phrases
			for _, node := range []string{"AnalyzeTask", "ExecutePlan", "ReflectOnOutcome", "SelfCorrect"} {
				if containsWord(wi, node) && !seen[node] {
					seen[node] = true
				}
			}
		}
	}
	var result []string
	for node := range seen {
		result = append(result, node)
	}
	return result
}

// --- Iterative Deepening (Stockfish: ID search) ---

// DeepeningResult holds the result of iterative deepening mutation search.
type DeepeningResult struct {
	Depth        int                 `json:"depth"` // how deep we searched
	BaseFitness  FitnessScore        `json:"base_fitness"`
	BestMutation *MutationCandidate  `json:"best_mutation"`
	BestFitness  *FitnessScore       `json:"best_fitness"`
	Candidates   []MutationCandidate `json:"candidates_ordered"`
	PrunedCount  int                 `json:"pruned"`
	TTProbes     int                 `json:"tt_probes"`
	TTProbeHits  int                 `json:"tt_probes_hit"`
}

// IterativeDeepening progressively tests deeper mutations.
// Depth 1: try single mutations
// Depth 2: try pairs of mutations
// Depth 3: try triples
// Like Stockfish, each depth prunes unpromising branches.
func IterativeDeepening(
	tree *evolution.SerializableNode,
	records []reflection.Record,
	tt *TranspositionTable,
	maxDepth int,
) DeepeningResult {
	baseFitness := EvaluateTree(tree, records)
	candidates := OrderMutations(tree, records, baseFitness)

	result := DeepeningResult{
		BaseFitness: baseFitness,
		Candidates:  candidates,
	}

	// Alpha-beta style pruning: best score so far (alpha)
	alpha := baseFitness.Composite

	for depth := 1; depth <= maxDepth; depth++ {
		result.Depth = depth

		// At each depth, try combinations of `depth` mutations
		combos := generateCombos(candidates, depth)
		for _, combo := range combos {
			// Clone tree and apply mutations
			clone := cloneTree(tree)
			for _, c := range combo {
				evolution.ApplyMutations(clone, []evolution.MutationOp{c.Op})
			}

			// Transposition table probe
			ttKey := hashTree(clone) + ":eval"
			result.TTProbes++
			if entry, ok := tt.Probe(tree, ttKey); ok {
				result.TTProbeHits++
				if entry.SuccessRate > alpha/100 {
					alpha = entry.SuccessRate * 100
				}
				continue
			}

			// Prune: if node count exploded, skip
			if evolution.CountNodes(clone) > 2*evolution.CountNodes(tree) {
				result.PrunedCount++
				continue
			}

			// Prune: if tree is too deep (>10 levels), skip
			if treeMaxDepth(clone, 0) > 10 {
				result.PrunedCount++
				continue
			}

			// Evaluate the hypothetical tree
			hypFitness := EvaluateTree(clone, records)
			tt.Store(clone, ttKey, TranspositionEntry{
				SuccessRate: hypFitness.Composite / 100,
			})

			if hypFitness.Composite > alpha {
				alpha = hypFitness.Composite
				result.BestFitness = &hypFitness
				if len(combo) > 0 {
					result.BestMutation = &combo[0]
				}
			}
		}
	}

	return result
}

// --- Helpers ---

func cloneTree(tree *evolution.SerializableNode) *evolution.SerializableNode {
	data, _ := json.Marshal(tree)
	var clone evolution.SerializableNode
	json.Unmarshal(data, &clone)
	return &clone
}

func generateCombos(candidates []MutationCandidate, depth int) [][]MutationCandidate {
	if depth > len(candidates) {
		depth = len(candidates)
	}
	if depth == 0 {
		return nil
	}
	// Simple: just take top `depth` candidates
	var result [][]MutationCandidate
	for i := 0; i <= len(candidates)-depth; i++ {
		combo := candidates[i : i+depth]
		result = append(result, append([]MutationCandidate{}, combo...))
	}
	return result
}

func hasNode(tree *evolution.SerializableNode, name string) bool {
	if tree.Name == name {
		return true
	}
	for i := range tree.Children {
		if hasNode(&tree.Children[i], name) {
			return true
		}
	}
	return false
}

func countSelectors(tree *evolution.SerializableNode) int {
	count := 0
	if tree.Type == "Selector" {
		count++
	}
	for i := range tree.Children {
		count += countSelectors(&tree.Children[i])
	}
	return count
}

func treeMaxDepth(tree *evolution.SerializableNode, depth int) int {
	current := depth + 1
	for i := range tree.Children {
		d := treeMaxDepth(&tree.Children[i], current)
		if d > current {
			current = d
		}
	}
	return current
}

func containsWord(s, word string) bool {
	return len(s) >= len(word) && (s == word ||
		(len(s) > len(word) && (s[:len(word)] == word || s[len(s)-len(word):] == word)))
}

func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
