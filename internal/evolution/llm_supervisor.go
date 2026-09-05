package evolution

import (
	"math"
	"time"
)

const (
	PhaseEarlyExploration      = "Early Exploration"
	PhaseExploitation          = "Exploitation"
	PhaseRefinement            = "Refinement"
	PhaseBalancedOptimization  = "Balanced Optimization"
	PhaseBalancedExploration   = "Balanced Exploration"
	PhaseAggressiveExploration = "Aggressive Exploration"
	PhaseCrisisIntervention    = "Crisis Intervention"
)

// PhaseRange defines the mutation-rate envelope for an evolutionary phase.
type PhaseRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// FitnessMetrics captures fitness trajectory for the current population.
type FitnessMetrics struct {
	MaxFitness      float64 `json:"max_fitness"`
	AvgFitness      float64 `json:"avg_fitness"`
	ImprovementRate float64 `json:"improvement_rate"`
	StagnationCount int     `json:"stagnation_count"`
}

// DiversityMetrics captures behavioral and niche diversity signals.
type DiversityMetrics struct {
	BehavioralDiversity float64 `json:"behavioral_diversity"`
	NicheFillRate       float64 `json:"niche_fill_rate"`
}

// QualityMetrics captures the quality tier distribution in the population.
type QualityMetrics struct {
	CompleteCount  int     `json:"complete_count"`
	ExcellentCount int     `json:"excellent_count"`
	WorkingRatio   float64 `json:"working_ratio"`
}

// EvolutionParameters captures generation-level control parameters.
type EvolutionParameters struct {
	Generation       int     `json:"generation"`
	PopulationSize   int     `json:"population_size"`
	MutationBudget   int     `json:"mutation_budget"`
	RegressionRate   float64 `json:"regression_rate"`
	CurrentBestDelta float64 `json:"current_best_delta"`
}

// SpecialistDistribution summarizes MAP-Elites/specialist coverage for S_t.
type SpecialistDistribution struct {
	Domain               string         `json:"domain"`
	OccupiedNiches       int            `json:"occupied_niches"`
	TotalEstimatedNiches int            `json:"total_estimated_niches"`
	DomainCounts         map[string]int `json:"domain_counts"`
}

// PopulationState is S_t — the structured snapshot used by the supervisor.
type PopulationState struct {
	FitnessMetrics         FitnessMetrics         `json:"fitness_metrics"`
	DiversityMetrics       DiversityMetrics       `json:"diversity_metrics"`
	QualityMetrics         QualityMetrics         `json:"quality_metrics"`
	EvolutionParameters    EvolutionParameters    `json:"evolution_parameters"`
	SpecialistDistribution SpecialistDistribution `json:"specialist_distribution"`
}

// SupervisorOutput is the mutation-control guidance emitted once per generation.
type SupervisorOutput struct {
	RecommendedRate float64 `json:"recommended_rate"`
	Phase           string  `json:"phase"`
	Pattern         string  `json:"pattern"`
	Reasoning       string  `json:"reasoning"`
	RiskAssessment  string  `json:"risk_assessment"`
	Intervention    bool    `json:"intervention"`
}

// PhaseRecord stores the supervisor decision trace for auditability.
type PhaseRecord struct {
	Generation int              `json:"generation"`
	State      PopulationState  `json:"state"`
	Output     SupervisorOutput `json:"output"`
	Timestamp  time.Time        `json:"timestamp"`
}

// LLMSupervisor performs phase-aware mutation control. The current implementation
// is deterministic and local: it mirrors the LLM policy contract from the research
// paper so production callers can consume stable JSON guidance without requiring a
// network LLM during tests or offline evolution runs.
type LLMSupervisor struct {
	PhaseClassifier map[string]PhaseRange `json:"phase_classifier"`
	History         []PhaseRecord         `json:"history"`
}

// NewLLMSupervisor creates a supervisor with Tan et al. phase mutation envelopes.
func NewLLMSupervisor() *LLMSupervisor {
	return &LLMSupervisor{PhaseClassifier: DefaultPhaseRanges()}
}

// DefaultPhaseRanges returns the seven phase mutation-rate ranges from the plan.
func DefaultPhaseRanges() map[string]PhaseRange {
	return map[string]PhaseRange{
		PhaseEarlyExploration:      {Min: 0.40, Max: 0.50},
		PhaseExploitation:          {Min: 0.10, Max: 0.20},
		PhaseRefinement:            {Min: 0.15, Max: 0.25},
		PhaseBalancedOptimization:  {Min: 0.18, Max: 0.28},
		PhaseBalancedExploration:   {Min: 0.30, Max: 0.40},
		PhaseAggressiveExploration: {Min: 0.35, Max: 0.45},
		PhaseCrisisIntervention:    {Min: 0.45, Max: 0.50},
	}
}

