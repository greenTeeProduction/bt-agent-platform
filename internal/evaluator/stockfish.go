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
	"sort"
	"strings"
	"sync"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// --- Multi-dimensional Fitness (Stockfish: Evaluation Function) ---

// FitnessScore evaluates a behavior tree on multiple dimensions, like Stockfish
// evaluates a chess position on material + positional factors.
type FitnessScore struct {
	SuccessRate       float64 `json:"success_rate"`       // 0.0–1.0, most important (like material)
	AvgDurationMs     int64   `json:"avg_duration_ms"`    // lower is better (like tempo)
	NodeCount         int     `json:"node_count"`         // lower is better (like mobility)
	Stability         float64 `json:"stability"`          // 1/variance of success rate (like king safety)
	PathCoverage      float64 `json:"path_coverage"`      // fraction of strategy paths used (like development)
	StructuralQuality float64 `json:"structural_quality"` // static safeguards/tooling quality, 0.0-1.0
	Composite         float64 `json:"composite"`          // weighted sum, in centipawns-like scale
}

// EvaluateTree computes a multi-dimensional fitness score for a tree given its history.
func EvaluateTree(tree *evolution.SerializableNode, records []evolution.Record) FitnessScore {
	n := len(records)
	if n == 0 {
		return FitnessScore{
			NodeCount:         evolution.CountNodes(tree),
			StructuralQuality: estimateStructuralQuality(tree),
			Composite:         0,
		}
	}

	// Success rate
	successes := 0
	var totalDuration int64
	for _, r := range records {
		if r.Outcome == evolution.Success {
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
		if r.Outcome == evolution.Success {
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
	// Scale: 0–100 "centipawns" where 100 = perfect.
	// Static structural quality rewards mutations that add verifiable safeguards,
	// retry bounds, tool access, and prompt discipline before new outcome data exists.
	structuralQuality := estimateStructuralQuality(tree)
	composite := successRate*50 +
		stability*15 +
		pathCoverage*15 +
		(1.0-minFloat64(float64(avgDuration)/120000.0, 1.0))*10 +
		structuralQuality*8 +
		(1.0-minFloat64(float64(evolution.CountNodes(tree))/100.0, 1.0))*2

	nodeCount := evolution.CountNodes(tree)

	return FitnessScore{
		SuccessRate:       successRate,
		AvgDurationMs:     avgDuration,
		NodeCount:         nodeCount,
		Stability:         stability,
		PathCoverage:      pathCoverage,
		StructuralQuality: structuralQuality,
		Composite:         composite,
	}
}

func estimatePathCoverage(records []evolution.Record) float64 {
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

func estimateStructuralQuality(tree *evolution.SerializableNode) float64 {
	if tree == nil {
		return 0
	}
	var total, validation, retry, prompt, tools, selector float64
	walkNodes(tree, func(n *evolution.SerializableNode) {
		total++
		switch n.Type {
		case "Condition":
			if isValidationGate(n.Name) {
				validation++
			}
		case "Retry":
			if n.MaxRetries > 0 && n.MaxRetries <= 5 && len(n.Children) > 0 {
				retry++
			}
		case "ChainAction":
			if hasVerifiedPrompt(n) {
				prompt++
			}
			if hasUsefulTooling(n) || hasAdequateIterations(n) {
				tools++
			}
		case "Selector":
			if len(n.Children) >= 2 || hasNode(n, "DefaultFallback") || hasNode(n, "OutcomeSelector") {
				selector++
			}
		}
	})
	if total == 0 {
		return 0
	}
	chainCount := len(findChainAgentNodes(tree))
	selectorCount := countSelectors(tree)
	score := 0.0
	if validation > 0 {
		score += 0.25
	}
	if retry > 0 {
		score += 0.20
	}
	if chainCount == 0 || prompt > 0 {
		score += 0.20
	}
	if chainCount == 0 || tools > 0 {
		score += 0.20
	}
	if selectorCount == 0 || selector > 0 {
		score += 0.15
	}
	return minFloat64(score, 1.0)
}

func isValidationGate(name string) bool {
	switch name {
	case "HasClearTask", "CheckConfidence", "ValidateInput", "CheckPrerequisites", "WasSuccessful":
		return true
	default:
		return false
	}
}

func hasVerifiedPrompt(n *evolution.SerializableNode) bool {
	if n == nil || n.Metadata == nil {
		return false
	}
	sysMsg, _ := n.Metadata["system_msg"].(string)
	lower := strings.ToLower(sysMsg)
	return strings.Contains(lower, "verify") || strings.Contains(lower, "real data") || strings.Contains(lower, "never fabricate") || strings.Contains(lower, "tool output")
}

func hasUsefulTooling(n *evolution.SerializableNode) bool {
	if n == nil || n.Metadata == nil {
		return false
	}
	switch tools := n.Metadata["tools"].(type) {
	case []any:
		return len(tools) > 0
	case []string:
		return len(tools) > 0
	default:
		return false
	}
}

func hasAdequateIterations(n *evolution.SerializableNode) bool {
	if n == nil || n.Metadata == nil {
		return false
	}
	if maxIter, ok := n.Metadata["max_iterations"].(float64); ok {
		return maxIter >= 10
	}
	if maxIter, ok := n.Metadata["max_iterations"].(int); ok {
		return maxIter >= 10
	}
	return false
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
func OrderMutations(tree *evolution.SerializableNode, records []evolution.Record, fitness FitnessScore) []MutationCandidate {
	var candidates []MutationCandidate

	failurePressure := 1.0 - fitness.SuccessRate
	if failurePressure < 0 {
		failurePressure = 0
	}

	// Highest-value mutation for stuck trees: reject unclear work before expensive execution.
	if fitness.SuccessRate < 0.7 && hasNode(tree, "PreGate") && !hasNode(tree, "HasClearTask") {
		candidates = append(candidates, MutationCandidate{
			Op: evolution.MutationOp{Operation: "add_before", Target: "PreGate", Node: &evolution.SerializableNode{
				Type: "Condition", Name: "HasClearTask", Description: "Reject empty, gibberish, or underspecified tasks before execution",
			}},
			Score:  0.92 + failurePressure*0.05,
			Reason: fmt.Sprintf("success rate %.0f%% — add clear-task validation before routing", fitness.SuccessRate*100),
		})
	}

	// Find nodes that appear in failure plans — wrap them with retry (killer move)
	failureNodes := findFailureNodes(records)
	for _, nodeName := range failureNodes {
		if !hasNode(tree, nodeName) {
			continue
		}
		candidates = append(candidates, MutationCandidate{
			Op:     evolution.MutationOp{Operation: "wrap_retry", Target: nodeName},
			Score:  0.84 + failurePressure*0.06,
			Reason: fmt.Sprintf("node %q appears in failure plans", nodeName),
		})
	}

	// Increase retries on existing Retry nodes (history heuristic)
	if hasNode(tree, "RetrySelfCorrect") && fitness.Stability < 0.9 {
		candidates = append(candidates, MutationCandidate{
			Op:     evolution.MutationOp{Operation: "increase_retries", Target: "RetrySelfCorrect"},
			Score:  0.72 + (1.0-fitness.Stability)*0.10,
			Reason: "unstable outcomes — increase bounded retries on existing self-correction node",
		})
	}

	// Prune if node count > 40 (complexity penalty), but only target an existing leaf.
	if fitness.NodeCount > 40 {
		if target := findPruneTarget(tree); target != "" {
			candidates = append(candidates, MutationCandidate{
				Op:     evolution.MutationOp{Operation: "prune_node", Target: target},
				Score:  0.58,
				Reason: fmt.Sprintf("tree has %d nodes — prune low-value leaf %q", fitness.NodeCount, target),
			})
		}
	}

	// Add fallback to an actual selector that lacks a default path.
	if target := findSelectorNeedingFallback(tree); target != "" && (fitness.PathCoverage < 0.7 || fitness.SuccessRate < 0.8) {
		candidates = append(candidates, MutationCandidate{
			Op: evolution.MutationOp{Operation: "add_fallback", Target: target, Node: &evolution.SerializableNode{
				Type: "Action", Name: "DefaultFallback", Description: "Generic fallback action",
			}},
			Score:  0.54 + (0.7-fitness.PathCoverage)*0.10,
			Reason: fmt.Sprintf("selector %q lacks a default fallback under low coverage", target),
		})
	}

	// --- Prompt-level mutations (content evolution) ---

	// Find ChainAgent nodes and suggest content improvements
	chainNodes := findChainAgentNodes(tree)
	for _, cn := range chainNodes {
		// improve_prompt: best prompt-level fix when verification discipline is missing.
		if !hasVerifiedPrompt(cn) {
			if sysMsg, ok := cn.Metadata["system_msg"].(string); ok && sysMsg != "" {
				improvedMsg := sysMsg + " Verify outputs against real tool results and cited source data. Never fabricate results; if a tool call fails, report the error honestly and stop."
				candidates = append(candidates, MutationCandidate{
					Op: evolution.MutationOp{
						Operation: "improve_prompt",
						Target:    cn.Name,
						Metadata:  map[string]any{"system_msg": improvedMsg},
					},
					Score:  0.82 + failurePressure*0.08,
					Reason: fmt.Sprintf("ChainAgent %q lacks verification/fabrication guardrails", cn.Name),
				})
			} else {
				baselineMsg := "You are a thorough and honest agent. Use tools to gather real information. Verify every claim with actual tool output or cited source data. If a tool fails, report the error — never fabricate. Produce complete, well-structured results."
				candidates = append(candidates, MutationCandidate{
					Op: evolution.MutationOp{
						Operation: "improve_prompt",
						Target:    cn.Name,
						Metadata:  map[string]any{"system_msg": baselineMsg},
					},
					Score:  0.86 + failurePressure*0.08,
					Reason: fmt.Sprintf("ChainAgent %q has no system_msg — add verified-output baseline prompt", cn.Name),
				})
			}
		}

		// add_tool: give tool-using agent nodes file access when missing.
		if !chainHasTool(cn, "file_read") {
			candidates = append(candidates, MutationCandidate{
				Op:     evolution.MutationOp{Operation: "add_tool", Target: cn.Name, Metadata: map[string]any{"recommended_tool": "file_read"}},
				Score:  0.76 + failurePressure*0.06,
				Reason: fmt.Sprintf("ChainAgent %q missing file_read tool — add concrete file access", cn.Name),
			})
		}

		// increase_iterations: bump shallow agent loops only when below the useful floor.
		if needsMoreIterations(cn) {
			candidates = append(candidates, MutationCandidate{
				Op:     evolution.MutationOp{Operation: "increase_iterations", Target: cn.Name},
				Score:  0.70 + failurePressure*0.05,
				Reason: fmt.Sprintf("ChainAgent %q has shallow iteration/token budget", cn.Name),
			})
		}
	}

	// Sort all candidates by descending score for deterministic ordering
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates
}

// findFailureNodes extracts node names that appear in failure records.
func findFailureNodes(records []evolution.Record) []string {
	seen := make(map[string]bool)
	for _, r := range records {
		if r.Outcome != evolution.Failure {
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
	records []evolution.Record,
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
	if tree == nil {
		return false
	}
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
	if tree == nil {
		return 0
	}
	count := 0
	if tree.Type == "Selector" {
		count++
	}
	for i := range tree.Children {
		count += countSelectors(&tree.Children[i])
	}
	return count
}

func findPruneTarget(tree *evolution.SerializableNode) string {
	var target string
	walkNodes(tree, func(n *evolution.SerializableNode) {
		if target != "" || n == nil || len(n.Children) > 0 {
			return
		}
		if n.Name == "" || isValidationGate(n.Name) || strings.Contains(n.Name, "Fallback") {
			return
		}
		if strings.Contains(strings.ToLower(n.Name), "cache") || strings.Contains(strings.ToLower(n.Description), "cache") || n.Type == "Action" {
			target = n.Name
		}
	})
	return target
}

func findSelectorNeedingFallback(tree *evolution.SerializableNode) string {
	var target string
	walkNodes(tree, func(n *evolution.SerializableNode) {
		if target != "" || n == nil || n.Type != "Selector" {
			return
		}
		if hasNode(n, "DefaultFallback") || hasNode(n, "EscalateToDeepSeek") {
			return
		}
		if len(n.Children) > 0 {
			target = n.Name
		}
	})
	return target
}

func chainHasTool(n *evolution.SerializableNode, tool string) bool {
	if n == nil || n.Metadata == nil {
		return false
	}
	switch tools := n.Metadata["tools"].(type) {
	case []any:
		for _, t := range tools {
			if ts, ok := t.(string); ok && ts == tool {
				return true
			}
		}
	case []string:
		for _, t := range tools {
			if t == tool {
				return true
			}
		}
	}
	return false
}

func needsMoreIterations(n *evolution.SerializableNode) bool {
	if n == nil || n.Metadata == nil {
		return true
	}
	if maxIter, ok := n.Metadata["max_iterations"].(float64); ok {
		return maxIter < 10
	}
	if maxIter, ok := n.Metadata["max_iterations"].(int); ok {
		return maxIter < 10
	}
	if maxTok, ok := n.Metadata["max_tokens"].(float64); ok {
		return maxTok < 100
	}
	return true
}

// findChainAgentNodes returns all ChainAction nodes with agent chain type.
func findChainAgentNodes(tree *evolution.SerializableNode) []*evolution.SerializableNode {
	var result []*evolution.SerializableNode
	walkNodes(tree, func(n *evolution.SerializableNode) {
		if n.Type == "ChainAction" && n.Metadata != nil {
			result = append(result, n)
		}
	})
	return result
}

func walkNodes(node *evolution.SerializableNode, fn func(*evolution.SerializableNode)) {
	if node == nil {
		return
	}
	fn(node)
	for i := range node.Children {
		walkNodes(&node.Children[i], fn)
	}
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
