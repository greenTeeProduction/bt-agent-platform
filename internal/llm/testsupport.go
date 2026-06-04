package llm

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
)

// EnvSkipLLMTests disables LLM integration tests when set to 1, true, or yes.
const EnvSkipLLMTests = "BT_SKIP_LLM_TESTS"

var (
	configuredOnce sync.Once
	configuredVal  bool
)

// TestsDisabled reports explicit opt-out via BT_SKIP_LLM_TESTS.
func TestsDisabled() bool {
	v := strings.TrimSpace(os.Getenv(EnvSkipLLMTests))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// OllamaReachable probes the Ollama /api/tags endpoint (fast; no model load).
func OllamaReachable(cfg Config) bool {
	url := strings.TrimRight(cfg.ServerURL, "/")
	if url == "" {
		url = "http://localhost:11434"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Configured reports whether a real LLM backend is available for integration tests.
// Honors BT_SKIP_LLM_TESTS. For ollama, probes the server; for deepseek/acp, checks credentials/command.
func Configured() bool {
	configuredOnce.Do(func() {
		configuredVal = configured()
	})
	return configuredVal
}

func configured() bool {
	if TestsDisabled() {
		return false
	}
	cfg := DefaultConfig()
	c, err := config.Load()
	provider := "ollama"
	if err == nil && c != nil && strings.TrimSpace(c.LLMProvider) != "" {
		provider = strings.TrimSpace(c.LLMProvider)
	}
	switch provider {
	case "deepseek":
		return c != nil && strings.TrimSpace(c.DeepSeekKey) != ""
	case "acp":
		return c != nil && strings.TrimSpace(c.ACPCommand) != ""
	default:
		return OllamaReachable(cfg)
	}
}

// SkipIfUnavailable skips the test when no LLM is configured or reachable.
func SkipIfUnavailable(t *testing.T) {
	t.Helper()
	if Configured() {
		return
	}
	t.Skip("skipping: no LLM configured or reachable (unset BT_SKIP_LLM_TESTS, configure Ollama/ACP/DeepSeek, or start Ollama)")
}

// SkipUnlessIntegration skips under -short, when BT_SKIP_LLM_TESTS is set, or when the LLM is unreachable.
func SkipUnlessIntegration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}
	SkipIfUnavailable(t)
}

// NewClientOrSkip returns a real Ollama client or skips the test.
func NewClientOrSkip(t *testing.T) LLM {
	t.Helper()
	SkipUnlessIntegration(t)
	client, err := NewClient(DefaultConfig())
	if err != nil {
		t.Skipf("skipping: LLM client: %v", err)
	}
	return client
}
