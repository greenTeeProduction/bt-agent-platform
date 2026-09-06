package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

func rateLimitFailoverEnabled() bool {
	enabled, _ := strconv.ParseBool(os.Getenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER"))
	return enabled
}

func alternateDelegationProvider(p DelegationProvider) DelegationProvider {
	if p == DelegationProviderCodex {
		return DelegationProviderClaude
	}
	return DelegationProviderCodex
}

// DelegationRateLimitError preserves retry information through wrapped runtime errors.
// The runner owns these cooldowns; callers must not re-arm the configured primary.
type DelegationRateLimitError struct {
	Provider DelegationProvider
	RetryAt  time.Time
}

func (e *DelegationRateLimitError) Error() string {
	return fmt.Sprintf("delegation rate limit: retry %s at %s", e.Provider, e.RetryAt.UTC().Format(time.RFC3339))
}

func (d delegatingRunner) runProvider(ctx context.Context, dir, prompt string, p DelegationProvider) CommandResult {
	var result CommandResult
	if err := ctx.Err(); err != nil {
		return CommandResult{Provider: p, Dir: dir, Err: err}
	}
	if p == DelegationProviderCodex {
		result = d.codex.RunCodex(ctx, dir, prompt)
	} else {
		result = d.claude.RunClaude(ctx, dir, prompt)
	}
	result.Provider = p
	return result
}

// Two attempts maximum, in the same workspace and with the same context and
// permission policy. Never mutate process environment or retry unrelated errors.
func (d delegatingRunner) runWithRateLimitFailover(ctx context.Context, dir, prompt string, primary DelegationProvider) CommandResult {
	var earliest time.Time
	var retryProvider DelegationProvider
	var lastProvider DelegationProvider
	var duration time.Duration
	for _, p := range []DelegationProvider{primary, alternateDelegationProvider(primary)} {
		if err := ctx.Err(); err != nil {
			return CommandResult{Dir: dir, Err: err, BackoffManaged: true}
		}
		now := time.Now()
		until, ok := readSharedBackoff(backoffPathFor(p))
		if !ok || !until.After(now) {
			result := d.runProvider(ctx, dir, prompt, p)
			lastProvider = result.Provider
			duration += result.Duration
			result.Duration = duration
			result.BackoffManaged = true
			if ctx.Err() != nil {
				result.Err = ctx.Err()
				return result
			}
			if errors.Is(result.Err, context.Canceled) || errors.Is(result.Err, context.DeadlineExceeded) {
				return result
			}
			// Successful prose is not a quota signal.
			if result.Err == nil {
				return result
			}
			text := result.Output + "\n" + result.Err.Error()
			if !isDelegationRateLimit(p, text) {
				return result
			}
			until = delegationBackoffDeadline(p, text, now, delegationBackoffWindow(p))
			writeSharedBackoff(backoffPathFor(p), until, "delegatingRunner")
		}
		if earliest.IsZero() || until.Before(earliest) {
			earliest = until
			retryProvider = p
		}
	}
	return CommandResult{Provider: lastProvider, Dir: dir, Duration: duration, BackoffManaged: true, RetryAt: earliest,
		Err: &DelegationRateLimitError{Provider: retryProvider, RetryAt: earliest}}
}

// delegationPreflightBackoff honors legacy/chain state as well as fleet stamps.
// With failover enabled a closed primary is not a reason to skip a healthy peer.
func delegationPreflightBackoff(bb *Blackboard, p DelegationProvider, now time.Time) (time.Time, bool) {
	if !rateLimitFailoverEnabled() {
		if !delegationBackoffActive(bb, p, now) {
			return time.Time{}, false
		}
		return loadDelegationBackoffState(bb, p)
	}
	// Promote legacy run/agent stamps so the shared runner observes the same
	// eligibility decision as this preflight (its interface has no Blackboard).
	for _, candidate := range delegationRuntimeBinaries(p) {
		if stamp, valid := loadDelegationBackoffState(bb, candidate); valid && stamp.After(now) {
			if shared, exists := readSharedBackoff(backoffPathFor(candidate)); !exists || stamp.After(shared) {
				writeSharedBackoff(backoffPathFor(candidate), stamp, "legacy-preflight")
			}
		}
	}
	until, ok := loadDelegationBackoffState(bb, p)
	if !ok || !until.After(now) {
		return time.Time{}, false
	}
	alternate, ok := loadDelegationBackoffState(bb, alternateDelegationProvider(p))
	if !ok || !alternate.After(now) {
		return time.Time{}, false
	}
	if alternate.Before(until) {
		until = alternate
	}
	return until, true
}

// Managed runners classify quota once; callers must not reinterpret canceled
// or successful output as a quota closure. Legacy injected runners retain their
// historical text classifier.
func delegationResultRateLimited(result CommandResult, p DelegationProvider, text string) bool {
	if result.BackoffManaged {
		var limited *DelegationRateLimitError
		return errors.As(result.Err, &limited)
	}
	return isDelegationRateLimit(p, text)
}

func delegationRuntimeBinaries(p DelegationProvider) []DelegationProvider {
	if rateLimitFailoverEnabled() {
		return []DelegationProvider{p, alternateDelegationProvider(p)}
	}
	return []DelegationProvider{p}
}
