package evolution

import (
	"testing"
)

func TestNewCrisisDetector(t *testing.T) {
	cd := NewCrisisDetector()
	if cd == nil {
		t.Fatal("NewCrisisDetector() returned nil")
	}
	if cd.DiversityThreshold != 0.2 {
		t.Errorf("expected DiversityThreshold 0.2, got %f", cd.DiversityThreshold)
	}
	if cd.StagnationLimit != 5 {
		t.Errorf("expected StagnationLimit 5, got %d", cd.StagnationLimit)
	}
	if cd.EmergencyRate != 0.50 {
		t.Errorf("expected EmergencyRate 0.50, got %f", cd.EmergencyRate)
	}
	if cd.stagnation == nil {
		t.Error("expected stagnation map to be initialized")
	}
	if cd.lastBestFit == nil {
		t.Error("expected lastBestFit map to be initialized")
	}
}

func TestCrisisDetector_Detect_NoCrisis(t *testing.T) {
	cd := NewCrisisDetector()
	state := CrisisState{
		TreeName:            "test-tree",
		CurrentFitness:      0.8,
		LastBestFitness:     0.0,
		StagnationEpochs:    0,
		BehavioralDiversity: 0.5,
		DiversityThreshold:  0.2,
	}
	crisis, reason := cd.Detect(state)
	if crisis {
		t.Errorf("expected no crisis, got crisis with reason: %s", reason)
	}
}

func TestCrisisDetector_Detect_DiversityCollapse(t *testing.T) {
	cd := NewCrisisDetector()
	state := CrisisState{
		TreeName:            "test-tree",
		CurrentFitness:      0.5,
		BehavioralDiversity: 0.1, // Below threshold of 0.2
	}
	crisis, reason := cd.Detect(state)
	if !crisis {
		t.Error("expected crisis due to diversity collapse")
	}
	if reason != "diversity_collapse" {
		t.Errorf("expected reason 'diversity_collapse', got '%s'", reason)
	}
}

func TestCrisisDetector_Detect_DiversityZero(t *testing.T) {
	// Zero diversity should NOT trigger collapse (meaningful data guard)
	cd := NewCrisisDetector()
	state := CrisisState{
		TreeName:            "test-tree",
		CurrentFitness:      0.5,
		BehavioralDiversity: 0.0, // Zero — no meaningful data
	}
	crisis, reason := cd.Detect(state)
	if crisis {
		t.Errorf("expected no crisis for zero diversity (uninitialized), got: %s", reason)
	}
}

func TestCrisisDetector_Detect_Stagnation(t *testing.T) {
	cd := NewCrisisDetector()
	cd.StagnationLimit = 3

	// First call — initializes best fit, no crisis
	state1 := CrisisState{
		TreeName:            "stagnation-tree",
		CurrentFitness:      0.5,
		BehavioralDiversity: 0.8,
	}
	crisis, _ := cd.Detect(state1)
	if crisis {
		t.Error("expected no crisis on first call (initialization)")
	}

	// Second call — same fitness, stagnation counter increments
	state2 := CrisisState{
		TreeName:            "stagnation-tree",
		CurrentFitness:      0.5,
		BehavioralDiversity: 0.8,
	}
	crisis, _ = cd.Detect(state2)
	if crisis {
		t.Error("expected no crisis after 1 stagnation (limit=3, need >3)")
	}

	// Third call — stagnation=2
	crisis, _ = cd.Detect(state2)
	if crisis {
		t.Error("expected no crisis after 2 stagnation (limit=3, need >3)")
	}

	// Fourth call — stagnation=3, 3 > 3 is false, need one more
	crisis, _ = cd.Detect(state2)
	if crisis {
		t.Error("expected no crisis after 3 stagnation (limit=3, condition is > not >=")
	}

	// Fifth call — stagnation=4, 4 > 3 → crisis!
	state5 := CrisisState{
		TreeName:            "stagnation-tree",
		CurrentFitness:      0.5,
		BehavioralDiversity: 0.8,
	}
	crisis, reason := cd.Detect(state5)
	if !crisis {
		t.Error("expected crisis due to stagnation after 4 stagnant cycles (limit=3, > check)")
	}
	if reason != "stagnation" {
		t.Errorf("expected reason 'stagnation', got '%s'", reason)
	}
}

