package a2a

import (
	"os"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

func TestConvertToAgentCard_Basic(t *testing.T) {
	def := agent.Definition{
		Name:        "hermes-researcher",
		Description: "Hermes daily research agent — web search, NotebookLM queries, vault save",
		Version:     "1.0.0",
		Tree:        "research:deep_research",
		Inputs: []agent.InputSpec{
			{Name: "task", Type: "text", Required: true, Description: "Research topic"},
		},
		Outputs: []agent.OutputSpec{
			{Name: "result", Type: "markdown", Description: "Research findings"},
		},
		CreatedAt: time.Now(),
	}

	card, err := ConvertToAgentCard(def, "http://localhost:8686")
	if err != nil {
		t.Fatalf("ConvertToAgentCard failed: %v", err)
	}

	if card.Name != "hermes-researcher" {
		t.Errorf("expected name 'hermes-researcher', got %q", card.Name)
	}
	if card.Description != def.Description {
		t.Errorf("expected description %q, got %q", def.Description, card.Description)
	}
	if len(card.Skills) == 0 {
		t.Error("expected at least one skill in Agent Card")
	}
	if len(card.DefaultInputModes) == 0 {
		t.Error("expected at least one input mode")
	}
	if len(card.DefaultOutputModes) == 0 {
		t.Error("expected at least one output mode")
	}
	if len(card.SupportedInterfaces) == 0 {
		t.Error("expected at least one supported interface")
	}
	skill := card.Skills[0]
	if skill.ID != "research:deep_research" {
		t.Errorf("expected skill ID 'research:deep_research', got %q", skill.ID)
	}
}

func TestConvertToAgentCard_NoInputs(t *testing.T) {
	def := agent.Definition{
		Name:        "minimal-agent",
		Description: "A minimal agent",
		Version:     "1.0.0",
		Tree:        "domain:code_review",
	}

	card, err := ConvertToAgentCard(def, "http://localhost:8686")
	if err != nil {
		t.Fatalf("ConvertToAgentCard failed: %v", err)
	}

	if card.Name != "minimal-agent" {
		t.Errorf("expected name 'minimal-agent', got %q", card.Name)
	}
	if len(card.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(card.Skills))
	}
}

func TestConvertToAgentCard_EmptyName(t *testing.T) {
	def := agent.Definition{
		Name: "",
	}

	_, err := ConvertToAgentCard(def, "http://localhost:8686")
	if err == nil {
		t.Error("expected error for empty agent name")
	}
}

func TestBuildCardRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := tmpDir + "/agents"
	_ = os.MkdirAll(agentsDir, 0755)

	agentYAML := `name: test-agent
description: A test agent
version: 1.0.0
tree: domain:code_review
created_at: 2026-01-01T00:00:00Z
`
	_ = os.WriteFile(agentsDir+"/test-agent.yaml", []byte(agentYAML), 0644)

	reg, err := agent.NewRegistry(agentsDir)
	if err != nil {
		t.Fatalf("NewRegistry failed: %v", err)
	}

	cards, err := BuildCardRegistry(reg, "http://localhost:8686")
	if err != nil {
		t.Fatalf("BuildCardRegistry failed: %v", err)
	}

	if len(cards) == 0 {
		t.Error("expected at least one card")
	}
	card, ok := cards["test-agent"]
	if !ok {
		t.Error("expected card for 'test-agent'")
	} else if card.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", card.Name)
	}
}

// TestConvertToAgentCard_SkillTagsFromKnowledgeGraphCapabilities pins the goal
// of this task: a card's skill tags must come from
// internal/knowledge.GlobalGraph's real, fitness-weighted Capability list for
// the tree (the canonical "what can this tree do" model), not from an ad hoc
// split of the tree ID string (the old treeTags behavior, which for
// "domain:code_review" produced ["domain", "code", "review"] — none of which
// are real capabilities). knowledge.GlobalGraph registers "domain:code_review"
// with capabilities review_code, detect_bugs, suggest_improvements, and
// audit_security (internal/knowledge/registry.go) — those actions are what
// the auction's capability matching must key on.
func TestConvertToAgentCard_SkillTagsFromKnowledgeGraphCapabilities(t *testing.T) {
	def := agent.Definition{
		Name:        "code-reviewer",
		Description: "Reviews code for bugs and style",
		Version:     "1.0.0",
		Tree:        "domain:code_review",
	}

	card, err := ConvertToAgentCard(def, "http://localhost:8686")
	if err != nil {
		t.Fatalf("ConvertToAgentCard failed: %v", err)
	}
	if len(card.Skills) == 0 {
		t.Fatal("expected at least one skill")
	}

	treeMeta, ok := knowledge.GlobalGraph.Trees[def.Tree]
	if !ok {
		t.Fatalf("expected %q registered in knowledge.GlobalGraph", def.Tree)
	}

	tags := make(map[string]bool)
	for _, tag := range card.Skills[0].Tags {
		tags[tag] = true
	}
	for _, cap := range treeMeta.Capabilities {
		if !tags[cap.Action] {
			t.Errorf("expected skill tags to include capability action %q from knowledge.GlobalGraph, got tags=%v", cap.Action, card.Skills[0].Tags)
		}
	}
	if tags["domain"] || tags["code"] {
		t.Errorf("expected skill tags to no longer contain the ad hoc tree-ID split fragments, got tags=%v", card.Skills[0].Tags)
	}
}

// TestEligibleBidders_MatchesOnKnowledgeGraphCapabilityActions verifies the
// auction side of the same routing: an announcement whose RequiredTags name a
// real capability action (e.g. "review_code", from
// internal/knowledge.GlobalGraph's Capability list for "domain:code_review")
// must find the agent eligible. Under the old ad hoc treeTags splitter the
// card only ever carried tags like "domain", "code", "review" — never the
// actual capability action strings — so this requirement could never be
// satisfied.
func TestEligibleBidders_MatchesOnKnowledgeGraphCapabilityActions(t *testing.T) {
	def := agent.Definition{
		Name:        "code-reviewer",
		Description: "Reviews code for bugs and style",
		Version:     "1.0.0",
		Tree:        "domain:code_review",
	}
	card, err := ConvertToAgentCard(def, "http://localhost:8686")
	if err != nil {
		t.Fatalf("ConvertToAgentCard failed: %v", err)
	}

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"review_code"}}
	cards := map[string]*a2a.AgentCard{"code-reviewer": card}
	eligible := EligibleBidders(cards, ann)

	found := false
	for _, name := range eligible {
		if name == "code-reviewer" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q eligible for RequiredTags %v (a real knowledge-graph capability action), got eligible=%v", "code-reviewer", ann.RequiredTags, eligible)
	}
}
