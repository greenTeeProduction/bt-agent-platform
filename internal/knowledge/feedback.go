package knowledge

import "time"

// RunRecord captures the outcome of a single agent execution.
type RunRecord struct {
	TreeID   string
	Task     string
	Outcome  string // "success", "failure", "chain_failed", "chain_panic", "evolved"
	Duration time.Duration
	Tools    []string // tools used during execution
	Quality  float64  // 0-100 quality score; for "evolved", the elite's structural fitness
}

// RecordRun updates the knowledge graph with execution feedback.
func (kg *KnowledgeGraph) RecordRun(rec RunRecord) {
	kg.mu.Lock()
	defer kg.mu.Unlock()
	tree, ok := kg.Trees[rec.TreeID]
	if !ok {
		return
	}

	tree.LastOutcome = rec.Outcome
	tree.LastDuration = rec.Duration

	if rec.Outcome == "evolved" {
		// An evolution pass is not a genuine execution: it must not inflate
		// RunCount (which drives cold-start confidence weighting). It is counted
		// separately in EvolvedCount instead.
		tree.EvolvedCount++
		// A winning QD/island elite's structural fitness is captured in its own
		// StructuralFitness field, leaving Fitness a pure runtime-success EMA that
		// only genuine executions maintain. Selection blends the two (gated by
		// RunCount) so fitness-aware discovery can surface archive-improved trees
		// on the very next run without an evolution pass ever overwriting what
		// real executions measured. The write-back is clamped to [0,100] and
		// monotone: a weaker elite never regresses a tree that a stronger run
		// already illuminated.
		tree.StructuralFitness = evolvedFitness(tree.StructuralFitness, rec.Quality)
	} else {
		// A genuine agent execution.
		tree.RunCount++
		// Exponential moving average of success (0-100)
		successScore := outcomeScore(rec.Outcome)
		tree.Fitness = 0.9*tree.Fitness + 0.1*(successScore*100)
	}

	// Record tool usage as edges (Connect handles dedup)
	for _, tool := range rec.Tools {
		toolID := "tool:" + tool
		kg.connectLocked(rec.TreeID, toolID, "uses_tool")
	}
}

// connectLocked adds an edge without locking (caller must hold kg.mu).
func (kg *KnowledgeGraph) connectLocked(from, to, relType string) {
	for _, e := range kg.Edges {
		if e.From == from && e.To == to && e.Type == relType {
			return
		}
	}
	kg.Edges = append(kg.Edges, Edge{
		From:   from,
		To:     to,
		Type:   relType,
		Weight: 1.0,
	})
}

// evolvedFitness clamps an elite's reported structural fitness into [0,100]
// and returns it only when it improves on the tree's current fitness, keeping
// the write-back monotone.
func evolvedFitness(current, elite float64) float64 {
	if elite < 0 {
		elite = 0
	} else if elite > 100 {
		elite = 100
	}
	if elite < current {
		return current
	}
	return elite
}

func outcomeScore(outcome string) float64 {
	switch outcome {
	case "success", "chain_success":
		return 1.0
	case "failure":
		return 0.3
	case "chain_failed":
		return 0.1
	case "chain_panic":
		return 0.0
	default:
		return 0.5
	}
}
