package engine

import (
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── extractDuckDuckGoResults edge-case tests ───

func TestExtractDuckDuckGoResults_MultipleResults(t *testing.T) {
	html := `<div class="result"><span class="result__snippet">Go programming guide</span><span class="result__url">example.com/go</span></div>
	<div class="result"><span class="result__snippet">Another result here</span><span class="result__url">example.org/other</span></div>`
	result := extractDuckDuckGoResults(html)
	if !strings.Contains(result, "Go programming guide") {
		t.Errorf("expected 'Go programming guide' in result, got %q", result)
	}
	if !strings.Contains(result, "example.com/go") {
		t.Errorf("expected 'example.com/go' in result, got %q", result)
	}
}

func TestExtractDuckDuckGoResults_NoMatches(t *testing.T) {
	html := `<html><body>No relevant content here</body></html>`
	result := extractDuckDuckGoResults(html)
	if result != "" {
		t.Errorf("expected empty result for non-DDG HTML, got %q", result)
	}
}

func TestExtractDuckDuckGoResults_FallbackResultA(t *testing.T) {
	html := `<a class="result__a" href="https://example.com/page">Example Title</a>`
	result := extractDuckDuckGoResults(html)
	if !strings.Contains(result, "Example Title") {
		t.Errorf("expected 'Example Title' in fallback result, got %q", result)
	}
	if !strings.Contains(result, "example.com/page") {
		t.Errorf("expected 'example.com/page' in fallback result, got %q", result)
	}
}

func TestExtractDuckDuckGoResults_ManyResults(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString(`<div class="result"><span class="result__snippet">`)
		sb.WriteString(strings.Repeat("a", 100))
		sb.WriteString(`</span><span class="result__url">example.com/`)
		sb.WriteString(strings.Repeat("b", 30))
		sb.WriteString(`</span></div>`)
	}
	html := sb.String()
	result := extractDuckDuckGoResults(html)
	if result == "" {
		t.Error("expected non-empty result from many snippets")
	}
}

// ─── trim function (registry.go) ───

func TestTrim_SpaceVariants(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello  ", "hello"},
		{"\t\tworld\n\n", "world"},
		{"  \t\n  ", ""},
		{"no-space", "no-space"},
		{"", ""},
		{"   multiple   words   ", "multiple   words"},
		{"\nleading", "leading"},
		{"trailing\n", "trailing"},
		{"\t\ttab\t\t", "tab"},
	}
	for _, tt := range tests {
		got := trim(tt.input)
		if got != tt.expected {
			t.Errorf("trim(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ─── ValidateTreeFull edge cases ───

func TestValidateTreeFull_UnknownNodeType(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "UnknownType",
		Name: "TestNode",
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	foundUnknown := false
	for _, err := range info.Errors {
		if strings.Contains(err, "unknown node type") {
			foundUnknown = true
			break
		}
	}
	if !foundUnknown {
		t.Errorf("expected 'unknown node type' error, got errors: %v", info.Errors)
	}
}

func TestValidateTreeFull_ActionNode(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Action",
		Name: "TestAction",
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.ActionName != "TestAction" {
		t.Errorf("expected ActionName 'TestAction', got %q", info.ActionName)
	}
}

func TestValidateTreeFull_SingleNode(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "RootSeq",
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.NodeCount < 1 {
		t.Errorf("expected at least 1 node, got %d", info.NodeCount)
	}
}

// ─── computeTreeMetrics edge cases ───

func TestComputeTreeMetrics_SingleLeaf(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Action", Name: "test"}
	info := &evolution.NodeValidationInfo{}
	depth, count, par, retries, timeout, sec := computeTreeMetrics(tree, info)
	if depth != 0 {
		t.Errorf("expected depth 0 for single leaf node, got %d", depth)
	}
	if count != 1 {
		t.Errorf("expected count 1 for single node, got %d", count)
	}
	if par != 0 {
		t.Errorf("expected parallel width 0 for single node, got %d", par)
	}
	if retries != 0 {
		t.Errorf("expected retries 0 for single node, got %d", retries)
	}
	if timeout != 0 {
		t.Errorf("expected timeout 0 for single node, got %d", timeout)
	}
	if sec != "" {
		t.Errorf("expected empty side-effect class, got %q", sec)
	}
}

func TestComputeTreeMetrics_NestedChildren(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
			{Type: "Condition", Name: "C"},
			{
				Type: "Selector",
				Name: "Inner",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "B"},
					{Type: "Action", Name: "D"},
				},
			},
		},
	}
	info := &evolution.NodeValidationInfo{}
	depth, count, _, retries, timeout, sec := computeTreeMetrics(tree, info)
	if depth != 2 {
		t.Errorf("expected depth 2 (Root→Inner→leaf), got %d", depth)
	}
	if count != 6 {
		t.Errorf("expected count 6 (Root + 3 direct + 2 nested), got %d", count)
	}
	if retries != 0 {
		t.Errorf("expected retries 0, got %d", retries)
	}
	if timeout != 0 {
		t.Errorf("expected timeout 0, got %d", timeout)
	}
	if sec != "" {
		t.Errorf("expected empty side-effect class, got %q", sec)
	}
}

