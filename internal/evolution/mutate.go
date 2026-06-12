// Package evolution implements tree definitions, mutation operators, and optimization
// algorithms for the behavior tree platform.
//
// It provides 46 behavior trees across 7 categories (core, domain, finance, research,
// startup, thinktank, kanban) and 7 algorithm engines:
//
//   - Expert Knowledge Base — 6 proven design patterns, 5 anti-patterns, 10 heuristics
//   - Genetic Algorithm — tournament selection, crossover, elitism, memetic refinement
//   - Q-Learning — epsilon-greedy mutation selection with state encoding
//   - Ensemble Methods — voting, weighted, stacking, boosting
//   - Decision Tree Optimizer — C4.5/CART metrics for Selector reordering
//   - SelectorOptimizer — IG/Gini/Killer heuristic for dynamic child ordering
//   - Memetic Local Search — hill climbing, simulated annealing, tabu search
//
// Key functions:
//   - DefaultTree() — the base 17-node behavior tree with agent-based execution
//   - MergedTree() — 51-node universal router combining all domain trees
//   - GoDeveloperTree() — Go-specific tree with build/test/review paths
//   - randomMutation() — applies one of 6 mutation types to any tree
package evolution

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
)

// SerializableNode represents a behavior tree node in a serializable format.
// Mirrors the Rust BT framework's SerializableNode pattern.
type SerializableNode struct {
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Children    []SerializableNode `json:"children,omitempty"`
	MaxRetries  int                `json:"max_retries,omitempty"`
	TimeoutMs   int64              `json:"timeout_ms,omitempty"`
	Metadata    map[string]any     `json:"metadata,omitempty"` // chain config, tags, etc.
	Edges       []TypedEdge        `json:"edges,omitempty"`    // typed edge relationships
}

// TreeStore persists a serializable behavior tree to disk.
type TreeStore struct {
	dir  string
	path string
}

// NewTreeStore creates a TreeStore in the given directory.
func NewTreeStore(dir string) (*TreeStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create tree dir: %w", err)
	}
	return &TreeStore{
		dir:  dir,
		path: filepath.Join(dir, "tree.json"),
	}, nil
}

// Path returns the full path to the tree JSON file.
func (ts *TreeStore) Path() string { return ts.path }

// Dir returns the store directory.
func (ts *TreeStore) Dir() string { return ts.dir }

// MetaPath returns the path to the evolution metadata file.
func (ts *TreeStore) MetaPath() string { return filepath.Join(ts.dir, "metadata.json") }

// SaveMeta writes evolution metadata to disk alongside the tree.
func (ts *TreeStore) SaveMeta(meta *EvolutionMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	return os.WriteFile(ts.MetaPath(), data, 0644)
}

// LoadMeta reads evolution metadata from disk. Returns nil if no metadata exists.
func (ts *TreeStore) LoadMeta() (*EvolutionMetadata, error) {
	data, err := os.ReadFile(ts.MetaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read metadata: %w", err)
	}
	var meta EvolutionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	return &meta, nil
}

// SaveTo writes the tree to a specific path atomically.
func (ts *TreeStore) SaveTo(tree *SerializableNode, path string) error {
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tree: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Load reads the tree from disk. Returns nil if no tree exists yet.
func (ts *TreeStore) Load() (*SerializableNode, error) {
	data, err := os.ReadFile(ts.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read tree: %w", err)
	}
	var tree SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("unmarshal tree: %w", err)
	}
	return &tree, nil
}

// Save writes the tree to disk atomically.
func (ts *TreeStore) Save(tree *SerializableNode) error {
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tree: %w", err)
	}
	tmp := ts.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, ts.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// GoDeveloperTree returns a behavior tree specialized for Go software development.
