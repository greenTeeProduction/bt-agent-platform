package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestRealToolFactory_DiscoveryAndFailClosed(t *testing.T) {
	factory := NewRealToolFactory()
	for _, name := range []string{"shell_exec", "file_read", "file_write", "notebooklm_server_info", "notebooklm_notebook_query"} {
		if _, ok := factory[name]; !ok {
			t.Fatalf("missing real tool factory entry %q; available=%v", name, allRealToolNames())
		}
	}

	if tool, ok := buildRealTool("definitely_not_real"); ok || tool != nil {
		t.Fatalf("unknown tools must fail closed, got ok=%v tool=%v", ok, tool)
	}

	tools := buildRealTools("file_read", "definitely_not_real", "calculator")
	bb := &Blackboard{ChainTools: tools}
	available := availableToolNames(bb)
	if !strings.Contains(available, "file_read") || !strings.Contains(available, "calculator") {
		t.Fatalf("factory-built tools not discoverable: %q", available)
	}
	if strings.Contains(available, "definitely_not_real") {
		t.Fatalf("unknown tool leaked into discovered tools: %q", available)
	}
}

func TestExecuteAgentTool_NeverSimulatesMissingTool(t *testing.T) {
	bb := &Blackboard{
		ChainTools: []any{newCalculatorTool()},
		LLM:        &MockLLM{GenerateResp: "SIMULATED_OUTPUT_SHOULD_NOT_APPEAR"},
	}

	result := executeAgentTool("shell_exec", "echo should-not-run", bb)

	if !strings.Contains(result, "TOOL_UNAVAILABLE") {
		t.Fatalf("expected TOOL_UNAVAILABLE, got %q", result)
	}
	if strings.Contains(result, "SIMULATED_OUTPUT_SHOULD_NOT_APPEAR") {
		t.Fatalf("missing real tool was simulated by LLM: %q", result)
	}
	if !strings.Contains(result, "calculator") {
		t.Fatalf("expected available real tool discovery in error, got %q", result)
	}
}

func TestToolAction_MissingToolFailsClosed(t *testing.T) {
	bb := &Blackboard{Task: "test", LLM: &MockLLM{GenerateResp: "SIMULATED_OUTPUT_SHOULD_NOT_APPEAR"}}
	tree := &evolution.SerializableNode{Type: "ChainAction", Name: "tool_action:nonexistent:test"}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Fatalf("expected fail-closed failure, got outcome=%q result=%q", bb.Outcome, bb.Result)
	}
	if !strings.Contains(bb.Result, "TOOL_UNAVAILABLE") {
		t.Fatalf("expected TOOL_UNAVAILABLE result, got %q", bb.Result)
	}
	if strings.Contains(bb.Result, "SIMULATED_OUTPUT_SHOULD_NOT_APPEAR") {
		t.Fatalf("missing real tool was simulated by LLM: %q", bb.Result)
	}
}

func TestSetupDefaultToolsRegistersRealTools(t *testing.T) {
	bb := &Blackboard{}
	fn := GetAction("SetupDefaultTools")
	if fn == nil {
		t.Fatal("SetupDefaultTools action missing")
	}
	if fn(&btcore.BTContext[Blackboard]{Blackboard: bb}) != 1 {
		t.Fatal("SetupDefaultTools failed")
	}
	available := availableToolNames(bb)
	for _, name := range []string{"shell_exec", "file_read", "file_write", "http_get", "web_search", "calculator"} {
		if !strings.Contains(available, name) {
			t.Fatalf("default toolset missing %q: %s", name, available)
		}
	}
	if got, _ := bb.ChainState["available_tools"].(string); !strings.Contains(got, "file_read") {
		t.Fatalf("available tools not recorded in ChainState: %#v", bb.ChainState)
	}
}

func TestEnsureTaskToolsCreatesRequestedTools(t *testing.T) {
	bb := &Blackboard{Task: "NotebookLM research and query a notebook"}
	fn := GetAction("EnsureTaskTools")
	if fn == nil {
		t.Fatal("EnsureTaskTools action missing")
	}
	if fn(&btcore.BTContext[Blackboard]{Blackboard: bb}) != 1 {
		t.Fatal("EnsureTaskTools failed")
	}
	available := availableToolNames(bb)
	for _, name := range []string{"notebooklm_server_info", "notebooklm_list", "notebooklm_notebook_query", "notebooklm_research_start"} {
		if !strings.Contains(available, name) {
			t.Fatalf("on-demand tool factory did not create %q; available=%s", name, available)
		}
	}
	if got, _ := bb.ChainState["created_tools"].(string); !strings.Contains(got, "notebooklm_server_info") {
		t.Fatalf("created_tools missing NotebookLM evidence: %#v", bb.ChainState)
	}
}

func TestAgentWithRealToolsBlocksFinalAnswerWithoutToolUse(t *testing.T) {
	bb := &Blackboard{
		Task:       "review code in /tmp/example.go",
		ChainTools: buildRealTools("file_read"),
		LLM:        &MockLLM{GenerateResp: "Final Answer: fabricated review without reading files"},
	}
	cfg := ChainConfig{ChainType: "agent", Prompt: "{{.Task}}", MaxTokens: 1}

	result := execAgent(cfg, bb)

	if result != 1 {
		t.Fatalf("expected honest blocked result, got result=%d outcome=%q output=%q", result, bb.Outcome, bb.Result)
	}
	if !strings.Contains(bb.Result, "No Tool Evidence") || strings.Contains(bb.Result, "fabricated review") {
		t.Fatalf("expected blocked anti-fabrication output, got %q", bb.Result)
	}
}
