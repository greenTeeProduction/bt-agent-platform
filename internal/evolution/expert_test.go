package evolution

import "testing"

// TestExpertKnowledge_Observe_CapsLearnedPatterns pins milestone 1/3 of the Q2
// Evolvability program: Observe must cap LearnedPatterns growth, evicting the
// lowest-gain entry once a fixed cap is exceeded — mirroring ExperienceBank's
// ADR-018 cap-500 pattern (experience_bank.go's experienceBankCap) — instead
// of appending forever.
func TestExpertKnowledge_Observe_CapsLearnedPatterns(t *testing.T) {
	const patternCap = 500
	ek := NewExpertKnowledge()

	// Observe more entries than the cap, with strictly increasing gain so the
	// lowest-gain entries are the first patternCap+100 observed (gain 1..100)
	// and the highest-gain entries are the last 500 observed (gain 101..600).
	for i := 1; i <= patternCap+100; i++ {
		ek.Observe("action", "category", float64(i))
	}

	if len(ek.LearnedPatterns) > patternCap {
		t.Fatalf("LearnedPatterns grew to %d entries, want capped at %d", len(ek.LearnedPatterns), patternCap)
	}

	minGain := ek.LearnedPatterns[0].Gain
	for _, lp := range ek.LearnedPatterns {
		if lp.Gain < minGain {
			minGain = lp.Gain
		}
	}
	if minGain <= 100 {
		t.Fatalf("lowest surviving gain is %.0f, want > 100 (lowest-gain entries should have been evicted first)", minGain)
	}
}
