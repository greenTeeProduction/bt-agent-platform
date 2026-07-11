package blocks

import (
	"slices"
	"strings"
	"testing"
)

// Characterization tests for builtin.go. These pin the *current* exported
// behavior of the built-in block set (registered via builtinBlocks() and
// surfaced through the registry) plus the DefaultTaskBlocks pipeline order.
// They exist to lock in observable behavior before future refactors; they make
// no production changes.

// wantBuiltinBlockCount is the golden total pinned by this characterization
// test: 24 core blocks declared in builtin.go plus the 5 tool-profile blocks
// appended by ToolProfileBlocks(). This locks the size of the built-in set so a
// future refactor that accidentally drops or double-registers a block is caught.
const wantBuiltinBlockCount = 29

func TestBuiltinBlocks_Count(t *testing.T) {
	got := len(builtinBlocks())
	if got != wantBuiltinBlockCount {
		t.Fatalf("builtinBlocks() returned %d blocks, want %d (pin the observed count in GREEN)", got, wantBuiltinBlockCount)
	}
}

// TestDefaultTaskBlocks_Order pins the non-obvious pipeline order: the default
// task pipeline is [pre_gate, tool_execution, error_handling], but
// PipelineWithToolsProfile inserts core:tools_default right after pre_gate.
func TestDefaultTaskBlocks_Order(t *testing.T) {
	want := []string{
		"core:pre_gate",
		"core:tools_default",
		"core:tool_execution",
		"core:error_handling",
	}
	if !slices.Equal(DefaultTaskBlocks, want) {
		t.Fatalf("DefaultTaskBlocks = %v, want %v", DefaultTaskBlocks, want)
	}
}

// TestBuiltinBlocks_Metadata pins category / mutability / version for a
// representative sample of built-in blocks as read back through the registry.
func TestBuiltinBlocks_Metadata(t *testing.T) {
	reg := NewRegistry("")
	cases := []struct {
		id       string
		category Category
		mutable  bool
		version  int
	}{
		{"core:pre_gate", CategoryCore, false, 3},
		{"core:tool_execution", CategoryTool, true, 2},
		{"core:error_handling", CategoryRecovery, false, 2},
		{"core:human_gate", CategoryCore, false, 1},
		{"core:plan", CategoryCore, false, 1},
		{"core:rag_gate", CategoryCore, true, 1},
		{"core:reflect_only", CategoryRecovery, true, 2},
		{"core:dlq_escalate", CategoryRecovery, false, 1},
		{"core:merge_results", CategoryCore, false, 1},
		{"core:tools_default", CategoryTool, false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			b := reg.Get(tc.id)
			if b == nil {
				t.Fatalf("block %q not registered", tc.id)
			}
			if b.Category != tc.category {
				t.Errorf("category = %q, want %q", b.Category, tc.category)
			}
			if b.Mutable != tc.mutable {
				t.Errorf("mutable = %v, want %v", b.Mutable, tc.mutable)
			}
			if b.Version != tc.version {
				t.Errorf("version = %d, want %d", b.Version, tc.version)
			}
		})
	}
}

// TestBuiltinBlocks_ReliabilityWrapping pins the reliability-wrapper behavior:
// blocks named in builtinBlocks' switch are wrapped by ApplyReliability, which
// renames the tree root to "<Name>_Reliable" and makes the root a Selector when
// the spec is graceful, or a Sequence when it is not. Blocks outside the switch
// keep their raw tree root.
func TestBuiltinBlocks_ReliabilityWrapping(t *testing.T) {
	reg := NewRegistry("")
	wrapped := []struct {
		id       string
		rootName string
		rootType string
	}{
		{"core:pre_gate", "PreGate_Reliable", "Selector"},
		{"core:tool_execution", "ToolExecution_Reliable", "Selector"},
		{"core:error_handling", "ErrorHandling_Reliable", "Selector"},
		{"core:plan", "Plan_Reliable", "Selector"},
		{"core:rag_gate", "RAGGate_Reliable", "Selector"},
		{"core:clarify_gate", "ClarifyGate_Reliable", "Selector"},
		{"core:quality_gate", "QualityGate_Reliable", "Selector"},
		{"core:delegate", "Delegate_Reliable", "Selector"},
		{"core:a2a_handoff", "A2AHandoff_Reliable", "Selector"},
		{"core:parallel_fanout", "ParallelFanout_Reliable", "Selector"},
		{"core:reflect_only", "ReflectOnly_Reliable", "Selector"},
		{"core:tools_default", "ToolsDefault_Reliable", "Sequence"},
	}
	for _, tc := range wrapped {
		t.Run(tc.id, func(t *testing.T) {
			b := reg.Get(tc.id)
			if b == nil || b.Tree == nil {
				t.Fatalf("block %q missing tree", tc.id)
			}
			if b.Tree.Name != tc.rootName {
				t.Errorf("root name = %q, want %q", b.Tree.Name, tc.rootName)
			}
			if b.Tree.Type != tc.rootType {
				t.Errorf("root type = %q, want %q", b.Tree.Type, tc.rootType)
			}
		})
	}

	// Blocks outside the reliability switch keep their raw tree root (no wrapper).
	for _, id := range []string{"core:merge_results", "core:audit_log", "core:human_gate"} {
		b := reg.Get(id)
		if b == nil || b.Tree == nil {
			t.Fatalf("block %q missing tree", id)
		}
		if strings.HasSuffix(b.Tree.Name, "_Reliable") {
			t.Errorf("block %q root %q unexpectedly wrapped", id, b.Tree.Name)
		}
	}
}

// TestBuiltinBlocks_UniqueNonEmptyIDs pins the invariant that every built-in
// block has a non-empty, unique, core:-prefixed ID and a non-nil tree.
func TestBuiltinBlocks_UniqueNonEmptyIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range builtinBlocks() {
		if b.ID == "" {
			t.Errorf("built-in block with empty ID: %+v", b)
			continue
		}
		if !strings.HasPrefix(b.ID, "core:") {
			t.Errorf("built-in block %q missing core: prefix", b.ID)
		}
		if seen[b.ID] {
			t.Errorf("duplicate built-in block ID %q", b.ID)
		}
		seen[b.ID] = true
		if b.Tree == nil {
			t.Errorf("built-in block %q has nil tree", b.ID)
		}
	}
}
