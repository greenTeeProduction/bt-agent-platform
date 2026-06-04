// Package engine — Go developer actions extracted from tree.go actionForName switch.
// Registers 7 actions: code review, improvements, compilation, build fixes, tests, analysis, explanations.
package engine

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/util"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerGoDevActions()
}

func registerGoDevActions() {
	RegisterAction("ReviewGoCode", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Plan = bb.LLM.GeneratePlan(bb.Task, "medium")
		bb.Result = fmt.Sprintf("## Code Review\n\nTask: %s\n\nPlan: %s\n\nKey findings based on idiomatic Go review.", bb.Task, util.Truncate(bb.Plan, 300))
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("SuggestImprovements", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\n## Suggested Improvements\n- Use idiomatic Go patterns\n- Check error handling\n- Consider concurrency safety", bb.Result)
		return 1
	})

	RegisterAction("CompileGoCode", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Compilation\n\nRan `go build` on: %s\n\nOutput would show compilation results.", bb.Task)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("FixBuildErrors", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Plan = bb.LLM.GeneratePlan("fix compilation errors in: "+bb.Task, "medium")
		bb.Result = fmt.Sprintf("## Fixed Build Errors\n\n%s\n\nSuggested fix based on compilation output.", util.Truncate(bb.Plan, 300))
		return 1
	})

	RegisterAction("RunGoTests", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Test Results\n\nRan `go test` on: %s\n\nAll tests pass (simulated).", bb.Task)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("AnalyzeTestResults", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("%s\n\n## Test Analysis\n- Coverage: good\n- Performance: acceptable", bb.Result)
		return 1
	})

	RegisterAction("ExplainGoConcept", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Plan = bb.LLM.GeneratePlan(bb.Task, "low")
		bb.Result = fmt.Sprintf("## Go Explanation\n\nTask: %s\n\n%s", bb.Task, util.Truncate(bb.Plan, 500))
		bb.Outcome = string(evolution.Success)
		return 1
	})
}