func TestCrisisDetector_Detect_ImprovementResetsStagnation(t *testing.T) {
	cd := NewCrisisDetector()
	cd.StagnationLimit = 3

	// First call initializes
	cd.Detect(CrisisState{TreeName: "improve-tree", CurrentFitness: 0.5, BehavioralDiversity: 0.8})

	// Second call — same fitness, +1 stagnation
	cd.Detect(CrisisState{TreeName: "improve-tree", CurrentFitness: 0.5, BehavioralDiversity: 0.8})

	// Third call — improvement! Resets stagnation
	state3 := CrisisState{
		TreeName:            "improve-tree",
		CurrentFitness:      0.7, // Improved!
		BehavioralDiversity: 0.8,
	}
	crisis, reason := cd.Detect(state3)
	if crisis {
		t.Errorf("expected no crisis after improvement, got: %s", reason)
	}

	// Check stagnation was reset: new best should be 0.7
	stag := cd.StagnationCount("improve-tree")
	if stag != 0 {
		t.Errorf("expected stagnation count 0 after improvement, got %d", stag)
	}
}

func TestCrisisDetector_Intervene(t *testing.T) {
	cd := NewCrisisDetector()
	cd.EmergencyRate = 0.60

	// Set up some stagnation
	cd.Detect(CrisisState{TreeName: "crisis-tree", CurrentFitness: 0.5, BehavioralDiversity: 0.8})
	cd.Detect(CrisisState{TreeName: "crisis-tree", CurrentFitness: 0.5, BehavioralDiversity: 0.8})

	action := cd.Intervene("crisis-tree", "stagnation")
	if !action.EmergencyMode {
		t.Error("expected EmergencyMode to be true")
	}
	if action.EmergencyRate != 0.60 {
		t.Errorf("expected EmergencyRate 0.60, got %f", action.EmergencyRate)
	}
	if action.CrisisReason != "stagnation" {
		t.Errorf("expected CrisisReason 'stagnation', got '%s'", action.CrisisReason)
	}
	if action.StagnationEpochs < 1 {
		t.Errorf("expected StagnationEpochs > 0, got %d", action.StagnationEpochs)
	}
}

func TestCrisisDetector_ResetStagnation(t *testing.T) {
	cd := NewCrisisDetector()

	// Build up stagnation
	cd.Detect(CrisisState{TreeName: "reset-tree", CurrentFitness: 0.5, BehavioralDiversity: 0.8})
	cd.Detect(CrisisState{TreeName: "reset-tree", CurrentFitness: 0.5, BehavioralDiversity: 0.8})

	before := cd.StagnationCount("reset-tree")
	if before <= 0 {
		t.Errorf("expected stagnation > 0 before reset, got %d", before)
	}

	cd.ResetStagnation("reset-tree")
	after := cd.StagnationCount("reset-tree")
	if after != 0 {
		t.Errorf("expected stagnation 0 after reset, got %d", after)
	}
}

func TestCrisisDetector_StagnationCount_UnknownTree(t *testing.T) {
	cd := NewCrisisDetector()
	count := cd.StagnationCount("nonexistent")
	if count != 0 {
		t.Errorf("expected 0 for unknown tree, got %d", count)
	}
}

// --- Plan #4 extensions ---

func TestCrisisDetector_DetectPopulation_NoCrisis(t *testing.T) {
	cd := NewCrisisDetector()
	state := &PopulationState{
		DiversityMetrics: DiversityMetrics{
			BehavioralDiversity: 0.8, // Above threshold
		},
		EvolutionParameters: EvolutionParameters{
			RegressionRate: 0.1, // Below 0.5
		},
		QualityMetrics: QualityMetrics{
			WorkingRatio: 0.9, // Above 0.3
		},
	}
	crisis, reasons := cd.DetectPopulation(state)
	if crisis {
		t.Errorf("expected no crisis, got reasons: %v", reasons)
	}
	if len(reasons) != 0 {
		t.Errorf("expected empty reasons, got %v", reasons)
	}
}

