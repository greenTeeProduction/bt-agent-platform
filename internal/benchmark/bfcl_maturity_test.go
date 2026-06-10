package benchmark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func benchmarkSuccessTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "BenchmarkSuccessRoot",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
}

func benchmarkPlanTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "BenchmarkPlanRoot",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "ExecutePlan"},
		},
	}
}

func TestLoadBFCLSuiteAndEvaluate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bfcl_custom.json")
	data := `[
		{"id":"one","query":"build the Go project","expected_tool":"BuildPath","category":"simple"},
		{"id":"two","query":"what is a goroutine?","expected_tool":"KnowledgePath","category":"simple"}
	]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	suite, err := LoadBFCLSuite(path)
	if err != nil {
		t.Fatalf("LoadBFCLSuite returned error: %v", err)
	}
	if suite.Name != "bfcl_custom.json" || len(suite.Entries) != 2 {
		t.Fatalf("unexpected suite: name=%q entries=%d", suite.Name, len(suite.Entries))
	}

	metrics := suite.Evaluate(benchmarkSuccessTree(), DefaultMock())
	if metrics.TotalEntries != 2 || metrics.CorrectRoutes != 2 || metrics.Accuracy != 1 || metrics.SuccessRate != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
	if metrics.Results[0].ActualPath != "BuildPath" || !metrics.Results[1].Correct {
		t.Fatalf("unexpected per-entry results: %+v", metrics.Results)
	}
}

func TestLoadBFCLSuiteErrors(t *testing.T) {
	if _, err := LoadBFCLSuite(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing file error")
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"not":"an array"}`), 0o600); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	if _, err := LoadBFCLSuite(bad); err == nil {
		t.Fatal("expected invalid JSON shape error")
	}
}

func TestBuiltinBFCLSuitesHaveStableShape(t *testing.T) {
	cases := []struct {
		name string
		s    *BFCLSuite
		want int
	}{
		{"simple", BuiltinBFCLSimple(), 15},
		{"relevance", BuiltinBFCLRelevance(), 5},
		{"multiple", BuiltinBFCLMultiple(), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.s == nil {
				t.Fatal("suite is nil")
			}
			if len(tc.s.Entries) != tc.want {
				t.Fatalf("entries=%d want %d", len(tc.s.Entries), tc.want)
			}
			for _, entry := range tc.s.Entries {
				if entry.ID == "" || entry.Query == "" || entry.ExpectedTool == "" || entry.Category == "" {
					t.Fatalf("incomplete entry in %s: %+v", tc.s.Name, entry)
				}
			}
		})
	}
}