// BuildPopulationState constructs S_t from a standard population.
func BuildPopulationState(pop *Population) PopulationState {
	return BuildPopulationStateWithGrid(pop, nil, "")
}

// BuildPopulationStateWithGrid constructs S_t and enriches it with MAP-Elites stats when available.
func BuildPopulationStateWithGrid(pop *Population, grid *MAPElitesGrid, domain string) PopulationState {
	state := PopulationState{}
	if pop == nil || len(pop.Individuals) == 0 {
		return state
	}

	totalFitness := 0.0
	complete := 0
	excellent := 0
	working := 0
	for _, ind := range pop.Individuals {
		totalFitness += ind.Fitness
		if ind.Fitness >= 0.90 {
			complete++
		}
		if ind.Fitness >= 0.75 {
			excellent++
		}
		if ind.Fitness > 0 {
			working++
		}
	}

	stagnation := 0
	improvement := pop.BestFitness - pop.PrevBestFitness
	if pop.Generation > 0 && improvement <= 0 {
		stagnation = 1
	}

	behavioralDiversity := pop.Diversity()
	nicheFillRate := pop.NicheDiversity()
	specialists := SpecialistDistribution{Domain: domain, DomainCounts: map[string]int{}}
	if grid != nil {
		stats := grid.Stats()
		behavioralDiversity = stats.DiversityScore
		nicheFillRate = stats.DiversityScore
		specialists.OccupiedNiches = stats.OccupiedCells
		specialists.TotalEstimatedNiches = stats.TotalCells
		specialists.DomainCounts = grid.SpecialistDistribution()
	}

	state.FitnessMetrics = FitnessMetrics{
		MaxFitness:      pop.BestFitness,
		AvgFitness:      totalFitness / float64(len(pop.Individuals)),
		ImprovementRate: improvement,
		StagnationCount: stagnation,
	}
	state.DiversityMetrics = DiversityMetrics{
		BehavioralDiversity: behavioralDiversity,
		NicheFillRate:       nicheFillRate,
	}
	state.QualityMetrics = QualityMetrics{
		CompleteCount:  complete,
		ExcellentCount: excellent,
		WorkingRatio:   float64(working) / float64(len(pop.Individuals)),
	}
	state.EvolutionParameters = EvolutionParameters{
		Generation:       pop.Generation,
		PopulationSize:   len(pop.Individuals),
		MutationBudget:   pop.TotalMutations,
		RegressionRate:   pop.RegressionRate(),
		CurrentBestDelta: improvement,
	}
	state.SpecialistDistribution = specialists
	return state
}

// Guide classifies the phase and returns a rate recommendation with a reasoning trace.
func (s *LLMSupervisor) Guide(state PopulationState) SupervisorOutput {
	if s == nil {
		s = NewLLMSupervisor()
	}
	if s.PhaseClassifier == nil {
		s.PhaseClassifier = DefaultPhaseRanges()
	}
	phase := s.Classify(state)
	rate := s.rateForPhase(phase, state)
	out := SupervisorOutput{
		RecommendedRate: rate,
		Phase:           phase,
		Pattern:         patternForState(state),
		Reasoning:       reasoningForPhase(phase),
		RiskAssessment:  riskForState(state),
		Intervention:    phase == PhaseCrisisIntervention || phase == PhaseAggressiveExploration,
	}
	s.History = append(s.History, PhaseRecord{Generation: state.EvolutionParameters.Generation, State: state, Output: out, Timestamp: time.Now().UTC()})
	return out
}

