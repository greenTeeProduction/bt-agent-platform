package engine

import (
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

func TestQualityGateValidatesRecoveryAndResumesIt(t *testing.T) {
	for _, good := range []bool{false, true} {
		t.Run(map[bool]string{false: "bad_recovery", true: "good_recovery"}[good], func(t *testing.T) {
			bb := &Blackboard{}
			primaryCalls, recoveryCalls := 0, 0
			primary := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
				primaryCalls++
				ctx.Blackboard.Result = "x"
				return 1
			})
			recovery := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
				recoveryCalls++
				if recoveryCalls == 1 {
					return 0
				}
				ctx.Blackboard.Result = "still short"
				if good {
					ctx.Blackboard.Result = "The report contains the requested findings and their supporting evidence."
				}
				return 1
			})
			gate := qualityGateCommand(primary, recovery)
			ctx := btcore.NewBTContext(t.Context(), bb)
			if got := gate.Run(ctx); got != 0 {
				t.Fatalf("recovery must remain running, got %d", got)
			}
			want := -1
			if good {
				want = 1
			}
			if got := gate.Run(ctx); got != want {
				t.Fatalf("recovery quality: got %d, want %d", got, want)
			}
			if primaryCalls != 1 || recoveryCalls != 2 {
				t.Fatalf("replayed work: primary=%d recovery=%d", primaryCalls, recoveryCalls)
			}
			if !good && bb.Outcome != "quality_gate_failed" {
				t.Fatalf("lost rejection: %q", bb.Outcome)
			}
		})
	}
}

func TestGovernanceGatesDoNotReplayStoppedExecution(t *testing.T) {
	for _, kind := range []string{"quality", "checkpoint"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
				calls++
				ctx.Blackboard.stopExecution("needs approval", &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("approval required")})
				return -1
			})
			cmd := qualityGateCommand(child, child)
			if kind == "checkpoint" {
				cmd = NewCheckpointVerifier(child, 3, map[string]bool{"done": true})
			}
			bb := &Blackboard{}
			ctx := btcore.NewBTContext(t.Context(), bb)
			for range 2 {
				if code := cmd.Run(ctx); code != -1 {
					t.Fatalf("stop returned %d", code)
				}
			}
			if calls != 1 || bb.Outcome != "input-required" {
				t.Fatalf("stop was retried/hidden: calls=%d outcome=%q", calls, bb.Outcome)
			}
		})
	}
}

func TestCheckpointRequiresEvidenceEvenWhenWorldStateIsMissing(t *testing.T) {
	for _, expected := range []bool{false, true} {
		bb := &Blackboard{}
		child := btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 })
		cmd := NewCheckpointVerifier(child, 1, map[string]bool{"verified": expected})
		if got := cmd.Run(btcore.NewBTContext(t.Context(), bb)); got != -1 {
			t.Fatalf("absent evidence proved verified=%v: status=%d", expected, got)
		}
	}
}
