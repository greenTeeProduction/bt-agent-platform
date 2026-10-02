package reliability

import (
	"errors"
	"fmt"
)

// ExecutionPersistenceError reports a failed record write after healthy work
// completed. Retrying the work can repeat side effects; repair persistence
// separately. Callers must preserve both the completed result and this error.
type ExecutionPersistenceError struct{ Err error }

func (e *ExecutionPersistenceError) Error() string {
	return fmt.Sprintf("execution completed; persistence failed: %v", e.Err)
}
func (e *ExecutionPersistenceError) Unwrap() error { return e.Err }

// Uncertainty wins when parallel branches join completed and unknown work.
func IsExecutionPersistenceError(err error) bool {
	if IsExecutionUncertainError(err) || IsExecutionStoppedError(err) {
		return false
	}
	var diagnostic *ExecutionPersistenceError
	return errors.As(err, &diagnostic)
}

// ExecutionStoppedError preserves a known non-completed disposition. A paused
// task needs input/authentication, and a failed/canceled/rejected task needs an
// explicit new decision; none is a transient transport error permitting replay.
// Outcome uses the canonical BT names accepted by IsStoppedOutcome.
type ExecutionStoppedError struct {
	Outcome string
	Err     error
}

func (e *ExecutionStoppedError) Error() string {
	return fmt.Sprintf("execution stopped (%s): %v", e.Outcome, e.Err)
}
func (e *ExecutionStoppedError) Unwrap() error { return e.Err }

func IsExecutionStoppedError(err error) bool {
	if IsExecutionUncertainError(err) {
		return false
	}
	var diagnostic *ExecutionStoppedError
	return errors.As(err, &diagnostic)
}

// IsStoppedOutcome rejects healthy or unknown wire outcomes. Unknown metadata
// must become uncertainty, never an invented healthy or deferred result.
func IsStoppedOutcome(outcome string) bool { return stoppedOutcomeRank(outcome) > 0 }

// IsHealthyOutcome is the shared canonical completion classifier. Successful
// no-code states remain healthy but distinct from delivered implementation.
func IsHealthyOutcome(outcome string) bool {
	switch outcome {
	case "success", "completed", "no_change", "degraded":
		return true
	default:
		return false
	}
}

func stoppedOutcomeRank(outcome string) int {
	switch outcome {
	case "failure", "timeout", "panic", "escalated":
		return 70
	case "aborted":
		return 60
	case "partial":
		return 55
	case "cancelled":
		return 50
	case "rejected":
		return 40
	case "auth-required":
		return 30
	case "pending_approval":
		return 21
	case "input-required":
		return 20
	case "goap_fusion_rate_limited":
		return 10
	default:
		return 0
	}
}

// ExecutionStopOutcome resolves joined branches conservatively: uncertainty
// wins; failed/aborted work outranks pauses. A completed child's persistence
// error aborts its surrounding workflow without claiming that workflow finished.
func ExecutionStopOutcome(err error) string {
	if IsExecutionUncertainError(err) {
		return "uncertain"
	}
	outcome, rank := "aborted", 0
	var visit func(error)
	visit = func(current error) {
		if current == nil {
			return
		}
		candidate := ""
		switch diagnostic := current.(type) {
		case *ExecutionStoppedError:
			candidate = diagnostic.Outcome
			if !IsStoppedOutcome(candidate) {
				candidate = "aborted"
			}
		case *ExecutionPersistenceError:
			candidate = "aborted"
		}
		if candidateRank := stoppedOutcomeRank(candidate); candidateRank > rank {
			outcome, rank = candidate, candidateRank
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range wrapped.Unwrap() {
				visit(child)
			}
		case interface{ Unwrap() error }:
			visit(wrapped.Unwrap())
		}
	}
	visit(err)
	return outcome
}

// IsPausedOutcome is a known wait, not delivery success or an execution fault.
func IsPausedOutcome(outcome string) bool {
	switch outcome {
	case "input-required", "pending_approval", "auth-required", "goap_fusion_rate_limited":
		return true
	default:
		return false
	}
}

// IsExecutionPause requires matching typed evidence when a terminal error is
// present. This prevents a stale pause string from hiding joined failed/unknown
// work. Older callers with untyped carryover errors retain their compatibility.
func IsExecutionPause(outcome string, err error) bool {
	if !IsPausedOutcome(outcome) || IsExecutionUncertainError(err) {
		return false
	}
	if IsExecutionTerminalError(err) {
		return IsExecutionStoppedError(err) && ExecutionStopOutcome(err) == outcome
	}
	return err == nil || outcome == "goap_fusion_rate_limited"
}

// ExecutionUncertainError reports a dispatched request whose completion cannot
// be established. Replaying it can duplicate side effects. Reconcile evidence
// at the execution owner before an operator chooses another attempt.
type ExecutionUncertainError struct{ Err error }

func (e *ExecutionUncertainError) Error() string {
	return fmt.Sprintf("execution outcome unknown; reconcile before retrying: %v", e.Err)
}
func (e *ExecutionUncertainError) Unwrap() error { return e.Err }

func IsExecutionUncertainError(err error) bool {
	var diagnostic *ExecutionUncertainError
	return errors.As(err, &diagnostic)
}

const (
	ExecutionPersistenceKind = "persistence"
	ExecutionUncertainKind   = "uncertain"
	ExecutionStoppedKind     = "stopped"
	// ExecutionAdmissionHeader is a trusted peer's explicit assertion that an
	// unsuccessful HTTP request was rejected before execution admission.
	ExecutionAdmissionHeader = "X-BT-Execution-Admitted"
)

// ExecutionErrorKind carries terminal execution diagnostics across HTTP without
// guessing disposition from human-readable messages or a collapsed success bool.
func ExecutionErrorKind(err error) string {
	if IsExecutionUncertainError(err) {
		return ExecutionUncertainKind
	}
	if IsExecutionStoppedError(err) {
		return ExecutionStoppedKind
	}
	if IsExecutionPersistenceError(err) {
		return ExecutionPersistenceKind
	}
	return ""
}

// IsExecutionTerminalError identifies diagnostics that must not trigger another
// execution attempt. Persistence repair and uncertain-work reconciliation remain
// separate from retrying the operation itself.
func IsExecutionTerminalError(err error) bool {
	return IsExecutionPersistenceError(err) || IsExecutionUncertainError(err) || IsExecutionStoppedError(err)
}