// Classify assigns one of the seven supervisor phases using population health signals.
func (s *LLMSupervisor) Classify(state PopulationState) string {
	diversity := state.DiversityMetrics.BehavioralDiversity
	maxFitness := state.FitnessMetrics.MaxFitness
	avgFitness := state.FitnessMetrics.AvgFitness
	improvement := state.FitnessMetrics.ImprovementRate
	stagnation := state.FitnessMetrics.StagnationCount
	complete := state.QualityMetrics.CompleteCount
	excellent := state.QualityMetrics.ExcellentCount
	working := state.QualityMetrics.WorkingRatio
	regressionRate := state.EvolutionParameters.RegressionRate

	if (diversity > 0 && diversity < 0.15 && maxFitness < 0.55) || regressionRate >= 60 || stagnation >= 8 {
		return PhaseCrisisIntervention
	}
	if diversity > 0 && (diversity < 0.25 || working < 0.35) {
		return PhaseAggressiveExploration
	}
	if maxFitness >= 0.80 && diversity < 0.45 && stagnation >= 2 {
		return PhaseBalancedOptimization
	}
	if maxFitness >= 0.50 && maxFitness < 0.80 && diversity < 0.45 {
		return PhaseBalancedExploration
	}
	if (complete > 0 || excellent >= 2 || avgFitness >= 0.65) && improvement >= 0.03 {
		return PhaseExploitation
	}
	if excellent == 0 && complete == 0 && maxFitness < 0.65 {
		return PhaseEarlyExploration
	}
	if (excellent > 0 || maxFitness >= 0.65) && improvement > 0 && improvement < 0.08 {
		return PhaseRefinement
	}
	return PhaseRefinement
}

func (s *LLMSupervisor) rateForPhase(phase string, state PopulationState) float64 {
	rng, ok := s.PhaseClassifier[phase]
	if !ok {
		rng = PhaseRange{Min: 0.20, Max: 0.30}
	}
	span := rng.Max - rng.Min
	if span <= 0 {
		return clampMutationRate(rng.Min)
	}

	diversity := state.DiversityMetrics.BehavioralDiversity
	stagnation := math.Min(float64(state.FitnessMetrics.StagnationCount), 10) / 10
	regressionPressure := math.Min(state.EvolutionParameters.RegressionRate/100, 1)
	qualityPressure := math.Min(state.FitnessMetrics.MaxFitness, 1)

	pressure := 0.5
	switch phase {
	case PhaseExploitation, PhaseRefinement:
		pressure = 0.35 - 0.20*qualityPressure + 0.15*stagnation
	case PhaseBalancedOptimization:
		pressure = 0.45 + 0.25*stagnation
	case PhaseBalancedExploration, PhaseAggressiveExploration, PhaseCrisisIntervention:
		pressure = 0.55 + 0.30*(1-diversity) + 0.15*stagnation
	case PhaseEarlyExploration:
		pressure = 0.60 + 0.20*(1-diversity)
	}
	if regressionPressure > 0.4 && phase != PhaseCrisisIntervention {
		pressure -= 0.15
	}
	return clampMutationRate(rng.Min + span*clamp01(pressure))
}

func patternForState(state PopulationState) string {
	if state.EvolutionParameters.RegressionRate >= 60 {
		return "Regression Spike"
	}
	if state.FitnessMetrics.StagnationCount >= 3 {
		return "Stagnation"
	}
	if state.FitnessMetrics.ImprovementRate > 0.03 {
		return "Healthy Progress"
	}
	if state.DiversityMetrics.BehavioralDiversity < 0.25 && state.DiversityMetrics.BehavioralDiversity > 0 {
		return "Diversity Collapse"
	}
	return "Stable Search"
}

func riskForState(state PopulationState) string {
	if state.FitnessMetrics.StagnationCount >= 8 || state.EvolutionParameters.RegressionRate >= 60 {
		return "high"
	}
	if state.DiversityMetrics.BehavioralDiversity > 0 && state.DiversityMetrics.BehavioralDiversity < 0.30 {
		return "medium"
	}
	return "low"
}

func reasoningForPhase(phase string) string {
	switch phase {
	case PhaseEarlyExploration:
		return "No high-quality individuals are established; maintain high mutation to discover viable structures."
	case PhaseExploitation:
		return "Population quality is improving; reduce mutation to preserve high-quality lineages."
	case PhaseRefinement:
		return "Quality is emerging with modest gains; use moderate mutation for local improvement."
	case PhaseBalancedOptimization:
		return "High-quality population is stagnating; slightly increase mutation while protecting elites."
	case PhaseBalancedExploration:
		return "Moderate quality with diversity pressure; increase mutation to replenish search coverage."
	case PhaseAggressiveExploration:
		return "Population health is unstable or diversity is low; force exploratory mutation."
	case PhaseCrisisIntervention:
		return "Severe diversity/quality collapse detected; trigger emergency mutation intervention."
	default:
		return "Fallback phase selected; use conservative moderate mutation."
	}
}

func clampMutationRate(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 0.50 {
		return 0.50
	}
	return v
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
