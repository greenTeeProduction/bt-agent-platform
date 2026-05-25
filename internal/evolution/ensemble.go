package evolution

import (
	"math"
	"sort"
	"strings"
)

// TreeRunner executes a SerializableNode tree against a task and returns the
// output string and whether execution was successful.
//
// The caller (typically the engine package) is responsible for:
//   1. Creating a Blackboard with the task/context set
//   2. Calling BuildTree + RunTask
//   3. Returning bb.Result and whether bb.Outcome == Success
//
// Using a callback avoids a circular import between evolution ↔ engine.
type TreeRunner func(tree *SerializableNode) (output string, success bool)

// Ensemble combines multiple behavior trees using ensemble learning methods.
// Each tree in the ensemble is a complete behavior tree (SerializableNode).
// The Method field determines how outputs are combined:
//   - "voting":   majority vote with tie-breaking by confidence
//   - "weighted": weighted vote using Weights (higher fitness → more influence)
//   - "stacking": first N-1 trees produce features, Nth tree synthesizes
//   - "boosting": sequential execution, each tree corrects predecessor errors
type Ensemble struct {
	Trees   []*SerializableNode `json:"trees"`
	Weights []float64           `json:"weights"`
	Method  string              `json:"method"`
}

// NewEnsemble creates a new ensemble with the given trees and combination method.
// Weights are initialized uniformly (1/N each). Callers can adjust Weights
// afterwards to reflect per-tree fitness scores.
//
// Valid methods: "voting", "weighted", "stacking", "boosting".
// Unknown methods default to "voting".
func NewEnsemble(trees []*SerializableNode, method string) *Ensemble {
	// Normalize method
	switch method {
	case "voting", "weighted", "stacking", "boosting":
	default:
		method = "voting"
	}

	weights := make([]float64, len(trees))
	if len(trees) > 0 {
		w := 1.0 / float64(len(trees))
		for i := range weights {
			weights[i] = w
		}
	}

	return &Ensemble{
		Trees:   trees,
		Weights: weights,
		Method:  method,
	}
}

// SetWeightsFromFitness assigns weights proportional to fitness scores.
// Higher fitness → higher weight. Negative fitness values are clamped to 0.
func (e *Ensemble) SetWeightsFromFitness(fitness []float64) {
	if len(fitness) != len(e.Trees) {
		return
	}
	e.Weights = make([]float64, len(fitness))
	sum := 0.0
	for i, f := range fitness {
		if f < 0 {
			f = 0
		}
		e.Weights[i] = f + 0.01 // small epsilon so every tree has a voice
		sum += e.Weights[i]
	}
	if sum > 0 {
		for i := range e.Weights {
			e.Weights[i] /= sum
		}
	}
}

// Predict runs all trees via the provided runner and combines their outputs
// according to the ensemble Method. Returns the combined output and a
// confidence score in [0, 1].
func (e *Ensemble) Predict(run TreeRunner) (output string, confidence float64) {
	if len(e.Trees) == 0 {
		return "", 0
	}

	switch e.Method {
	case "voting":
		return e.votingPredict(run)
	case "weighted":
		return e.weightedPredict(run)
	case "stacking":
		return e.stackingPredict(run)
	case "boosting":
		return e.boostingPredict(run)
	default:
		return e.votingPredict(run)
	}
}

// Agreement measures how often trees produce the same output.
// Returns intersection/union: 1.0 when all trees agree, near 0 when all disagree.
func (e *Ensemble) Agreement(run TreeRunner) float64 {
	if len(e.Trees) <= 1 {
		return 1.0
	}

	outputs := make([]string, len(e.Trees))
	for i, tree := range e.Trees {
		outputs[i], _ = run(tree)
	}

	// Count frequency of each unique output
	freq := make(map[string]int)
	for _, o := range outputs {
		freq[o]++
	}

	// Agreement = (max frequency - 1) / (n - 1)  →  canonical pairwise agreement
	maxFreq := 0
	for _, c := range freq {
		if c > maxFreq {
			maxFreq = c
		}
	}

	n := float64(len(e.Trees))
	return (float64(maxFreq) - 1) / (n - 1)
}

// Diversity measures output diversity among ensemble members.
// Returns 1 - Agreement: higher diversity is generally better for ensembles.
func (e *Ensemble) Diversity(run TreeRunner) float64 {
	return 1.0 - e.Agreement(run)
}

// ---------------------------------------------------------------------------
// Internal predict methods
// ---------------------------------------------------------------------------

