package thinktank

import (
	"context"
	"testing"
	"time"
)

type mockLLM struct{}

func (m *mockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *mockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return m.Generate(prompt)
}

func (m *mockLLM) Generate(prompt string) (string, error) {
	// Return structured mock response for synthesis/parsing
	if len(prompt) > 100 {
		return "THESIS: Main argument thesis text\nANTITHESIS: Counter argument antithesis text\nSYNTHESIS: Resolved synthesis position\nRECOMMENDATION: Recommended action\nDISSENTING: Minority view", nil
	}
	l := len(prompt)
	if l > 50 {
		l = 50
	}
	return "Mock analysis: " + prompt[:l] + "...", nil
}
func (m *mockLLM) AnalyzeComplexity(task string) string { return "medium" }
func (m *mockLLM) GeneratePlan(task, complexity string) string {
	l := len(task)
	if l > 30 {
		l = 30
	}
	return "Execute: " + task[:l]
}
func (m *mockLLM) Reflect(task, outcome, plan string) (string, string) {
	return "completed", "nothing to improve"
}

func TestNewThinkTank(t *testing.T) {
	tt := NewThinkTank("Test", "topic")
	if tt.Name != "Test" || tt.Topic != "topic" {
		t.Error("fields mismatch")
	}
	if len(tt.Fellows) != 5 {
		t.Errorf("expected 5 fellows, got %d", len(tt.Fellows))
	}
}

func TestDefaultFellows(t *testing.T) {
	fellows := DefaultFellows()
	roles := map[string]bool{}
	for _, f := range fellows {
		if f.Name == "" || f.Role == "" || f.Perspective == "" {
			t.Error("fellow missing required fields")
		}
		roles[f.Role] = true
	}
	for _, r := range []string{"bull", "bear", "technical", "macro", "contrarian"} {
		if !roles[r] {
			t.Errorf("missing role: %s", r)
		}
	}
}

func TestFellowConfidence(t *testing.T) {
	for _, f := range DefaultFellows() {
		if f.Confidence < 0 || f.Confidence > 1 {
			t.Errorf("%s confidence out of range: %.2f", f.Name, f.Confidence)
		}
	}
}

func TestResearchFinding(t *testing.T) {
	rf := ResearchFinding{
		FellowName: "Test", Role: "bull",
		KeyInsights: []string{"a", "b"}, Evidence: []string{"src"},
		ConfidenceScore: 0.85,
	}
	if rf.ConfidenceScore != 0.85 || len(rf.KeyInsights) != 2 {
		t.Error("fields")
	}
}

func TestDebateTurn(t *testing.T) {
	dt := DebateTurn{Round: 2, Speaker: "Test", Role: "bull", Statement: "arg"}
	if dt.Round != 2 || dt.Statement == "" {
		t.Error("fields")
	}
}

func TestSynthesis(t *testing.T) {
	s := Synthesis{Thesis: "A", Antithesis: "B", Synthesis: "C", Recommendation: "D"}
	if s.Thesis == "" {
		t.Error("thesis")
	}
}

func TestReviewComment(t *testing.T) {
	rc := ReviewComment{Reviewer: "R", Issue: "factual_error", Severity: "high", Comment: "c"}
	if rc.Issue != "factual_error" {
		t.Error("issue")
	}
}

func TestReport(t *testing.T) {
	r := Report{Title: "T", ExecutiveSummary: "S", Recommendation: "R"}
	if r.Title == "" {
		t.Error("title")
	}
}

func TestScenario(t *testing.T) {
	s := Scenario{Name: "N", Probability: 0.5, Impact: "high"}
	if s.Probability < 0 {
		t.Error("prob")
	}
}

func TestOrchestrator_ResearchRound(t *testing.T) {
	tt := NewThinkTank("Test", "topic")
	orch := NewOrchestrator(tt, &mockLLM{})
	err := orch.RunResearchRound()
	if err != nil {
		t.Fatal(err)
	}
	if len(tt.ResearchFindings) != 5 {
		t.Errorf("expected 5 findings, got %d", len(tt.ResearchFindings))
	}
}

func TestOrchestrator_Debate(t *testing.T) {
	tt := NewThinkTank("Test", "topic")
	orch := NewOrchestrator(tt, &mockLLM{})
	orch.RunResearchRound()
	err := orch.RunDebate()
	if err != nil {
		t.Fatal(err)
	}
	if len(tt.DebateTranscript) == 0 {
		t.Error("no debate transcript")
	}
}

func TestOrchestrator_Synthesis(t *testing.T) {
	tt := NewThinkTank("Test", "topic")
	orch := NewOrchestrator(tt, &mockLLM{})
	orch.RunResearchRound()
	err := orch.RunSynthesis()
	if err != nil {
		t.Fatal(err)
	}
	if tt.Synthesis == nil || tt.Synthesis.Thesis == "" {
		t.Error("synthesis incomplete")
	}
}

func TestOrchestrator_PeerReview(t *testing.T) {
	tt := NewThinkTank("Test", "topic")
	orch := NewOrchestrator(tt, &mockLLM{})
	orch.RunResearchRound()
	orch.RunSynthesis()
	err := orch.RunPeerReview()
	if err != nil {
		t.Fatal(err)
	}
	// Peer review may produce 0 comments with minimal mock output
	t.Logf("peer review comments: %d", len(tt.PeerReview))
}

func TestOrchestrator_Report(t *testing.T) {
	tt := NewThinkTank("Test", "topic")
	orch := NewOrchestrator(tt, &mockLLM{})
	orch.RunResearchRound()
	orch.RunSynthesis()
	err := orch.RunReportGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if tt.FinalReport == nil || tt.FinalReport.Title == "" {
		t.Error("report incomplete")
	}
}

func TestOrchestrator_FullAnalysis(t *testing.T) {
	tt := NewThinkTank("Full", "Should we invest in AI?")
	orch := NewOrchestrator(tt, &mockLLM{})
	err := orch.RunFullAnalysis("Should we invest in AI?")
	if err != nil {
		t.Fatal(err)
	}
	if len(tt.ResearchFindings) != 5 {
		t.Error("research")
	}
	if tt.Synthesis == nil {
		t.Error("synthesis")
	}
	if tt.FinalReport == nil {
		t.Error("report")
	}
}

func TestOrchestrator_EmptyTopic(t *testing.T) {
	tt := NewThinkTank("Test", "")
	orch := NewOrchestrator(tt, &mockLLM{})
	err := orch.RunResearchRound()
	if err != nil {
		t.Fatal(err)
	}
	if len(tt.ResearchFindings) != 5 {
		t.Error("empty topic should still produce findings")
	}
}

func TestFullAnalysis_MultipleTopics(t *testing.T) {
	topics := []string{"Tesla acquisition strategy", "AI regulation impact", "Cloud migration 2026"}
	for _, topic := range topics {
		tt := NewThinkTank("Council", topic)
		orch := NewOrchestrator(tt, &mockLLM{})
		err := orch.RunFullAnalysis(topic)
		if err != nil {
			t.Errorf("full analysis failed for %q: %v", topic, err)
		}
		if tt.FinalReport == nil {
			t.Errorf("no report for %q", topic)
		}
	}
}
