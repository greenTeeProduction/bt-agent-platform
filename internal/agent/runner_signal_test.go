package agent

import "testing"

// TestApplyOutcomeRefinement pins the runner-side signal rules: a refinement
// only applies to a "success" tree outcome; authoritative quality bypasses the
// max(estimate, bbScore) rule; a non-authoritative score keeps the max.
func TestApplyOutcomeRefinement(t *testing.T) {
	cases := []struct {
		name          string
		outcome       string
		estimate      float64
		bbScore       float64
		authoritative bool
		refinement    string
		wantOutcome   string
		wantQuality   float64
	}{
		{"committed", "success", 0.8, 0.9, true, "", "success", 0.9},
		{"no_change", "success", 0.8, 0.5, true, "no_change", "no_change", 0.5},
		{"degraded", "success", 0.8, 0.3, true, "degraded", "degraded", 0.3},
		{"failure_not_refined", "failure", 0.2, 0.0, false, "no_change", "failure", 0.2},
		{"non_authoritative_keeps_max", "success", 0.8, 0.4, false, "", "success", 0.8},
		{"non_authoritative_bbscore_wins", "success", 0.4, 0.7, false, "", "success", 0.7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotOutcome, gotQuality := applyOutcomeRefinement(c.outcome, c.estimate, c.bbScore, c.authoritative, c.refinement)
			if gotOutcome != c.wantOutcome || gotQuality != c.wantQuality {
				t.Fatalf("applyOutcomeRefinement = (%q, %v), want (%q, %v)", gotOutcome, gotQuality, c.wantOutcome, c.wantQuality)
			}
		})
	}
}

// TestIsHealthyOutcome: success, no_change and degraded are all healthy terminal
// states — the scheduler must not retry or dead-letter them.
func TestIsHealthyOutcome(t *testing.T) {
	for _, o := range []string{"success", "no_change", "degraded"} {
		if !isHealthyOutcome(o) {
			t.Fatalf("%q must be a healthy outcome", o)
		}
	}
	for _, o := range []string{"failure", "timeout", "partial", ""} {
		if isHealthyOutcome(o) {
			t.Fatalf("%q must not be a healthy outcome", o)
		}
	}
}
