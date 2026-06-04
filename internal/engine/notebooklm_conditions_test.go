package engine

import "testing"

func TestNotebookLMConditionsRouteResearchBeforeQueryAndIngest(t *testing.T) {
	bb := &Blackboard{Task: "Autonomous daily researcher for BT Platform: run deep research, add sources, query notebook, synthesize findings"}

	if !bb.conditionForName("IsResearchTask")(bb) {
		t.Fatal("daily research task should match IsResearchTask")
	}
	if bb.conditionForName("IsQueryTask")(bb) {
		t.Fatal("daily research task should not match IsQueryTask; it must route to ResearchPath")
	}
	if bb.conditionForName("IsIngestTask")(bb) {
		t.Fatal("daily research task should not match IsIngestTask; it must route to ResearchPath")
	}
}

func TestNotebookLMIngestDoesNotMatchBareNotebookLM(t *testing.T) {
	bb := &Blackboard{Task: "Query the NotebookLM BT Platform Research notebook for current source count"}

	if bb.conditionForName("IsIngestTask")(bb) {
		t.Fatal("bare NotebookLM mention must not route to ingestion")
	}
	if !bb.conditionForName("IsQueryTask")(bb) {
		t.Fatal("query notebook task should match IsQueryTask")
	}
}
