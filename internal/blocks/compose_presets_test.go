package blocks

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestPipelineWithToolsProfile_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		blocks  []string
		profile string
		want    []string
	}{
		{
			name:    "inserts after core:plan when present",
			blocks:  []string{"core:pre_gate", "core:plan", "core:tool_execution"},
			profile: "dev",
			want:    []string{"core:pre_gate", "core:plan", "core:tools_dev", "core:tool_execution"},
		},
		{
			name:    "inserts after core:pre_gate when core:plan absent",
			blocks:  []string{"core:pre_gate", "core:tool_execution", "core:error_handling"},
			profile: "research",
			want:    []string{"core:pre_gate", "core:tools_research", "core:tool_execution", "core:error_handling"},
		},
		{
			name:    "prepends when neither core:plan nor core:pre_gate present",
			blocks:  []string{"core:tool_execution", "core:error_handling"},
			profile: "startup",
			want:    []string{"core:tools_startup", "core:tool_execution", "core:error_handling"},
		},
		{
			name:    "unknown profile falls back to default tools block",
			blocks:  []string{"core:pre_gate", "core:tool_execution"},
			profile: "bogus",
			want:    []string{"core:pre_gate", "core:tools_default", "core:tool_execution"},
		},
		{
			name:    "empty blocks with core:plan-less input still prepends",
			blocks:  []string{},
			profile: "universal",
			want:    []string{"core:tools_universal"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PipelineWithToolsProfile(tt.blocks, tt.profile)
			if len(got) != len(tt.want) {
				t.Fatalf("PipelineWithToolsProfile(%v, %q) = %v, want %v", tt.blocks, tt.profile, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("PipelineWithToolsProfile(%v, %q) = %v, want %v", tt.blocks, tt.profile, got, tt.want)
				}
			}
		})
	}
}

func TestComposePresetWithTools_Branches(t *testing.T) {
	reg := NewRegistry("")

	t.Run("default preset via empty string", func(t *testing.T) {
		tree, err := ComposePresetWithTools(reg, "", "", "DefaultPreset", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Children) != len(DefaultTaskBlocks) {
			t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocks), len(tree.Children))
		}
	})

	t.Run("default preset literal", func(t *testing.T) {
		tree, err := ComposePresetWithTools(reg, "default", "", "DefaultPreset2", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Children) != len(DefaultTaskBlocks) {
			t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocks), len(tree.Children))
		}
	})

	t.Run("hitl preset without strategy", func(t *testing.T) {
		tree, err := ComposePresetWithTools(reg, "hitl", "", "HitlPreset", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Children) != len(DefaultTaskBlocksWithHITL) {
			t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocksWithHITL), len(tree.Children))
		}
	})

	t.Run("hitl preset with strategy inserted after core:human_gate", func(t *testing.T) {
		strategy := &evolution.SerializableNode{
			Type: "Selector",
			Name: "StrategyRouter",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "MarkSuccessful"},
			},
		}
		tree, err := ComposePresetWithTools(reg, "hitl", "", "HitlStrategy", strategy)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Children) != len(DefaultTaskBlocksWithHITL)+1 {
			t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocksWithHITL)+1, len(tree.Children))
		}
		idx := -1
		for i, c := range tree.Children {
			if BlockIDFromNode(&c) == "core:human_gate" {
				idx = i
			}
		}
		if idx == -1 {
			t.Fatal("expected core:human_gate ref in hitl compose")
		}
		if tree.Children[idx+1].Name != "StrategyRouter" {
			t.Fatalf("expected strategy immediately after core:human_gate, got %q", tree.Children[idx+1].Name)
		}
	})

	t.Run("full preset without strategy", func(t *testing.T) {
		tree, err := ComposePresetWithTools(reg, "full", "", "FullPreset", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Children) != len(DefaultTaskBlocksFull) {
			t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocksFull), len(tree.Children))
		}
	})

	t.Run("full preset with strategy inserted after core:plan", func(t *testing.T) {
		strategy := &evolution.SerializableNode{
			Type: "Selector",
			Name: "StrategyRouter",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "MarkSuccessful"},
			},
		}
		tree, err := ComposePresetWithTools(reg, "full", "", "FullStrategy", strategy)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Children) != len(DefaultTaskBlocksFull)+1 {
			t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocksFull)+1, len(tree.Children))
		}
		idx := -1
		for i, c := range tree.Children {
			if BlockIDFromNode(&c) == "core:plan" {
				idx = i
			}
		}
		if idx == -1 {
			t.Fatal("expected core:plan ref in full compose")
		}
		if tree.Children[idx+1].Name != "StrategyRouter" {
			t.Fatalf("expected strategy immediately after core:plan, got %q", tree.Children[idx+1].Name)
		}
	})

	t.Run("delegate preset ignores strategy and tools profile", func(t *testing.T) {
		tree, err := ComposePresetWithTools(reg, "delegate", "dev", "DelegatePreset", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"core:pre_gate", "core:delegate"}
		if len(tree.Children) != len(want) {
			t.Fatalf("expected %d refs, got %d", len(want), len(tree.Children))
		}
		for i, id := range want {
			if BlockIDFromNode(&tree.Children[i]) != id {
				t.Fatalf("child %d = %q, want %q", i, BlockIDFromNode(&tree.Children[i]), id)
			}
		}
	})

	t.Run("unknown preset returns error", func(t *testing.T) {
		_, err := ComposePresetWithTools(reg, "no-such-preset", "", "Bad", nil)
		if err == nil {
			t.Fatal("expected error for unknown preset")
		}
		if !strings.Contains(err.Error(), "no-such-preset") {
			t.Fatalf("expected error to mention preset name, got %q", err.Error())
		}
	})

	t.Run("nil registry falls back to DefaultRegistry", func(t *testing.T) {
		tree, err := ComposePresetWithTools(nil, "agentic", "", "NilReg", nil)
		if err != nil {
			t.Fatal(err)
		}
		if tree == nil {
			t.Fatal("expected non-nil tree")
		}
	})
}

