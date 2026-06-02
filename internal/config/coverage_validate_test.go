package config

import (
	"testing"
)

// ─── Retry Configuration Validation Tests ─────────────────────────────────

func TestValidate_RetryMaxRetries_TooLow(t *testing.T) {
	c := newDefaultConfig()
	c.RetryMaxRetries = 0 // below min of 1

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryMaxRetries < 1")
	}
}

func TestValidate_RetryMaxRetries_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.RetryMaxRetries = 11 // above max of 10

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryMaxRetries > 10")
	}
}

func TestValidate_RetryBaseDelayMs_TooLow(t *testing.T) {
	c := newDefaultConfig()
	c.RetryBaseDelayMs = 50 // below min of 100

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryBaseDelayMs < 100")
	}
}

func TestValidate_RetryBaseDelayMs_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.RetryBaseDelayMs = 60001 // above max of 60000

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryBaseDelayMs > 60000")
	}
}

func TestValidate_RetryMaxDelayMs_TooLow(t *testing.T) {
	c := newDefaultConfig()
	c.RetryMaxDelayMs = 500 // below min of 1000

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryMaxDelayMs < 1000")
	}
}

func TestValidate_RetryMaxDelayMs_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.RetryMaxDelayMs = 600001 // above max of 600000

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryMaxDelayMs > 600000")
	}
}

func TestValidate_RetryLLMBaseMs_TooLow(t *testing.T) {
	c := newDefaultConfig()
	c.RetryLLMBaseMs = 50 // below min of 100

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryLLMBaseMs < 100")
	}
}

func TestValidate_RetryLLMBaseMs_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.RetryLLMBaseMs = 120001 // above max of 120000

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when RetryLLMBaseMs > 120000")
	}
}

func TestValidate_RetryJitter_Invalid(t *testing.T) {
	c := newDefaultConfig()
	c.RetryJitter = "unknown_jitter" // not one of the valid values

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error for invalid RetryJitter")
	}
}

// ─── Circuit Breaker Validation Tests ─────────────────────────────────────

func TestValidate_CBThreshold_TooLow(t *testing.T) {
	c := newDefaultConfig()
	c.CBThreshold = 0 // below min of 1

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when CBThreshold < 1")
	}
}

func TestValidate_CBThreshold_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.CBThreshold = 21 // above max of 20

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when CBThreshold > 20")
	}
}

func TestValidate_CBCooldownSecs_TooLow(t *testing.T) {
	c := newDefaultConfig()
	c.CBCooldownSecs = 5 // below min of 10

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when CBCooldownSecs < 10")
	}
}

func TestValidate_CBCooldownSecs_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.CBCooldownSecs = 3601 // above max of 3600

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when CBCooldownSecs > 3600")
	}
}

// ─── Dead Letter Queue Validation Tests ───────────────────────────────────

func TestValidate_DLQMaxEntries_TooLow(t *testing.T) {
	c := newDefaultConfig()
	c.DLQMaxEntries = 5 // below min of 10

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when DLQMaxEntries < 10")
	}
}

func TestValidate_DLQMaxEntries_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.DLQMaxEntries = 100001 // above max of 100000

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when DLQMaxEntries > 100000")
	}
}

// ─── Retry Jitter Valid Values (boundary test) ────────────────────────────

func TestValidate_RetryJitter_ValidValues(t *testing.T) {
	for _, jitter := range []string{"no_jitter", "full_jitter", "equal_jitter", "decorrelated_jitter"} {
		c := newDefaultConfig()
		c.RetryJitter = jitter
		if err := c.Validate(); err != nil {
			t.Errorf("expected RetryJitter=%q to be valid, got: %v", jitter, err)
		}
	}
}
