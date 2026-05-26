package evolution

import (
	"math"
	"sort"
	"strings"
)

// ─── Decision Tree Analyzer ───
// Applies C4.5/CART decision tree principles to behavior tree Selectors.
// Treats each Selector as a decision node and each child path as a class label.

// PathStats tracks execution statistics for a Selector path.
type PathStats struct {
	PathName     string  `json:"path_name"`
	Condition    string  `json:"condition"`
	HitCount     int     `json:"hit_count"`     // how often this path was taken
	SuccessCount int     `json:"success_count"` // how often it succeeded
	TotalTasks   int     `json:"total_tasks"`   // total tasks seen by this selector
}

// DTSelectorStats holds decision-tree statistics for a Selector node.
type DTSelectorStats struct {
	NodeName    string      `json:"node_name"`
	Paths       []PathStats  `json:"paths"`
	TotalTasks  int          `json:"total_tasks"`
}

// DTAnalyzer computes decision-tree metrics for behavior trees.
type DTAnalyzer struct {
	Stats map[string]*DTSelectorStats `json:"stats"` // selector name → stats
}

// NewDTAnalyzer creates a new decision tree analyzer.
func NewDTAnalyzer() *DTAnalyzer {
	return &DTAnalyzer{Stats: make(map[string]*DTSelectorStats)}
}

// RecordHit records that a Selector path was chosen and whether it succeeded.
func (d *DTAnalyzer) RecordHit(selectorName, pathName, condition string, success bool) {
	if _, ok := d.Stats[selectorName]; !ok {
		d.Stats[selectorName] = &DTSelectorStats{NodeName: selectorName}
	}
	ss := d.Stats[selectorName]
	ss.TotalTasks++

	for i := range ss.Paths {
		if ss.Paths[i].PathName == pathName {
			ss.Paths[i].HitCount++
			if success { ss.Paths[i].SuccessCount++ }
			ss.Paths[i].TotalTasks = ss.TotalTasks
			return
		}
	}
	ss.Paths = append(ss.Paths, PathStats{
		PathName: pathName, Condition: condition,
		HitCount: 1, TotalTasks: ss.TotalTasks,
	})
	if success { ss.Paths[len(ss.Paths)-1].SuccessCount = 1 }
}

// Entropy computes Shannon entropy for a set of path probabilities.
// H(S) = -sum(p_i * log2(p_i))
func (d *DTAnalyzer) Entropy(selectorName string) float64 {
	ss, ok := d.Stats[selectorName]
	if !ok || ss == nil || ss.TotalTasks == 0 { return 0 }

	entropy := 0.0
	for _, p := range ss.Paths {
		prob := float64(p.HitCount) / float64(ss.TotalTasks)
		if prob > 0 {
			entropy -= prob * math.Log2(prob)
		}
	}
	return entropy
}

// InformationGain computes how much a condition reduces entropy.
// IG = H(parent) - weighted_sum(H(children))
func (d *DTAnalyzer) InformationGain(selectorName, condition string) float64 {
	parentEntropy := d.Entropy(selectorName)

	// Split by condition: path with this condition vs all other paths
	withCond := 0
	withoutCond := 0
	ss := d.Stats[selectorName]
	if ss == nil { return 0 }

	for _, p := range ss.Paths {
		if strings.Contains(strings.ToLower(p.Condition), strings.ToLower(condition)) {
			withCond += p.HitCount
		} else {
			withoutCond += p.HitCount
		}
	}

	total := withCond + withoutCond
	if total == 0 { return 0 }

	// Weighted child entropy
	childEntropy := 0.0
	if withCond > 0 {
		prob := float64(withCond) / float64(total)
		childEntropy += prob * pathEntropy(ss.Paths, condition, true)
	}
	if withoutCond > 0 {
		prob := float64(withoutCond) / float64(total)
		childEntropy += prob * pathEntropy(ss.Paths, condition, false)
	}

	return parentEntropy - childEntropy
}

func pathEntropy(paths []PathStats, condition string, match bool) float64 {
	total := 0
	for _, p := range paths {
		matches := strings.Contains(strings.ToLower(p.Condition), strings.ToLower(condition))
		if matches == match { total += p.HitCount }
	}
	if total == 0 { return 0 }

	entropy := 0.0
	for _, p := range paths {
		matches := strings.Contains(strings.ToLower(p.Condition), strings.ToLower(condition))
		if matches == match {
			prob := float64(p.HitCount) / float64(total)
			if prob > 0 { entropy -= prob * math.Log2(prob) }
		}
	}
	return entropy
}

// GiniImpurity computes Gini impurity = 1 - sum(p_i^2)
// Lower is better (more pure splits).
func (d *DTAnalyzer) GiniImpurity(selectorName string) float64 {
	ss, ok := d.Stats[selectorName]
	if !ok || ss == nil || ss.TotalTasks == 0 { return 1.0 }

	sumSq := 0.0
	for _, p := range ss.Paths {
		prob := float64(p.HitCount) / float64(ss.TotalTasks)
		sumSq += prob * prob
	}
	return 1.0 - sumSq
}

