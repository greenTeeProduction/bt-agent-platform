package blocks

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestRegistry_Phase2Blocks(t *testing.T) {
	reg := NewRegistry("")
	for _, id := range []string{
		"core:tools_default", "core:tools_dev", "core:tools_research",
		"core:delegate", "core:a2a_handoff", "core:parallel_fanout",
		"core:merge_results", "core:memory_load", "core:memory_write",
	} {
		if reg.Get(id) == nil {
			t.Fatalf("missing %s", id)
		}
	}
}

func TestPipelineWithToolsProfile(t *testing.T) {
	got := PipelineWithToolsProfile([]string{"core:pre_gate", "core:plan", "core:tool_execution"}, "dev")
	want := []string{"core:pre_gate", "core:plan", "core:tools_dev", "core:tool_execution"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestComposePreset_AgenticDev(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposePresetWithTools(reg, "agentic", "dev", "DevAgent", nil)
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
		t.Fatal("expected core:tools_dev ref in agentic:dev compose")
	}
}

func TestToolsDev_SetsDevTools(t *testing.T) {
	reg := NewRegistry("")
	tree, err := Compose(reg, ComposeSpec{
		Name:   "DevOnly",
		Blocks: []string{"core:pre_gate", "core:tools_dev"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	bb := &engine.Blackboard{
		Task:       "run go test on the blocks package",
		ChainState: make(map[string]any),
	}
	cmd, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	code := cmd.Run(ctx)
	if code != 1 {
		t.Fatalf("pre+tools should succeed, code=%d", code)
	}
	if len(bb.ChainTools) == 0 {
		t.Fatal("SetupDevTools should populate ChainTools")
	}
}

func TestMemoryWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	engine.AgentMemoryBaseDir = dir
	t.Cleanup(func() { engine.AgentMemoryBaseDir = "" })

	bb := &engine.Blackboard{
		Task:       "remember this",
		Result:     "summary of run for memory test with enough content",
		ChainState: map[string]any{"agent_name": "test-agent"},
	}
	load := engine.GetAction("LoadAgentMemory")
	write := engine.GetAction("WriteAgentMemory")
	if load == nil || write == nil {
		t.Fatal("memory actions not registered")
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if write(ctx) != 1 {
		t.Fatal("write failed")
	}
	bb2 := &engine.Blackboard{
		Task:       "new task",
		ChainState: map[string]any{"agent_name": "test-agent"},
	}
	ctx2 := btcore.NewBTContext(t.Context(), bb2)
	if load(ctx2) != 1 {
		t.Fatal("load failed")
	}
	mem, _ := bb2.ChainState["agent_memory"].(string)
	if mem == "" {
		t.Fatal("expected memory context")
	}
}
