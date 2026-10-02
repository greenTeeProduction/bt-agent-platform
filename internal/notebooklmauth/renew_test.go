package notebooklmauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRenewedSessionRecheckedWithoutBrowser(t *testing.T) {
	for _, initiallyValid := range []bool{false, true} {
		p, dir, requests := fixture(t, map[string]any{})
		if initiallyValid {
			if err := os.WriteFile(filepath.Join(dir, "saved"), []byte("valid"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		calls := 0
		p.renew = func(context.Context) Result {
			calls++
			if err := os.WriteFile(filepath.Join(dir, "saved"), []byte("valid"), 0600); err != nil {
				t.Fatal(err)
			}
			return Result{Status: "valid"}
		}
		if result := p.ensure(context.Background()); !result.OK() {
			t.Fatal(result)
		}
		if calls != 1 || requests.Load() != 0 || read(t, dir, "events") != "check\ncheck\n" {
			t.Fatal("renewal was not independently verified without browser")
		}
		if result := p.ensure(context.Background()); !result.OK() || calls != 1 {
			t.Fatal("successful renewal was repeated during the keepalive interval", result)
		}
	}
}

func TestRenewalRateLimitDoesNotInvalidateWorkingAuth(t *testing.T) {
	p, dir, requests := fixture(t, map[string]any{})
	if err := os.WriteFile(filepath.Join(dir, "saved"), []byte("valid"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	p.renew = func(context.Context) Result {
		calls++
		return Result{Status: "network_error", Detail: "Google renewal HTTP 429; saved profile preserved"}
	}
	for range 2 {
		result := p.ensure(context.Background())
		if !result.OK() || result.RetryAfter.IsZero() {
			t.Fatal("working auth was invalidated by keepalive backoff", result)
		}
	}
	if calls != 1 || requests.Load() != 0 || read(t, dir, "saved") != "valid" {
		t.Fatal("rate-limited keepalive was repeated or touched browser/profile")
	}
	// The renewal backoff must not be mistaken for cached proof of valid auth.
	if err := os.WriteFile(filepath.Join(dir, "saved"), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := p.ensure(context.Background()); result.OK() {
		t.Fatal("expired auth inherited an earlier valid verdict")
	}
}

func TestRenewalNetworkFailurePreservesProfileAndAvoidsBrowser(t *testing.T) {
	p, dir, requests := fixture(t, map[string]any{})
	p.renew = func(context.Context) Result { return Result{Status: "network_error"} }
	if result := p.ensure(context.Background()); result.Status != "network_error" {
		t.Fatal(result)
	}
	if requests.Load() != 0 || read(t, dir, "saved") != "stale" {
		t.Fatal("network failure attempted restoration")
	}
	if result := p.ensure(context.Background()); result.Status != "cooldown" {
		t.Fatal(result)
	}
}
