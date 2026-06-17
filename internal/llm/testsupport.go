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

// EnvRunLLMTests opts INTO LLM integration tests. When unset, integration tests
// skip by default — even if a backend happens to be reachable — so that a plain
// `go test ./...` never hangs on real (slow) LLM calls. Set to 1, true, or yes
// to actually exercise the LLM backend.
const EnvRunLLMTests = "BT_RUN_LLM_TESTS"

var (
	configuredOnce sync.Once
	configuredVal  bool
)

// envEnabled reports whether the named env var is set to 1, true, or yes.
func envEnabled(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// TestsDisabled reports explicit opt-out via BT_SKIP_LLM_TESTS.
func TestsDisabled() bool {
	return envEnabled(EnvSkipLLMTests)
}

// IntegrationOptedIn reports explicit opt-in via BT_RUN_LLM_TESTS.
func IntegrationOptedIn() bool {
	return envEnabled(EnvRunLLMTests)
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

// SkipUnlessIntegration skips an LLM integration test unless it is explicitly
// opted in. Integration tests run only when BT_RUN_LLM_TESTS is set AND not in
// -short mode AND a backend is reachable/configured. This makes `go test ./...`
// safe by default: real LLM calls (which can take minutes) never run — and never
// hang the suite — unless the caller asks for them.
func SkipUnlessIntegration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping LLM integration test in short mode")
	}
	if !IntegrationOptedIn() {
		t.Skip("skipping LLM integration test; set BT_RUN_LLM_TESTS=1 to enable")
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
