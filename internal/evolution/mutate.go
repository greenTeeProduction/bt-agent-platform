package evolution

import (
	"encoding/json"
	"fmt"
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

// DefaultTree returns the master behavior tree (same structure as the Rust framework).
func DefaultTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "MainSequence",
		Children: []SerializableNode{
		{
			Type: "Sequence",
			Name: "PreGate",
			Children: []SerializableNode{
				{Type: "Condition", Name: "ValidateInput", Description: "Check input is non-empty"},
				{Type: "Condition", Name: "CheckPrerequisites", Description: "Verify capability"},
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate bb.ChainTools with standard tools"},
			},
		},
			{
				Type: "Selector",
				Name: "StrategyRouter",
				Children: []SerializableNode{
					{
						Type: "Sequence",
						Name: "KnowledgePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "CheckKnowledgeGap", Description: "Detect if task needs external knowledge"},
							{Type: "Action", Name: "QueryKG", Description: "Search knowledge graph"},
							{Type: "Action", Name: "ApplyKnowledge", Description: "Enrich task with KG results"},
						},
					},
					{
						Type: "Sequence",
						Name: "CachePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "CheckCache", Description: "Look up cached result"},
							{Type: "Action", Name: "UseCachedResult", Description: "Return cached result"},
						},
					},
					{
						Type: "Sequence",
						Name: "ExecutionPath",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "agent:Complete this task: {{.Task}}. Use available tools. Think step by step and provide a thorough answer.",
								Metadata: map[string]any{"max_tokens": float64(2048)},
							},
						},
					},
				},
			},
			{
				Type: "Action",
				Name: "ReflectOnOutcome",
				Description: "Generate reflection and validate output quality",
			},
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful", Description: "Exit if task succeeded and output is valid"},
					{
						Type: "ChainAction",
						Name: "agent:Self-correct the previous task. Fix errors and produce a correct answer.",
						Metadata: map[string]any{"max_tokens": float64(2048)},
					},
					{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate to external LLM for difficult tasks"},
				},
			},
			{
				Type: "Action",
				Name: "UpdateBehaviorTree",
				Description: "Adapt tree on 3+ consecutive failures",
			},
		},
	}
}

// GoDeveloperTree returns a behavior tree specialized for Go software development.
// Adds code review, compilation, linting, testing, and Go-specific knowledge paths.
func GoDeveloperTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "GoDev_Main",
		Children: []SerializableNode{
			// PreGate: validate Go-specific input
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Check input is non-empty"},
					{Type: "Condition", Name: "IsGoRelated", Description: "Check if task involves Go code or concepts"},
					{Type: "Action", Name: "SetupDevTools", Description: "Populate bb.ChainTools with go_build, go_test, go_vet, web_search"},
				},
			},
			// StrategyRouter: Go-specific strategy selection
			{
				Type: "Selector",
				Name: "StrategyRouter",
				Children: []SerializableNode{
					// Path 1: Code review
					{
						Type: "Sequence",
						Name: "CodeReviewPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsCodeReview", Description: "Detect review/audit/check keywords"},
							{Type: "Action", Name: "ReviewGoCode", Description: "LLM: review Go code for bugs, style, patterns"},
							{Type: "Action", Name: "SuggestImprovements", Description: "LLM: suggest idiomatic Go improvements"},
						},
					},
					// Path 2: Compilation / build
					{
						Type: "Sequence",
						Name: "BuildPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "NeedsCompilation", Description: "Detect build/compile/run keywords"},
							{Type: "Action", Name: "CompileGoCode", Description: "Run 'go build' and capture errors"},
							{Type: "Action", Name: "FixBuildErrors", Description: "LLM: analyze and fix compilation errors"},
						},
					},
					// Path 3: Testing
					{
						Type: "Sequence",
						Name: "TestPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "NeedsTesting", Description: "Detect test/coverage/benchmark keywords"},
							{Type: "Action", Name: "RunGoTests", Description: "Run 'go test' and capture results"},
							{Type: "Action", Name: "AnalyzeTestResults", Description: "Analyze test output for failures"},
						},
					},
					// Path 4: Go knowledge query
					{
						Type: "Sequence",
						Name: "GoKnowledgePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsGoQuestion", Description: "Detect Go concept/pattern/best-practice questions"},
							{Type: "Action", Name: "ExplainGoConcept", Description: "LLM: explain Go concept with examples"},
						},
					},
					// Path 5: Agent-based generic execution (replaces AnalyzeTask+ExecutePlan)
					{
						Type: "Sequence",
						Name: "ExecutionPath",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "agent:Complete this Go development task: {{.Task}}. Use available tools if needed. Provide a complete solution.",
								Metadata: map[string]any{
									"max_tokens": float64(10),
								},
							},
						},
					},
				},
			},
			// Always reflect
			{
				Type: "Action",
				Name: "ReflectOnOutcome",
				Description: "Generate reflection: what went well, what to improve",
			},
			// Outcome handling with agent-based self-correction
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful", Description: "Exit if task succeeded"},
					{
						Type: "ChainAction",
						Name: "agent:Self-correct the previous task. Analyze what went wrong, fix the issues, and produce a corrected solution.",
						Metadata: map[string]any{
							"max_tokens": float64(5),
						},
					},
					{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate to external LLM for difficult tasks"},
				},
			},
			{
				Type: "Action",
				Name: "UpdateBehaviorTree",
				Description: "Adapt tree on 3+ consecutive failures",
			},
		},
	}
}

// MutationOp describes a tree mutation to apply.
type MutationOp struct {
	Operation string `json:"operation"` // add_before, add_after, wrap_retry, add_fallback, increase_retries, prune_node
	Target    string `json:"target"`    // name of the target node
	Node      *SerializableNode `json:"node,omitempty"`
}

// ApplyMutations applies a list of MutationOps to a tree in-place.
func ApplyMutations(tree *SerializableNode, ops []MutationOp) int {
	applied := 0
	for _, op := range ops {
		switch op.Operation {
		case "add_before":
			if op.Node != nil {
				if applyAddBefore(tree, op.Target, *op.Node) {
					applied++
				}
			}
		case "add_after":
			if op.Node != nil {
				if applyAddAfter(tree, op.Target, *op.Node) {
					applied++
				}
			}
		case "wrap_retry":
			if applyWrapRetry(tree, op.Target) {
				applied++
			}
		case "add_fallback":
			if op.Node != nil {
				if applyAddFallback(tree, op.Target, *op.Node) {
					applied++
				}
			}
		case "increase_retries":
			if applyIncreaseRetries(tree, op.Target) {
				applied++
			}
		case "prune_node":
			if applyPruneNode(tree, op.Target) {
				applied++
			}
		}
	}
	return applied
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
			// Insert before
			tree.Children = append(tree.Children[:i], append([]SerializableNode{newNode}, tree.Children[i:]...)...)
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
			// Insert after
			insertAt := i + 1
			tree.Children = append(tree.Children[:insertAt], append([]SerializableNode{newNode}, tree.Children[insertAt:]...)...)
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
			tree.Children[i].MaxRetries += 2
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
