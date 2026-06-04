package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"

	"github.com/tmc/langchaingo/llms"
)

// Compile-time check: engine.MockLLM implements llm.LLM.
var _ llm.LLM = (*engine.MockLLM)(nil)

type mockModel struct{}

func (m *mockModel) Call(ctx context.Context, prompt string, opts ...llms.CallOption) (string, error) {
	return "Final Answer: test passed", nil
}

func (m *mockModel) GenerateContent(ctx context.Context, msgs []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: "test"}},
	}, nil
}

// --- helpers ---

func newTestStores(t *testing.T) (*evolution.Store, *evolution.TreeStore) {
	t.Helper()
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
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
		LLMClient:    engine.NewMockLLM(),
		LangLLM:      &mockModel{},
		RefStore:     refStore,
		TreeStore:    treeStore,
		AgentFactory: nil, // no factory
		RunTaskFn: func(task string) string {
			return "task completed"
		},
		BB: &engine.Blackboard{
			Reflections: refStore,
			TreeStore:   treeStore,
			LLM:         engine.NewMockLLM(),
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
