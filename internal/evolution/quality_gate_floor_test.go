package evolution

import "testing"

// The composite fitness scale is 0-100 (evaluator.FitnessScore.Composite);
// the floor must live on that scale and protect weak trees more strictly than
// the relative regression check, without ever blocking their improvements.
func TestQualityGateFloorOn100Scale(t *testing.T) {
	tests := []struct {
		name string
		pre  float64
		post float64
		want GateResult
	}{
		{
			// Below the floor and declining: rejected even though the drop is
			// within the 20% regression tolerance.
			name: "small decline below floor rejected",
			pre:  29, post: 28.5,
			want: GateRejected,
		},
		{
			// Weak trees must be allowed to climb out: improvement below the
			// floor passes.
			name: "improvement below floor accepted",
			pre:  25, post: 27,
			want: GateAccepted,
		},
		{
			// Healthy tree, small regression above floor: accepted (unchanged).
			name: "small regression above floor accepted",
			pre:  80, post: 75,
			want: GateAccepted,
		},
		{
			// Large regression that stays above the floor: rollback (unchanged).
			name: "large regression above floor rolls back",
			pre:  80, post: 55,
			want: GateRollback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewQualityGate(t.TempDir())
			if got := q.Validate(tt.pre, tt.post); got != tt.want {
				t.Errorf("Validate(%v, %v) = %v, want %v", tt.pre, tt.post, got, tt.want)
			}
		})
	}
}
