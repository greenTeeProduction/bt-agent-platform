package evolution

import "testing"

func TestLLMSupervisor_ClassifyPhases(t *testing.T) {
	s := NewLLMSupervisor()
	tests := []struct {
		name  string
		state PopulationState
		want  string
	}{
		{
			name: "early exploration with no quality individuals",
			state: PopulationState{
				FitnessMetrics:   FitnessMetrics{MaxFitness: 0.20, AvgFitness: 0.10},
				DiversityMetrics: DiversityMetrics{BehavioralDiversity: 0.80},
				QualityMetrics:   QualityMetrics{WorkingRatio: 0.80},
			},
			want: PhaseEarlyExploration,
		},
		{
			name: "exploitation with strong improvement and quality",
			state: PopulationState{
				FitnessMetrics:   FitnessMetrics{MaxFitness: 0.88, AvgFitness: 0.72, ImprovementRate: 0.05},
				DiversityMetrics: DiversityMetrics{BehavioralDiversity: 0.70},
				QualityMetrics:   QualityMetrics{CompleteCount: 1, ExcellentCount: 3, WorkingRatio: 1.0},
			},
			want: PhaseExploitation,
		},
		{
			name: "refinement with emerging quality and modest gains",
			state: PopulationState{
				FitnessMetrics:   FitnessMetrics{MaxFitness: 0.70, AvgFitness: 0.55, ImprovementRate: 0.02},
				DiversityMetrics: DiversityMetrics{BehavioralDiversity: 0.70},
				QualityMetrics:   QualityMetrics{ExcellentCount: 1, WorkingRatio: 1.0},
			},
			want: PhaseRefinement,
		},
		{
			name: "balanced optimization for high quality stagnation",
			state: PopulationState{
				FitnessMetrics:   FitnessMetrics{MaxFitness: 0.86, AvgFitness: 0.70, StagnationCount: 3},
				DiversityMetrics: DiversityMetrics{BehavioralDiversity: 0.35},
				QualityMetrics:   QualityMetrics{ExcellentCount: 3, WorkingRatio: 1.0},
			},
			want: PhaseBalancedOptimization,
		},
		{
			name: "balanced exploration for moderate quality and diversity decline",
			state: PopulationState{
				FitnessMetrics:   FitnessMetrics{MaxFitness: 0.60, AvgFitness: 0.45},
				DiversityMetrics: DiversityMetrics{BehavioralDiversity: 0.35},
				QualityMetrics:   QualityMetrics{WorkingRatio: 0.80},
			},
			want: PhaseBalancedExploration,
		},
		{
			name: "aggressive exploration for low diversity",
			state: PopulationState{
				FitnessMetrics:   FitnessMetrics{MaxFitness: 0.70, AvgFitness: 0.45},
				DiversityMetrics: DiversityMetrics{BehavioralDiversity: 0.20},
				QualityMetrics:   QualityMetrics{WorkingRatio: 0.90},
			},
			want: PhaseAggressiveExploration,
		},
		{
			name: "crisis intervention for collapse",
			state: PopulationState{
				FitnessMetrics:      FitnessMetrics{MaxFitness: 0.40, AvgFitness: 0.20, StagnationCount: 9},
				DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.10},
				QualityMetrics:      QualityMetrics{WorkingRatio: 0.20},
				EvolutionParameters: EvolutionParameters{RegressionRate: 65},
			},
			want: PhaseCrisisIntervention,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Classify(tt.state); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLLMSupervisor_GuideRateWithinPhaseRange(t *testing.T) {
	s := NewLLMSupervisor()
	state := PopulationState{
		FitnessMetrics:   FitnessMetrics{MaxFitness: 0.90, AvgFitness: 0.70, ImprovementRate: 0.05},
		DiversityMetrics: DiversityMetrics{BehavioralDiversity: 0.75},
		QualityMetrics:   QualityMetrics{CompleteCount: 2, ExcellentCount: 4, WorkingRatio: 1.0},
	}
	out := s.Guide(state)
	rng := s.PhaseClassifier[out.Phase]
	if out.RecommendedRate < rng.Min || out.RecommendedRate > rng.Max {
		t.Fatalf("rate %.3f outside %s range [%.2f, %.2f]", out.RecommendedRate, out.Phase, rng.Min, rng.Max)
	}
	if len(s.History) != 1 {
		t.Fatalf("history length = %d, want 1", len(s.History))
	}
	if out.Pattern != "Healthy Progress" {
		t.Fatalf("pattern = %q, want Healthy Progress", out.Pattern)
	}
}

func TestBuildPopulationStateWithGrid(t *testing.T) {
	base := DefaultTree()
	pop := NewPopulation(4, base)
	for i := range pop.Individuals {
		pop.Individuals[i].Fitness = 0.25 * float64(i+1)
	}
	pop.BestFitness = 1.0
	pop.PrevBestFitness = 0.8
	pop.Generation = 3
	grid := NewMAPElitesGrid(4)
	grid.InsertFromPopulation(pop, "go-dev")

	state := BuildPopulationStateWithGrid(pop, grid, "go-dev")
	if state.FitnessMetrics.MaxFitness != 1.0 {
		t.Fatalf("max fitness = %.2f, want 1.0", state.FitnessMetrics.MaxFitness)
	}
	if state.FitnessMetrics.ImprovementRate <= 0 {
		t.Fatalf("improvement rate = %.2f, want positive", state.FitnessMetrics.ImprovementRate)
	}
	if state.SpecialistDistribution.OccupiedNiches == 0 {
		t.Fatal("expected occupied MAP-Elites niches")
	}
	if state.SpecialistDistribution.DomainCounts["go-dev"] == 0 {
		t.Fatalf("expected go-dev specialist count, got %#v", state.SpecialistDistribution.DomainCounts)
	}
}
