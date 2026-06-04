package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
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

func TestDataPipelineTree_UsesObservedFileMetrics(t *testing.T) {
	tmp := t.TempDir()
	csvPath := filepath.Join(tmp, "input.csv")
	if err := os.WriteFile(csvPath, []byte("name,value\na,1\nb,2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{Task: "extract data from " + csvPath, ChainState: map[string]any{}}
	bt := BuildTree(domains.DataPipelineTree(), bb)
	RunTask(bb, bt)

	if !strings.Contains(bb.Result, "csv_records_observed: 3") {
		t.Fatalf("expected real csv record count, got: %s", bb.Result)
	}
	if strings.Contains(bb.Result, "10,420") || strings.Contains(bb.Result, "10,418") {
		t.Fatalf("fabricated canned row count leaked: %s", bb.Result)
	}
	if !strings.Contains(bb.Result, "verification: passed anti-fabrication gate") {
		t.Fatalf("expected anti-fabrication verification, got: %s", bb.Result)
	}
}

func TestDataPipelineTree_NoSourceReportsBlockedNotFabricated(t *testing.T) {
	bb := &Blackboard{Task: "run ETL workflow with no source path", ChainState: map[string]any{}}
	bt := BuildTree(domains.DataPipelineTree(), bb)
	RunTask(bb, bt)

	if !strings.Contains(bb.Result, "status: blocked") {
		t.Fatalf("expected blocked report, got: %s", bb.Result)
	}
	if strings.Contains(bb.Result, "10,420") || strings.Contains(bb.Result, "10,418") {
		t.Fatalf("fabricated canned row count leaked: %s", bb.Result)
	}
	if !strings.Contains(bb.Result, "available_tools:") {
		t.Fatalf("expected discovered tool list in report, got: %s", bb.Result)
	}
}

func TestNotebookLMTree_PreGateDiscoversRealToolset(t *testing.T) {
	tree := domains.NotebookLMTree()
	if len(tree.Children) == 0 || tree.Children[0].Name != "NotebookLM_PreGate" {
		t.Fatalf("expected NotebookLM_PreGate first child, got %#v", tree.Children)
	}
	preGate := tree.Children[0]
	seenSetup := false
	seenDiscover := false
	for _, child := range preGate.Children {
		if child.Name == "SetupNotebookLMTools" {
			seenSetup = true
		}
		if child.Name == "DiscoverAvailableTools" {
			seenDiscover = true
		}
	}
	if !seenSetup || !seenDiscover {
		t.Fatalf("NotebookLM pre-gate must setup and discover real tools before auth/use; setup=%v discover=%v", seenSetup, seenDiscover)
	}

	bb := &Blackboard{ChainState: map[string]any{}}
	if fn := GetAction("SetupNotebookLMTools"); fn == nil || fn(&btcore.BTContext[Blackboard]{Blackboard: bb}) != 1 {
		t.Fatal("SetupNotebookLMTools action missing or failed")
	}
	if fn := GetAction("DiscoverAvailableTools"); fn == nil || fn(&btcore.BTContext[Blackboard]{Blackboard: bb}) != 1 {
		t.Fatal("DiscoverAvailableTools action missing or failed")
	}
	available, _ := bb.ChainState["available_tools"].(string)
	for _, name := range []string{"notebooklm_server_info", "notebooklm_list", "notebooklm_notebook_query", "file_write"} {
		if !strings.Contains(available, name) {
			t.Fatalf("NotebookLM available tools missing %q: %s", name, available)
		}
	}
}
