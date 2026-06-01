package evolution

import (
	"fmt"
	"sort"
	"strings"
)

// ─── SEARCH/REPLACE Diff Format (AlphaEvolve-style targeted edits) ───

// DiffMutation represents a targeted node-level edit using SEARCH/REPLACE semantics.
// Instead of whole-tree mutation, this finds a specific node and replaces it.
type DiffMutation struct {
	Operation string           `json:"operation"` // replace_condition, replace_action, swap_subtree, adjust_retries
	Search    NodeMatcher      `json:"search"`    // what to find
	Replace   SerializableNode `json:"replace"`   // what to replace with
}

// NodeMatcher specifies criteria to find a node in the tree.
type NodeMatcher struct {
	Type         string            `json:"type"`                    // "Condition", "Action", "Sequence", "Selector"
	Name         string            `json:"name,omitempty"`          // exact name match
	NameContains string            `json:"name_contains,omitempty"` // substring match
	Metadata     map[string]string `json:"metadata,omitempty"`      // key-value metadata match
	MaxDepth     int               `json:"max_depth,omitempty"`     // only search up to this depth (-1 = unlimited)
}

// Matches returns true if the node satisfies all matcher criteria.
func (m NodeMatcher) Matches(node *SerializableNode) bool {
	if node == nil {
		return false
	}
	if m.Type != "" && node.Type != m.Type {
		return false
	}
	if m.Name != "" && node.Name != m.Name {
		return false
	}
	if m.NameContains != "" && !strings.Contains(node.Name, m.NameContains) {
		return false
	}
	for k, v := range m.Metadata {
		if node.Metadata == nil {
			return false
		}
		nv, ok := node.Metadata[k]
		if !ok || fmt.Sprint(nv) != v {
			return false
		}
	}
	return true
}

// FindNode searches the tree for the first node matching the criteria.
// Returns nil if no match found.
func (m NodeMatcher) FindNode(tree *SerializableNode, currentDepth int) *SerializableNode {
	if tree == nil {
		return nil
	}
	if m.MaxDepth >= 0 && currentDepth > m.MaxDepth {
		return nil
	}
	if m.Matches(tree) {
		return tree
	}
	for i := range tree.Children {
		if found := m.FindNode(&tree.Children[i], currentDepth+1); found != nil {
			return found
		}
	}
	return nil
}

// ReplaceNode finds and replaces a node in the tree.
// Returns the number of replacements made (0 or 1 for single-target).
func (m NodeMatcher) ReplaceNode(tree *SerializableNode, replacement *SerializableNode, currentDepth int) int {
	if tree == nil {
		return 0
	}
	if m.MaxDepth >= 0 && currentDepth > m.MaxDepth {
		return 0
	}

	// Check children
	for i := range tree.Children {
		if m.Matches(&tree.Children[i]) {
			tree.Children[i] = *cloneTree(replacement)
			return 1
		}
	}

	// Recurse
	for i := range tree.Children {
		if m.ReplaceNode(&tree.Children[i], replacement, currentDepth+1) > 0 {
			return 1
		}
	}
	return 0
}

// ReplaceAllNodes replaces ALL matching nodes (not just first).
func (m NodeMatcher) ReplaceAllNodes(tree *SerializableNode, replacement *SerializableNode, currentDepth int) int {
	if tree == nil {
		return 0
	}
	if m.MaxDepth >= 0 && currentDepth > m.MaxDepth {
		return 0
	}

	count := 0
	for i := range tree.Children {
		if m.Matches(&tree.Children[i]) {
			tree.Children[i] = *cloneTree(replacement)
			count++
		}
	}
	for i := range tree.Children {
		count += m.ReplaceAllNodes(&tree.Children[i], replacement, currentDepth+1)
	}
	return count
}