// Adds code review, compilation, linting, testing, and Go-specific knowledge paths.
func GoDeveloperTree() *SerializableNode {
	return NewTree("GoDev_Main",
		NewPreGate(
			NewCondition("HasClearTask", "Check task has sufficient context, a verb, and a clear goal — >10 chars and not ambiguous"),
			NewCondition("ValidateInput", "Check input is non-empty"),
			NewCondition("IsGoRelated", "Check if task involves Go code or concepts"),
			NewAction("SetupDevTools", "Populate bb.ChainTools with go_build, go_test, go_vet, web_search"),
		),
		NewStrategyRouter(
			NewStrategy("CodeReviewPath",
				NewCondition("IsCodeReview", "Detect review/audit/check keywords"),
				NewAction("ReviewGoCode", "LLM: review Go code for bugs, style, patterns"),
				NewAction("SuggestImprovements", "LLM: suggest idiomatic Go improvements"),
			),
			NewStrategy("BuildPath",
				NewCondition("NeedsCompilation", "Detect build/compile/run keywords"),
				NewAction("CompileGoCode", "Run 'go build' and capture errors"),
				NewAction("FixBuildErrors", "LLM: analyze and fix compilation errors"),
			),
			NewStrategy("TestPath",
				NewCondition("NeedsTesting", "Detect test/coverage/benchmark keywords"),
				NewAction("RunGoTests", "Run 'go test' and capture results"),
				NewAction("AnalyzeTestResults", "Analyze test output for failures"),
			),
			NewStrategy("GoKnowledgePath",
				NewCondition("IsGoQuestion", "Detect Go concept/pattern/best-practice questions"),
				NewAction("ExplainGoConcept", "LLM: explain Go concept with examples"),
			),
			NewStrategy("ExecutionPath",
				NewChainAction(
					"llm_call:Complete this Go development task: {{.Task}}. Use available tools if needed. Provide a complete solution.",
					400,
				),
			),
		),
		NewAction("ReflectOnOutcome", "Generate reflection: what went well, what to improve"),
		NewDefaultOutcomeSelector(400),
		NewAdapt(),
	)
}

// MutationOp describes a tree mutation to apply.
type MutationOp struct {
	Operation string            `json:"operation"` // add_before, add_after, wrap_retry, add_fallback, increase_retries, prune_node, increase_iterations, add_tool, improve_prompt
	Target    string            `json:"target"`    // name of the target node
	Node      *SerializableNode `json:"node,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"` // extra data (tool name, prompt text, etc.)
}

// ApplyMutations applies a list of MutationOps to a tree in-place.
// Validates the tree after each mutation; skips mutations that would produce an invalid tree.
func ApplyMutations(tree *SerializableNode, ops []MutationOp) int {
	applied := 0
	for _, op := range ops {
		// Clone the tree so we can rollback if mutation is a no-op or invalid.
		clone := cloneTree(tree)
		if !applyOp(tree, op) {
			*tree = *clone
			continue
		}
		errors := tree.Validate()
		if len(errors) > 0 {
			// Rollback: restore from clone
			*tree = *clone
			continue
		}
		applied++
	}
	return applied
}

func applyOp(tree *SerializableNode, op MutationOp) bool {
	switch op.Operation {
	case "add_before":
		if op.Node != nil {
			return applyAddBefore(tree, op.Target, *op.Node)
		}
	case "add_after":
		if op.Node != nil {
			return applyAddAfter(tree, op.Target, *op.Node)
		}
	case "wrap_retry":
		return applyWrapRetry(tree, op.Target)
	case "add_fallback":
		if op.Node != nil {
			return applyAddFallback(tree, op.Target, *op.Node)
		}
	case "increase_retries":
		return applyIncreaseRetries(tree, op.Target)
	case "prune_node":
		return applyPruneNode(tree, op.Target)
	case "replace_node":
		return applyReplaceNode(tree, op.Target)
	case "replace_children":
		return applyReplaceChildren(tree, op.Target)
	case "reorder_children":
		return applyReorderChildren(tree, op.Target)
	case "increase_iterations":
		return applyIncreaseIterations(tree, op.Target)
	case "add_tool":
		return applyAddTool(tree, op.Target, op.Metadata)
	case "improve_prompt":
		return applyImprovePrompt(tree, op.Target, op.Metadata)
	case "insert_block_before", "insert_block_after", "replace_with_block", "compose_blocks":
		return tryApplyBlockMutations(tree, op)
	}
	return false
}

