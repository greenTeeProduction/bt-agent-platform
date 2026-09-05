package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"
)

// The research pipeline must anchor on the platform's own architecture goals:
// arc42 §1.2 defines the quality goals (Q1 Correctness, Q2 Evolvability,
// Q3 Reliability) that every research query, review prompt, and seeded
// program is supposed to advance. Until 2026-07-08 research direction came
// from static hardcoded topic lists with no tie to the documented goals.

const arc42GoalsTestDoc = `# arc42 Architecture Documentation — test

# arc42 Section 1 — Introduction and Goals

## 1.1 Requirements Overview

Some text.

## 1.2 Quality Goals (Top 3)

| # | Quality Goal | Motivation |
|---|---|---|
| Q1 | **Correctness** | Trees must route correctly. All actions must register properly. |
| Q2 | **Evolvability** | The platform must improve over time. Benchmarks gate acceptance. |
| Q3 | **Reliability** | The platform degrades gracefully rather than failing silently. |

## 1.3 Stakeholders

| Role | Name |
|---|---|
| Q9 | **NotAGoal** |
`

func withArc42Doc(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "arc42.md")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := arc42GoalsDocPaths
	arc42GoalsDocPaths = []string{path}
	t.Cleanup(func() { arc42GoalsDocPaths = old })
}

func TestLoadArc42QualityGoalsParsesSection12Table(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	goals := loadArc42QualityGoals()
	if len(goals) != 3 {
		t.Fatalf("expected 3 goals, got %d: %+v", len(goals), goals)
	}
	if goals[0].ID != "Q1" || goals[0].Name != "Correctness" || !strings.Contains(goals[0].Motivation, "route correctly") {
		t.Fatalf("Q1 parsed wrong: %+v", goals[0])
	}
	if goals[1].ID != "Q2" || goals[1].Name != "Evolvability" {
		t.Fatalf("Q2 parsed wrong: %+v", goals[1])
	}
	if goals[2].ID != "Q3" || goals[2].Name != "Reliability" {
		t.Fatalf("Q3 parsed wrong: %+v", goals[2])
	}
}

func TestLoadArc42QualityGoalsIgnoresTablesOutsideSection12(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	for _, g := range loadArc42QualityGoals() {
		if g.Name == "NotAGoal" {
			t.Fatal("row from §1.3 table must not be parsed as a quality goal")
		}
	}
}

func TestLoadArc42QualityGoalsMissingDocReturnsNil(t *testing.T) {
	withArc42Doc(t, "")
	if goals := loadArc42QualityGoals(); goals != nil {
		t.Fatalf("expected nil for missing doc, got %+v", goals)
	}
}

func TestLoadArc42QualityGoalsAgainstRealRepoDoc(t *testing.T) {
	// The production doc must stay parseable: if the arc42 §1.2 table format
	// drifts, goal anchoring silently degrades — fail loudly here instead.
	// The doc location derives from the arc42GoalsDocPaths production var (a
	// hardcoded second literal here rotted silently when the doc moved to
	// 01-introduction-goals.md, leaving this pin skipping — inert — for days);
	// relative entries resolve against the repo root two levels up.
	var path string
	for _, p := range arc42GoalsDocPaths {
		candidates := []string{p}
		if !filepath.IsAbs(p) {
			candidates = append(candidates, filepath.Join("..", "..", p))
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				path = c
				break
			}
		}
		if path != "" {
			break
		}
	}
	if path == "" {
		t.Fatalf("no production arc42 doc found at any of %v — goal anchoring is silently degraded", arc42GoalsDocPaths)
	}
	old := arc42GoalsDocPaths
	arc42GoalsDocPaths = []string{path}
	t.Cleanup(func() { arc42GoalsDocPaths = old })
	goals := loadArc42QualityGoals()
	if len(goals) < 3 {
		t.Fatalf("production arc42 doc must yield >= 3 quality goals, got %d", len(goals))
	}
	var hasQ5 bool
	for _, g := range goals {
		if g.ID == "Q5" && strings.Contains(g.Name, "Consistency") {
			hasQ5 = true
		}
	}
	if !hasQ5 {
		t.Fatalf("production arc42 doc must include Q5 Consistency & Reuse, got %+v", goals)
	}
}

func TestArc42GoalsPromptBlockContainsEveryGoal(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	block := arc42GoalsPromptBlock()
	for _, want := range []string{"arc42", "Q1", "Correctness", "Q2", "Evolvability", "Q3", "Reliability"} {
		if !strings.Contains(block, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, block)
		}
	}
}

func TestArc42GoalsPromptBlockEmptyWhenDocMissing(t *testing.T) {
	withArc42Doc(t, "")
	if block := arc42GoalsPromptBlock(); block != "" {
		t.Fatalf("expected empty block without a doc, got %q", block)
	}
}

func TestArc42ResearchTopicsAnchoredToGoals(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	topics := arc42ResearchTopics()
	if len(topics) != 3 {
		t.Fatalf("expected one topic per goal, got %d", len(topics))
	}
	for i, want := range []string{"Correctness", "Evolvability", "Reliability"} {
		if !strings.Contains(topics[i], want) || !strings.Contains(topics[i], "arc42") {
			t.Fatalf("topic %d not anchored to goal %s: %q", i, want, topics[i])
		}
	}
}

func TestBuildClaudeReviewPromptIncludesArc42Goals(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	p := buildClaudeReviewPrompt("task ctx", goapReviewContext{mode: "structure", rangeDesc: "r", body: "b"})
	for _, want := range []string{"Q1", "Correctness", "Q2", "Evolvability", "Q3", "Reliability"} {
		if !strings.Contains(p, want) {
			t.Fatalf("claude review prompt missing arc42 goal %q", want)
		}
	}
	if !strings.Contains(p, "quality goal") {
		t.Fatal("claude review prompt must instruct goals to be named")
	}
}

func TestBuildSeedProgramPromptIncludesArc42Goals(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	ps, err := research.OpenPrograms(filepath.Join(t.TempDir(), "programs.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := buildSeedProgramPrompt(ps)
	for _, want := range []string{"Q1", "Correctness", "Q3", "Reliability"} {
		if !strings.Contains(p, want) {
			t.Fatalf("seed program prompt missing arc42 goal %q", want)
		}
	}
}

func TestBuildGrillRound1QueryIncludesArc42Goals(t *testing.T) {
	withArc42Doc(t, arc42GoalsTestDoc)
	q := buildGrillRound1Query("graph snippet here", "")
	if !strings.Contains(q, "graph snippet here") {
		t.Fatal("grill query must keep the graph snippet")
	}
	for _, want := range []string{"Q1", "Correctness", "Q2", "Evolvability"} {
		if !strings.Contains(q, want) {
			t.Fatalf("grill round-1 query missing arc42 goal %q", want)
		}
	}
}
