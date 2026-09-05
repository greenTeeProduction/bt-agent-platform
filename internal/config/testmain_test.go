package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain strips every ambient BT_* variable before the suite runs. These
// tests exercise env-override and validation behavior by setting exactly the
// variables they need; anything inherited from the outside changes Load()'s
// behavior. Observed live 2026-07-02: the goap-fusion cycle's pre-commit hook
// runs this suite under the daemon environment (BT_LLM_PROVIDER=deepseek, no
// BT_DEEPSEEK_KEY), and every TestEnvOverride_* died on "DeepSeekKey: must
// not be empty" — blocking the run's commit. Env-override tests must own
// their environment completely.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, "BT_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}
