package evolution

// PhaseManager determines the evolution phase based on population state
// and adapts mutation parameters accordingly.
type PhaseManager struct {
	CurrentPhase       string
	PhaseCounter       int // consecutive generations in current phase
	HysteresisRequired int // must be in phase this many generations before switching
}

// Phase constants.
const (
	PhaseExploration = "exploration"
	PhaseStagnation  = "stagnation"
	PhaseCrisis      = "crisis"
)

// PhaseStrategy defines mutation parameters for each phase.
type PhaseStrategy struct {
	MutationRate  float64
	CrossoverRate float64
	Strategy      string
}

// NewPhaseManager creates a new PhaseManager starting in exploration phase.
func NewPhaseManager() *PhaseManager {
	return &PhaseManager{
		CurrentPhase:       PhaseExploration,
		HysteresisRequired: 2,
	}
}

// DeterminePhase evaluates the population state and returns the appropriate phase.
func (pm *PhaseManager) DeterminePhase(state *PopulationState, crisisDetected bool, gen int) string {
	var targetPhase string

	switch {
	case crisisDetected:
		targetPhase = PhaseCrisis
	case state.FitnessMetrics.StagnationCount >= 5:
		targetPhase = PhaseStagnation
	case gen >= 20 && state.FitnessMetrics.AvgFitness >= 0.3:
		targetPhase = PhaseRefinement
	default:
		targetPhase = PhaseExploration
	}

	// Hysteresis: only switch if target differs and has been consistent
	if targetPhase != pm.CurrentPhase {
		pm.PhaseCounter++
		if pm.PhaseCounter >= pm.HysteresisRequired {
			pm.CurrentPhase = targetPhase
			pm.PhaseCounter = 0
		}
	} else {
		pm.PhaseCounter = 0
	}

	return pm.CurrentPhase
}

// GetStrategy returns the mutation strategy for the current phase.
func (pm *PhaseManager) GetStrategy() PhaseStrategy {
	switch pm.CurrentPhase {
	case PhaseExploration:
		return PhaseStrategy{
			MutationRate:  0.4,
			CrossoverRate: 0.8,
			Strategy:      "high_exploration",
		}
	case PhaseRefinement:
		return PhaseStrategy{
			MutationRate:  0.2,
			CrossoverRate: 0.5,
			Strategy:      "conservative",
		}
	case PhaseStagnation:
		return PhaseStrategy{
			MutationRate:  0.6,
			CrossoverRate: 0.7,
			Strategy:      "aggressive_escape",
		}
	case PhaseCrisis:
		return PhaseStrategy{
			MutationRate:  0.8,
			CrossoverRate: 0.3,
			Strategy:      "emergency_intervention",
		}
	default:
		return PhaseStrategy{
			MutationRate:  0.3,
			CrossoverRate: 0.6,
			Strategy:      "default",
		}
	}
}

// Reset resets the phase manager to exploration.
func (pm *PhaseManager) Reset() {
	pm.CurrentPhase = PhaseExploration
	pm.PhaseCounter = 0
}
