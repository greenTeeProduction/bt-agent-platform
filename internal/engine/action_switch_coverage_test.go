package engine

import (
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestActionSwitch_SetupDevTools(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("SetupDevTools")
	if fn == nil {
		t.Fatal("actionForName returned nil for SetupDevTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
	if bb.ChainTools == nil {
		t.Error("expected ChainTools to be set")
	}
}

func TestActionSwitch_SetupUniversalTools(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("SetupUniversalTools")
	if fn == nil {
		t.Fatal("actionForName returned nil for SetupUniversalTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
	if bb.ChainTools == nil {
		t.Error("expected ChainTools to be set")
	}
}

func TestActionSwitch_SetupResearchTools(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("SetupResearchTools")
	if fn == nil {
		t.Fatal("actionForName returned nil for SetupResearchTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
	if bb.ChainTools == nil {
		t.Error("expected ChainTools to be set")
	}
}

func TestActionSwitch_ReadGoMod(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("ReadGoMod")
	if fn == nil {
		t.Fatal("actionForName returned nil for ReadGoMod")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_ReadConfigFiles(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("ReadConfigFiles")
	if fn == nil {
		t.Fatal("actionForName returned nil for ReadConfigFiles")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_DetectHardware(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("DetectHardware")
	if fn == nil {
		t.Fatal("actionForName returned nil for DetectHardware")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_DetectProcesses(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("DetectProcesses")
	if fn == nil {
		t.Fatal("actionForName returned nil for DetectProcesses")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_CheckConfidence(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("CheckConfidence")
	if fn == nil {
		t.Fatal("actionForName returned nil for CheckConfidence")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_BuildCompsTable(t *testing.T) {
	bb := &Blackboard{Task: "comps"}
	fn := bb.actionForName("BuildCompsTable")
	if fn == nil {
		t.Fatal("actionForName returned nil for BuildCompsTable")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_CalculateValuationRange(t *testing.T) {
	bb := &Blackboard{Task: "valuation"}
	fn := bb.actionForName("CalculateValuationRange")
	if fn == nil {
		t.Fatal("actionForName returned nil for CalculateValuationRange")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_ScanForBugs(t *testing.T) {
	bb := &Blackboard{Task: "scan code"}
	fn := bb.actionForName("ScanForBugs")
	if fn == nil {
		t.Fatal("actionForName returned nil for ScanForBugs")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_CompileAndTest(t *testing.T) {
	bb := &Blackboard{Task: "compile test"}
	fn := bb.actionForName("CompileAndTest")
	if fn == nil {
		t.Fatal("actionForName returned nil for CompileAndTest")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_WriteGoCode(t *testing.T) {
	bb := &Blackboard{Task: "write code", LLM: &MockLLM{PlanResp: "implement feature"}}
	fn := bb.actionForName("WriteGoCode")
	if fn == nil {
		t.Fatal("actionForName returned nil for WriteGoCode")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_SetupTestEnv(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.actionForName("SetupTestEnv")
	if fn == nil {
		t.Fatal("actionForName returned nil for SetupTestEnv")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_DeployToStaging(t *testing.T) {
	bb := &Blackboard{Task: "deploy"}
	fn := bb.actionForName("DeployToStaging")
	if fn == nil {
		t.Fatal("actionForName returned nil for DeployToStaging")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_ReportDeploymentStatus(t *testing.T) {
	bb := &Blackboard{Result: "deployed"}
	fn := bb.actionForName("ReportDeploymentStatus")
	if fn == nil {
		t.Fatal("actionForName returned nil for ReportDeploymentStatus")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_DocumentSecurityIssues(t *testing.T) {
	bb := &Blackboard{Task: "security audit"}
	fn := bb.actionForName("DocumentSecurityIssues")
	if fn == nil {
		t.Fatal("actionForName returned nil for DocumentSecurityIssues")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_SuggestSecurityFixes(t *testing.T) {
	bb := &Blackboard{Result: "vulnerabilities found"}
	fn := bb.actionForName("SuggestSecurityFixes")
	if fn == nil {
		t.Fatal("actionForName returned nil for SuggestSecurityFixes")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_OptimizePipeline(t *testing.T) {
	bb := &Blackboard{Task: "optimize build"}
	fn := bb.actionForName("OptimizePipeline")
	if fn == nil {
		t.Fatal("actionForName returned nil for OptimizePipeline")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_MonitorBuildHealth(t *testing.T) {
	bb := &Blackboard{Task: "monitor"}
	fn := bb.actionForName("MonitorBuildHealth")
	if fn == nil {
		t.Fatal("actionForName returned nil for MonitorBuildHealth")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_ExtractDataFromSource(t *testing.T) {
	bb := &Blackboard{Task: "extract api data"}
	fn := bb.actionForName("ExtractDataFromSource")
	if fn == nil {
		t.Fatal("actionForName returned nil for ExtractDataFromSource")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_TransformDataFormat(t *testing.T) {
	bb := &Blackboard{Task: "transform json"}
	fn := bb.actionForName("TransformDataFormat")
	if fn == nil {
		t.Fatal("actionForName returned nil for TransformDataFormat")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_LoadToWarehouse(t *testing.T) {
	bb := &Blackboard{Task: "load data"}
	fn := bb.actionForName("LoadToWarehouse")
	if fn == nil {
		t.Fatal("actionForName returned nil for LoadToWarehouse")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_BuildDCFModel(t *testing.T) {
	bb := &Blackboard{Task: "dcf analysis", LLM: &MockLLM{PlanResp: "dcf plan"}}
	fn := bb.actionForName("BuildDCFModel")
	if fn == nil {
		t.Fatal("actionForName returned nil for BuildDCFModel")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_AssemblePitchDeck(t *testing.T) {
	bb := &Blackboard{Task: "pitch deck", Result: "preliminary deck"}
	fn := bb.actionForName("AssemblePitchDeck")
	if fn == nil {
		t.Fatal("actionForName returned nil for AssemblePitchDeck")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_ReviewLLMResponse(t *testing.T) {
	bb := &Blackboard{Task: "review response", LLM: &MockLLM{PlanResp: "review plan"}}
	fn := bb.actionForName("ReviewLLMResponse")
	if fn == nil {
		t.Fatal("actionForName returned nil for ReviewLLMResponse")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestActionSwitch_OptimizePrompt(t *testing.T) {
	bb := &Blackboard{Task: "optimize prompt"}
	fn := bb.actionForName("OptimizePrompt")
	if fn == nil {
		t.Fatal("actionForName returned nil for OptimizePrompt")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}
