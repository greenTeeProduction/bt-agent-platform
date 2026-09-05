// Package engine — Research actions extracted from tree.go actionForName switch.
// Registers 22 actions: query decomposition, parallel search, deep dive,
// fact cross-referencing, report drafting, visualization, citations.
// (QueryKG, ApplyKnowledge, UseCachedResult are already in registry.go)
package engine

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerResearchActions()
}

func registerResearchActions() {
	RegisterAction("AskClarifyingQuestions", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Clarifying Questions\n\nTo better answer: %s\n\n1. What is the scope?\n2. Any specific constraints?\n3. Preferred depth?", bb.Task)
		return 1
	})

	RegisterAction("RefineQueryWithAnswers", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Task = fmt.Sprintf("%s [refined with clarifications]", bb.Task)
		return 1
	})

	RegisterAction("ProceedDirectly", func(ctx *btcore.BTContext[Blackboard]) int {
		return 1
	})

	RegisterAction("DecomposeQuery", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Plan = bb.LLM.GeneratePlan("decompose into sub-questions: "+bb.Task, "medium")
		bb.Result = fmt.Sprintf("## Research Plan\n\nQuery: %s\n\nDecomposed into sub-questions.", bb.Task)
		return 1
	})

	RegisterAction("AssessComplexity", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Complexity = bb.LLM.AnalyzeComplexity(bb.Task)
		bb.Result = fmt.Sprintf("%s\n\nComplexity: %s", bb.Result, bb.Complexity)
		return 1
	})

	RegisterAction("ExecuteSingleSearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Quick Research: %s\n\n1 agent, broad search, top sources extracted.", bb.Task)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("SpawnResearchThreads", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Comparison Research: %s\n\n2-4 parallel research threads launched. Each: 10-15 searches.", bb.Task)
		return 1
	})

	RegisterAction("SpawnDeepThreads", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Deep Investigation: %s\n\n10+ parallel research threads. Iterative refinement enabled.", bb.Task)
		return 1
	})

	RegisterAction("SearchBroadFirst", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nBroad search complete. Landscape mapped.", bb.Result)
		return 1
	})

	RegisterAction("FilterAndRankSources", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nSources filtered by authority, recency, relevance.", bb.Result)
		return 1
	})

	RegisterAction("ExtractKeyFindings", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nKey claims, data points, and quotes extracted.", bb.Result)
		return 1
	})

	RegisterAction("CrossReferenceFacts", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nFacts cross-referenced across 2+ independent sources.", bb.Result)
		return 1
	})

	RegisterAction("TargetedDeepDive", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nTargeted deep dive into knowledge gaps.", bb.Result)
		return 1
	})

	RegisterAction("PivotOnDeadEnds", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nDead ends detected — pivoting to alternative sources.", bb.Result)
		return 1
	})

	RegisterAction("CoverageComplete", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nCoverage complete: all sub-questions answered.", bb.Result)
		return 1
	})

	RegisterAction("IterateSearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nIterating search with refined queries.", bb.Result)
		return 1
	})

	RegisterAction("StructureReport", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Research Report: %s\n\n**Executive Summary**\n\n**Background**\n\n**Findings**\n\n**Analysis**\n\n**Conclusion**", bb.Task)
		return 1
	})

	RegisterAction("DraftSections", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Plan = bb.LLM.GeneratePlan("draft research report sections for: "+bb.Task, "high")
		bb.Result = fmt.Sprintf("%s\n\nSections drafted with inline citations.", bb.Result)
		return 1
	})

	RegisterAction("GenerateVisualizations", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\nCharts, tables, and comparison matrices generated.", bb.Result)
		return 1
	})

	RegisterAction("AddCitations", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\n**Sources:** [1] ... [2] ... [3] ...", bb.Result)
		return 1
	})

	RegisterAction("AddReasoningChain", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\n## Research Methodology\n- Search strategy: broad → narrow\n- Key decisions made\n- Pivots from dead ends\n- Coverage: all sub-questions addressed", bb.Result)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("FlagRemainingGaps", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\n## Limitations\n- Areas for further research noted.", bb.Result)
		return 1
	})
}
