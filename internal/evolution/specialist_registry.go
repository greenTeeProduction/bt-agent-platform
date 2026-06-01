package evolution

import "sync"

// SpecialistArchetype stores a canonical specialist tree for resurrection.
type SpecialistArchetype struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Genotype    string   `json:"genotype"`
	Fitness     float64  `json:"fitness"`
	LastSeenGen int      `json:"last_seen_gen"`
	Tags        []string `json:"tags,omitempty"`
	TreeJSON    string   `json:"tree_json,omitempty"` // Serialized tree for fast resurrection
}

// SpecialistRegistry manages specialist archetypes for resurrection during crisis.
type SpecialistRegistry struct {
	mu         sync.RWMutex
	archetypes map[string]*SpecialistArchetype
}

// NewSpecialistRegistry creates a new SpecialistRegistry.
func NewSpecialistRegistry() *SpecialistRegistry {
	return &SpecialistRegistry{
		archetypes: make(map[string]*SpecialistArchetype),
	}
}

// Register stores a specialist archetype.
func (sr *SpecialistRegistry) Register(id string, archetype *SpecialistArchetype) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.archetypes[id] = archetype
}

// Get retrieves a specialist archetype by ID.
func (sr *SpecialistRegistry) Get(id string) *SpecialistArchetype {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.archetypes[id]
}

// GetAll returns all registered specialist archetypes.
func (sr *SpecialistRegistry) GetAll() []*SpecialistArchetype {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	result := make([]*SpecialistArchetype, 0, len(sr.archetypes))
	for _, a := range sr.archetypes {
		result = append(result, a)
	}
	return result
}

// FindExtinct returns specialists that haven't been seen for extinctThreshold generations
// and had fitness above the minimum threshold.
func (sr *SpecialistRegistry) FindExtinct(currentGen, extinctThreshold int, minFitness float64) []*SpecialistArchetype {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var extinct []*SpecialistArchetype
	for _, a := range sr.archetypes {
		genSinceSeen := currentGen - a.LastSeenGen
		if genSinceSeen >= extinctThreshold && a.Fitness >= minFitness {
			extinct = append(extinct, a)
		}
	}
	return extinct
}

// UpdateSeen marks a specialist as seen in the current generation.
func (sr *SpecialistRegistry) UpdateSeen(id string, gen int) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if a, ok := sr.archetypes[id]; ok {
		a.LastSeenGen = gen
	}
}

// Remove removes a specialist archetype.
func (sr *SpecialistRegistry) Remove(id string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	delete(sr.archetypes, id)
}

// Count returns the number of registered archetypes.
func (sr *SpecialistRegistry) Count() int {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return len(sr.archetypes)
}

// Resurrect returns the tree JSON for a specialist archetype.
// Returns empty string if not found or if TreeJSON is empty.
func (sr *SpecialistRegistry) Resurrect(id string) string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	if a, ok := sr.archetypes[id]; ok {
		return a.TreeJSON
	}
	return ""
}