// ApplyDiffMutation applies a single SEARCH/REPLACE mutation to the tree.
// Returns true if the mutation was applied successfully.
func ApplyDiffMutation(tree *SerializableNode, mutation DiffMutation) bool {
	if tree == nil {
		return false
	}

	switch mutation.Operation {
	case "replace_condition", "replace_action", "replace_node":
		return mutation.Search.ReplaceNode(tree, &mutation.Replace, 0) > 0

	case "replace_all":
		return mutation.Search.ReplaceAllNodes(tree, &mutation.Replace, 0) > 0

	case "swap_subtree":
		// Find two nodes and swap them
		// Simplified: just replace first match with replacement
		return mutation.Search.ReplaceNode(tree, &mutation.Replace, 0) > 0

	case "adjust_retries":
		// Find matching node and adjust MaxRetries
		found := mutation.Search.FindNode(tree, 0)
		if found != nil {
			if mutation.Replace.MaxRetries > 0 {
				found.MaxRetries = mutation.Replace.MaxRetries
			}
			return true
		}
		return false

	default:
		return false
	}
}

// ─── Meta-Prompt Evolution ───

// MutationTemplate is a prompt template for generating mutations.
// Templates themselves evolve based on their success rate.
type MutationTemplate struct {
	Name        string   `json:"name"`
	Instruction string   `json:"instruction"`  // natural language instruction
	Examples    []string `json:"examples"`     // few-shot examples
	SuccessRate float64  `json:"success_rate"` // how often mutations from this template improve fitness
	UsageCount  int      `json:"usage_count"`  // total times used
}

// MetaPromptEvolver manages a population of mutation templates.
type MetaPromptEvolver struct {
	Templates []MutationTemplate `json:"templates"`
	TopK      int                `json:"top_k"` // keep top N templates
}

// NewMetaPromptEvolver creates an evolver with default templates.
func NewMetaPromptEvolver(topK int) *MetaPromptEvolver {
	return &MetaPromptEvolver{
		TopK: topK,
		Templates: []MutationTemplate{
			{
				Name:        "add_condition_check",
				Instruction: "Add a Condition node before the target to validate task quality. Use keyword matching for routing.",
				Examples:    []string{"Add HasClearTask before PreGate to filter bad tasks"},
			},
			{
				Name:        "wrap_retry",
				Instruction: "Wrap the target in a RetrySequence with MaxRetries=3. This adds resilience to transient failures.",
				Examples:    []string{"Wrap ExecutePlan in RetrySequence(3)"},
			},
			{
				Name:        "add_fallback_path",
				Instruction: "Add a fallback Action after the target that runs if the main path fails. Simple, safe, always succeeds.",
				Examples:    []string{"Add MarkFailed after ExecutePlan as fallback"},
			},
			{
				Name:        "adjust_retries",
				Instruction: "Increase or decrease MaxRetries on the target node based on its failure rate.",
				Examples:    []string{"Set MaxRetries=5 on SelfCorrect (was 3)"},
			},
		},
	}
}

// RecordFeedback updates a template's success rate based on mutation outcome.
func (mpe *MetaPromptEvolver) RecordFeedback(templateName string, improved bool) {
	for i := range mpe.Templates {
		if mpe.Templates[i].Name == templateName {
			t := &mpe.Templates[i]
			t.UsageCount++
			// Exponential moving average for success rate
			alpha := 0.2
			val := 0.0
			if improved {
				val = 1.0
			}
			t.SuccessRate = (1-alpha)*t.SuccessRate + alpha*val
			return
		}
	}
}

// BestTemplate returns the template with the highest success rate.
func (mpe *MetaPromptEvolver) BestTemplate() *MutationTemplate {
	if len(mpe.Templates) == 0 {
		return nil
	}
	best := &mpe.Templates[0]
	for i := 1; i < len(mpe.Templates); i++ {
		if mpe.Templates[i].SuccessRate > best.SuccessRate {
			best = &mpe.Templates[i]
		}
	}
	return best
}

// EvolveTemplates mutates and selects templates, keeping the top K.
// Low-success templates are replaced by variants of high-success ones.
func (mpe *MetaPromptEvolver) EvolveTemplates() {
	if len(mpe.Templates) <= 1 {
		return
	}

	// Sort by success rate descending
	sort.Slice(mpe.Templates, func(i, j int) bool {
		return mpe.Templates[i].SuccessRate > mpe.Templates[j].SuccessRate
	})

	// Keep top K
	if len(mpe.Templates) > mpe.TopK {
		mpe.Templates = mpe.Templates[:mpe.TopK]
	}

	// Mutate the best template to replace the worst
	if len(mpe.Templates) >= 2 {
		best := mpe.Templates[0]
		mutated := MutationTemplate{
			Name:        best.Name + "_variant",
			Instruction: best.Instruction + " Prioritize diversity over raw fitness.",
			SuccessRate: 0.1, // start with low confidence
		}
		mpe.Templates[len(mpe.Templates)-1] = mutated
	}
}