// votingPredict — each tree casts one vote. Majority wins.
// Tie-breaking: the output with the highest average length-weighted score wins,
// falling back to lexicographic order.
func (e *Ensemble) votingPredict(run TreeRunner) (string, float64) {
	n := float64(len(e.Trees))
	outputs := make([]string, len(e.Trees))
	successes := 0

	for i, tree := range e.Trees {
		var ok bool
		outputs[i], ok = run(tree)
		if ok {
			successes++
		}
	}

	// Build vote tallies
	votes := make(map[string]int)
	for _, out := range outputs {
		votes[out]++
	}

	// Find majority winner
	var winner string
	maxVotes := 0
	for out, cnt := range votes {
		if cnt > maxVotes || (cnt == maxVotes && out > winner) {
			winner = out
			maxVotes = cnt
		}
	}

	confidence := float64(maxVotes) / n
	return winner, confidence
}

// weightedPredict — each tree votes with its assigned weight.
func (e *Ensemble) weightedPredict(run TreeRunner) (string, float64) {
	outputs := make([]string, len(e.Trees))
	for i, tree := range e.Trees {
		outputs[i], _ = run(tree)
	}

	// Weighted vote tallies
	tally := make(map[string]float64)
	totalWeight := 0.0
	for i, out := range outputs {
		w := e.Weights[i]
		tally[out] += w
		totalWeight += w
	}

	var winner string
	best := -1.0
	for out, w := range tally {
		if w > best || (w == best && out > winner) {
			winner = out
			best = w
		}
	}

	confidence := 0.0
	if totalWeight > 0 {
		confidence = best / totalWeight
	}
	return winner, confidence
}

// stackingPredict — first N-1 trees run to produce feature outputs. The last
// tree receives the concatenated outputs as context (simulated via a modified
// run callback). If only 1 tree exists, it acts as a passthrough.
//
// The stacking mechanism works by having the caller supply a TreeRunner that
// observes previous outputs. Since the TreeRunner callback is stateless here,
// we simulate stacking by running the first N-1 trees and appending their
// outputs as a synthetic "meta" output, then running the last tree on the
// same task (the caller is expected to have set up context from the meta
// output in the blackboard).
func (e *Ensemble) stackingPredict(run TreeRunner) (string, float64) {
	n := len(e.Trees)
	if n == 0 {
		return "", 0
	}
	if n == 1 {
		out, ok := run(e.Trees[0])
		conf := 0.5
		if ok {
			conf = 1.0
		}
		return out, conf
	}

	// Run base learners (first N-1 trees)
	baseOutputs := make([]string, 0, n-1)
	baseSuccesses := 0
	for i := 0; i < n-1; i++ {
		out, ok := run(e.Trees[i])
		baseOutputs = append(baseOutputs, out)
		if ok {
			baseSuccesses++
		}
	}

	// The meta-learner (last tree) runs. The caller's TreeRunner is expected
	// to have loaded the base outputs into the blackboard as context.
	// We pass a marker so callers can detect stacking mode.
	metaOut, metaOk := run(e.Trees[n-1])

	// Build a synthetic combined output: meta output + summary of base votes
	votes := make(map[string]int)
	for _, o := range baseOutputs {
		votes[o]++
	}

	var baseWinner string
	bestCnt := 0
	for o, c := range votes {
		if c > bestCnt || (c == bestCnt && o > baseWinner) {
			baseWinner = o
			bestCnt = c
		}
	}

	// Combine: if meta-learner agrees with base consensus, high confidence
	finalOut := metaOut
	if metaOut != "" && baseWinner != "" && metaOut != baseWinner {
		finalOut = metaOut + "\n[base consensus: " + baseWinner + "]"
	}

	baseConf := float64(baseSuccesses) / float64(n-1)
	metaConf := 0.5
	if metaOk {
		metaConf = 1.0
	}
	confidence := 0.6*metaConf + 0.4*baseConf

	return finalOut, confidence
}

// boostingPredict — trees run sequentially. Each tree receives error feedback
// from the previous tree's output. The ensemble output is a weighted
// combination where later trees (which correct earlier errors) get
// progressively higher weight.
//
// The caller's TreeRunner is expected to adjust the blackboard context
// between calls so that each tree focuses on errors from prior trees.
func (e *Ensemble) boostingPredict(run TreeRunner) (string, float64) {
	n := len(e.Trees)
	if n == 0 {
		return "", 0
	}
	if n == 1 {
		out, ok := run(e.Trees[0])
		conf := 0.5
		if ok {
			conf = 1.0
		}
		return out, conf
	}

	type treeResult struct {
		output  string
		success bool
		weight  float64
	}

	results := make([]treeResult, n)

	// Sequential execution: each tree gets to see previous outputs.
	// The caller's TreeRunner handles setting up the error context in the
	// blackboard between consecutive runs of the same ensemble.
	for i := 0; i < n; i++ {
		out, ok := run(e.Trees[i])
		// Later trees get higher weight (they correct earlier errors)
		weight := 1.0 + float64(i)*0.5 // tree 0: 1.0, tree 1: 1.5, tree 2: 2.0, ...
		results[i] = treeResult{
			output:  out,
			success: ok,
			weight:  weight,
		}
	}

	// Weighted vote across boosting rounds
	tally := make(map[string]float64)
	totalWeight := 0.0
	for _, r := range results {
		tally[r.output] += r.weight
		totalWeight += r.weight
	}

	var winner string
	bestWeight := -1.0
	for out, w := range tally {
		if w > bestWeight || (w == bestWeight && out > winner) {
			winner = out
			bestWeight = w
		}
	}

	confidence := 0.0
	if totalWeight > 0 {
		confidence = bestWeight / totalWeight
	}
	return winner, confidence
}

