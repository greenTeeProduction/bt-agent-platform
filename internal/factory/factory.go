// Package factory implements a skill-to-behavior-tree compiler that
// converts SKILL.md specifications into executable behavior trees.
// The pipeline: Analyzer extracts pre_checks, strategy_paths, and
// fallback patterns → Generator produces SerializableNodes →
// AgentFactory persists the tree for runtime execution.
package factory

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"
)

// AgentFactory orchestrates skill → behavior tree → runnable agent.
type AgentFactory struct {
	analyzer  *Analyzer
	generator *Generator
	treeStore *evolution.TreeStore
	refStore  *reflection.Store
	llmClient llm.LLM
}

// NewAgentFactory creates a new factory.
func NewAgentFactory(llmClient llm.LLM, homeDir string) (*AgentFactory, error) {
	ts, err := evolution.NewTreeStore(filepath.Join(homeDir, ".go-bt-reflections"))
	if err != nil {
		return nil, fmt.Errorf("tree store: %w", err)
	}
	rs, err := reflection.NewStore(filepath.Join(homeDir, ".go-bt-reflections"))
	if err != nil {
		return nil, fmt.Errorf("reflection store: %w", err)
	}
	return &AgentFactory{
		analyzer:  NewAnalyzer(llmClient),
		generator: NewGenerator(),
		treeStore: ts,
		refStore:  rs,
		llmClient: llmClient,
	}, nil
}

// CreateFromFile loads a SKILL.md file and produces a GeneratedAgent.
func (f *AgentFactory) CreateFromFile(skillPath string) (*GeneratedAgent, error) {
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("read skill file: %w", err)
	}

	name := skillName(skillPath)
	return f.CreateFromContent(name, string(content))
}

// CreateFromContent analyzes skill content and produces a GeneratedAgent.
func (f *AgentFactory) CreateFromContent(name, content string) (*GeneratedAgent, error) {
	// Step 1: Analyze skill → TreeSpec
	spec, err := f.analyzer.Analyze(content)
	if err != nil {
		return nil, fmt.Errorf("analyze skill %q: %w", name, err)
	}

	// Step 2: Build blackboard with the shared LLM client
	bb := &engine.Blackboard{
		Reflections: f.refStore,
		TreeStore:   f.treeStore,
		LLM:         f.llmClient,
	}

	// Step 3: Generate tree
	agent, err := f.generator.Generate(spec, name, bb)
	if err != nil {
		return nil, fmt.Errorf("generate tree: %w", err)
	}

	// Step 4: Persist the tree under a skill-specific name
	skillTreePath := filepath.Join(f.treeStore.Dir(), fmt.Sprintf("tree-%s.json", name))
	if err := f.treeStore.SaveTo(agent.SerTree, skillTreePath); err != nil {
		return nil, fmt.Errorf("save tree: %w", err)
	}

	return agent, nil
}

// CreateFromSkillDir scans a skill directory for SKILL.md and generates an agent.
func (f *AgentFactory) CreateFromSkillDir(skillDir string) (*GeneratedAgent, error) {
	// If the path is already a .md file, use it directly
	if filepath.Ext(skillDir) == ".md" {
		return f.CreateFromFile(skillDir)
	}
	// Otherwise, look for SKILL.md inside the directory
	mdPath := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(mdPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no SKILL.md found in %s", skillDir)
	}
	return f.CreateFromFile(mdPath)
}

// skillName extracts a human-readable name from a skill file path.
func skillName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	if name == "SKILL" {
		parent := filepath.Base(filepath.Dir(path))
		if parent != "." && parent != "/" {
			return parent
		}
	}
	return name
}
