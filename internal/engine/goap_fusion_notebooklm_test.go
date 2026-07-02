package engine

import (
	"strings"
	"testing"
)

func TestGoapFusionNotebookLMRecommendationExtraction(t *testing.T) {
	answer := `**GOAL**: Add a deterministic failure-cluster-to-regression-test action for BT incidents [1].
**GAP**: The platform stores failures but does not yet convert representative clusters into executable regression tests [1-2].
FILES: internal/engine/actions_failures.go, internal/engine/actions_failures_test.go
TESTS: /usr/local/go/bin/go test ./internal/engine -run TestFailureClusterRegression -count=1`

	goal, gap := extractGoapNotebookLMRecommendation(answer)
	if !strings.Contains(goal, "failure-cluster-to-regression-test") {
		t.Fatalf("goal not extracted from NotebookLM answer: %q", goal)
	}
	if !strings.Contains(gap, "executable regression tests") {
		t.Fatalf("gap not extracted from NotebookLM answer: %q", gap)
	}
}

func TestExtractNotebookLMAnswerFromTruncatedJSON(t *testing.T) {
	raw := "{\"answer\": \"**GOAL**: Implement GatedAction\\n\\n**GAP**: Missing deterministic gates [1]\", \"conversation_id\": \"abc\", \"references\": ["
	answer := extractNotebookLMAnswer(raw)
	if !strings.Contains(answer, "Implement GatedAction") || strings.Contains(answer, `"answer"`) {
		t.Fatalf("extractNotebookLMAnswer did not recover answer field from partial JSON: %q", answer)
	}
}

func TestGoapFusionNotebookLMGoalFromGaps(t *testing.T) {
	gaps := "CHECK: static gap\nNOTEBOOKLM_GOAL: Add citation-backed mutation scoring\nNOTEBOOKLM_GAP: scoring lacks research grounding"
	got := goapFusionNotebookLMGoalFromGaps(gaps)
	if got != "Add citation-backed mutation scoring" {
		t.Fatalf("goapFusionNotebookLMGoalFromGaps()=%q", got)
	}
}

func TestGoapFusionNotebookLMFailureDetection(t *testing.T) {
	for _, out := range []string{
		`{"error": "NotebookLM circuit breaker open", "retry_after": "5m0s"}`,
		"Error: Query failed: Authentication expired. Run 'nlm login' in your terminal",
		"Error: Google rejected the query (error code 8: RESOURCE_EXHAUSTED) . This may \nindicate account-level restrictions on programmatic access.",
		`{"error": "Google rejected the query (error code 8: RESOURCE_EXHAUSTED) [type.googleapis.com/google.internal.labs.tailwind.orchestration.v1.UserDisplayableError]."}`,
		"Error: Failed to import sources: API error (code 9): unknown",
		"Error: something novel the CLI has not printed before",
	} {
		if !isGoapNotebookLMFailure(out) {
			t.Fatalf("expected NotebookLM failure for %q", out)
		}
	}

	for _, ok := range []string{
		`{"answer":"GOAL: Add a test\nGAP: missing test evidence with error budgets [1]","citations":{"1":"src"}}`,
		`{"answer":"GOAL: Implement the Triple-Level Error Attribution Dispatcher\nGAP: Error: prefixed strings appear mid-answer [1]"}`,
	} {
		if isGoapNotebookLMFailure(ok) {
			t.Fatalf("valid NotebookLM answer should not be classified as failure: %q", ok)
		}
	}
}

func TestGoapFusionNotebookLMActionRegistered(t *testing.T) {
	if GetAction("RunGoapFusionNotebookLMResearch") == nil {
		t.Fatal("RunGoapFusionNotebookLMResearch action is not registered")
	}
}