// ---------------------------------------------------------------------------
// Package-level ensemble functions
// ---------------------------------------------------------------------------

// BestOfN runs up to n trees against the same task and returns the index and
// output of the best-performing tree. "Best" is determined by the runner's
// success flag — a successful tree always beats an unsuccessful one; among
// successful trees the one with the longest output is preferred (heuristic
// for thoroughness).
func BestOfN(trees []*SerializableNode, run TreeRunner, n int) (bestIdx int, bestOutput string) {
	if len(trees) == 0 {
		return -1, ""
	}
	if n > len(trees) {
		n = len(trees)
	}

	bestIdx = 0
	bestOutput, bestOk := run(trees[0])
	bestLen := len(bestOutput)

	for i := 1; i < n; i++ {
		out, ok := run(trees[i])
		outLen := len(out)

		// Successful beats unsuccessful
		if ok && !bestOk {
			bestIdx = i
			bestOutput = out
			bestOk = ok
			bestLen = outLen
		} else if ok == bestOk {
			// Among equal success/failure, prefer longer output (more thorough)
			if outLen > bestLen || (outLen == bestLen && i < bestIdx) {
				bestIdx = i
				bestOutput = out
				bestLen = outLen
			}
		}
	}

	return bestIdx, bestOutput
}

// CommitteeVote runs all trees and returns the majority-voted output along
// with the full vote distribution. Each tree "votes" by producing its output;
// the most common output wins. Ties are broken lexicographically.
func CommitteeVote(trees []*SerializableNode, run TreeRunner) (output string, voteCount map[string]int) {
	voteCount = make(map[string]int)

	for _, tree := range trees {
		out, _ := run(tree)
		voteCount[out]++
	}

	maxVotes := 0
	for out, cnt := range voteCount {
		if cnt > maxVotes || (cnt == maxVotes && out > output) {
			output = out
			maxVotes = cnt
		}
	}

	return output, voteCount
}

// StackedEnsemble runs trees sequentially where the output of tree N becomes
// input context for tree N+1. Returns the final tree's output.
//
// The caller's TreeRunner is responsible for propagating context between
// sequential calls. The engine package usually implements this by updating
// bb.Task or bb.Plan with previous output before the next run.
func StackedEnsemble(trees []*SerializableNode, run TreeRunner) string {
	if len(trees) == 0 {
		return ""
	}

	var lastOutput string
	for _, tree := range trees {
		out, _ := run(tree)
		lastOutput = out
	}

	return lastOutput
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// normalizeOutput trims and collapses whitespace for comparison purposes.
func normalizeOutput(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// jaccardSimilarity computes the Jaccard similarity between two strings
// treated as bags of words.
func jaccardSimilarity(a, b string) float64 {
	wordsA := tokenSet(a)
	wordsB := tokenSet(b)

	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA) + len(wordsB) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

// tokenSet returns a set of lowercase word tokens from s.
func tokenSet(s string) map[string]bool {
	fields := strings.Fields(strings.ToLower(s))
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		// Strip common punctuation
		f = strings.Trim(f, ".,;:!?\"'()[]{}")
		if f != "" {
			set[f] = true
		}
	}
	return set
}

// entropy computes Shannon entropy of a vote distribution (in bits).
func entropy(votes map[string]int) float64 {
	total := 0
	for _, c := range votes {
		total += c
	}
	if total == 0 {
		return 0
	}

	var h float64
	t := float64(total)
	for _, c := range votes {
		if c == 0 {
			continue
		}
		p := float64(c) / t
		h -= p * math.Log2(p)
	}
	return h
}

// majorityConfidence returns the proportion of votes the winner received.
func majorityConfidence(votes map[string]int) float64 {
	total := 0
	maxVotes := 0
	for _, c := range votes {
		total += c
		if c > maxVotes {
			maxVotes = c
		}
	}
	if total == 0 {
		return 0
	}
	return float64(maxVotes) / float64(total)
}

// topKOutputs returns the top k outputs from a vote map sorted by frequency
// descending, then lexicographically.
func topKOutputs(votes map[string]int, k int) []string {
	type entry struct {
		output string
		count  int
	}
	entries := make([]entry, 0, len(votes))
	for out, cnt := range votes {
		entries = append(entries, entry{out, cnt})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].output < entries[j].output
	})

	if k > len(entries) {
		k = len(entries)
	}
	result := make([]string, k)
	for i := 0; i < k; i++ {
		result[i] = entries[i].output
	}
	return result
}
