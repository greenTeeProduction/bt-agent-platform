package domains

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
)

type fusionFakeLLM struct{}

func (fusionFakeLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "Return ONLY JSON") || strings.Contains(prompt, "strict JSON") {
		return `{"consensus":["shared"],"contradictions":[],"partial_coverage":[],"unique_insights":[{"model":"fake","insight":"native fusion exercised"}],"blind_spots":["none"]}`, nil
	}
	if strings.Contains(prompt, "Fusion result") {
		return "# Consensus\nshared\n\n# Contradictions\nnone\n\n# Blind Spots\nnone", nil
	}
	return "panel answer", nil
}
func (f fusionFakeLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return f.Generate(prompt)
}
func (f fusionFakeLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return f.Generate(prompt)
}
func (fusionFakeLLM) AnalyzeComplexity(task string) string        { return "high" }
func (fusionFakeLLM) GeneratePlan(task, complexity string) string { return "plan" }
func (fusionFakeLLM) Reflect(task, outcome, plan string) (string, string) {
	return "fusion ran", "none"
}
func (f fusionFakeLLM) GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error) {
	return f.Generate(system + "\n" + prompt)
}

func TestFusionBT_EndToEndWithFakeLLM(t *testing.T) {
	tree := ResolveTreeID("fusion")
	bb := &engine.Blackboard{
		Task:       "Research and compare two approaches with risks and tradeoffs.",
		LLM:        fusionFakeLLM{},
		Complexity: "high",
		ChainState: map[string]any{"fusion_force": true},
	}
	cmd, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatalf("BuildAndValidate: %v", err)
	}
	output := engine.RunTask(bb, cmd)
	if output == "" {
		t.Fatalf("empty RunTask output outcome=%q result=%q state=%#v", bb.Outcome, bb.Result, bb.ChainState)
	}
	if bb.Result == "" || !strings.Contains(bb.Result, "Consensus") {
		t.Fatalf("expected fusion final result, got %q", bb.Result)
	}
	if bb.ChainState["fusion_status"] != "ok" || bb.ChainState["fusion_panel_count"] == nil || bb.ChainState["fusion_success_count"] == nil {
		t.Fatalf("fusion metrics missing: %#v", bb.ChainState)
	}
}
