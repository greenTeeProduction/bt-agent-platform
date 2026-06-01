package evolution

import (
	"math"
	"sort"
)

// ─── Selector Node Optimization ──────────────────────────────────────────
//
// Based on NotebookLM research (2026-05-26):
// Decision tree algorithms (ID3, C4.5, CART) can be adapted to optimize
// Selector (fallback) node ordering by treating child return statuses as
// classification data. References:
//   - Information Gain (Shannon Entropy) for ordering by informativeness
//   - Gini Impurity for ordering by outcome predictability
//   - Killer Heuristic for caching successful children
//   - Alpha-Beta pruning to skip statistically inferior children
//
// Sources: Wikipedia (Decision Tree Learning, Alpha-Beta Pruning,
// Behavior Tree, Iterative Deepening)

// ─── Data Structures ─────────────────────────────────────────────────────

// NodeExecutionRecord captures one execution of a BT node.
type NodeExecutionRecord struct {
	NodeName string // which node was executed
	Outcome  string // "success", "failure", "running"
}

// SelectorStats tracks execution history for children of a Selector node.
type SelectorStats struct {
	ParentName string
	Children   map[string]*ChildStats // child name → stats
}

// ChildStats accumulates execution outcomes for one child node.
type ChildStats struct {
	Name      string
	Successes int
	Failures  int
	Running   int
	// Killer heuristic: last time this child caused a beta-cutoff
	LastSuccessTick int
	TotalTicks      int
}

// Total returns the total number of recorded outcomes.
func (cs *ChildStats) Total() int {
	return cs.Successes + cs.Failures + cs.Running
}

// SuccessRate returns the fraction of outcomes that succeeded.
func (cs *ChildStats) SuccessRate() float64 {
	t := cs.Total()
	if t == 0 {
		return 0
	}
	return float64(cs.Successes) / float64(t)
}

// ─── Information Gain (ID3/C4.5 metric) ──────────────────────────────────

// Entropy computes Shannon entropy of a probability distribution.
// H = -Σ p_i * log2(p_i)
func Entropy(probs ...float64) float64 {
	var h float64
	for _, p := range probs {
		if p > 0 {
			h -= p * math.Log2(p)
		}
	}
	return h
}

// SelectorEntropy computes the entropy of the Selector's overall outcome
// distribution (what fraction of ticks result in success vs failure).
func SelectorEntropy(stats *SelectorStats) float64 {
	total, successes, failures := 0, 0, 0
	for _, cs := range stats.Children {
		successes += cs.Successes
		failures += cs.Failures
		total += cs.Total()
	}
	if total == 0 {
		return 0
	}
	return Entropy(
		float64(successes)/float64(total),
		float64(failures)/float64(total),
	)
}

// InformationGain computes the expected reduction in entropy from trying
// a specific child first. Higher IG = more informative child.
//
// IG = H(parent) - Σ (weight_i * H(child_i))
func InformationGain(child *ChildStats, allStats *SelectorStats) float64 {
	parentEntropy := SelectorEntropy(allStats)
	childTotal := child.Total()
	total := 0
	for _, cs := range allStats.Children {
		total += cs.Total()
	}
	if total == 0 || childTotal == 0 {
		return 0
	}
	// Child entropy: success vs failure proportion for this specific child
	childEntropy := Entropy(
		float64(child.Successes)/float64(childTotal),
		float64(child.Failures)/float64(childTotal),
	)
	weight := float64(childTotal) / float64(total)
	return parentEntropy - weight*childEntropy
}

// ─── Gini Impurity (CART metric) ─────────────────────────────────────────

// GiniImpurity computes the Gini impurity for a child node.
// Gini = 1 - Σ p_i²
// Low Gini = predictable outcomes (almost always succeeds or almost always fails).
func GiniImpurity(child *ChildStats) float64 {
	t := float64(child.Total())
	if t == 0 {
		return 1.0 // maximum impurity when no data
	}
	s := float64(child.Successes) / t
	f := float64(child.Failures) / t
	r := float64(child.Running) / t
	return 1.0 - (s*s + f*f + r*r)
}