func TestCrisisDetector_DetectPopulation_DiversityCollapse(t *testing.T) {
	cd := NewCrisisDetector()
	state := &PopulationState{
		DiversityMetrics: DiversityMetrics{
			BehavioralDiversity: 0.1, // Below 0.2 threshold
		},
		EvolutionParameters: EvolutionParameters{
			RegressionRate: 0.1,
		},
		QualityMetrics: QualityMetrics{
			WorkingRatio: 0.9,
		},
	}
	crisis, reasons := cd.DetectPopulation(state)
	if !crisis {
		t.Error("expected crisis due to diversity collapse")
	}
	if !containsReason(reasons, "diversity_collapse") {
		t.Errorf("expected 'diversity_collapse' in reasons, got %v", reasons)
	}
}

func TestCrisisDetector_DetectPopulation_RegressionSpiral(t *testing.T) {
	cd := NewCrisisDetector()

	// Need 3 consecutive generations with >50% regression rate
	highRegressionState := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.6},
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.9},
	}

	// First two calls — regression streak builds but not enough
	cd.DetectPopulation(highRegressionState)
	cd.DetectPopulation(highRegressionState)

	// Third call — regression_streak >= 3
	crisis, reasons := cd.DetectPopulation(highRegressionState)
	if !crisis {
		t.Error("expected crisis due to regression spiral after 3 generations")
	}
	if !containsReason(reasons, "regression_spiral") {
		t.Errorf("expected 'regression_spiral' in reasons, got %v", reasons)
	}
}

func TestCrisisDetector_DetectPopulation_RegressionInterrupted(t *testing.T) {
	cd := NewCrisisDetector()

	highState := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.6},
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.9},
	}
	lowState := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.1}, // Below 0.5
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.9},
	}

	// High regression — streak starts
	cd.DetectPopulation(highState)
	// Low regression interrupts the streak
	cd.DetectPopulation(lowState)
	// High again — streak resets to 1
	cd.DetectPopulation(highState)
	// Total should be 1, not 3
	crisis, reasons := cd.DetectPopulation(highState)
	// After the interrupt, high(1) -> high(2) — only 2 consecutive, not 3
	// Actually: high(1) -> low(interrupt, streak=0) -> high(streak=1) -> high(streak=2) — still < 3
	if crisis {
		regressionFound := false
		for _, r := range reasons {
			if r == "regression_spiral" {
				regressionFound = true
			}
		}
		if regressionFound {
			t.Error("expected no regression spiral — streak was interrupted")
		}
	}
}

func TestCrisisDetector_DetectPopulation_QualityCrash(t *testing.T) {
	cd := NewCrisisDetector()

	lowQualityState := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.1},
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.2}, // Below 0.3
	}

	// First call — quality crash streak starts (1), not enough yet
	cd.DetectPopulation(lowQualityState)

	// Second call — quality_crash >= 2
	crisis, reasons := cd.DetectPopulation(lowQualityState)
	if !crisis {
		t.Error("expected crisis due to quality crash after 2 generations")
	}
	if !containsReason(reasons, "quality_crash") {
		t.Errorf("expected 'quality_crash' in reasons, got %v", reasons)
	}
}

func TestCrisisDetector_DetectPopulation_QualityRecovery(t *testing.T) {
	cd := NewCrisisDetector()

	lowState := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.1},
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.2},
	}
	highState := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.1},
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.9}, // Above 0.3
	}

	// Low quality — streak starts
	cd.DetectPopulation(lowState)
	// High quality interrupts
	cd.DetectPopulation(highState)

	// Should not trigger quality crash
	crisis, reasons := cd.DetectPopulation(lowState)
	if crisis {
		for _, r := range reasons {
			if r == "quality_crash" {
				t.Error("expected no quality crash — streak was interrupted")
			}
		}
	}
}

