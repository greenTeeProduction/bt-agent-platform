package knowledge

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/util"
)

// TreeTemplate captures the structural pattern of an existing tree for reuse.
type TreeTemplate struct {
	SourceID       string                        `json:"source_id"`
	Category       string                        `json:"category"`
	PreGate        *evolution.SerializableNode   `json:"pre_gate"`
	StrategyRouter *evolution.SerializableNode   `json:"strategy_router"`
	AgentNodes     []*evolution.SerializableNode `json:"agent_nodes"`
	ReflectNode    *evolution.SerializableNode   `json:"reflect_node"`
	OutcomeHandler *evolution.SerializableNode   `json:"outcome_handler"`
	Metadata       map[string]any                `json:"metadata"`
}

// Factory breeds new behavior trees from existing templates.
type Factory struct {
	Graph     *KnowledgeGraph
	Expert    *evolution.ExpertKnowledge
	Templates map[string]*TreeTemplate // category → representative template
}

// NewFactory creates a tree factory backed by the knowledge graph.
func NewFactory(kg *KnowledgeGraph) *Factory {
	if kg == nil {
		kg = NewKnowledgeGraph()
	}
	f := &Factory{
		Graph:     kg,
		Expert:    evolution.NewExpertKnowledge(),
		Templates: make(map[string]*TreeTemplate),
	}
	f.extractTemplates()
	return f
}

// extractTemplates learns structural patterns from all registered trees.
func (f *Factory) extractTemplates() {
	// Collect and sort IDs for deterministic template selection.
	ids := make([]string, 0, len(f.Graph.Trees))
	for id := range f.Graph.Trees {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		meta := f.Graph.Trees[id]
		// For now we store metadata only — trees are resolved at breed time
		tmpl := &TreeTemplate{
			SourceID: id,
			Category: meta.Category,
			Metadata: map[string]any{
				"node_count": meta.NodeCount,
				"fitness":    meta.Fitness,
				"keywords":   meta.Keywords,
			},
		}
		f.Templates[id] = tmpl
		if existing := f.Templates[meta.Category]; existing == nil || templateFitness(tmpl) > templateFitness(existing) {
			f.Templates[meta.Category] = tmpl
		}
	}
}

func templateFitness(tmpl *TreeTemplate) float64 {
	if tmpl == nil || tmpl.Metadata == nil {
		return 0
	}
	if f, ok := tmpl.Metadata["fitness"].(float64); ok {
		return f
	}
	return 0
}

// Breed creates a new tree by crossing over templates from 2-3 parent categories.
// Parents are selected based on relevance to the task description.
func (f *Factory) Breed(task, category string, parentIDs []string) *evolution.SerializableNode {
	if len(parentIDs) == 0 {
		// Auto-select parents from the same category
		parentIDs = f.selectParents(category, task)
	}
	if len(parentIDs) < 2 {
		return f.breedFromArchetype(category)
	}

	return f.crossoverBreed(category, parentIDs, task)
}

// crossoverBreed combines PreGate from parent A with StrategyRouter from parent B.
func (f *Factory) crossoverBreed(category string, parentIDs []string, task string) *evolution.SerializableNode {
	// Get templates from parent trees
	var templates []*TreeTemplate
	for _, pid := range parentIDs {
		if tmpl, ok := f.Templates[pid]; ok {
			templates = append(templates, tmpl)
		} else {
			// Try by category prefix for callers that pass "category:name" IDs.
			category := pid
			if parts := strings.SplitN(pid, ":", 2); len(parts) == 2 {
				category = parts[0]
			}
			if tmpl, ok := f.Templates[category]; ok {
				templates = append(templates, tmpl)
			}
		}
	}

	// Default: breed from archetype if no templates found
	if len(templates) < 2 {
		return f.breedFromArchetype(category)
	}

	// Crossover: select best PreGate from parent A, StrategyRouter from parent B
	preGate := f.clonePreGate(templates[0])
	strategyRouter := f.cloneStrategyRouter(templates[1])

	// Build the hybrid tree
	tree := &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     f.generateTreeName(category, task),
		Children: []evolution.SerializableNode{},
	}

	// Add PreGate
	if preGate != nil {
		tree.Children = append(tree.Children, *preGate)
	} else {
		tree.Children = append(tree.Children, f.defaultPreGate())
	}

	// Add StrategyRouter
	if strategyRouter != nil {
		tree.Children = append(tree.Children, *strategyRouter)
	} else {
		tree.Children = append(tree.Children, f.defaultAgentPath(task))
	}

	// Add Reflect + Outcome + Update
	tree.Children = append(tree.Children,
		evolution.SerializableNode{Type: "Action", Name: "ReflectOnOutcome"},
		f.defaultOutcomeSelector(),
	)

	return tree
}

