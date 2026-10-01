package reliability

import (
	"context"
	"errors"
	"testing"
)

func TestCompletedExecutionPersistenceFailureCannotRetry(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.RetryUnknown = true
	diagnostic := &ExecutionPersistenceError{Err: errors.New("network timeout while recording")}
	calls := 0
	err := policy.ExecuteContext(context.Background(), func() error { calls++; return diagnostic })
	if !errors.Is(err, diagnostic) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestExecutionPauseCannotHideJoinedFault(t *testing.T) {
	pause := &ExecutionStoppedError{Outcome: "input-required", Err: errors.New("network timeout requires input")}
	for _, fixture := range []struct {
		name    string
		err     error
		outcome string
		paused  bool
	}{
		{"pause", pause, "input-required", true},
		{"failed sibling", errors.Join(pause, &ExecutionStoppedError{Outcome: "failure", Err: errors.New("sibling failed")}), "failure", false},
		{"aborted child", errors.Join(pause, &ExecutionPersistenceError{Err: errors.New("child history failed")}), "aborted", false},
		{"unknown sibling", errors.Join(pause, &ExecutionUncertainError{Err: errors.New("lost response")}), "uncertain", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			policy := DefaultRetryPolicy()
			policy.RetryUnknown = true
			calls := 0
			err := policy.ExecuteContext(t.Context(), func() error { calls++; return fixture.err })
			if calls != 1 || ExecutionStopOutcome(err) != fixture.outcome || IsExecutionPause("input-required", err) != fixture.paused || IsExecutionPersistenceError(err) {
				t.Fatalf("calls=%d outcome=%s paused=%v err=%v", calls, ExecutionStopOutcome(err), IsExecutionPause("input-required", err), err)
			}
		})
	}
	if IsExecutionPause("input-required", errors.New("unproven network timeout")) {
		t.Fatal("generic error invented a proven owner pause")
	}
}
