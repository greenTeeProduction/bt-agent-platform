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
	stagnation    map[string]int     // treeName → accumulated strict-decline epochs
	lastBestFit   map[string]float64 // treeName → last OBSERVED composite fitness
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

// Crisis reason strings returned by Detect/DetectPopulation. They are exported
// because consumers branch on WHICH crisis fired, not merely that one did — the
// gardener answers a diversity collapse by reseeding from its MAP-Elites
// archive, which would be the wrong response to stagnation.
const (
	// CrisisDiversityCollapse means behavioral diversity fell below
	// DiversityThreshold while still being non-zero (i.e. real data, not a
	// cold start).
	CrisisDiversityCollapse = "diversity_collapse"
	// CrisisStagnation means fitness declined for more than StagnationLimit
	// consecutive observations.
	CrisisStagnation = "stagnation"
	// CrisisRegressionSpiral and CrisisQualityCrash are population-level
	// reasons reported by DetectPopulation.
	CrisisRegressionSpiral = "regression_spiral"
	CrisisQualityCrash     = "quality_crash"
)

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
		return true, CrisisDiversityCollapse
	}

	// Stagnation = fitness ACTIVELY DECLINING for more than StagnationLimit
	// observations. Flat fitness is a plateau, not a crisis: a converged
	// production tree reports the identical composite every cycle, and the
	// old `<=` comparison latched every plateaued tree in permanent crisis
	// (50 trees × every cycle ≈ 4,185 intervention lines/day, 2026-07-23
	// review gap 6). Decline accumulates evidence, any upward move resets it
	// (recovery is measured against the previous observation, not the
	// all-time best — chasing the historic best would count a genuine
	// rebound as decline), and flat leaves the counter unchanged.
	lastFit, exists := c.lastBestFit[treeName]
	if !exists {
		c.lastBestFit[treeName] = state.CurrentFitness
		c.stagnation[treeName] = 0
		return false, ""
	}

	switch {
	case state.CurrentFitness > lastFit:
		c.stagnation[treeName] = 0
	case state.CurrentFitness < lastFit:
		c.stagnation[treeName]++
	}
	c.lastBestFit[treeName] = state.CurrentFitness

	if c.stagnation[treeName] > c.StagnationLimit {
		return true, CrisisStagnation
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
//
// Intervening CONSUMES the accumulated stagnation evidence: the intervention
// happened, so re-firing on every subsequent cycle would be noise, not
// signal — a crisis is a transition, not a state. A new intervention
// requires a fresh StagnationLimit run of declines.
func (c *CrisisDetector) Intervene(treeName string, reason string) InterveneAction {
	c.mu.Lock()
	defer c.mu.Unlock()

	stag := c.stagnation[treeName]
	if reason == "stagnation" {
		c.stagnation[treeName] = 0
	}

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
		reasons = append(reasons, CrisisDiversityCollapse)
	}

	// Regression spiral: >50% regression rate for 3+ consecutive generations
	if state.EvolutionParameters.RegressionRate > 0.5 {
		c.regressionStreak++
	} else {
		c.regressionStreak = 0
	}
	if c.regressionStreak >= 3 {
		reasons = append(reasons, CrisisRegressionSpiral)
	}

	// Quality crash: <30% working ratio
	if state.QualityMetrics.WorkingRatio < 0.3 {
		c.qualityCrash++
	} else {
		c.qualityCrash = 0
	}
	if c.qualityCrash >= 2 {
		reasons = append(reasons, CrisisQualityCrash)
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
