package hitl

import (
	"os"
	"strconv"
	"time"
)

// Policy controls when approvals are required or auto-granted.
type Policy struct {
	Enabled       bool
	AutoApprove   bool          // skip human wait (dev/test)
	Timeout       time.Duration // pending request TTL
	DefaultPrompt string
}

// DefaultPolicy loads policy from environment.
func DefaultPolicy() Policy {
	p := Policy{
		Enabled:       true,
		AutoApprove:   os.Getenv("BT_HITL_AUTO_APPROVE") == "true" || os.Getenv("BT_HITL_AUTO_APPROVE") == "1",
		Timeout:       24 * time.Hour,
		DefaultPrompt: "Review and approve before the agent continues.",
	}
	if v := os.Getenv("BT_HITL_TIMEOUT_SECS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			p.Timeout = time.Duration(secs) * time.Second
		}
	}
	if os.Getenv("BT_HITL_ENABLED") == "false" || os.Getenv("BT_HITL_ENABLED") == "0" {
		p.Enabled = false
		p.AutoApprove = true
	}
	return p
}

// Global policy (mutable for tests).
var globalPolicy = DefaultPolicy()

// SetPolicy replaces the global HITL policy.
func SetPolicy(p Policy) {
	globalPolicy = p
}

// GetPolicy returns the active policy.
func GetPolicy() Policy {
	return globalPolicy
}


// HITLConfig is loaded from internal/config.Config.
type HITLConfig struct {
	Enabled      bool
	AutoApprove  bool
	TimeoutSecs  int
}

// ApplyConfig merges file config; env still wins via DefaultPolicy when called after.
func ApplyConfig(c HITLConfig) {
	p := globalPolicy
	if c.TimeoutSecs > 0 {
		p.Timeout = time.Duration(c.TimeoutSecs) * time.Second
	}
	p.Enabled = c.Enabled
	p.AutoApprove = c.AutoApprove
	globalPolicy = p
}
