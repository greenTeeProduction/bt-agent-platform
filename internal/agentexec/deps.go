package agentexec

import (
	"fmt"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/util"
)

// NewRunDeps builds agent.RunDeps for in-process execution (CLI, dashboard, tests).
func NewRunDeps() (*agent.RunDeps, error) {
	cfg, err := config.Load()
	if err != nil && cfg == nil {
		cfg = config.DefaultConfig()
	}
	cfg.ResolvePaths()
	if !util.PlatformPathsConfigured() && cfg.Paths.HomeDir == "" {
		return nil, fmt.Errorf("platform home: set BT_AGENT_HOME or supply a user home/agent definition directory")
	}
	boardRoot := filepath.Join(cfg.Paths.HomeDir, "blackboard")
	if util.PlatformPathsConfigured() {
		boardRoot = agent.BlackboardDir()
	}
	boards, err := blackboard.NewPersistentManager(boardRoot)
	if err != nil {
		return nil, fmt.Errorf("initialize agent blackboard persistence: %w", err)
	}

	refPath, err := cfg.SharedReflectionsDir()
	if root := util.ConfiguredReflectionsDir(); root != "" {
		refPath, err = root, nil
	}
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

	registryDir := cfg.AgentDefsDir
	if registryDir == "" {
		registryDir = filepath.Join(cfg.Paths.HomeDir, "agents")
	}
	historyDir := cfg.Paths.HistoryDir
	if util.PlatformPathsConfigured() {
		registryDir, historyDir = agent.RegistryDir(), agent.HistoryDir()
	}
	reg, err := agent.NewRegistry(registryDir)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	hist, err := agent.NewHistory(historyDir)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}

	return &agent.RunDeps{
		Registry:    reg,
		History:     hist,
		LLM:         llmClient,
		RefStore:    refStore,
		TreeStore:   treeStore,
		Blackboards: boards,
		ResolveTree: func(id string) *evolution.SerializableNode {
			return domains.ResolveTreeID(id)
		},
		ResolveTreeForUser: func(user, id string) *evolution.SerializableNode {
			return domains.ResolveTreeIDForUser(user, id)
		},
	}, nil
}