func TestBFCLV3LoadFlattenAndEvaluate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bfcl_v3.json")
	data := `{
		"multi_turn_base": [
			{"id":"base-1","category":"multi_turn_base","turns":[{"role":"user","content":"build the Go project"},{"role":"user","content":"what is a goroutine?"}],"expected_tools":["BuildPath","KnowledgePath"]}
		],
		"multi_turn_miss_func": [
			{"id":"miss-1","category":"multi_turn_miss_func","turns":[{"role":"user","content":"unclassifiable but clear task"}],"expected_tools":["GeneralPath"]}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write v3 fixture: %v", err)
	}

	categories, err := LoadBFCLV3MultiTurn(path)
	if err != nil {
		t.Fatalf("LoadBFCLV3MultiTurn returned error: %v", err)
	}
	if len(categories) != 2 || len(categories["multi_turn_base"]) != 1 {
		t.Fatalf("unexpected categories: %+v", categories)
	}
	entries, err := LoadBFCLV3Entries(path)
	if err != nil {
		t.Fatalf("LoadBFCLV3Entries returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("flattened entries=%d want 2", len(entries))
	}

	metrics := EvaluateBFCLV3(benchmarkSuccessTree(), entries, DefaultMock())
	if metrics.TotalEntries != 2 || metrics.TotalTurns != 3 || metrics.CorrectTurns != 3 || metrics.TurnAccuracy != 1 || metrics.FullyCorrect != 2 {
		t.Fatalf("unexpected v3 metrics: %+v", metrics)
	}
}

func TestBFCLV3LoadErrorsAndEmptyEvaluation(t *testing.T) {
	if _, err := LoadBFCLV3MultiTurn(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing v3 file error")
	}
	bad := filepath.Join(t.TempDir(), "bad-v3.json")
	if err := os.WriteFile(bad, []byte(`[`), 0o600); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	if _, err := LoadBFCLV3Entries(bad); err == nil {
		t.Fatal("expected invalid v3 JSON error")
	}

	metrics := EvaluateBFCLV3(benchmarkSuccessTree(), nil, DefaultMock())
	if metrics.TotalEntries != 0 || metrics.TotalTurns != 0 || metrics.TurnAccuracy != 0 || metrics.MultiStepSuccessRate != 0 || len(metrics.Results) != 0 {
		t.Fatalf("unexpected empty metrics: %+v", metrics)
	}
}

func TestIsToolMatchVariants(t *testing.T) {
	cases := []struct {
		name     string
		output   string
		path     string
		expected string
		want     bool
	}{
		{"empty expected needs output", "some output", "", "", true},
		{"empty expected rejects empty output", "", "", "", false},
		{"case insensitive exact path", "", "buildpath", "BuildPath", true},
		{"path contains expected", "", "GoKnowledgePath", "Knowledge", true},
		{"expected contains path", "", "KYC", "KYCPath", true},
		{"output mentions expected", "selected dcfpath for valuation", "", "DCFPath", true},
		{"no match", "selected another tool", "GeneralPath", "SecurityReview", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isToolMatch(tc.output, tc.path, tc.expected); got != tc.want {
				t.Fatalf("isToolMatch()=%v want %v", got, tc.want)
			}
		})
	}
}

func TestBuiltinBFCLV3StableShape(t *testing.T) {
	entries := BuiltinBFCLV3()
	if len(entries) != 8 {
		t.Fatalf("BuiltinBFCLV3 entries=%d want 8", len(entries))
	}
	seenCategories := map[string]bool{}
	for _, entry := range entries {
		if entry.ID == "" || entry.Category == "" || len(entry.Turns) == 0 || len(entry.ExpectedTools) == 0 {
			t.Fatalf("incomplete v3 entry: %+v", entry)
		}
		seenCategories[entry.Category] = true
	}
	for _, category := range []string{"multi_turn_base", "multi_turn_composite", "multi_turn_long_context", "multi_turn_miss_func", "multi_turn_miss_param"} {
		if !seenCategories[category] {
			t.Fatalf("missing category %s in %+v", category, seenCategories)
		}
	}
}

func TestGAIABuiltinAndEvaluation(t *testing.T) {
	entries := BuiltinGAIADev()
	if len(entries) != 8 {
		t.Fatalf("BuiltinGAIADev entries=%d want 8", len(entries))
	}
	for _, entry := range entries {
		if entry.ID == "" || entry.Question == "" || entry.Answer == "" || entry.Level < 1 || entry.Level > 3 {
			t.Fatalf("incomplete GAIA entry: %+v", entry)
		}
	}

	metrics := EvaluateGAIA(benchmarkPlanTree(), []GAIAEntry{
		// ExecutePlan returns the generated plan itself since b5c4d00
		// (placeholder output removed); match DefaultMock's plan text.
		{ID: "g1", Question: "what is LiFePO4", Answer: "execute workflow", Level: 1},
		{ID: "g2", Question: "compare systems", Answer: "not-present", Level: 2},
	}, DefaultMock())
	if metrics.TotalQuestions != 2 || metrics.CorrectAnswers != 1 || metrics.Accuracy != 0.5 {
		t.Fatalf("unexpected GAIA metrics: %+v", metrics)
	}
	if metrics.ByLevel[1].Accuracy != 1 || metrics.ByLevel[2].Accuracy != 0 {
		t.Fatalf("unexpected GAIA by-level metrics: %+v", metrics.ByLevel)
	}
}
