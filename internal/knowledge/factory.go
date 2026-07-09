package knowledge

import (
	"encoding/json"
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

	// Resolve loads the actual SerializableNode structure of a KG-registered
	// tree (compiled-in catalogs + persisted generated trees). Wired by the
	// caller (cmd/bt-agent: domains.ResolveTreeID + dynamic resolver). Nil →
	// structural crossover is skipped and breeding uses synthetic templates.
	Resolve func(id string) *evolution.SerializableNode
	// Validate gates structural-crossover children (wired to
	// engine.ValidateTreeFull; knowledge cannot import engine). A child that
	// fails validation is discarded in favor of the synthetic template path.
	Validate func(tree *evolution.SerializableNode) error

	// rng, when non-nil, drives parent selection so tests can seed a
	// deterministic draw. Nil → the process-global math/rand source is used.
	rng *rand.Rand
}

// SetSeed pins the factory's parent-selection RNG to a fixed seed, making
// fitness-weighted parent draws reproducible in tests.
func (f *Factory) SetSeed(seed int64) {
	f.rng = rand.New(rand.NewSource(seed))
}

func (f *Factory) randIntn(n int) int {
	if f.rng != nil {
		return f.rng.Intn(n)
	}
	return rand.Intn(n)
}

func (f *Factory) randFloat64() float64 {
	if f.rng != nil {
		return f.rng.Float64()
	}
	return rand.Float64()
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
				"node_count":         meta.NodeCount,
				"fitness":            meta.Fitness,
				"structural_fitness": meta.StructuralFitness,
				"run_count":          meta.RunCount,
				"keywords":           meta.Keywords,
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

// templateStructuralFitness reads a template's evolved structural fitness,
// tolerating the float64 form a JSON round-trip produces (0 when unset).
func templateStructuralFitness(tmpl *TreeTemplate) float64 {
	if tmpl == nil || tmpl.Metadata == nil {
		return 0
	}
	if f, ok := tmpl.Metadata["structural_fitness"].(float64); ok {
		return f
	}
	return 0
}

// templateRunCount reads a template's recorded run count, tolerating both the
// int form set in-process (extractTemplates) and the float64 form a JSON
// round-trip produces.
func templateRunCount(tmpl *TreeTemplate) int {
	if tmpl == nil || tmpl.Metadata == nil {
		return 0
	}
	switch v := tmpl.Metadata["run_count"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// templateSelectionWeight is the cold-start-discounted, structural-blended
// fitness used to weight parent selection, so a lucky high-fitness/low-run
// template does not out-draw a proven one while an unproven-but-archive-improved
// template can still be surfaced. Shares the blend with stringMatch's discovery
// tie-break so breeding and discovery apply the same selection pressure.
func templateSelectionWeight(tmpl *TreeTemplate) float64 {
	return blendedSelectionFitness(templateFitness(tmpl), templateStructuralFitness(tmpl), templateRunCount(tmpl))
}

// Breed creates a new tree by crossing over templates from 2-3 parent categories.
// Parents are selected based on relevance to the task description.
func (f *Factory) Breed(task, category string, parentIDs []string) *evolution.SerializableNode {
	if len(parentIDs) == 0 {
		// Auto-select parents from the same category
		parentIDs = f.selectParents(category, task)
	} else {
		// Caller-supplied parents: refresh their fitness from the live graph
		// too, so any breeding path weights off current fitness (milestone 4/5).
		for _, pid := range parentIDs {
			f.refreshTemplateFitness(f.Templates[pid])
		}
	}
	if len(parentIDs) < 2 {
		return f.breedFromArchetype(category)
	}

	return f.crossoverBreed(category, parentIDs, task)
}

// crossoverBreed combines PreGate from parent A with StrategyRouter from parent B.
// Real structural crossover (actual parent subtrees) is attempted first; the
// synthetic template path below is the fallback when parents have no stored
// structure or the spliced child fails validation.
func (f *Factory) crossoverBreed(category string, parentIDs []string, task string) *evolution.SerializableNode {
	if tree := f.structuralCrossover(category, parentIDs, task); tree != nil {
		return tree
	}

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

// structuralCrossover splices real parent subtrees: parent A's PreGate ×
// parent B's StrategyRouter (ADR-010 Phase 3 — fixes R10 "shallow
// crossover"). Returns nil when fewer than two parents resolve to actual
// structures or when the spliced child fails validation, letting the caller
// fall back to synthetic templates.
func (f *Factory) structuralCrossover(category string, parentIDs []string, task string) *evolution.SerializableNode {
	if f.Resolve == nil {
		return nil
	}

	// Resolve up to two parents that are KG-registered AND have structure.
	type parent struct {
		id   string
		tree *evolution.SerializableNode
	}
	var parents []parent
	for _, pid := range parentIDs {
		if len(parents) == 2 {
			break
		}
		if _, registered := f.Graph.Trees[pid]; !registered {
			continue
		}
		if tree := f.Resolve(pid); tree != nil && len(tree.Children) > 0 {
			parents = append(parents, parent{id: pid, tree: tree})
		}
	}
	if len(parents) < 2 {
		return nil
	}

	preGate := extractSubtree(parents[0].tree, isPreGateNode)
	router := extractSubtree(parents[1].tree, isStrategyRouterNode)
	if preGate == nil && router == nil {
		return nil // neither parent contributes real structure
	}
	if preGate == nil {
		g := f.defaultPreGate()
		preGate = &g
	}
	if router == nil {
		r := f.defaultAgentPath(task)
		router = &r
	}

	// Cache the extracted structure on the parents' templates so repeated
	// breeding doesn't re-extract.
	if tmpl, ok := f.Templates[parents[0].id]; ok && tmpl.PreGate == nil {
		tmpl.PreGate = preGate
	}
	if tmpl, ok := f.Templates[parents[1].id]; ok && tmpl.StrategyRouter == nil {
		tmpl.StrategyRouter = router
	}

	child := &evolution.SerializableNode{
		Type: "Sequence",
		Name: f.generateTreeName(category, task),
		Metadata: map[string]any{
			"generated_by": "structural_crossover",
			"parent_a":     parents[0].id,
			"parent_b":     parents[1].id,
		},
		Children: []evolution.SerializableNode{
			*jsonCloneNode(preGate),
			*jsonCloneNode(router),
			{Type: "Action", Name: "ReflectOnOutcome"},
			f.defaultOutcomeSelector(),
		},
	}

	if f.Validate != nil {
		if err := f.Validate(child); err != nil {
			return nil // invalid splice → synthetic fallback
		}
	}
	return child
}

// isPreGateNode matches a parent's input-gating subtree.
func isPreGateNode(n *evolution.SerializableNode) bool {
	return n.Name == "PreGate" || (n.Type == "Sequence" && strings.Contains(n.Name, "Gate"))
}

// isStrategyRouterNode matches a parent's routing subtree: the canonical
// StrategyRouter name, or any multi-branch Selector/DecisionTree.
func isStrategyRouterNode(n *evolution.SerializableNode) bool {
	if n.Name == "StrategyRouter" {
		return true
	}
	return (n.Type == "Selector" || n.Type == "DecisionTree") && len(n.Children) >= 2
}

// extractSubtree returns the first node (depth-first, root included)
// matching the predicate, or nil.
func extractSubtree(node *evolution.SerializableNode, match func(*evolution.SerializableNode) bool) *evolution.SerializableNode {
	if node == nil {
		return nil
	}
	if match(node) {
		return node
	}
	for i := range node.Children {
		if found := extractSubtree(&node.Children[i], match); found != nil {
			return found
		}
	}
	return nil
}

// jsonCloneNode deep-copies a subtree so a bred child never aliases its
// parent's nodes (mutating the child must not corrupt the parent).
func jsonCloneNode(node *evolution.SerializableNode) *evolution.SerializableNode {
	data, err := json.Marshal(node)
	if err != nil {
		return node
	}
	var clone evolution.SerializableNode
	if err := json.Unmarshal(data, &clone); err != nil {
		return node
	}
	return &clone
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
	// Prefer parents from same category. extractTemplates stores each template
	// twice — under its SourceID and under a category alias key — both pointing at
	// the same *TreeTemplate. Skip the alias keys (id != SourceID) so a single
	// template cannot enter the candidate pool twice and be drawn as both parents
	// of a crossover.
	var candidates []string
	for id, tmpl := range f.Templates {
		if id != tmpl.SourceID {
			continue // category-alias key aliasing another template
		}
		if tmpl.Category == category {
			candidates = append(candidates, id)
		}
	}
	// Fall back to any category
	if len(candidates) < 2 {
		for id, tmpl := range f.Templates {
			if id != tmpl.SourceID {
				continue // category-alias key aliasing another template
			}
			candidates = append(candidates, id)
		}
	}
	// Refresh each candidate's fitness from the live graph so selection reflects
	// fitness updates applied after NewFactory snapshotted templates (milestone
	// 4/5 of the selection-pressure program: breed off live, not stale, fitness).
	for _, id := range candidates {
		f.refreshTemplateFitness(f.Templates[id])
	}
	// Pick 2-3 parents, weighted by template fitness so high-fitness parents
	// are drawn far more often than uniform shuffle would allow (milestone 1/5
	// of the selection-pressure program: fitness-driven breeding).
	n := 2 + f.randIntn(2)
	if n > len(candidates) {
		n = len(candidates)
	}
	return f.weightedSampleParents(candidates, n)
}

// refreshTemplateFitness re-reads a template's fitness and run count from the
// live KnowledgeGraph, overwriting the snapshot cached when NewFactory extracted
// templates. Called at breed time so a tree whose KG fitness changed after
// factory construction is weighted by its current fitness, not the stale one
// (milestone 4/5). Templates whose SourceID is no longer registered keep their
// snapshot values.
func (f *Factory) refreshTemplateFitness(tmpl *TreeTemplate) {
	if tmpl == nil || f.Graph == nil {
		return
	}
	meta, ok := f.Graph.Trees[tmpl.SourceID]
	if !ok || meta == nil {
		return
	}
	if tmpl.Metadata == nil {
		tmpl.Metadata = make(map[string]any)
	}
	tmpl.Metadata["fitness"] = meta.Fitness
	tmpl.Metadata["structural_fitness"] = meta.StructuralFitness
	tmpl.Metadata["run_count"] = meta.RunCount
}

// weightedSampleParents draws n distinct parents via roulette-wheel sampling
// over templateSelectionWeight — cold-start-discounted fitness, so a lucky
// low-run template cannot out-draw a proven one (without replacement). Zero- or
// negative-weight templates keep a small floor weight so they remain reachable
// and an all-zero pool degrades gracefully to uniform selection.
func (f *Factory) weightedSampleParents(candidates []string, n int) []string {
	// weightFloor keeps unrated templates selectable without diluting the
	// pressure a genuinely high-fitness parent exerts.
	const weightFloor = 0.01

	remaining := append([]string(nil), candidates...)
	weights := make([]float64, len(remaining))
	total := 0.0
	for i, id := range remaining {
		w := templateSelectionWeight(f.Templates[id])
		if w < weightFloor {
			w = weightFloor
		}
		weights[i] = w
		total += w
	}

	selected := make([]string, 0, n)
	for len(selected) < n && len(remaining) > 0 {
		r := f.randFloat64() * total
		idx := 0
		for ; idx < len(remaining)-1; idx++ {
			r -= weights[idx]
			if r <= 0 {
				break
			}
		}
		selected = append(selected, remaining[idx])
		total -= weights[idx]
		remaining = append(remaining[:idx], remaining[idx+1:]...)
		weights = append(weights[:idx], weights[idx+1:]...)
	}
	return selected
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
			Name:     fmt.Sprintf("llm_call:%s", util.Truncate(task, 80)),
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

// ─── Backward compatibility ───

// AutoCreateTree is the legacy interface — discovers or creates a tree for a task.
// Returns (nil, existingTreeID, nil) if found, or (newTree, newTreeID, nil) if created.
func AutoCreateTree(kg *KnowledgeGraph, task string) (*evolution.SerializableNode, string, error) {
	return AutoCreateTreeWith(NewFactory(kg), task)
}

// AutoCreateTreeWith discovers or creates a tree using a caller-configured
// factory (Resolve/Validate hooks for real structural crossover).
func AutoCreateTreeWith(f *Factory, task string) (*evolution.SerializableNode, string, error) {
	treeID, confidence := f.Graph.Discover(task)
	if confidence > 0.5 && treeID != "" {
		return nil, treeID, nil // existing tree found
	}

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
		Description: "Auto-generated tree for: " + util.Truncate(task, 100),
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
		Description: fmt.Sprintf("Bred from %s × %s for: %s", parentA, parentB, util.Truncate(task, 80)),
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
