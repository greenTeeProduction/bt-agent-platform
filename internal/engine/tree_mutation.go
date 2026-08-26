// Structural mutation ops applied to a live run's serializable tree
// (spec: docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md).
package engine

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Mutation origins. Only llm-origin grafts pass the error-handler proposal
// policy; the MCP entry point stamps operator, in-engine callers declare
// tree or llm.
const (
	OriginOperator = "operator"
	OriginTree     = "tree"
	OriginLLM      = "llm"
)

// MutationOp is one structural edit to a live tree.
type MutationOp struct {
	Kind       string                      `json:"kind"`                  // "add" | "remove"
	ParentPath string                      `json:"parent_path,omitempty"` // add: index path of parent ("" = root, "0.2" = root.Children[0].Children[2])
	Index      int                         `json:"index,omitempty"`       // add: insertion position; -1 = append
	Path       string                      `json:"path,omitempty"`        // remove: index path of the node to remove
	ExpectName string                      `json:"expect_name,omitempty"` // optional: resolved node's Name must match
	Subtree    *evolution.SerializableNode `json:"subtree,omitempty"`     // add: subtree to graft
	Persist    bool                        `json:"persist,omitempty"`     // snapshot resulting tree to the store
	Origin     string                      `json:"origin,omitempty"`
}

// cloneNode deep-copies n. Correspondence for state migration is computed
// separately by mapCorrespondence AFTER mutation ops run: applyMutationOp's
// slice insert can reallocate a parent's children backing array and move
// sibling addresses, so pairings captured during the clone would go stale.
func cloneNode(n *evolution.SerializableNode) *evolution.SerializableNode {
	if n == nil {
		return nil
	}
	cp := *n
	if n.Metadata != nil {
		cp.Metadata = make(map[string]any, len(n.Metadata))
		maps.Copy(cp.Metadata, n.Metadata)
	}
	if n.Edges != nil {
		cp.Edges = append([]evolution.TypedEdge(nil), n.Edges...)
		for i := range cp.Edges {
			if cp.Edges[i].Blackboard != nil {
				bb := make(map[string]string, len(cp.Edges[i].Blackboard))
				maps.Copy(bb, cp.Edges[i].Blackboard)
				cp.Edges[i].Blackboard = bb
			}
		}
	}
	if len(n.Children) > 0 {
		cp.Children = make([]evolution.SerializableNode, len(n.Children))
		for i := range n.Children {
			cp.Children[i] = *cloneNode(&n.Children[i])
		}
	}
	return &cp
}

// mapCorrespondence pairs every node of the old tree with its counterpart in
// the new tree (old → new in corr), walking both in lockstep. mutParent, when
// non-nil, is the node IN THE NEW TREE whose direct child list was changed by
// one (kind, at) op; its children pair with the appropriate index shift and
// the inserted/removed child stays unpaired. Everywhere else the shapes are
// identical by construction; a length mismatch stops pairing that subtree
// (defensive — unpaired nodes just start with fresh state).
func mapCorrespondence(oldN, newN, mutParent *evolution.SerializableNode, kind string, at int, corr map[*evolution.SerializableNode]*evolution.SerializableNode) {
	if oldN == nil || newN == nil {
		return
	}
	corr[oldN] = newN
	if newN == mutParent {
		switch kind {
		case "add":
			for i := range oldN.Children {
				ni := i
				if i >= at {
					ni = i + 1
				}
				if ni < len(newN.Children) {
					mapCorrespondence(&oldN.Children[i], &newN.Children[ni], mutParent, kind, at, corr)
				}
			}
		case "remove":
			for i := range oldN.Children {
				if i == at {
					continue // the removed child has no counterpart
				}
				ni := i
				if i > at {
					ni = i - 1
				}
				if ni < len(newN.Children) {
					mapCorrespondence(&oldN.Children[i], &newN.Children[ni], mutParent, kind, at, corr)
				}
			}
		}
		return
	}
	if len(oldN.Children) != len(newN.Children) {
		return
	}
	for i := range oldN.Children {
		mapCorrespondence(&oldN.Children[i], &newN.Children[i], mutParent, kind, at, corr)
	}
}

// maxChildrenForType bounds how many children a node type meaningfully
// executes: 0 for leaves; 1 for single-child decorators and for single-child
// builder-contract composites (ForEachTask, ReviewCycle, AbortOnEvent — their
// builders execute exactly Children[0]); 2 for two-child types; -1 for
// unbounded composites. buildNodeInner silently ignores extra children on
// leaf and single-child types, so an add beyond the cap would graft dead structure.
func maxChildrenForType(t string) int {
	switch t {
	case "Action", "Condition", "ChainAction", "AlwaysSucceed", "SubTreeRef":
		return 0
	case "QualityGate":
		return 2 // primary child + recovery child
	case "Retry", "Inverter", "Succeeder", "Repeater", "Runner", "Timeout",
		"Budget", "RateLimit", "CircuitBreaker", "Monitor",
		"CheckpointVerifier", "SemaphoreGuard", "CachedCondition",
		"ClaudeErrorHandler", "ForEachTask", "ReviewCycle", "AbortOnEvent":
		return 1
	default:
		return -1
	}
}

