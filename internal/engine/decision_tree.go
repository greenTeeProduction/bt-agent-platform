package engine

import (
	"fmt"
	"strconv"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildDecisionTree builds a deterministic branch selector for explicit
// decision-tree routing. A DecisionTree node reads a blackboard value and runs
// the first child whose metadata matches that value.
//
// Node metadata:
//   - key: ChainState key to inspect (default: node.Name)
//   - source: "chain_state" (default), "task", "complexity", "outcome", "result"
//   - default: fallback branch label when no child match succeeds
//
// Child metadata:
//   - match: exact branch value matched against the selected blackboard value
//   - matches: []string/[]any of accepted branch values
//   - branch: symbolic branch name, also used for default fallback
//   - default: true marks the child as fallback
func BuildDecisionTree(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	children := make([]btcore.Command[Blackboard], len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		idx := chooseDecisionBranch(node, ctx.Blackboard)
		if idx < 0 || idx >= len(children) {
			ctx.Blackboard.Outcome = fmt.Sprintf("decision_tree_failed: no matching branch for %s", decisionKey(node))
			return -1
		}
		child := &node.Children[idx]
		pathName := child.Name
		if pathName == "" {
			pathName = fmt.Sprintf("child_%d", idx)
		}
		ctx.Blackboard.CurrentPath = node.Name + "/" + pathName
		ctx.Blackboard.VisitedPaths = append(ctx.Blackboard.VisitedPaths, ctx.Blackboard.CurrentPath)
		return children[idx].Run(ctx)
	})
}

func chooseDecisionBranch(node *evolution.SerializableNode, bb *Blackboard) int {
	value := decisionValue(node, bb)
	for i := range node.Children {
		if childMatchesDecision(&node.Children[i], value) {
			return i
		}
	}
	if fallback := defaultDecisionBranch(node); fallback != -1 {
		return fallback
	}
	return -1
}

func decisionKey(node *evolution.SerializableNode) string {
	if node.Metadata != nil {
		if key, ok := node.Metadata["key"].(string); ok && key != "" {
			return key
		}
	}
	return node.Name
}

func decisionValue(node *evolution.SerializableNode, bb *Blackboard) string {
	if node.Metadata != nil {
		if source, ok := node.Metadata["source"].(string); ok {
			switch source {
			case "task":
				return bb.Task
			case "complexity":
				return bb.Complexity
			case "outcome":
				return bb.Outcome
			case "result":
				return bb.Result
			}
		}
	}
	if bb.ChainState == nil {
		return ""
	}
	return stringifyDecisionValue(bb.ChainState[decisionKey(node)])
}

func childMatchesDecision(child *evolution.SerializableNode, value string) bool {
	if child.Metadata == nil {
		return false
	}
	if match, ok := child.Metadata["match"]; ok && stringifyDecisionValue(match) == value {
		return true
	}
	if matches, ok := child.Metadata["matches"]; ok {
		switch vals := matches.(type) {
		case []string:
			for _, v := range vals {
				if v == value {
					return true
				}
			}
		case []any:
			for _, v := range vals {
				if stringifyDecisionValue(v) == value {
					return true
				}
			}
		}
	}
	if branch, ok := child.Metadata["branch"].(string); ok && branch == value {
		return true
	}
	return false
}

func defaultDecisionBranch(node *evolution.SerializableNode) int {
	defaultBranch := ""
	if node.Metadata != nil {
		if d, ok := node.Metadata["default"].(string); ok {
			defaultBranch = d
		}
	}
	for i := range node.Children {
		child := &node.Children[i]
		if child.Metadata == nil {
			continue
		}
		if isDefault, ok := child.Metadata["default"].(bool); ok && isDefault {
			return i
		}
		if defaultBranch != "" {
			if branch, ok := child.Metadata["branch"].(string); ok && branch == defaultBranch {
				return i
			}
			if match, ok := child.Metadata["match"]; ok && stringifyDecisionValue(match) == defaultBranch {
				return i
			}
		}
	}
	return -1
}

func stringifyDecisionValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}
