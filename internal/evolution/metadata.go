package evolution

import "time"

// EvolutionMetadata stores lineage, fitness, and mutation history for a tree.
type EvolutionMetadata struct {
	TreeID    string        `json:"tree_id"`
	Genotype  string        `json:"genotype"`
	Phenotype string        `json:"phenotype,omitempty"`
	ParentIDs []string      `json:"parent_ids,omitempty"`
	Mutations []MutationLog `json:"mutations,omitempty"`
	Fitness   FitnessRecord `json:"fitness"`
	Phase     string        `json:"phase"`
	Tags      []string      `json:"tags,omitempty"`
	Rationale string        `json:"rationale,omitempty"`
	Errors    []string      `json:"errors,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	Version   int           `json:"version"`
}

// MutationLog records a single mutation operation.
type MutationLog struct {
	Op        string    `json:"op"`
	ParentID  string    `json:"parent_id"`
	NodePath  string    `json:"node_path,omitempty"`
	OldValue  string    `json:"old_value,omitempty"`
	NewValue  string    `json:"new_value,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// FitnessRecord stores fitness evaluation results.
type FitnessRecord struct {
	Score        float64       `json:"score"`
	Regressions  int           `json:"regressions"`
	Validated    bool          `json:"validated"`
	EvalDuration time.Duration `json:"eval_duration"`
	EvalDate     time.Time     `json:"eval_date"`
}

// AddMutation records a mutation in the lineage.
func (m *EvolutionMetadata) AddMutation(op MutationLog) {
	m.Mutations = append(m.Mutations, op)
	m.Version++
}

// RecordFitness updates the fitness record.
func (m *EvolutionMetadata) RecordFitness(score float64, validated bool) {
	m.Fitness.Score = score
	m.Fitness.Validated = validated
	m.Fitness.EvalDate = time.Now()
	if score < m.Fitness.Score {
		m.Fitness.Regressions++
	}
}

// SetPhase updates the evolution phase.
func (m *EvolutionMetadata) SetPhase(phase string) {
	m.Phase = phase
}

// AddError records an error encountered during evolution.
func (m *EvolutionMetadata) AddError(err string) {
	m.Errors = append(m.Errors, err)
}

// PruneMutationHistory removes old mutation entries beyond the given window.
func (m *EvolutionMetadata) PruneMutationHistory(maxEntries int) {
	if len(m.Mutations) > maxEntries {
		m.Mutations = m.Mutations[len(m.Mutations)-maxEntries:]
	}
}

// IsSpecialist returns true if the tree is tagged as a specialist type.
func (m *EvolutionMetadata) IsSpecialist() bool {
	for _, tag := range m.Tags {
		if tag == "specialist:goap" || tag == "specialist:security" ||
			tag == "specialist:code_reviewer" || tag == "specialist:planner" {
			return true
		}
	}
	return false
}

// IsResurrected returns true if the tree was resurrected from the specialist registry.
func (m *EvolutionMetadata) IsResurrected() bool {
	for _, tag := range m.Tags {
		if tag == "resurrected:true" {
			return true
		}
	}
	return false
}
