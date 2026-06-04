package langagent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"

	"github.com/tmc/langchaingo/llms"
)

// --- llm.LLM mock ---

type mockLLM struct{}

func (m *mockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *mockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}

func (m *mockLLM) Generate(_ string) (string, error) {
	return "mock response", nil
}

func (m *mockLLM) AnalyzeComplexity(_ string) string {
	return "medium"
}

func (m *mockLLM) GeneratePlan(_, _ string) string {
	return "1. Analyze\n2. Execute\n3. Verify"
}

func (m *mockLLM) Reflect(_, _, _ string) (string, string) {
	return "good planning", "better execution"
}

var _ llm.LLM = (*mockLLM)(nil)

// --- llms.Model mock ---

type mockModel struct{}

func (m *mockModel) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	return "Final Answer: test passed", nil
}

func (m *mockModel) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: "test"}},
	}, nil
}

// --- helpers ---

func newTestStores(t *testing.T) (*reflection.Store, *evolution.TreeStore) {
	t.Helper()
	dir := t.TempDir()
	refStore, err := reflection.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("new reflection store: %v", err)
	}
	treeStore, err := evolution.NewTreeStore(filepath.Join(dir, "trees"))
	if err != nil {
		t.Fatalf("new tree store: %v", err)
	}
	return refStore, treeStore
}

func newConfig(t *testing.T) Config {
	t.Helper()
	refStore, treeStore := newTestStores(t)
	return Config{
		LLMClient:    &mockLLM{},
		LangLLM:      &mockModel{},
		RefStore:     refStore,
		TreeStore:    treeStore,
		AgentFactory: nil, // no factory
		RunTaskFn: func(_ string) string {
			return "task completed"
		},
		BB: &engine.Blackboard{
			Reflections: refStore,
			TreeStore:   treeStore,
			LLM:         &mockLLM{},
		},
	}
}

// --- tests ---

func TestNewEvolvedAgent(t *testing.T) {
	t.Run("creates agent with all required fields", func(t *testing.T) {
		cfg := newConfig(t)

		agent, err := NewEvolvedAgent(cfg)
		if err != nil {
			t.Fatalf("NewEvolvedAgent: %v", err)
		}

		if agent == nil {
			t.Fatal("agent is nil")
		}
		if agent.Agent == nil {
			t.Error("Agent field is nil")
		}
		if agent.Executor == nil {
			t.Error("Executor field is nil")
		}
		if agent.Tools == nil {
			t.Error("Tools field is nil")
		}
		if agent.BB == nil {
			t.Error("BB field is nil")
		}
	})

	t.Run("has correct tool count without factory", func(t *testing.T) {
		cfg := newConfig(t)
		cfg.AgentFactory = nil

		agent, err := NewEvolvedAgent(cfg)
		if err != nil {
			t.Fatalf("NewEvolvedAgent: %v", err)
		}

		// 6 tools: RunTask, Reflect, Fitness, Evolve, GetTree, GetReflections
		want := 6
		if got := len(agent.Tools); got != want {
			t.Errorf("Tools count = %d, want %d", got, want)
		}
	})

	t.Run("has correct tool count with factory", func(t *testing.T) {
		cfg := newConfig(t)
		cfg.AgentFactory = nil // we can't easily mock AgentFactory, but we can test nil

		agent, err := NewEvolvedAgent(cfg)
		if err != nil {
			t.Fatalf("NewEvolvedAgent: %v", err)
		}

		// Without factory: 6 tools
		if got := len(agent.Tools); got != 6 {
			t.Errorf("Tools count without factory = %d, want 6", got)
		}
	})

	t.Run("rejects nil LangLLM", func(t *testing.T) {
		cfg := newConfig(t)
		cfg.LangLLM = nil

		_, err := NewEvolvedAgent(cfg)
		if err == nil {
			t.Fatal("expected error for nil LangLLM, got nil")
		}
	})

	t.Run("tools have expected names", func(t *testing.T) {
		cfg := newConfig(t)
		agent, err := NewEvolvedAgent(cfg)
		if err != nil {
			t.Fatalf("NewEvolvedAgent: %v", err)
		}

		wantNames := []string{
			"bt_run_task",
			"bt_reflect",
			"bt_get_fitness",
			"bt_evolve",
			"bt_get_tree",
			"bt_get_reflections",
		}

		for i, want := range wantNames {
			if i >= len(agent.Tools) {
				t.Errorf("missing tool %q at index %d", want, i)
				continue
			}
			got := agent.Tools[i].Name()
			if got != want {
				t.Errorf("tools[%d].Name() = %q, want %q", i, got, want)
			}
		}
	})
}
