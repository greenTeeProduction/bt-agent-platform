// Package evolution — CrisisDetector monitors population/tree health and triggers
// emergency diversity injection before fitness degrades. Based on Tan et al.
// "Hybrid LLM-GP" (MDPI Robotics 2026): proactively detect diversity collapse
// and stagnation to prevent death spirals, complementing the reactive QualityGate.
package evolution

import "sync"

// CrisisDetector monitors behavioral diversity and fitness stagnation across
// evolution cycles. When diversity drops below a threshold or stagnation
// exceeds a limit, it signals an emergency intervention — forcing the
// mutation rate to an emergency level and triggering diversity injection.
//
// This is the PROACTIVE counterpart to QualityGate (which is REACTIVE —
// rollback after regression). Crisis detection catches diversity collapse
// before regression happens.
//
// Plan #4 extensions: population-level crisis detection, regression spiral
// tracking, quality crash detection, and emergency action recommendations.
type CrisisDetector struct {
	DiversityThreshold float64 // δ_d, default 0.2
	StagnationLimit    int     // δ_s, default 5
	EmergencyRate      float64 // μ_emergency, default 0.50

	// Per-tree state
	mu            sync.Mutex
	stagnation    map[string]int     // treeName → consecutive epochs w/o improvement
	lastBestFit   map[string]float64 // treeName → last best composite fitness
	lastDiversity float64            // most recent diversity score

	// Plan #4: population-level state
	regressionStreak int // consecutive generations with high regression rate
	qualityCrash     int // consecutive generations with low working ratio
}

// NewCrisisDetector creates a crisis detector with sensible defaults.
func NewCrisisDetector() *CrisisDetector {
	return &CrisisDetector{
		DiversityThreshold: 0.2,
		StagnationLimit:    5,
		EmergencyRate:      0.50,
		stagnation:         make(map[string]int),
		lastBestFit:        make(map[string]float64),
	}
}

// CrisisState describes the current health of a tree's evolution cycle.
type CrisisState struct {
	TreeName            string
	CurrentFitness      float64
	LastBestFitness     float64
	StagnationEpochs    int
	BehavioralDiversity float64
	DiversityThreshold  float64
}

// Detect checks whether a crisis is occurring for a given tree.
// Returns true and a reason string if crisis is detected.
func (c *CrisisDetector) Detect(state CrisisState) (crisis bool, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	treeName := state.TreeName

	// Check diversity collapse (MAP-Elites behavioral diversity)
	c.lastDiversity = state.BehavioralDiversity
	if state.BehavioralDiversity < c.DiversityThreshold && state.BehavioralDiversity > 0 {
		// Only fire if we have meaningful diversity data (non-zero)
		return true, "diversity_collapse"
	}

	// Check stagnation: fitness has not improved over N consecutive cycles
	lastFit, exists := c.lastBestFit[treeName]
	if !exists {
		c.lastBestFit[treeName] = state.CurrentFitness
		c.stagnation[treeName] = 0
		return false, ""
	}

	if state.CurrentFitness <= lastFit {
		c.stagnation[treeName]++
	} else {
		// Improvement — reset stagnation counter
		c.stagnation[treeName] = 0
		c.lastBestFit[treeName] = state.CurrentFitness
	}

	if c.stagnation[treeName] > c.StagnationLimit {
		return true, "stagnation"
	}

	// Update last best if improved
	if state.CurrentFitness > lastFit {
		c.lastBestFit[treeName] = state.CurrentFitness
	}

	return false, ""
}

// InterveneAction describes the crisis intervention to apply.
type InterveneAction struct {
	EmergencyMode    bool
	EmergencyRate    float64
	StagnationEpochs int
	CrisisReason     string
}

// Intervene returns the action to take for a detected crisis.
// Caller should force mutation rate to the emergency level and inject
// a diverse individual into the population.
func (c *CrisisDetector) Intervene(treeName string, reason string) InterveneAction {
	c.mu.Lock()
	defer c.mu.Unlock()

	stag := c.stagnation[treeName]

	return InterveneAction{
		EmergencyMode:    true,
		EmergencyRate:    c.EmergencyRate,
		StagnationEpochs: stag,
		CrisisReason:     reason,
	}
}

// ResetStagnation clears the stagnation counter for a tree (e.g., after successful intervention).
func (c *CrisisDetector) ResetStagnation(treeName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stagnation[treeName] = 0
}

// StagnationCount returns the current stagnation count for a tree.
func (c *CrisisDetector) StagnationCount(treeName string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stagnation[treeName]
}

// LastDiversity returns the most recent diversity score.
func (c *CrisisDetector) LastDiversity() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastDiversity
}

// ——— Plan #4 extensions ———

// DetectPopulation checks for population-level crisis conditions.
// This is separate from per-tree Detect; it evaluates the entire population
// for regression spirals, quality crashes, and overall diversity collapse.
func (c *CrisisDetector) DetectPopulation(state *PopulationState) (crisis bool, reasons []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Diversity collapse
	if state.DiversityMetrics.BehavioralDiversity < c.DiversityThreshold &&
		state.DiversityMetrics.BehavioralDiversity > 0 {
		reasons = append(reasons, "diversity_collapse")
	}

	// Regression spiral: >50% regression rate for 3+ consecutive generations
	if state.EvolutionParameters.RegressionRate > 0.5 {
		c.regressionStreak++
	} else {
		c.regressionStreak = 0
	}
	if c.regressionStreak >= 3 {
		reasons = append(reasons, "regression_spiral")
	}

	// Quality crash: <30% working ratio
	if state.QualityMetrics.WorkingRatio < 0.3 {
		c.qualityCrash++
	} else {
		c.qualityCrash = 0
	}
	if c.qualityCrash >= 2 {
		reasons = append(reasons, "quality_crash")
	}

	return len(reasons) > 0, reasons
}

// EmergencyActions returns a list of recommended crisis actions.
func (c *CrisisDetector) EmergencyActions() []string {
	return []string{
		"inject_diversity_candidates",
		"resurrect_specialists",
		"elevate_mutation_rate",
		"freeze_elites",
	}
}

// GetEmergencyMutationRate returns the mutation rate to use during crisis.
func (c *CrisisDetector) GetEmergencyMutationRate() float64 {
	return c.EmergencyRate
}

// ResetPopulation resets population-level counters.
func (c *CrisisDetector) ResetPopulation() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.regressionStreak = 0
	c.qualityCrash = 0
}
