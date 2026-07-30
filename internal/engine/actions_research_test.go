package engine

import (
	"fmt"
	"testing"
)

// Characterization tests for the research domain's action nodes registered
// in actions_research.go — clarifying questions, query decomposition,
// parallel search fan-out, source synthesis, and report drafting.
//
// runFusionAction (defined in actions_btfusion_research_test.go) is reused
// here since both files exercise engine.GetAction against the same registry.

// --- Task-templated actions: overwrite bb.Result, ignoring any prior value --

type researchTaskTemplatedCase struct {
	name        string
	format      string // fmt template with exactly one %s for bb.Task
	wantOutcome string
}

func researchTaskTemplatedCases() []researchTaskTemplatedCase {
	return []researchTaskTemplatedCase{
		{name: "AskClarifyingQuestions", format: "## Clarifying Questions\n\nTo better answer: %s\n\n1. What is the scope?\n2. Any specific constraints?\n3. Preferred depth?"},
		{name: "ExecuteSingleSearch", format: "## Quick Research: %s\n\n1 agent, broad search, top sources extracted.", wantOutcome: "success"},
		{name: "SpawnResearchThreads", format: "## Comparison Research: %s\n\n2-4 parallel research threads launched. Each: 10-15 searches."},
		{name: "SpawnDeepThreads", format: "## Deep Investigation: %s\n\n10+ parallel research threads. Iterative refinement enabled."},
		{name: "StructureReport", format: "## Research Report: %s\n\n**Executive Summary**\n\n**Background**\n\n**Findings**\n\n**Analysis**\n\n**Conclusion**"},
	}
}

func TestResearchActions_TaskTemplatedOverwrite(t *testing.T) {
	for _, c := range researchTaskTemplatedCases() {
		t.Run(c.name, func(t *testing.T) {
			bb := &Blackboard{Task: "evaluate distributed consensus algorithms", Result: "PRIOR RESULT MUST BE DISCARDED"}
			status := runFusionAction(t, c.name, bb)

			if status != 1 {
				t.Errorf("status = %d, want 1", status)
			}
			want := fmt.Sprintf(c.format, bb.Task)
			if bb.Result != want {
				t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, want)
			}
			if bb.Outcome != c.wantOutcome {
				t.Errorf("Outcome = %q, want %q", bb.Outcome, c.wantOutcome)
			}
		})
	}
}

// --- Fixed-text actions: append a constant suffix onto the existing Result --

type researchAppendCase struct {
	name        string
	text        string
	wantOutcome string
}

func researchAppendCases() []researchAppendCase {
	return []researchAppendCase{
		{name: "SearchBroadFirst", text: "\n\nBroad search complete. Landscape mapped."},
		{name: "FilterAndRankSources", text: "\n\nSources filtered by authority, recency, relevance."},
		{name: "ExtractKeyFindings", text: "\n\nKey claims, data points, and quotes extracted."},
		{name: "CrossReferenceFacts", text: "\n\nFacts cross-referenced across 2+ independent sources."},
		{name: "TargetedDeepDive", text: "\n\nTargeted deep dive into knowledge gaps."},
		{name: "PivotOnDeadEnds", text: "\n\nDead ends detected — pivoting to alternative sources."},
		{name: "CoverageComplete", text: "\n\nCoverage complete: all sub-questions answered."},
		{name: "IterateSearch", text: "\n\nIterating search with refined queries."},
		{name: "GenerateVisualizations", text: "\n\nCharts, tables, and comparison matrices generated."},
		{name: "AddCitations", text: "\n\n**Sources:** [1] ... [2] ... [3] ..."},
		{name: "FlagRemainingGaps", text: "\n\n## Limitations\n- Areas for further research noted."},
		{name: "AddReasoningChain", text: "\n\n## Research Methodology\n- Search strategy: broad → narrow\n- Key decisions made\n- Pivots from dead ends\n- Coverage: all sub-questions addressed", wantOutcome: "success"},
	}
}