// ─── Ordering Strategies ─────────────────────────────────────────────────

// SelectorOrderingStrategy defines how to order Selector children.
type SelectorOrderingStrategy string

const (
	// OrderByIG ranks by Information Gain descending (most informative first).
	OrderByIG SelectorOrderingStrategy = "information_gain"
	// OrderByGini ranks by Gini impurity ascending (most predictable first).
	OrderByGini SelectorOrderingStrategy = "gini_impurity"
	// OrderBySuccessRate ranks by success rate descending.
	OrderBySuccessRate SelectorOrderingStrategy = "success_rate"
	// OrderByKiller uses killer heuristic: last-successful child first.
	OrderByKiller SelectorOrderingStrategy = "killer_heuristic"
	// OrderByHybrid combines IG (70%) and Gini (30%) for balanced ordering.
	OrderByHybrid SelectorOrderingStrategy = "hybrid"
)

// SelectorOptimizer reorders Selector children based on execution history.
type SelectorOptimizer struct {
	Stats      map[string]*SelectorStats // selector name → stats
	Strategy   SelectorOrderingStrategy
	MinSamples int // minimum samples before reordering (default: 10)
}

// NewSelectorOptimizer creates a new optimizer with the given strategy.
func NewSelectorOptimizer(strategy SelectorOrderingStrategy) *SelectorOptimizer {
	return &SelectorOptimizer{
		Stats:      make(map[string]*SelectorStats),
		Strategy:   strategy,
		MinSamples: 10,
	}
}

// Record records an execution outcome for a child node.
func (so *SelectorOptimizer) Record(parentName string, rec NodeExecutionRecord) {
	stats, ok := so.Stats[parentName]
	if !ok {
		stats = &SelectorStats{
			ParentName: parentName,
			Children:   make(map[string]*ChildStats),
		}
		so.Stats[parentName] = stats
	}
	cs, ok := stats.Children[rec.NodeName]
	if !ok {
		cs = &ChildStats{Name: rec.NodeName}
		stats.Children[rec.NodeName] = cs
	}
	switch rec.Outcome {
	case "success":
		cs.Successes++
		cs.LastSuccessTick = cs.TotalTicks
	case "failure":
		cs.Failures++
	case "running":
		cs.Running++
	}
	cs.TotalTicks++
}

// OrderChildren returns the recommended child ordering for a Selector,
// as a string slice of child names in priority order.
func (so *SelectorOptimizer) OrderChildren(selectorName string) []string {
	stats, ok := so.Stats[selectorName]
	if !ok {
		return nil
	}
	// Check minimum sample threshold
	total := 0
	for _, cs := range stats.Children {
		total += cs.Total()
	}
	if total < so.MinSamples {
		return nil // not enough data
	}

	children := make([]*ChildStats, 0, len(stats.Children))
	for _, cs := range stats.Children {
		children = append(children, cs)
	}

	switch so.Strategy {
	case OrderByIG:
		sort.Slice(children, func(i, j int) bool {
			return InformationGain(children[i], stats) > InformationGain(children[j], stats)
		})
	case OrderByGini:
		sort.Slice(children, func(i, j int) bool {
			return GiniImpurity(children[i]) < GiniImpurity(children[j])
		})
	case OrderBySuccessRate:
		sort.Slice(children, func(i, j int) bool {
			return children[i].SuccessRate() > children[j].SuccessRate()
		})
	case OrderByKiller:
		sort.Slice(children, func(i, j int) bool {
			return children[i].LastSuccessTick > children[j].LastSuccessTick
		})
	case OrderByHybrid:
		sort.Slice(children, func(i, j int) bool {
			// Normalize IG and Gini into [0,1] and combine
			scoreI := normalizedIG(children[i], stats)
			scoreJ := normalizedIG(children[j], stats)
			giniI := 1.0 - GiniImpurity(children[i]) // invert so high = good
			giniJ := 1.0 - GiniImpurity(children[j])
			hybridI := 0.7*scoreI + 0.3*giniI
			hybridJ := 0.7*scoreJ + 0.3*giniJ
			return hybridI > hybridJ
		})
	}

	names := make([]string, len(children))
	for i, cs := range children {
		names[i] = cs.Name
	}
	return names
}

