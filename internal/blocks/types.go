// Package blocks provides reusable behavior-tree building blocks that can be
// composed into task/action trees on demand and referenced via SubTreeRef nodes.
package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// Category groups building blocks for discovery and evolution.
type Category string

const (
	CategoryCore     Category = "core"
	CategoryTool     Category = "tool"
	CategoryRecovery Category = "recovery"
	CategoryCustom   Category = "custom"
)

// Block is a named, reusable subtree registered for composition and evolution.
type Block struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Category    Category                    `json:"category"`
	Tree        *evolution.SerializableNode `json:"tree"`
	Mutable         bool                        `json:"mutable"`
	Version         int                         `json:"version"`
	PromotedVersion int                         `json:"promoted_version,omitempty"`
}

// ComposeSpec assembles a new tree from block IDs and optional middle section.
type ComposeSpec struct {
	Name   string   `json:"name"`
	Blocks []string `json:"blocks"`
	// Middle is inserted between leading and trailing blocks when set (e.g. StrategyRouter).
	Middle *evolution.SerializableNode `json:"middle,omitempty"`
}

// SubTreeRefNode returns a reference node that Expand resolves to a registered block.
func SubTreeRefNode(blockID string) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "SubTreeRef",
		Name:        "ref:" + blockID,
		Description: "Reference to reusable block " + blockID,
		Metadata: map[string]any{
			"block_id": blockID,
		},
	}
}

// BlockIDFromNode extracts the block id from a SubTreeRef or ref: name.
func BlockIDFromNode(n *evolution.SerializableNode) string {
	if n == nil {
		return ""
	}
	if n.Metadata != nil {
		if id, ok := n.Metadata["block_id"].(string); ok && id != "" {
			return id
		}
	}
	if len(n.Name) > 4 && n.Name[:4] == "ref:" {
		return n.Name[4:]
	}
	return ""
}
