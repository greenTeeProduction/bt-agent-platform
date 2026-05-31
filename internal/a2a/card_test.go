package a2a

import (
	"os"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
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
	os.MkdirAll(agentsDir, 0755)

	agentYAML := `name: test-agent
description: A test agent
version: 1.0.0
tree: domain:code_review
created_at: 2026-01-01T00:00:00Z
`
	os.WriteFile(agentsDir+"/test-agent.yaml", []byte(agentYAML), 0644)

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