// parseIndexPath turns "0.2.1" into []int{0, 2, 1}; "" is the root.
func parseIndexPath(p string) ([]int, error) {
	if p == "" {
		return nil, nil
	}
	parts := strings.Split(p, ".")
	idx := make([]int, len(parts))
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("bad index path %q: segment %q", p, s)
		}
		idx[i] = n
	}
	return idx, nil
}

// resolveIndexPath walks root by child indices.
func resolveIndexPath(root *evolution.SerializableNode, idx []int) (*evolution.SerializableNode, error) {
	n := root
	for depth, i := range idx {
		if i >= len(n.Children) {
			return nil, fmt.Errorf("cannot resolve path: index %d at depth %d exceeds %d children of %s %q",
				i, depth, len(n.Children), n.Type, n.Name)
		}
		n = &n.Children[i]
	}
	return n, nil
}

// applyMutationOp applies op to root in place. It returns the directly-mutated
// parent node and the child index the op touched (for MemSequence cursor
// arithmetic in the apply loop). Structural guards live here; whole-tree
// validation is validateMutatedTree.
func applyMutationOp(root *evolution.SerializableNode, op MutationOp) (*evolution.SerializableNode, int, error) {
	switch op.Kind {
	case "add":
		if op.Subtree == nil {
			return nil, 0, fmt.Errorf("add: subtree is nil")
		}
		if treeHasSubTreeRefs(op.Subtree) {
			return nil, 0, fmt.Errorf("add: subtree contains SubTreeRef nodes — expand before grafting")
		}
		idx, err := parseIndexPath(op.ParentPath)
		if err != nil {
			return nil, 0, err
		}
		parent, err := resolveIndexPath(root, idx)
		if err != nil {
			return nil, 0, err
		}
		if op.ExpectName != "" && parent.Name != op.ExpectName {
			return nil, 0, fmt.Errorf("add: expect_name %q does not match parent %q", op.ExpectName, parent.Name)
		}
		if maxN := maxChildrenForType(parent.Type); maxN >= 0 && len(parent.Children)+1 > maxN {
			if maxN == 0 {
				return nil, 0, fmt.Errorf("add: %s %q is a leaf type and cannot take children", parent.Type, parent.Name)
			}
			return nil, 0, fmt.Errorf("add: %s %q executes at most %d children", parent.Type, parent.Name, maxN)
		}
		at := op.Index
		if at < 0 {
			at = len(parent.Children)
		}
		if at > len(parent.Children) {
			return nil, 0, fmt.Errorf("add: index %d out of range (parent has %d children)", at, len(parent.Children))
		}
		sub := cloneNode(op.Subtree) // never alias caller-owned memory
		parent.Children = append(parent.Children, evolution.SerializableNode{})
		copy(parent.Children[at+1:], parent.Children[at:])
		parent.Children[at] = *sub
		evolution.ShiftEdgeIndices(parent, at, 1)
		return parent, at, nil
	case "remove":
		idx, err := parseIndexPath(op.Path)
		if err != nil {
			return nil, 0, err
		}
		if len(idx) == 0 {
			return nil, 0, fmt.Errorf("remove: cannot remove the root node")
		}
		parent, err := resolveIndexPath(root, idx[:len(idx)-1])
		if err != nil {
			return nil, 0, err
		}
		at := idx[len(idx)-1]
		if at >= len(parent.Children) {
			return nil, 0, fmt.Errorf("cannot resolve path %q: index %d exceeds %d children", op.Path, at, len(parent.Children))
		}
		target := &parent.Children[at]
		if op.ExpectName != "" && target.Name != op.ExpectName {
			return nil, 0, fmt.Errorf("remove: expect_name %q does not match node %q", op.ExpectName, target.Name)
		}
		parent.Children = append(parent.Children[:at], parent.Children[at+1:]...)
		evolution.ShiftEdgeIndices(parent, at, -1)
		return parent, at, nil
	default:
		return nil, 0, fmt.Errorf("unknown mutation kind %q", op.Kind)
	}
}

// validateMutatedTree gates the whole post-op tree, plus the llm-origin
// allowlist policy on the grafted subtree (the same security boundary that
// guards ClaudeErrorHandler's auto-executed proposals).
func validateMutatedTree(root *evolution.SerializableNode, op MutationOp) error {
	info := ValidateTreeFull(root)
	if !info.Valid() {
		return fmt.Errorf("mutated tree fails validation: %v", info.Errors)
	}
	if op.Kind == "add" && op.Origin == OriginLLM {
		if err := validateErrorHandlerProposal(op.Subtree, map[string]bool{}); err != nil {
			return fmt.Errorf("llm-origin subtree rejected by proposal policy: %w", err)
		}
	}
	return nil
}
