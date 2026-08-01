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

// MCTSAffinity reports, on [0,1], how much a speculative MCTS structural
// search is worth for tree from the registry's point of view — 1.0 when the
// registry knows nothing about this shape, 0.0 when the shape IS a preserved
// specialist archetype.
//
// A registered archetype is a validated, high-fitness tree the registry exists
// to keep alive across generations; gambling extra structural mutations on the
// very shape crisis recovery would resurrect works against that purpose, so
// such a tree scores no affinity. A nil or empty registry has no archetype
// knowledge at all and therefore cannot argue against the search.
// See [SelectStructuralStrategy], which combines this with
// [SelectorOptimizer.MCTSAffinity].
func (r *SpecialistRegistry) MCTSAffinity(tree *SerializableNode) float64 {
	if r == nil || tree == nil || len(r.Archetypes) == 0 {
		return 1.0
	}
	shape := treeShapeSignature(tree)
	if shape == "" {
		return 1.0
	}
	for _, archetype := range r.Archetypes {
		if archetype.Tree == nil {
			continue
		}
		if treeShapeSignature(archetype.Tree) == shape {
			return 0.0
		}
	}
	return 1.0
}

// treeShapeSignature renders a tree's structural skeleton — node types and
// nesting only, no names — as a comparable string. Matching on shape rather
// than on an exact tree hash is what makes "this is a known archetype" survive
// the node renaming that ordinary evolution performs.
func treeShapeSignature(node *SerializableNode) string {
	if node == nil {
		return ""
	}
	sig := node.Type
	if len(node.Children) == 0 {
		return sig
	}
	sig += "("
	for i := range node.Children {
		if i > 0 {
			sig += ","
		}
		sig += treeShapeSignature(&node.Children[i])
	}
	return sig + ")"
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