// BestSplitCondition finds the condition with highest information gain.
func (d *DTAnalyzer) BestSplitCondition(selectorName string) string {
	ss, ok := d.Stats[selectorName]
	if !ok || ss == nil || len(ss.Paths) < 2 { return "" }

	best := ""
	bestGain := -1.0
	for _, p := range ss.Paths {
		if p.Condition == "" { continue }
		gain := d.InformationGain(selectorName, p.Condition)
		if gain > bestGain {
			bestGain = gain
			best = p.Condition
		}
	}
	return best
}

// ─── BT Optimizer ───
// Uses decision tree analysis to improve behavior tree structure.

// BTOptimizer applies decision-tree insights to behavior trees.
type BTOptimizer struct {
	Analyzer *DTAnalyzer
}

// NewBTOptimizer creates a BT optimizer with decision tree analysis.
func NewBTOptimizer() *BTOptimizer {
	return &BTOptimizer{Analyzer: NewDTAnalyzer()}
}

// OptimizeSelectors reorders Selector children based on decision tree metrics.
// Rules:
//   1. Conditions with highest information gain go first
//   2. Most frequently hit paths go before rarely used paths
//   3. Default/fallback path stays last
// Returns the number of changes made.
func (o *BTOptimizer) OptimizeSelectors(tree *SerializableNode) int {
	changes := 0
	o.optimizeNode(tree, &changes)
	return changes
}

func (o *BTOptimizer) optimizeNode(node *SerializableNode, changes *int) {
	if node.Type == "Selector" && len(node.Children) > 1 {
		// Find conditions in children
		type childInfo struct {
			idx       int
			name      string
			condition string
			isDefault bool
		}
		var children []childInfo
		for i, child := range node.Children {
			condition := extractCondition(&child)
			isDefault := isDefaultPath(&child)
			children = append(children, childInfo{i, child.Name, condition, isDefault})
		}

		// Compute information gain for each condition
		selectorName := node.Name
		gains := make(map[string]float64)
		for _, c := range children {
			if c.condition != "" && !c.isDefault {
				gains[c.condition] = o.Analyzer.InformationGain(selectorName, c.condition)
			}
		}

		// Sort: high info gain first, default last
		sort.SliceStable(children, func(i, j int) bool {
			if children[i].isDefault != children[j].isDefault {
				return !children[i].isDefault // non-default first
			}
			if children[i].isDefault { return false }
			gi := gains[children[i].condition]
			gj := gains[children[j].condition]
			return gi > gj
		})

		// Reorder if needed
		newOrder := make([]SerializableNode, len(node.Children))
		for newPos, c := range children {
			newOrder[newPos] = node.Children[c.idx]
			if c.idx != newPos { *changes++ }
		}
		copy(node.Children, newOrder)
	}

	// Recurse into children
	for i := range node.Children {
		o.optimizeNode(&node.Children[i], changes)
	}
}

// PruneDeadPaths removes Selector paths that never execute.
// Returns the number of paths removed.
func (o *BTOptimizer) PruneDeadPaths(tree *SerializableNode, minHitRatio float64) int {
	if minHitRatio == 0 { minHitRatio = 0.01 } // 1% minimum
	removed := 0
	o.pruneNode(tree, minHitRatio, &removed)
	return removed
}

func (o *BTOptimizer) pruneNode(node *SerializableNode, minRatio float64, removed *int) {
	if node.Type == "Selector" && len(node.Children) > 1 {
		ss := o.Analyzer.Stats[node.Name]
		if ss != nil && ss.TotalTasks > 10 { // enough data
			newChildren := make([]SerializableNode, 0, len(node.Children))
			for _, child := range node.Children {
				hitRatio := o.pathHitRatio(ss, child.Name)
				if hitRatio < minRatio && !isDefaultPath(&child) {
					*removed++
					continue
				}
				newChildren = append(newChildren, child)
			}
			node.Children = newChildren
		}
	}
	for i := range node.Children {
		o.pruneNode(&node.Children[i], minRatio, removed)
	}
}

// MergeOverlappingPaths merges paths with overlapping conditions.
// If two paths match the same keywords, combine them.
func (o *BTOptimizer) MergeOverlappingPaths(tree *SerializableNode) int {
	merged := 0
	o.mergeNode(tree, &merged)
	return merged
}

