package blocks

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Compose builds a task/action tree from block IDs and an optional middle section.
// Each block is inserted as a SubTreeRef (expanded at build time) or inlined when expand=true.
func Compose(reg *Registry, spec ComposeSpec, inline bool) (*evolution.SerializableNode, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	if len(spec.Blocks) == 0 && spec.Middle == nil {
		return nil, fmt.Errorf("compose: at least one block or middle section required")
	}

	var children []evolution.SerializableNode
	for _, id := range spec.Blocks {
		if reg.Get(id) == nil {
			return nil, fmt.Errorf("compose: unknown block %q", id)
		}
		if inline {
			b := reg.Get(id)
			children = append(children, *cloneTree(b.Tree))
		} else {
			children = append(children, SubTreeRefNode(id))
		}
	}

	if spec.Middle != nil {
		// Insert middle after first block when multiple leading blocks exist, else append before last recovery block
		mid := *cloneTree(spec.Middle)
		if len(children) == 0 {
			children = append(children, mid)
		} else if len(children) == 1 {
			children = append(children, mid)
		} else {
			// [pre..., middle, ...post] — insert middle between first and rest
			rest := append([]evolution.SerializableNode{mid}, children[1:]...)
			children = append([]evolution.SerializableNode{children[0]}, rest...)
		}
	}

	name := spec.Name
	if name == "" {
		name = "Composed_Main"
	}
	root := &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     name,
		Children: children,
	}
	if errs := root.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("compose: invalid tree: %v", errs)
	}
	return root, nil
}

// ComposeTaskTree is a convenience wrapper for the default pre_gate → tool_execution → error_handling pipeline.
func ComposeTaskTree(reg *Registry, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	spec := ComposeSpec{
		Name:   name,
		Blocks: append([]string{}, DefaultTaskBlocks...),
		Middle: strategy,
	}
	// Reorder: pre_gate, [strategy], tool_execution, error_handling
	if strategy != nil {
		spec.Blocks = []string{"core:pre_gate", "core:tool_execution", "core:error_handling"}
		spec.Middle = strategy
		// Compose with middle between pre_gate and tool_execution
		return composeWithMiddle(reg, spec, false)
	}
	return Compose(reg, spec, false)
}

func composeWithMiddle(reg *Registry, spec ComposeSpec, inline bool) (*evolution.SerializableNode, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	var children []evolution.SerializableNode
	addBlock := func(id string) error {
		if reg.Get(id) == nil {
			return fmt.Errorf("unknown block %q", id)
		}
		if inline {
			b := reg.Get(id)
			children = append(children, *cloneTree(b.Tree))
		} else {
			children = append(children, SubTreeRefNode(id))
		}
		return nil
	}

	// pre_gate, middle (strategy), tool_execution, error_handling
	if err := addBlock("core:pre_gate"); err != nil {
		return nil, err
	}
	if spec.Middle != nil {
		children = append(children, *cloneTree(spec.Middle))
	}
	if err := addBlock("core:tool_execution"); err != nil {
		return nil, err
	}
	if err := addBlock("core:error_handling"); err != nil {
		return nil, err
	}

	name := spec.Name
	if name == "" {
		name = "Composed_Main"
	}
	root := &evolution.SerializableNode{Type: "Sequence", Name: name, Children: children}
	if errs := root.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("compose: %v", errs)
	}
	return root, nil
}

// ComposeFromRefs builds a tree using only SubTreeRef children (expanded at runtime).
func ComposeFromRefs(blockIDs ...string) (*evolution.SerializableNode, error) {
	return Compose(DefaultRegistry, ComposeSpec{
		Name:   "Composed_Main",
		Blocks: blockIDs,
	}, false)
}
