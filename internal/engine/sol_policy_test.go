package engine

import (
	"strings"
	"testing"
)

func TestSolPolicyPinsCodingAndRejectsLegacyFailover(t *testing.T) {
	t.Setenv("BT_LLM_SOL_ONLY", "true")
	t.Setenv("BT_SUPERPOWERS_CODEX_ONLY", "false")
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "claude")
	args := strings.Join((execCodexRunner{}).buildCodexArgs("task", "/tmp/response"), "\n")
	if !strings.Contains(args, "--disable\nmulti_agent") || !strings.Contains(args, "review_model=\"gpt-6.1-sol\"") {
		t.Fatal("coding delegation permits a separately configured model")
	}
	if _, err := resolvedSuperpowersProvider(); err == nil {
		t.Fatal("legacy provider bypassed global Sol policy")
	}
	if rateLimitFailoverEnabled() {
		t.Fatal("legacy failover bypassed global Sol policy")
	}
	for _, model := range []string{"", "auto", "gpt-5.3-codex-spark", "another-model"} {
		t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", model)
		if resolvedSuperpowersCodexModel() != "gpt-6.1-sol" {
			t.Fatal("coding model bypassed global Sol policy")
		}
	}
}