// breedFromArchetype creates a tree matching the category's reference architecture.
func (f *Factory) breedFromArchetype(category string) *evolution.SerializableNode {
	arches := f.Expert.TreeArchetypes
	for _, arch := range arches {
		if arch.Category == category {
			return f.buildFromArchetype(arch)
		}
	}
	// Fallback: basic agent tree
	return f.buildBasicAgentTree()
}

// buildFromArchetype constructs a tree that satisfies the archetype requirements.
func (f *Factory) buildFromArchetype(arch evolution.TreeArchetype) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     arch.Category + "_generated",
		Children: []evolution.SerializableNode{},
	}

	// PreGate
	preGate := evolution.SerializableNode{
		Type: "Sequence", Name: "PreGate",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
		},
	}
	// Add tool setup if required
	for _, must := range arch.MustHave {
		if strings.Contains(must, "Setup") {
			preGate.Children = append(preGate.Children,
				evolution.SerializableNode{Type: "Action", Name: must})
		}
	}
	tree.Children = append(tree.Children, preGate)

	// StrategyRouter with agent nodes
	router := evolution.SerializableNode{Type: "Selector", Name: "StrategyRouter"}
	agentCount := 0
	for _, must := range arch.MustHave {
		if strings.Contains(must, "ChainAction") || strings.Contains(must, "agent") {
			agentCount++
		}
	}
	if agentCount == 0 {
		agentCount = 2
	}

	for i := 0; i < agentCount; i++ {
		path := evolution.SerializableNode{
			Type: "Sequence",
			Name: fmt.Sprintf("AgentPath_%d", i+1),
			Children: []evolution.SerializableNode{
				{
					Type:     "ChainAction",
					Name:     fmt.Sprintf("llm_call:Process step %d of the task", i+1),
					Metadata: map[string]any{"max_tokens": float64(10)},
				},
			},
		}
		router.Children = append(router.Children, path)
	}
	tree.Children = append(tree.Children, router,
		evolution.SerializableNode{Type: "Action", Name: "ReflectOnOutcome"},
		f.defaultOutcomeSelector(),
	)

	return tree
}

func (f *Factory) buildBasicAgentTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "BasicAgent",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: "SetupDefaultTools"},
				},
			},
			{
				Type: "Sequence", Name: "ExecutionPath",
				Children: []evolution.SerializableNode{{
					Type:     "ChainAction",
					Name:     "llm_call:Complete this task: {{.Task}}",
					Metadata: map[string]any{"max_tokens": float64(10)},
				}},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
			f.defaultOutcomeSelector(),
		},
	}
}

// ─── Helpers ───

func (f *Factory) selectParents(category, _ string) []string {
	// Prefer parents from same category
	var candidates []string
	for id, tmpl := range f.Templates {
		if tmpl.Category == category {
			candidates = append(candidates, id)
		}
	}
	// Fall back to any category
	if len(candidates) < 2 {
		for id := range f.Templates {
			candidates = append(candidates, id)
		}
	}
	// Pick 2-3 random parents
	n := 2 + rand.Intn(2)
	if n > len(candidates) {
		n = len(candidates)
	}
	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	return candidates[:n]
}

func (f *Factory) clonePreGate(tmpl *TreeTemplate) *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "PreGate",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
			{Type: "Action", Name: f.pickToolSetup(tmpl.Category)},
		},
	}
}

func (f *Factory) cloneStrategyRouter(tmpl *TreeTemplate) *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Selector", Name: "StrategyRouter",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PrimaryPath",
				Children: []evolution.SerializableNode{{
					Type:     "ChainAction",
					Name:     fmt.Sprintf("llm_call:Execute the primary workflow for %s", tmpl.Category),
					Metadata: map[string]any{"max_tokens": float64(10)},
				}},
			},
			{
				Type: "Sequence", Name: "FallbackPath",
				Children: []evolution.SerializableNode{{
					Type:     "ChainAction",
					Name:     "llm_call:Handle the task using fallback approach",
					Metadata: map[string]any{"max_tokens": float64(8)},
				}},
			},
		},
	}
}

func (f *Factory) pickToolSetup(category string) string {
	switch category {
	case "research":
		return "SetupResearchTools"
	case "startup":
		return "SetupStartupTools"
	case "domain":
		return "SetupDevTools"
	case "evolution":
		return "SetupDefaultTools"
	default:
		return "SetupDefaultTools"
	}
}

func (f *Factory) defaultPreGate() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type: "Sequence", Name: "PreGate",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
			{Type: "Action", Name: "SetupDefaultTools"},
		},
	}
}

func (f *Factory) defaultAgentPath(task string) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type: "Sequence", Name: "ExecutionPath",
		Children: []evolution.SerializableNode{{
			Type:     "ChainAction",
			Name:     fmt.Sprintf("llm_call:%s", truncateTask(task, 80)),
			Metadata: map[string]any{"max_tokens": float64(10)},
		}},
	}
}