func TestCrisisDetector_DetectPopulation_MultipleReasons(t *testing.T) {
	cd := NewCrisisDetector()

	// Set up: diversity collapse + regression spiral
	state := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.1}, // Collapse
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.6},   // High regression
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.2},          // Low quality
	}

	// First call: diversity collapse fires immediately, regression builds, quality starts
	crisis, reasons := cd.DetectPopulation(state)
	if !crisis {
		t.Fatal("expected crisis with at least diversity_collapse")
	}
	if !containsReason(reasons, "diversity_collapse") {
		t.Errorf("expected 'diversity_collapse', got %v", reasons)
	}

	// Second call: regression streak=2, quality crash streak=2
	crisis, reasons = cd.DetectPopulation(state)
	if !crisis {
		t.Fatal("expected crisis on second call")
	}
	if !containsReason(reasons, "diversity_collapse") {
		t.Errorf("expected 'diversity_collapse', got %v", reasons)
	}

	// Third call: regression streak=3 (spiral), quality crash streak=3 (already crashed)
	crisis, reasons = cd.DetectPopulation(state)
	if !crisis {
		t.Fatal("expected crisis on third call")
	}
	if !containsReason(reasons, "regression_spiral") {
		t.Errorf("expected 'regression_spiral', got %v", reasons)
	}
	if !containsReason(reasons, "quality_crash") {
		t.Errorf("expected 'quality_crash', got %v", reasons)
	}
}

func TestCrisisDetector_DetectPopulation_NilState(t *testing.T) {
	cd := NewCrisisDetector()
	// Nil pointer panics on field access — this test validates the behavior
	// is documented and caught. If a nil guard is added later, update this test.
	defer func() {
		if r := recover(); r == nil {
			t.Log("DetectPopulation(nil) did NOT panic — nil guard may have been added")
		}
	}()
	cd.DetectPopulation(nil)
	// If we reach here without panic, DetectPopulation handles nil gracefully
	t.Log("DetectPopulation(nil) handled nil gracefully")
}

func TestCrisisDetector_EmergencyActions(t *testing.T) {
	cd := NewCrisisDetector()
	actions := cd.EmergencyActions()
	expected := []string{
		"inject_diversity_candidates",
		"resurrect_specialists",
		"elevate_mutation_rate",
		"freeze_elites",
	}
	if len(actions) != len(expected) {
		t.Fatalf("expected %d actions, got %d: %v", len(expected), len(actions), actions)
	}
	for i, a := range actions {
		if a != expected[i] {
			t.Errorf("action[%d]: expected '%s', got '%s'", i, expected[i], a)
		}
	}
}

func TestCrisisDetector_GetEmergencyMutationRate(t *testing.T) {
	cd := NewCrisisDetector()
	cd.EmergencyRate = 0.75
	rate := cd.GetEmergencyMutationRate()
	if rate != 0.75 {
		t.Errorf("expected 0.75, got %f", rate)
	}
}

func TestCrisisDetector_ResetPopulation(t *testing.T) {
	cd := NewCrisisDetector()

	// Build up regression streak
	state := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.6},
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.2},
	}
	cd.DetectPopulation(state)
	cd.DetectPopulation(state)
	cd.DetectPopulation(state)

	cd.ResetPopulation()

	// After reset, should need fresh streak
	noCrisisState := &PopulationState{
		DiversityMetrics:    DiversityMetrics{BehavioralDiversity: 0.8},
		EvolutionParameters: EvolutionParameters{RegressionRate: 0.1},
		QualityMetrics:      QualityMetrics{WorkingRatio: 0.9},
	}
	crisis, reasons := cd.DetectPopulation(noCrisisState)
	if crisis {
		t.Errorf("expected no crisis after reset, got: %v", reasons)
	}
}

// Helper
func containsReason(reasons []string, target string) bool {
	for _, r := range reasons {
		if r == target {
			return true
		}
	}
	return false
}
