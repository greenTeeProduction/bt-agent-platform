package agentexec

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// NewRunDeps builds agent.RunDeps for in-process execution (CLI, dashboard, tests).
func NewRunDeps() (*agent.RunDeps, error) {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	refPath, err := ReflectionsPath()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	refStore, err := evolution.NewStore(refPath)
	if err != nil {
		return nil, fmt.Errorf("reflection store: %w", err)
	}
	blocks.InitRegistry(refPath)

	treeStore, err := evolution.NewTreeStore(refPath)
	if err != nil {
		return nil, fmt.Errorf("tree store: %w", err)
	}

	llmClient, err := llm.NewProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("llm provider: %w", err)
	}

	reg, err := agent.NewRegistry(agent.RegistryDir())
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	hist, err := agent.NewHistory(agent.HistoryDir())
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}

	return &agent.RunDeps{
		Registry:  reg,
		History:   hist,
		LLM:       llmClient,
		RefStore:  refStore,
		TreeStore: treeStore,
		ResolveTree: func(id string) *evolution.SerializableNode {
			return domains.ResolveTreeID(id)
		},
		ResolveTreeForUser: func(user, id string) *evolution.SerializableNode {
			return domains.ResolveTreeIDForUser(user, id)
		},
	}, nil
}