func (o *BTOptimizer) mergeNode(node *SerializableNode, merged *int) {
	if node.Type == "Selector" && len(node.Children) >= 2 {
		// Check for overlapping conditions
		for i := 0; i < len(node.Children)-1; i++ {
			ci := extractCondition(&node.Children[i])
			for j := i + 1; j < len(node.Children); j++ {
				cj := extractCondition(&node.Children[j])
				if conditionOverlap(ci, cj) > 0.7 {
					// Merge: keep the one with higher hit count, remove the other
					// For now, just flag — actual merge requires deeper restructuring
					*merged++
				}
			}
		}
	}
	for i := range node.Children {
		o.mergeNode(&node.Children[i], merged)
	}
}

// pathHitRatio returns the fraction of tasks that hit this path.
func (o *BTOptimizer) pathHitRatio(ss *DTSelectorStats, pathName string) float64 {
	if ss == nil || ss.TotalTasks == 0 { return 0 }
	for _, p := range ss.Paths {
		if p.PathName == pathName {
			return float64(p.HitCount) / float64(ss.TotalTasks)
		}
	}
	return 0
}

// ─── Helpers ───

func extractCondition(child *SerializableNode) string {
	// A Selector path's condition is usually the first Condition child
	for _, c := range child.Children {
		if c.Type == "Condition" { return c.Name }
	}
	return child.Name
}

func isDefaultPath(child *SerializableNode) bool {
	name := strings.ToLower(child.Name)
	return strings.Contains(name, "execut") ||
		strings.Contains(name, "fallback") ||
		strings.Contains(name, "default") ||
		strings.Contains(name, "knowledge") ||
		strings.Contains(name, "synthesis")
}

func conditionOverlap(a, b string) float64 {
	aWords := wordSet(a)
	bWords := wordSet(b)
	if len(aWords) == 0 || len(bWords) == 0 { return 0 }

	intersect := 0
	for w := range aWords {
		if bWords[w] { intersect++ }
	}
	return float64(intersect) / float64(max(len(aWords), len(bWords)))
}

func wordSet(s string) map[string]bool {
	words := make(map[string]bool)
	// Split camelCase into individual words: "IsCodeReview" → ["Is", "Code", "Review"]
	parts := splitCamelCase(s)
	for _, part := range parts {
		w := strings.ToLower(strings.Trim(part, ",.!?;:\""))
		if len(w) > 2 {
			words[w] = true
		}
	}
	return words
}

// splitCamelCase splits a camelCase string into words.
// "IsCodeReview" → ["Is", "Code", "Review"]
func splitCamelCase(s string) []string {
	if len(s) == 0 {
		return nil
	}
	var words []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			words = append(words, s[start:i])
			start = i
		}
	}
	words = append(words, s[start:])
	return words
}

// ─── DTImprovementReport ───

type DTImprovementReport struct {
	TreeName          string  `json:"tree_name"`
	NodeCount         int     `json:"node_count"`
	Entropy           float64 `json:"entropy"`
	Gini              float64 `json:"gini_impurity"`
	BestSplit         string  `json:"best_split_condition"`
	ReorderChanges    int     `json:"reorder_changes"`
	DeadPathsRemoved  int     `json:"dead_paths_removed"`
	OverlappingPaths  int     `json:"overlapping_paths"`
	OverallScore      float64 `json:"overall_score"` // 0-10
}

// AnalyzeTree runs full decision tree analysis on a behavior tree.
func (o *BTOptimizer) AnalyzeTree(tree *SerializableNode, name string) *DTImprovementReport {
	report := &DTImprovementReport{
		TreeName:  name,
		NodeCount: CountNodes(tree),
	}

	// Find the main Selector
	mainSelector := findMainSelector(tree)
	if mainSelector != "" {
		report.Entropy = o.Analyzer.Entropy(mainSelector)
		report.Gini = o.Analyzer.GiniImpurity(mainSelector)
		report.BestSplit = o.Analyzer.BestSplitCondition(mainSelector)
	}

	// Reorder and count changes
	report.ReorderChanges = o.OptimizeSelectors(tree)
	report.DeadPathsRemoved = o.PruneDeadPaths(tree, 0.01)
	report.OverlappingPaths = o.MergeOverlappingPaths(tree)

	// Score: lower entropy + well-ordered = better
	report.OverallScore = (1.0 - report.Entropy/3.0) * 10.0
	if report.OverallScore < 0 { report.OverallScore = 0 }
	if report.OverallScore > 10 { report.OverallScore = 10 }

	return report
}

func findMainSelector(tree *SerializableNode) string {
	names := collectSelectors(tree)
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), "strategy") ||
			strings.Contains(strings.ToLower(n), "router") ||
			strings.Contains(strings.ToLower(n), "selector") {
			return n
		}
	}
	if len(names) > 0 { return names[0] }
	return ""
}

func collectSelectors(node *SerializableNode) []string {
	var names []string
	if node.Type == "Selector" { names = append(names, node.Name) }
	for i := range node.Children {
		names = append(names, collectSelectors(&node.Children[i])...)
	}
	return names
}
