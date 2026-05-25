package knowledge

import "strings"

// TreeMeta describes a behavior tree in the knowledge graph.
type TreeMeta struct {
	ID          string   `json:"id"`          // unique identifier (e.g., "finance:pitch_agent")
	Name        string   `json:"name"`        // human-readable name
	Category    string   `json:"category"`    // finance, domain, research, startup, thinktank, evolution, core
	Description string   `json:"description"` // what it does
	NodeCount   int      `json:"node_count"`  // total nodes
	Fitness     float64  `json:"fitness"`     // current fitness score (0-100)

	// Capabilities — what tasks this tree handles
	Capabilities []Capability `json:"capabilities"`

	// Keywords that trigger this tree
	Keywords []string `json:"keywords"`

	// Relationships to other trees
	Relations []Relation `json:"relations,omitempty"`

	// Dependencies — other trees this tree uses or extends
	DependsOn []string `json:"depends_on,omitempty"`

	// Tags for discovery
	Tags []string `json:"tags,omitempty"`
}

// Capability describes what a tree can do.
type Capability struct {
	Action   string  `json:"action"`             // what it does (e.g., "analyze_financials", "review_code")
	Domain   string  `json:"domain"`             // domain area (e.g., "finance", "engineering", "strategy")
	Strength float64 `json:"strength,omitempty"` // 0-1 how good it is at this (from benchmarks)
}

// Relation describes a connection to another tree.
type Relation struct {
	Target string `json:"target"` // tree ID
	Type   string `json:"type"`   // specializes, composes, replaces, extends, depends_on
}

// KnowledgeGraph maps all behavior trees and their relationships.
type KnowledgeGraph struct {
	Trees    map[string]*TreeMeta `json:"trees"`
	Edges    []Edge               `json:"edges"`
	Synonyms map[string]string    `json:"synonyms"` // capability → tree mapping
}

// Edge is a directed relationship between two trees.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`
	Weight float64 `json:"weight"` // 0-1 relationship strength
}

// NewKnowledgeGraph creates an empty graph.
func NewKnowledgeGraph() *KnowledgeGraph {
	return &KnowledgeGraph{
		Trees:    make(map[string]*TreeMeta),
		Synonyms: make(map[string]string),
	}
}

// Register adds a tree to the knowledge graph.
func (kg *KnowledgeGraph) Register(tree *TreeMeta) {
	kg.Trees[tree.ID] = tree

	// Index keywords as synonyms → tree
	for _, kw := range tree.Keywords {
		kg.Synonyms[strings.ToLower(kw)] = tree.ID
	}
	// Index capabilities as synonyms
	for _, cap := range tree.Capabilities {
		kg.Synonyms[strings.ToLower(cap.Action)] = tree.ID
	}
}

// Connect adds a relationship between two trees.
func (kg *KnowledgeGraph) Connect(from, to, relType string) {
	kg.Edges = append(kg.Edges, Edge{
		From:   from,
		To:     to,
		Type:   relType,
		Weight: 1.0,
	})
}

// Discover finds the best tree for a given task description.
// Returns the tree ID and a confidence score (0-1).
func (kg *KnowledgeGraph) Discover(task string) (treeID string, confidence float64) {
	taskLower := strings.ToLower(task)

	// Phase 1: exact keyword match
	for keyword, matchedID := range kg.Synonyms {
		if strings.Contains(taskLower, keyword) {
			if _, ok := kg.Trees[matchedID]; ok {
				return matchedID, 0.8
			}
		}
	}

	// Phase 2: capability overlap scoring
	best := ""
	bestScore := 0.0
	for id, tree := range kg.Trees {
		score := kg.matchScore(taskLower, tree)
		if score > bestScore {
			bestScore = score
			best = id
		}
	}

	if bestScore > 0.3 {
		return best, bestScore
	}

	return "", 0.0
}

// matchScore computes how well a tree matches a task.
func (kg *KnowledgeGraph) matchScore(task string, tree *TreeMeta) float64 {
	score := 0.0

	// Keyword matches
	for _, kw := range tree.Keywords {
		if strings.Contains(task, strings.ToLower(kw)) {
			score += 0.2
		}
	}

	// Capability matches
	for _, cap := range tree.Capabilities {
		if strings.Contains(task, strings.ToLower(cap.Action)) {
			score += 0.15 * cap.Strength
		}
		if strings.Contains(task, strings.ToLower(cap.Domain)) {
			score += 0.1 * cap.Strength
		}
	}

	// Category match
	if strings.Contains(task, tree.Category) {
		score += 0.1
	}

	return score
}

// ListByCategory returns all trees in a category.
func (kg *KnowledgeGraph) ListByCategory(category string) []*TreeMeta {
	var result []*TreeMeta
	for _, tree := range kg.Trees {
		if tree.Category == category {
			result = append(result, tree)
		}
	}
	return result
}

// Query returns trees matching a capability.
func (kg *KnowledgeGraph) Query(capability string) []*TreeMeta {
	var result []*TreeMeta
	capLower := strings.ToLower(capability)
	for _, tree := range kg.Trees {
		for _, cap := range tree.Capabilities {
			if strings.Contains(strings.ToLower(cap.Action), capLower) ||
				strings.Contains(strings.ToLower(cap.Domain), capLower) {
				result = append(result, tree)
				break
			}
		}
	}
	return result
}

// Summary returns a human-readable graph summary.
func (kg *KnowledgeGraph) Summary() string {
	categories := make(map[string]int)
	for _, t := range kg.Trees {
		categories[t.Category]++
	}

	s := "Knowledge Graph: "
	first := true
	for cat, count := range categories {
		if !first { s += ", " }
		s += cat + "(" + itoa(count) + ")"
		first = false
	}
	s += " | " + itoa(len(kg.Trees)) + " trees, " + itoa(len(kg.Edges)) + " edges"
	return s
}

func itoa(n int) string {
	if n == 0 { return "0" }
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
