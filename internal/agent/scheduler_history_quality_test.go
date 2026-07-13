package agent

import "testing"

// The recorded history quality must reflect RunOnce's refined score
// (applyOutcomeRefinement: committed=0.9, no_change=0.5, degraded=0.3) for
// healthy outcomes — not the scheduler's own text-shape estimate, which
// previously discarded it (committed runs landed as 0.75/0.9/1.0 depending on
// output length, and no_change/degraded would have recorded 0.0).
func TestRecordedQuality(t *testing.T) {
	out := "## Superpowers Implementation Complete\nApply status: `committed`\nCommit: `abc1234`\n"

	cases := []struct {
		name    string
		outcome string
		res     *RunResult
		want    float64
	}{
		{"committed success uses authoritative 0.9", "success", &RunResult{Quality: 0.9}, 0.9},
		{"no_change uses refined 0.5", "no_change", &RunResult{Quality: 0.5}, 0.5},
		{"degraded uses refined 0.3", "degraded", &RunResult{Quality: 0.3}, 0.3},
		{"failure keeps the 0.0 convention", "failure", &RunResult{Quality: 0.8}, 0.0},
		{"timeout keeps the 0.0 convention", "timeout", &RunResult{Quality: 0.7}, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := recordedQuality(nil, c.outcome, out, c.res); got != c.want {
				t.Fatalf("recordedQuality = %v, want %v", got, c.want)
			}
		})
	}

	// A nil RunResult (e.g. a panicked run) falls back to the text-shape estimate.
	if got, want := recordedQuality(nil, "success", out, nil), historyQualityScore(nil, "success", out); got != want {
		t.Fatalf("nil RunResult: got %v, want fallback %v", got, want)
	}
}