func TestResearchActions_FixedAppends(t *testing.T) {
	for _, c := range researchAppendCases() {
		t.Run(c.name, func(t *testing.T) {
			bb := &Blackboard{Result: "SEED"}
			status := runFusionAction(t, c.name, bb)

			if status != 1 {
				t.Errorf("status = %d, want 1", status)
			}
			want := "SEED" + c.text
			if bb.Result != want {
				t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, want)
			}
			if bb.Outcome != c.wantOutcome {
				t.Errorf("Outcome = %q, want %q", bb.Outcome, c.wantOutcome)
			}
		})
	}
}

// --- ProceedDirectly: no-op ---------------------------------------------------

func TestProceedDirectly_NoSideEffects(t *testing.T) {
	bb := &Blackboard{Task: "t", Result: "r", Outcome: "o"}
	status := runFusionAction(t, "ProceedDirectly", bb)

	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
	if bb.Task != "t" || bb.Result != "r" || bb.Outcome != "o" {
		t.Errorf("ProceedDirectly must not mutate the blackboard, got Task=%q Result=%q Outcome=%q", bb.Task, bb.Result, bb.Outcome)
	}
}

// --- RefineQueryWithAnswers: mutates bb.Task, not bb.Result -------------------

func TestRefineQueryWithAnswers_AppendsRefinementMarkerToTask(t *testing.T) {
	bb := &Blackboard{Task: "how does gradient descent converge?", Result: "unrelated prior result"}
	status := runFusionAction(t, "RefineQueryWithAnswers", bb)

	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
	want := "how does gradient descent converge? [refined with clarifications]"
	if bb.Task != want {
		t.Errorf("Task = %q, want %q", bb.Task, want)
	}
	if bb.Result != "unrelated prior result" {
		t.Errorf("Result must be untouched, got %q", bb.Result)
	}
}

// --- DecomposeQuery: consults bb.LLM.GeneratePlan and overwrites bb.Result ---

func TestDecomposeQuery_GeneratesPlanAndOverwritesResult(t *testing.T) {
	llm := NewMockLLM()
	llm.PlanResp = "1. sub-question A\n2. sub-question B"
	bb := &Blackboard{Task: "compare event sourcing vs CQRS", Result: "PRIOR RESULT MUST BE DISCARDED", LLM: llm}
	status := runFusionAction(t, "DecomposeQuery", bb)

	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
	if bb.Plan != llm.PlanResp {
		t.Errorf("Plan = %q, want %q", bb.Plan, llm.PlanResp)
	}
	wantResult := fmt.Sprintf("## Research Plan\n\nQuery: %s\n\nDecomposed into sub-questions.", bb.Task)
	if bb.Result != wantResult {
		t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
	}
}

// --- AssessComplexity: consults bb.LLM.AnalyzeComplexity and appends --------

func TestAssessComplexity_AppendsComplexityFromLLM(t *testing.T) {
	llm := NewMockLLM()
	llm.ComplexityResp = "high"
	bb := &Blackboard{Task: "design a distributed consensus protocol", Result: "SEED", LLM: llm}
	status := runFusionAction(t, "AssessComplexity", bb)

	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
	if bb.Complexity != "high" {
		t.Errorf("Complexity = %q, want %q", bb.Complexity, "high")
	}
	wantResult := "SEED\n\nComplexity: high"
	if bb.Result != wantResult {
		t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
	}
}

// --- DraftSections: consults bb.LLM.GeneratePlan and appends ----------------

func TestDraftSections_GeneratesPlanAndAppendsResult(t *testing.T) {
	llm := NewMockLLM()
	llm.PlanResp = "1. Intro\n2. Findings\n3. Conclusion"
	bb := &Blackboard{Task: "quantum computing landscape", Result: "SEED", LLM: llm}
	status := runFusionAction(t, "DraftSections", bb)

	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
	if bb.Plan != llm.PlanResp {
		t.Errorf("Plan = %q, want %q", bb.Plan, llm.PlanResp)
	}
	wantResult := "SEED\n\nSections drafted with inline citations."
	if bb.Result != wantResult {
		t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
	}
}