// CountNodes returns the total number of nodes in the tree (including the root).
func CountNodes(n *SerializableNode) int {
	if n == nil {
		return 0
	}
	count := 1
	for i := range n.Children {
		count += CountNodes(&n.Children[i])
	}
	return count
}

// --- mutation helpers (recursive) ---

func applyAddBefore(tree *SerializableNode, target string, newNode SerializableNode) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			if hasChildNamed(tree, newNode.Name) {
				return false
			}
			// Insert before
			tree.Children = append(tree.Children[:i], append([]SerializableNode{newNode}, tree.Children[i:]...)...)
			shiftEdgeIndices(tree, i, 1)
			return true
		}
	}
	for i := range tree.Children {
		if applyAddBefore(&tree.Children[i], target, newNode) {
			return true
		}
	}
	return false
}

func applyAddAfter(tree *SerializableNode, target string, newNode SerializableNode) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			if hasChildNamed(tree, newNode.Name) {
				return false
			}
			// Insert after
			insertAt := i + 1
			tree.Children = append(tree.Children[:insertAt], append([]SerializableNode{newNode}, tree.Children[insertAt:]...)...)
			shiftEdgeIndices(tree, insertAt, 1)
			return true
		}
	}
	for i := range tree.Children {
		if applyAddAfter(&tree.Children[i], target, newNode) {
			return true
		}
	}
	return false
}

func applyWrapRetry(tree *SerializableNode, target string) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			if tree.Children[i].Type == "Retry" || tree.Type == "Retry" {
				return false
			}
			wrapped := SerializableNode{
				Type:       "Retry",
				Name:       "Retry_" + target,
				MaxRetries: 3,
				Children:   []SerializableNode{tree.Children[i]},
			}
			tree.Children[i] = wrapped
			return true
		}
	}
	for i := range tree.Children {
		if applyWrapRetry(&tree.Children[i], target) {
			return true
		}
	}
	return false
}

func applyAddFallback(tree *SerializableNode, target string, newNode SerializableNode) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target && tree.Children[i].Type == "Selector" {
			if hasChildNamed(&tree.Children[i], newNode.Name) {
				return false
			}
			tree.Children[i].Children = append(tree.Children[i].Children, newNode)
			return true
		}
	}
	for i := range tree.Children {
		if applyAddFallback(&tree.Children[i], target, newNode) {
			return true
		}
	}
	return false
}

func applyIncreaseRetries(tree *SerializableNode, target string) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target && tree.Children[i].Type == "Retry" {
			if tree.Children[i].MaxRetries >= 5 {
				return false
			}
			tree.Children[i].MaxRetries += 2
			if tree.Children[i].MaxRetries > 5 {
				tree.Children[i].MaxRetries = 5
			}
			return true
		}
	}
	for i := range tree.Children {
		if applyIncreaseRetries(&tree.Children[i], target) {
			return true
		}
	}
	return false
}

func applyPruneNode(tree *SerializableNode, target string) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			tree.Children = append(tree.Children[:i], tree.Children[i+1:]...)
			shiftEdgeIndices(tree, i, -1)
			return true
		}
	}
	for i := range tree.Children {
		if applyPruneNode(&tree.Children[i], target) {
			return true
		}
	}
	return false
}

// applyReplaceNode replaces a condition/action node with a simpler equivalent.
// For conditions, it swaps to a broader match. For actions, it simplifies the action.
func applyReplaceNode(tree *SerializableNode, target string) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			old := tree.Children[i]
			// Replace with a new node that has the same type but simplified name
			replacement := SerializableNode{
				Type:     old.Type + "Action", // Action/Condition → ActionAction/ConditionAction
				Name:     "Replaced_" + old.Name,
				Metadata: map[string]any{"original": old.Name, "evolved": true},
			}
			// Preserve children if any
			replacement.Children = old.Children
			tree.Children[i] = replacement
			return true
		}
	}
	for i := range tree.Children {
		if applyReplaceNode(&tree.Children[i], target) {
			return true
		}
	}
	return false
}