func TestComposePreset_ColonEmbeddedToolsProfile(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposePreset(reg, "agentic:dev", "ColonPreset", nil)
	if err != nil {
		t.Fatal(err)
	}
	foundDev := false
	for _, c := range tree.Children {
		if BlockIDFromNode(&c) == "core:tools_dev" {
			foundDev = true
		}
	}
	if !foundDev {
		t.Fatal("expected preset string \"agentic:dev\" to select the dev tools profile")
	}
}

func TestComposePreset_ExplicitProfileOverridesColonSuffix(t *testing.T) {
	reg := NewRegistry("")
	// When toolsProfile is explicitly set, the colon suffix on preset is ignored as profile
	// and only used to select the base preset.
	tree, err := ComposePresetWithTools(reg, "agentic:dev", "research", "OverridePreset", nil)
	if err != nil {
		t.Fatal(err)
	}
	foundResearch := false
	for _, c := range tree.Children {
		if BlockIDFromNode(&c) == "core:tools_research" {
			foundResearch = true
		}
	}
	if !foundResearch {
		t.Fatal("expected explicit toolsProfile \"research\" to take precedence over colon suffix")
	}
}

func TestListToolProfileBlocks(t *testing.T) {
	got := ListToolProfileBlocks()
	want := []string{
		"core:tools_default",
		"core:tools_dev",
		"core:tools_research",
		"core:tools_startup",
		"core:tools_universal",
	}
	if len(got) != len(want) {
		t.Fatalf("ListToolProfileBlocks() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListToolProfileBlocks()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestComposeOrderedWithMiddle_UnknownBlockErrors(t *testing.T) {
	reg := NewRegistry("")
	_, err := composeOrderedWithMiddle(reg, "Bad", []string{"core:pre_gate", "core:no-such-block"}, "core:pre_gate", nil, false)
	if err == nil {
		t.Fatal("expected error for unknown block id")
	}
	if !strings.Contains(err.Error(), "core:no-such-block") {
		t.Fatalf("expected error to mention unknown block id, got %q", err.Error())
	}
}

func TestComposeOrderedWithMiddle_DefaultsNameWhenEmpty(t *testing.T) {
	reg := NewRegistry("")
	tree, err := composeOrderedWithMiddle(reg, "", []string{"core:pre_gate"}, "core:pre_gate", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Name != "Composed_Main" {
		t.Fatalf("expected default name Composed_Main, got %q", tree.Name)
	}
}

func TestComposeOrderedWithMiddle_Inline(t *testing.T) {
	reg := NewRegistry("")
	tree, err := composeOrderedWithMiddle(reg, "Inline", []string{"core:pre_gate"}, "core:pre_gate", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tree.Children))
	}
	if tree.Children[0].Type == "SubTreeRef" {
		t.Fatal("expected inline expansion, not SubTreeRef")
	}
}

func TestProfileOrDefault(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "default"},
		{"   ", "default"},
		{"dev", "dev"},
		{"  research  ", "research"},
	}
	for _, tt := range tests {
		if got := profileOrDefault(tt.in); got != tt.want {
			t.Errorf("profileOrDefault(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSliceContains(t *testing.T) {
	ss := []string{"a", "b", "c"}
	if !sliceContains(ss, "b") {
		t.Error("expected sliceContains to find existing element")
	}
	if sliceContains(ss, "z") {
		t.Error("expected sliceContains to not find missing element")
	}
	if sliceContains(nil, "a") {
		t.Error("expected sliceContains(nil, ...) to be false")
	}
}
