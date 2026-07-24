package domains

import "github.com/nico/go-bt-evolve/internal/knowledge"

// NotebookLMRunSummary carries the per-run data needed for fitness computation.
type NotebookLMRunSummary struct {
	Outcome string
	Quality float64
}

// NotebookLMFitness computes a 0.0-1.0 fitness score for the NotebookLM agent
// based on real run data. Used by the gardener to decide whether to evolve.
func NotebookLMFitness(runs []NotebookLMRunSummary) float64 {
	if len(runs) == 0 {
		return 0
	}

	var (
		totalRuns        = float64(len(runs))
		successRuns      float64
		totalQuality     float64
		qualityRuns      float64
		fabricationFails float64
	)

	for _, r := range runs {
		if r.Outcome == "success" || r.Outcome == "chain_success" {
			successRuns++
		}
		if r.Outcome == "failure" && r.Quality <= 0.2 {
			fabricationFails++
		}
		if r.Quality > 0 {
			totalQuality += r.Quality
			qualityRuns++
		}
	}

	// Success rate (0-0.4)
	successScore := (successRuns / totalRuns) * 0.4

	// Quality score (0-0.3)
	qualityScore := 0.0
	if qualityRuns > 0 {
		qualityScore = (totalQuality / qualityRuns) * 0.3
	}

	// Anti-fabrication penalty (0-0.3)
	antiFabScore := 0.0
	if totalRuns > 0 {
		fabRate := fabricationFails / totalRuns
		antiFabScore = (1.0 - fabRate) * 0.3
	}

	return successScore + qualityScore + antiFabScore
}

// RegisterNotebookLMFitness wires NotebookLMFitness into kg's per-tree fitness
// update (see knowledge.KnowledgeGraph.RegisterDomainFitness) for the
// "notebooklm" tree and both the "notebooklm_consumer" (underscore) and
// "notebooklm-consumer" (hyphen) tree IDs, so their recorded run history
// drives Fitness through this domain-aware, anti-fabrication-penalizing score
// instead of the generic runtime-success EMA. The hyphenated ID is the real
// production chain-agent tree ID (see tree_resolver.go, knowledge/registry.go,
// agent/pipeline_map.go); the underscore ID also exists as a separate
// domain-category node and is kept wired for backward compatibility.
func RegisterNotebookLMFitness(kg *knowledge.KnowledgeGraph) {
	fn := func(runs []knowledge.RunSummary) float64 {
		converted := make([]NotebookLMRunSummary, len(runs))
		for i, r := range runs {
			converted[i] = NotebookLMRunSummary{Outcome: r.Outcome, Quality: r.Quality}
		}
		return NotebookLMFitness(converted)
	}
	kg.RegisterDomainFitness("notebooklm", fn)
	kg.RegisterDomainFitness("notebooklm_consumer", fn)
	kg.RegisterDomainFitness("notebooklm-consumer", fn)
}