// applyReplaceChildren replaces all children of a composite node (Sequence/Selector)
// with a single action node — restructures the subtree entirely.
func applyReplaceChildren(tree *SerializableNode, target string) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target && len(tree.Children[i].Children) > 0 {
			// Replace all children with a single action
			tree.Children[i].Children = []SerializableNode{{
				Type:     "Action",
				Name:     "Restructured_" + target,
				Metadata: map[string]any{"evolved": true, "restructured": true},
			}}
			return true
		}
	}
	for i := range tree.Children {
		if applyReplaceChildren(&tree.Children[i], target) {
			return true
		}
	}
	return false
}

// applyReorderChildren shuffles the order of a Selector's children to change priority.
// First-child-wins Selectors are sensitive to ordering — this explores better orders.
func applyReorderChildren(tree *SerializableNode, target string) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target &&
			(tree.Children[i].Type == "Selector" || tree.Children[i].Type == "Sequence") &&
			len(tree.Children[i].Children) >= 2 {
			children := tree.Children[i].Children
			// Cyclic shift: move first child to end (or vice versa)
			if rand.Intn(2) == 0 {
				// Shift first to last
				first := children[0]
				tree.Children[i].Children = append(tree.Children[i].Children[1:], first)
			} else {
				// Shift last to first
				last := children[len(children)-1]
				tree.Children[i].Children = append([]SerializableNode{last}, children[:len(children)-1]...)
			}
			// Remap Edge.ChildIndex on the parent since children were reordered.
			remapEdgeIndices(&tree.Children[i])
			return true
		}
	}
	for i := range tree.Children {
		if applyReorderChildren(&tree.Children[i], target) {
			return true
		}
	}
	return false
}

// remapEdgeIndices adjusts Edge.ChildIndex values after a mutation changes the
// children array. After insert/delete/reorder operations, the indices in typed
// edges must be re-established by walking the children and matching by name.
// This addresses the NotebookLM CRITICAL gap: mutation index remapping.
func remapEdgeIndices(node *SerializableNode) {
	if node == nil || len(node.Edges) == 0 {
		return
	}

	var activeEdges []TypedEdge
	for _, e := range node.Edges {
		if e.ChildIndex >= 0 && e.ChildIndex < len(node.Children) {
			activeEdges = append(activeEdges, e)
		}
	}
	if len(activeEdges) == 0 {
		return
	}

	// Build name→index map for current children
	nameToIndex := make(map[string]int, len(node.Children))
	for idx, child := range node.Children {
		if child.Name != "" {
			nameToIndex[child.Name] = idx
		}
	}

	// For each edge with ChildIndex ≥ 0, we need to find the child by its
	// original position. Since we don't store the edge's "target name"
	// separately, we approximate: if the ChildIndex uniquely maps to a child
	// name before the mutation, we can remap it after.
	// Strategy: use the edge Label as an implied hint, or fall back to
	// keeping the index bounded.
	for j, e := range node.Edges {
		if e.ChildIndex < 0 {
			continue
		}
		// Clamp to valid range
		if e.ChildIndex >= len(node.Children) {
			node.Edges[j].ChildIndex = -1 // invalidate out-of-range edges
		}
		// If the edge has a label matching a child name, use that
		if e.Label != "" {
			if idx, ok := nameToIndex[e.Label]; ok {
				node.Edges[j].ChildIndex = idx
			}
		}
	}
}

// shiftEdgeIndices adjusts Edge.ChildIndex values after inserting or removing
// a child at the given position. delta is +1 for inserts, -1 for removes.
func shiftEdgeIndices(node *SerializableNode, at, delta int) {
	for j, e := range node.Edges {
		if e.ChildIndex >= at {
			node.Edges[j].ChildIndex += delta
			if node.Edges[j].ChildIndex < 0 || node.Edges[j].ChildIndex >= len(node.Children) {
				node.Edges[j].ChildIndex = -1
			}
		}
	}
}

