package evolution

import (
	"testing"
)

func TestCrisisDetector_DiversityCollapse(t *testing.T) {
	cd := NewCrisisDetector()

	// Diversity below threshold should trigger crisis
	state := CrisisState{
		TreeName:            "test_tree",
		CurrentFitness:      50.0,
		BehavioralDiversity: 0.15, // below 0.2 threshold
	}

	crisis, reason := cd.Detect(state)
	if !crisis {
		t.Fatalf("expected crisis for diversity 0.15 below threshold 0.2, got none")
	}
	if reason != "diversity_collapse" {
		t.Fatalf("expected reason 'diversity_collapse', got %q", reason)
	}
}

func TestCrisisDetector_Stagnation(t *testing.T) {
	cd := NewCrisisDetector()
	cd.StagnationLimit = 3 // use smaller limit for test

	treeName := "test_tree"

	state := CrisisState{
		TreeName:            treeName,
		CurrentFitness:      50.0,
		BehavioralDiversity: 0.5, // above threshold
	}

	// Simulate cycles with no improvement.
	// Cycle 0: baseline, stagnation=0.
	// Cycles 1-4: stagnation 1..4. Fires when stagnation (4) > StagnationLimit (3).
	// Total: 5 Detect calls needed (0,1,2,3,4) — crisis fires at the 5th call.
	for i := 0; i < 5; i++ {
		crisis, reason := cd.Detect(state)
		if i < 4 && crisis {
			t.Fatalf("no crisis expected at cycle %d (stagnation=%d, threshold=%d)", i, cd.StagnationCount(treeName), cd.StagnationLimit)
		}
		if i == 4 {
			if !crisis {
				t.Fatal("expected stagnation crisis after 5 cycles with no improvement")
			}
			if reason != "stagnation" {
				t.Fatalf("expected reason 'stagnation', got %q", reason)
			}
		}
	}
}

func TestCrisisDetector_RecoveryResetsStagnation(t *testing.T) {
	cd := NewCrisisDetector()
	cd.StagnationLimit = 5

	treeName := "recovery_tree"

	// 2 cycles at same fitness (cycle 0=baseline, cycle 1=stagnation=1)
	for i := 0; i < 2; i++ {
		cd.Detect(CrisisState{
			TreeName:            treeName,
			CurrentFitness:      40.0,
			BehavioralDiversity: 0.5,
		})
	}
	if cd.StagnationCount(treeName) != 1 {
		t.Fatalf("expected stagnation 1 after 2 cycles at same fitness, got %d", cd.StagnationCount(treeName))
	}

	// Improvement — should reset counter
	crisis, _ := cd.Detect(CrisisState{
		TreeName:            treeName,
		CurrentFitness:      45.0, // improved!
		BehavioralDiversity: 0.5,
	})
	if crisis {
		t.Fatal("no crisis expected after fitness improvement")
	}
	if cd.StagnationCount(treeName) != 0 {
		t.Fatalf("expected stagnation reset to 0, got %d", cd.StagnationCount(treeName))
	}
}

func TestCrisisDetector_Intervene(t *testing.T) {
	cd := NewCrisisDetector()
	cd.EmergencyRate = 0.50

	treeName := "intervene_tree"

	// Trigger stagnation
	for i := 0; i < 6; i++ {
		cd.Detect(CrisisState{
			TreeName:            treeName,
			CurrentFitness:      30.0,
			BehavioralDiversity: 0.5,
		})
	}

	action := cd.Intervene(treeName, "stagnation")
	if !action.EmergencyMode {
		t.Fatal("expected EmergencyMode=true")
	}
	if action.EmergencyRate != 0.50 {
		t.Fatalf("expected emergency rate 0.50, got %f", action.EmergencyRate)
	}
	if action.CrisisReason != "stagnation" {
		t.Fatalf("expected crisis reason 'stagnation', got %q", action.CrisisReason)
	}

	// Reset should clear stagnation
	cd.ResetStagnation(treeName)
	if cd.StagnationCount(treeName) != 0 {
		t.Fatalf("expected 0 after reset, got %d", cd.StagnationCount(treeName))
	}
}

func TestCrisisDetector_ZeroDiversityDoesNotTrigger(t *testing.T) {
	cd := NewCrisisDetector()

	// Zero diversity means no data yet — should not fire
	state := CrisisState{
		TreeName:            "new_tree",
		CurrentFitness:      0,
		BehavioralDiversity: 0, // no data
	}

	crisis, _ := cd.Detect(state)
	if crisis {
		t.Fatal("zero diversity with no data should not trigger crisis")
	}
}

func TestCrisisDetector_NoCrisisWhenHealthy(t *testing.T) {
	cd := NewCrisisDetector()

	// Healthy: diversity above threshold and fitness improving
	state := CrisisState{
		TreeName:            "healthy_tree",
		CurrentFitness:      60.0,
		BehavioralDiversity: 0.75,
	}

	crisis, _ := cd.Detect(state)
	if crisis {
		t.Fatal("no crisis expected when diversity and fitness are healthy")
	}
}

func TestCrisisDetector_MultipleTrees(t *testing.T) {
	cd := NewCrisisDetector()
	cd.StagnationLimit = 2

	// Tree A stagnates (StagnationLimit=2, fires when stagnation>2 = 4th call)
	for i := 0; i < 4; i++ {
		crisis, _ := cd.Detect(CrisisState{
			TreeName:            "tree_a",
			CurrentFitness:      25.0,
			BehavioralDiversity: 0.5,
		})
		if i == 3 && !crisis {
			t.Fatalf("tree_a should detect stagnation at cycle %d (stagnation=%d)", i, cd.StagnationCount("tree_a"))
		}
	}

	// Tree B should be independent — no crisis
	crisis, _ := cd.Detect(CrisisState{
		TreeName:            "tree_b",
		CurrentFitness:      35.0,
		BehavioralDiversity: 0.6,
	})
	if crisis {
		t.Fatal("tree_b should not be in crisis")
	}
	if cd.StagnationCount("tree_b") != 0 {
		t.Fatalf("tree_b stagnation should be 0, got %d", cd.StagnationCount("tree_b"))
	}
}
