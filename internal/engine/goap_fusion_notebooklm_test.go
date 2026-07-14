package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	btcore "github.com/rvitorper/go-bt/core"
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

// A rejected-but-not-exhausted RPC (code 3 INVALID_ARGUMENT) must fail the
// cycle WITHOUT stamping the day-long quota cache, and the report must not
// blame auth — nlm's own error text already suggests re-authenticating, and
// the 2026-07-08→13 treadmill was misdiagnosed as "auth problems" for a week
// because both messages pointed away from the real error.
func TestRunGoapFusionNotebookLMResearch_InvalidArgumentIsNotQuotaOrAuth(t *testing.T) {
	invalidArgument := "Error: Google rejected the query (error code 3: INVALID_ARGUMENT). This may \nindicate account-level restrictions on programmatic access. Try \nre-authenticating with 'nlm login' or using a different account."
	orig := nlmRun
	nlmRun = func(timeout time.Duration, args ...string) string { return invalidArgument }
	t.Cleanup(func() { nlmRun = orig })

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}

	research := GetAction("RunGoapFusionNotebookLMResearch")
	if research == nil {
		t.Fatal("RunGoapFusionNotebookLMResearch action is not registered")
	}
	if got := research(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("status = %d, want -1 (fail fast to selector fallback)", got)
	}
	if bb.Outcome != "goap_fusion_notebooklm_failed" {
		t.Fatalf("outcome = %q, want goap_fusion_notebooklm_failed", bb.Outcome)
	}
	if nlmQuotaExhausted(bb) {
		t.Fatal("INVALID_ARGUMENT stamped the quota cache — one bad RPC would black out research until midnight Pacific")
	}
	if strings.Contains(bb.Result, "auth is unavailable") {
		t.Fatalf("report blames auth for a non-auth failure: %q", bb.Result)
	}
	if !strings.Contains(bb.Result, "INVALID_ARGUMENT") {
		t.Fatalf("report must carry the raw nlm error: %q", bb.Result)
	}
}