func TestComputeTreeMetrics_ParallelWidth(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Parallel",
		Name: "RootPar",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
			{Type: "Action", Name: "B"},
			{Type: "Action", Name: "C"},
		},
	}
	info := &evolution.NodeValidationInfo{}
	_, _, par, _, _, _ := computeTreeMetrics(tree, info)
	if par != 3 {
		t.Errorf("expected parallel width 3 for 3 children, got %d", par)
	}
}

func TestComputeTreeMetrics_SideEffectClass(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Action",
		Name: "test",
		Metadata: map[string]any{
			"side_effect_class": "database",
		},
	}
	info := &evolution.NodeValidationInfo{}
	_, _, _, _, _, sec := computeTreeMetrics(tree, info)
	if sec != "database" {
		t.Errorf("expected side-effect class 'database', got %q", sec)
	}
}

func TestComputeTreeMetrics_MaxRetriesAndTimeout(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:       "Action",
		Name:       "test",
		MaxRetries: 3,
		TimeoutMs:  5000,
	}
	info := &evolution.NodeValidationInfo{}
	_, _, _, retries, timeout, _ := computeTreeMetrics(tree, info)
	if retries != 3 {
		t.Errorf("expected retries 3, got %d", retries)
	}
	if timeout != 5000 {
		t.Errorf("expected timeout 5000, got %d", timeout)
	}
}

// ─── computeSubtreeMetrics edge cases ───

func TestComputeSubtreeMetrics_NilInput(t *testing.T) {
	depth, count, par, retries, timeout := computeSubtreeMetrics(nil)
	if depth != 0 || count != 0 || par != 0 || retries != 0 || timeout != 0 {
		t.Errorf("expected all zeros for nil input, got depth=%d count=%d par=%d retries=%d timeout=%d",
			depth, count, par, retries, timeout)
	}
}

func TestComputeSubtreeMetrics_MixedChildren(t *testing.T) {
	children := []evolution.SerializableNode{
		{
			Type: "Sequence",
			Name: "Seq1",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "A"},
			},
		},
		{Type: "Action", Name: "B"},
	}
	depth, count, _, _, _ := computeSubtreeMetrics(children)
	if count != 3 {
		t.Errorf("expected count 3 (Seq1 + A + B), got %d", count)
	}
	// Seq1 has depth 1 (Seq1→A), computeSubtreeMetrics returns max child depth
	if depth != 1 {
		t.Errorf("expected depth 1 (Seq1→A max child depth), got %d", depth)
	}
}

// ─── buildChainActionFn with ChainConfig edge cases ───

func TestBuildChainActionFn_UnknownChainType(t *testing.T) {
	bb := &Blackboard{Task: "test task", LLM: &mockLLM{}}
	cfg := ChainConfig{
		ChainType: "unknown_type_xyz",
		Prompt:    "test prompt",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	resultCode := fn(ctx)
	if resultCode != -1 {
		t.Errorf("expected -1 (failure) for unknown chain type, got %d", resultCode)
	}
}

func TestBuildChainActionFn_LlmCallNilLLM(t *testing.T) {
	bb := &Blackboard{Task: "test", LLM: nil}
	cfg := ChainConfig{
		ChainType: "llm_call",
		Prompt:    "hello {{.Task}}",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	code := fn(ctx)
	// With nil LLM, llm_call enters template-only mode and returns success
	if code != 1 {
		t.Errorf("expected 1 (success) for template-only mode, got %d", code)
	}
	if bb.Outcome != "template_only" {
		t.Errorf("expected outcome 'template_only', got %q", bb.Outcome)
	}
	if bb.Result == "" {
		t.Error("expected non-empty result from template-only mode")
	}
}