func (f *Factory) defaultOutcomeSelector() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type: "Selector", Name: "OutcomeSelector",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "WasSuccessful"},
			{
				Type:     "ChainAction",
				Name:     "llm_call:Self-correct the previous step and fix any issues.",
				Metadata: map[string]any{"max_tokens": float64(5)},
			},
		},
	}
}

func (f *Factory) generateTreeName(category, task string) string {
	words := strings.Fields(task)
	key := ""
	count := 0
	for _, w := range words {
		w = strings.ToLower(strings.Trim(w, ",.!?;:"))
		if len(w) > 3 && w != "this" && w != "that" && w != "with" && w != "from" && w != "what" && w != "when" && w != "where" {
			key += "_" + w
			count++
			if count >= 3 {
				break
			}
		}
	}
	if key == "" {
		key = "_" + category + "_agent"
	}
	return category + ":" + strings.TrimPrefix(key, "_")
}

func truncateTask(task string, n int) string {
	if len(task) <= n {
		return task
	}
	return task[:n-3] + "..."
}

// ─── Backward compatibility ───

// AutoCreateTree is the legacy interface — discovers or creates a tree for a task.
// Returns (nil, existingTreeID, nil) if found, or (newTree, newTreeID, nil) if created.
func AutoCreateTree(kg *KnowledgeGraph, task string) (*evolution.SerializableNode, string, error) {
	treeID, confidence := kg.Discover(task)
	if confidence > 0.5 && treeID != "" {
		return nil, treeID, nil // existing tree found
	}

	f := NewFactory(kg)
	category := determineCategory(task)
	tree, newID := f.CreateTree(task, category, nil)
	return tree, newID, nil
}

func determineCategory(task string) string {
	t := strings.ToLower(task)
	switch {
	case containsAnyStr(t, "finance", "invest", "stock", "trading", "money", "revenue", "earnings", "valuation"):
		return "finance"
	case containsAnyStr(t, "code", "debug", "refactor", "build", "test", "deploy", "review"):
		return "domain"
	case containsAnyStr(t, "research", "analyze", "study", "investigate"):
		return "research"
	case containsAnyStr(t, "startup", "company", "ceo", "strategy", "business", "hiring"):
		return "startup"
	case containsAnyStr(t, "think", "debate", "synthesize", "perspective", "argument"):
		return "thinktank"
	case containsAnyStr(t, "evolve", "optimize", "improve", "genetic"):
		return "evolution"
	default:
		return "core"
	}
}

func containsAnyStr(s string, substrs ...string) bool { return util.ContainsAnyStr(s, substrs...) }

// ─── Public API ───

// CreateTree breeds a new behavior tree for a task.
// Returns the new tree and its knowledge graph ID.
func (f *Factory) CreateTree(task, category string, parentIDs []string) (*evolution.SerializableNode, string) {
	tree := f.Breed(task, category, parentIDs)
	treeID := f.generateTreeName(category, task)

	// Register in knowledge graph
	meta := &TreeMeta{
		ID:          treeID,
		Name:        tree.Name,
		Category:    category,
		Description: "Auto-generated tree for: " + truncateTask(task, 100),
		NodeCount:   evolution.CountNodes(tree),
		Keywords:    extractKeywords(task),
		Capabilities: []Capability{
			{Action: category + "_automation", Domain: category, Strength: 0.7},
		},
	}
	f.Graph.Register(meta)

	return tree, treeID
}

// CreateFromParents breeds a tree from specific parent trees.
func (f *Factory) CreateFromParents(parentA, parentB string, task string) (*evolution.SerializableNode, string) {
	category := "hybrid"
	if tmpl, ok := f.Templates[parentA]; ok {
		category = tmpl.Category
	}
	tree := f.Breed(task, category, []string{parentA, parentB})
	treeID := f.generateTreeName(category, task)

	meta := &TreeMeta{
		ID:          treeID,
		Name:        tree.Name,
		Category:    category,
		Description: fmt.Sprintf("Bred from %s × %s for: %s", parentA, parentB, truncateTask(task, 80)),
		NodeCount:   evolution.CountNodes(tree),
		Keywords:    extractKeywords(task),
		Relations: []Relation{
			{Target: parentA, Type: "derived_from"},
			{Target: parentB, Type: "derived_from"},
		},
		Capabilities: []Capability{
			{Action: "hybrid_execution", Domain: category, Strength: 0.75},
		},
	}
	f.Graph.Register(meta)

	return tree, treeID
}

func extractKeywords(task string) []string {
	words := strings.Fields(strings.ToLower(task))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ",.!?;:\"")
		if len(w) > 3 {
			keywords = append(keywords, w)
		}
		if len(keywords) >= 8 {
			break
		}
	}
	return keywords
}