// ─── Evolution Blocks ───

// EvolveBlock marks a subtree as mutable or frozen.
// Frozen blocks are never mutated — they're stable infrastructure.
type EvolveBlock struct {
	Path    string `json:"path"`    // path name within the tree (e.g., "PreGate", "ExecutionPath")
	Mutable bool   `json:"mutable"` // true = can be mutated, false = frozen
}

// BlockConfig defines which paths in a tree are evolvable.
type BlockConfig struct {
	Blocks []EvolveBlock `json:"blocks"`
}

// DefaultBlockConfig returns sensible defaults: freeze infrastructure, mutate logic.
func DefaultBlockConfig() BlockConfig {
	return BlockConfig{
		Blocks: []EvolveBlock{
			{Path: "PreGate", Mutable: false},         // freeze task validation
			{Path: "OutcomeSelector", Mutable: false}, // freeze outcome routing
			{Path: "ExecutionPath", Mutable: true},    // mutate execution logic
			{Path: "SelfCorrect", Mutable: true},      // mutate self-correction
			{Path: "ReflectOnOutcome", Mutable: true}, // mutate reflection
		},
	}
}

// IsMutable returns true if the given path can be mutated.
func (bc BlockConfig) IsMutable(path string) bool {
	for _, block := range bc.Blocks {
		if strings.EqualFold(block.Path, path) {
			return block.Mutable
		}
	}
	return true // unknown paths are mutable by default
}

// FilterMutations removes mutations targeting frozen blocks.
func (bc BlockConfig) FilterMutations(mutations []MutationOp, tree *SerializableNode) []MutationOp {
	filtered := make([]MutationOp, 0, len(mutations))
	for _, m := range mutations {
		// Determine which block the target belongs to
		block := findBlockForNode(tree, m.Target, "")
		if bc.IsMutable(block) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// findBlockForNode walks the tree to find which block/path a node belongs to.
func findBlockForNode(tree *SerializableNode, targetName, currentBlock string) string {
	if tree == nil {
		return currentBlock
	}
	if tree.Name == targetName {
		return currentBlock
	}
	// Update block tracking when we encounter a named Selector/Sequence
	nextBlock := currentBlock
	if tree.Type == "Selector" || tree.Type == "Sequence" {
		if tree.Name != "" {
			nextBlock = tree.Name
		}
	}
	for i := range tree.Children {
		if result := findBlockForNode(&tree.Children[i], targetName, nextBlock); result != "" {
			return result
		}
	}
	return "" // not found
}

// ─── Combined Mutation Runner ───

// MutationContext bundles all evolution improvements for a single mutation cycle.
type MutationContext struct {
	Blocks        BlockConfig
	Templates     *MetaPromptEvolver
	DiffMutations []DiffMutation // accumulated SEARCH/REPLACE mutations
}

// NewMutationContext creates a context with sensible defaults.
func NewMutationContext() *MutationContext {
	return &MutationContext{
		Blocks:    DefaultBlockConfig(),
		Templates: NewMetaPromptEvolver(5),
	}
}

// ApplySafeMutation applies a mutation with block protection and template tracking.
// Returns true if the mutation was applied (passed block filter).
func (mc *MutationContext) ApplySafeMutation(tree *SerializableNode, op MutationOp) bool {
	// Check block protection
	if !mc.Blocks.IsMutable(op.Target) {
		// Check if target belongs to a frozen block
		block := findBlockForNode(tree, op.Target, "")
		if block != "" && !mc.Blocks.IsMutable(block) {
			return false // blocked
		}
	}

	// Apply the mutation
	ApplyMutations(tree, []MutationOp{op})

	// Record for template feedback
	// (caller records whether it improved)
	return true
}
