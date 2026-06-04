package evolution

// SpecialistArchetype stores the best observed serialized tree for a specialist
// family so crisis handling can resurrect it if that niche disappears.
type SpecialistArchetype struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Genotype    string            `json:"genotype"`
	Fitness     float64           `json:"fitness"`
	LastSeenGen int               `json:"last_seen_gen"`
	Tags        []string          `json:"tags"`
	Tree        *SerializableNode `json:"tree,omitempty"`
}

// SpecialistRegistry preserves high-performing specialist trees across
// generations. It prevents specialist extinction during aggressive mutation or
// crisis recovery by keeping the best validated archetype per specialist type.
type SpecialistRegistry struct {
	Archetypes map[string]SpecialistArchetype `json:"archetypes"`
}

// NewSpecialistRegistry creates an empty specialist registry.
func NewSpecialistRegistry() *SpecialistRegistry {
	return &SpecialistRegistry{Archetypes: make(map[string]SpecialistArchetype)}
}

// Observe records a specialist if it is validated and at least as fit as the
// current archetype for its type. Lower-fitness sightings still refresh
// LastSeenGen so extinction detection reflects the live population.
func (r *SpecialistRegistry) Observe(meta *EvolutionMetadata, tree *SerializableNode, generation int) {
	if r == nil || meta == nil || tree == nil || !meta.Fitness.Validated {
		return
	}
	specialistType := firstSpecialistType(meta.Tags)
	if specialistType == "" {
		return
	}
	if r.Archetypes == nil {
		r.Archetypes = make(map[string]SpecialistArchetype)
	}

	existing, exists := r.Archetypes[specialistType]
	if exists && meta.Fitness.Score < existing.Fitness {
		existing.LastSeenGen = generation
		r.Archetypes[specialistType] = existing
		return
	}

	r.Archetypes[specialistType] = SpecialistArchetype{
		ID:          meta.TreeID,
		Type:        specialistType,
		Genotype:    meta.Genotype,
		Fitness:     meta.Fitness.Score,
		LastSeenGen: generation,
		Tags:        append([]string(nil), meta.Tags...),
		Tree:        cloneTree(tree),
	}
}

// ExtinctSpecialists returns high-performing specialist archetypes missing from
// the current population for at least extinctAfter generations.
func (r *SpecialistRegistry) ExtinctSpecialists(current map[string]int, generation, extinctAfter int, minFitness float64) []SpecialistArchetype {
	if r == nil || len(r.Archetypes) == 0 {
		return nil
	}
	missing := make([]SpecialistArchetype, 0)
	for specialistType, archetype := range r.Archetypes {
		if archetype.Fitness < minFitness {
			continue
		}
		if current != nil && current[specialistType] > 0 {
			continue
		}
		if generation-archetype.LastSeenGen < extinctAfter {
			continue
		}
		missing = append(missing, archetype)
	}
	return missing
}

// Resurrect reconstructs a stored specialist as a new individual and metadata
// pair. The tree is cloned to avoid mutating the preserved archetype.
func (r *SpecialistRegistry) Resurrect(specialistType string, generation int) (Individual, *EvolutionMetadata, bool) {
	if r == nil || r.Archetypes == nil {
		return Individual{}, nil, false
	}
	archetype, ok := r.Archetypes[specialistType]
	if !ok || archetype.Tree == nil {
		return Individual{}, nil, false
	}
	tree := cloneTree(archetype.Tree)
	meta := &EvolutionMetadata{
		TreeID:    "resurrected:" + archetype.ID + ":g" + itoa(generation),
		Genotype:  archetype.Genotype,
		ParentIDs: []string{archetype.ID},
		Fitness:   FitnessRecord{Score: archetype.Fitness, Validated: true},
		Phase:     "crisis",
		Tags:      appendResurrectedTag(archetype.Tags),
		Version:   1,
	}
	return Individual{Tree: tree, Fitness: archetype.Fitness, Genome: hashTree(tree)}, meta, true
}

func firstSpecialistType(tags []string) string {
	const prefix = "specialist:"
	for _, tag := range tags {
		if len(tag) > len(prefix) && tag[:len(prefix)] == prefix {
			return tag[len(prefix):]
		}
	}
	return ""
}

func appendResurrectedTag(tags []string) []string {
	out := append([]string(nil), tags...)
	for _, tag := range out {
		if tag == "resurrected:true" {
			return out
		}
	}
	return append(out, "resurrected:true")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