// ApplyOrdering reorders children of a SerializableNode if it's a Selector.
// Returns true if the ordering changed.
func (so *SelectorOptimizer) ApplyOrdering(tree *SerializableNode, selectorName string) bool {
	newOrder := so.OrderChildren(selectorName)
	if len(newOrder) == 0 {
		return false
	}
	return reorderSelectorChildren(tree, selectorName, newOrder)
}

// ─── Alpha-Beta Pruning for Selectors ────────────────────────────────────

// ShouldPrune determines if a child can be skipped based on statistical
// inferiority. If a child has proven to be worse than a previously examined
// sibling (via Gini impurity), it can be pruned.
func (so *SelectorOptimizer) ShouldPrune(child *ChildStats, bestSoFar *ChildStats) bool {
	if bestSoFar == nil {
		return false
	}
	// Prune if this child has both higher impurity AND lower success rate
	childGini := GiniImpurity(child)
	bestGini := GiniImpurity(bestSoFar)
	if childGini > bestGini && child.SuccessRate() < bestSoFar.SuccessRate() {
		return true
	}
	return false
}

// ─── Killer Heuristic ────────────────────────────────────────────────────

// KillerChild returns the child that most recently caused success (beta-cutoff).
func (so *SelectorOptimizer) KillerChild(selectorName string) string {
	stats, ok := so.Stats[selectorName]
	if !ok {
		return ""
	}
	var killer string
	lastTick := -1
	for _, cs := range stats.Children {
		if cs.LastSuccessTick > lastTick {
			lastTick = cs.LastSuccessTick
			killer = cs.Name
		}
	}
	return killer
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// normalizedIG returns IG normalized to [0,1] within the Selector's children.
func normalizedIG(child *ChildStats, stats *SelectorStats) float64 {
	maxIG := 0.0
	for _, cs := range stats.Children {
		ig := InformationGain(cs, stats)
		if ig > maxIG {
			maxIG = ig
		}
	}
	if maxIG == 0 {
		return 0
	}
	return InformationGain(child, stats) / maxIG
}

// reorderSelectorChildren finds a Selector node by name and reorders its
// children to match newOrder. Returns true if reordering changed anything.
func reorderSelectorChildren(tree *SerializableNode, selectorName string, newOrder []string) bool {
	if tree.Name == selectorName && tree.Type == "Selector" {
		return applyOrderToNode(tree, newOrder)
	}
	for i := range tree.Children {
		if reorderSelectorChildren(&tree.Children[i], selectorName, newOrder) {
			return true
		}
	}
	return false
}

// applyOrderToNode reorders a node's children to match newOrder.
func applyOrderToNode(node *SerializableNode, newOrder []string) bool {
	if len(node.Children) != len(newOrder) {
		return false
	}
	// Check if ordering already matches
	matches := true
	for i, name := range newOrder {
		if i >= len(node.Children) || node.Children[i].Name != name {
			matches = false
			break
		}
	}
	if matches {
		return false
	}
	// Reorder
	ordered := make([]SerializableNode, len(node.Children))
	used := make(map[int]bool)
	for pos, name := range newOrder {
		for i := range node.Children {
			if used[i] {
				continue
			}
			if node.Children[i].Name == name {
				ordered[pos] = node.Children[i]
				used[i] = true
				break
			}
		}
	}
	// Fill any unmatched children at the end
	j := len(newOrder)
	for i := range node.Children {
		if !used[i] {
			ordered[j] = node.Children[i]
			j++
		}
	}
	node.Children = ordered
	return true
}

// NOTE: CountNodes is defined in mutate.go. Do not redeclare here.