// --- Prompt-level mutations (content, not just structure) ---

// applyIncreaseIterations bumps max_iterations/max_tokens on ChainAgent nodes.
func applyIncreaseIterations(tree *SerializableNode, targetName string) bool {
	found := false
	walkTree(tree, func(n *SerializableNode) {
		if found {
			return
		}
		if n.Name == targetName && n.Type == "ChainAction" {
			if n.Metadata == nil {
				n.Metadata = map[string]any{"max_iterations": float64(10)}
				found = true
				return
			}
			if maxIter, ok := n.Metadata["max_iterations"].(float64); ok {
				if maxIter >= 20 {
					return
				}
				n.Metadata["max_iterations"] = minFloat64Evolution(maxIter+5, 20)
				found = true
				return
			}
			if maxIter, ok := n.Metadata["max_iterations"].(int); ok {
				if maxIter >= 20 {
					return
				}
				n.Metadata["max_iterations"] = maxIter + 5
				if n.Metadata["max_iterations"].(int) > 20 {
					n.Metadata["max_iterations"] = 20
				}
				found = true
				return
			}
			if maxTok, ok := n.Metadata["max_tokens"].(float64); ok && maxTok < 100 {
				n.Metadata["max_tokens"] = float64(400)
				found = true
				return
			}
			n.Metadata["max_iterations"] = float64(10)
			found = true
		}
	})
	return found
}

// applyAddTool adds a recommended tool to a ChainAgent node's tools list.
func applyAddTool(tree *SerializableNode, targetName string, meta map[string]any) bool {
	found := false
	walkTree(tree, func(n *SerializableNode) {
		if found {
			return
		}
		if n.Name == targetName && n.Type == "ChainAction" {
			if n.Metadata == nil {
				n.Metadata = map[string]any{}
			}
			newTool := "file_read"
			if metaTool, ok := meta["recommended_tool"].(string); ok && metaTool != "" {
				newTool = metaTool
			}
			switch tools := n.Metadata["tools"].(type) {
			case []any:
				for _, t := range tools {
					if ts, ok := t.(string); ok && ts == newTool {
						return
					}
				}
				n.Metadata["tools"] = append(tools, newTool)
			case []string:
				for _, t := range tools {
					if t == newTool {
						return
					}
				}
				n.Metadata["tools"] = append(tools, newTool)
			default:
				n.Metadata["tools"] = []any{newTool}
			}
			found = true
		}
	})
	return found
}

// applyImprovePrompt uses metadata to update system_msg on a ChainAgent node.
func applyImprovePrompt(tree *SerializableNode, targetName string, meta map[string]any) bool {
	if meta == nil {
		return false
	}
	newSysMsg, ok := meta["system_msg"].(string)
	if !ok || newSysMsg == "" {
		return false
	}
	found := false
	walkTree(tree, func(n *SerializableNode) {
		if found {
			return
		}
		if n.Name == targetName && n.Type == "ChainAction" {
			if n.Metadata == nil {
				n.Metadata = map[string]any{}
			}
			oldSysMsg, _ := n.Metadata["system_msg"].(string)
			if oldSysMsg == newSysMsg {
				return
			}
			n.Metadata["system_msg"] = newSysMsg
			found = true
		}
	})
	return found
}

func minFloat64Evolution(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func hasChildNamed(node *SerializableNode, name string) bool {
	if node == nil || name == "" {
		return false
	}
	for i := range node.Children {
		if node.Children[i].Name == name {
			return true
		}
	}
	return false
}

// walkTree traverses a tree in pre-order, calling fn on each node.
func walkTree(node *SerializableNode, fn func(*SerializableNode)) {
	if node == nil {
		return
	}
	fn(node)
	for i := range node.Children {
		walkTree(&node.Children[i], fn)
	}
}
