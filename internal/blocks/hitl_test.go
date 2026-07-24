package blocks

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Characterization tests for hitl.go. These pin the *current* exported
// behavior of HumanGateBlock, DefaultTaskBlocksWithHITL, and
// ComposeTaskTreeWithHITL before any future refactor.

func TestHumanGateBlock_Shape(t *testing.T) {
	n := HumanGateBlock("ReviewGate", "Approve deployment?")
	if n.Type != "HumanApprovalGate" {
		t.Errorf("Type = %q, want %q", n.Type, "HumanApprovalGate")
	}
	if n.Name != "ReviewGate" {
		t.Errorf("Name = %q, want %q", n.Name, "ReviewGate")
	}
	if n.Description != "Approve deployment?" {
		t.Errorf("Description = %q, want %q", n.Description, "Approve deployment?")
	}
	if n.Children == nil {
		t.Error("Children = nil, want non-nil empty slice")
	}
	if len(n.Children) != 0 {
		t.Errorf("len(Children) = %d, want 0", len(n.Children))
	}
	prompt, ok := n.Metadata["prompt"]
	if !ok {
		t.Fatal("Metadata missing \"prompt\" key")
	}
	if prompt != "Approve deployment?" {
		t.Errorf("Metadata[\"prompt\"] = %v, want %q", prompt, "Approve deployment?")
	}
}

func TestHumanGateBlock_TableDriven(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"empty prompt", ""},
		{"empty name and prompt", ""},
		{"multiline prompt", "Line one.\nLine two."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := HumanGateBlock(tt.name, tt.prompt)
			if n.Name != tt.name {
				t.Errorf("Name = %q, want %q", n.Name, tt.name)
			}
			if n.Description != tt.prompt {
				t.Errorf("Description = %q, want %q", n.Description, tt.prompt)
			}
			if n.Metadata["prompt"] != tt.prompt {
				t.Errorf("Metadata[\"prompt\"] = %v, want %q", n.Metadata["prompt"], tt.prompt)
			}
		})
	}
}

// TestHumanGateBlock_ReturnsFreshValues pins that each call returns an
// independent Metadata map — mutating one call's result must not leak into
// a subsequent call.
func TestHumanGateBlock_ReturnsFreshValues(t *testing.T) {
	a := HumanGateBlock("Gate", "prompt")
	a.Metadata["prompt"] = "mutated"
	b := HumanGateBlock("Gate", "prompt")
	if b.Metadata["prompt"] != "prompt" {
		t.Errorf("mutation of one HumanGateBlock() result leaked into another: got %v", b.Metadata["prompt"])
	}
}

// TestDefaultTaskBlocksWithHITL_Order pins the exact block order produced by
// PipelineWithToolsProfile for the HITL pipeline: the "default" tools block
// is inserted immediately after core:pre_gate (core:plan is absent from the
// input list).
func TestDefaultTaskBlocksWithHITL_Order(t *testing.T) {
	want := []string{
		"core:pre_gate",
		"core:tools_default",
		"core:human_gate",
		"core:tool_execution",
		"core:error_handling",
	}
	if len(DefaultTaskBlocksWithHITL) != len(want) {
		t.Fatalf("DefaultTaskBlocksWithHITL = %v, want %v", DefaultTaskBlocksWithHITL, want)
	}
	for i := range want {
		if DefaultTaskBlocksWithHITL[i] != want[i] {
			t.Errorf("DefaultTaskBlocksWithHITL[%d] = %q, want %q", i, DefaultTaskBlocksWithHITL[i], want[i])
		}
	}
}

func TestComposeTaskTreeWithHITL_NilRegistryUsesDefault(t *testing.T) {
	tree, err := ComposeTaskTreeWithHITL(nil, "NilReg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestComposeTaskTreeWithHITL_RootName(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposeTaskTreeWithHITL(reg, "MyHITLTask", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Type != "Sequence" {
		t.Errorf("root Type = %q, want %q", tree.Type, "Sequence")
	}
	if tree.Name != "MyHITLTask" {
		t.Errorf("root Name = %q, want %q", tree.Name, "MyHITLTask")
	}
}

func TestComposeTaskTreeWithHITL_EmptyNameDefaults(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposeTaskTreeWithHITL(reg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Name != "Composed_Main" {
		t.Errorf("root Name = %q, want %q", tree.Name, "Composed_Main")
	}
}

// TestComposeTaskTreeWithHITL_IncludesHumanGate pins the documented contract
// of ComposeTaskTreeWithHITL ("composes the task pipeline with human
// approval before execution") and the "bt_hitl_compose_task" MCP tool built
// on top of it ("Compose a task tree with human approval before tool
// execution"): the composed tree must reference core:human_gate.
func TestComposeTaskTreeWithHITL_IncludesHumanGate(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposeTaskTreeWithHITL(reg, "HITLTask", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	gotIDs := make([]string, 0, len(tree.Children))
	for _, c := range tree.Children {
		id := BlockIDFromNode(&c)
		gotIDs = append(gotIDs, id)
		if id == "core:human_gate" {
			found = true
		}
	}
	if !found {
		t.Errorf("ComposeTaskTreeWithHITL tree does not reference core:human_gate; got children %v", gotIDs)
	}
}

func TestComposeTaskTreeWithHITL_WithStrategy(t *testing.T) {
	reg := NewRegistry("")
	strategy := &evolution.SerializableNode{
		Type: "Selector",
		Name: "StrategyRouter",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	tree, err := ComposeTaskTreeWithHITL(reg, "HITLWithStrategy", strategy)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range tree.Children {
		if c.Name == "StrategyRouter" {
			found = true
		}
	}
	if !found {
		t.Error("expected StrategyRouter middle section in composed HITL tree")
	}
}

func TestComposeTaskTreeWithHITL_Validates(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposeTaskTreeWithHITL(reg, "ValidateMe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if errs := tree.Validate(); len(errs) != 0 {
		t.Errorf("Validate() = %v, want no errors", errs)
	}
}
