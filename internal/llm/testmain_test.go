package llm

import (
	"os"
	"testing"
)

// The historical transport suite uses local HTTP/process doubles. Explicitly
// allow those adapters here; policy tests set true/unset/malformed themselves.
func TestMain(m *testing.M) {
	if err := os.Setenv("BT_LLM_SOL_ONLY", "false"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
