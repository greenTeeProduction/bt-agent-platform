package llm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/tmc/langchaingo/llms"
)

func fakeCodex(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCodexInferencePinsModelAndSeparatesPromptAndResponse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_CODEX_TEST_DIR", dir)
	t.Setenv("BT_SUPERPOWERS_CODEX_MODEL", "another-model")
	client := NewCodexClient(time.Second)
	client.Bin = fakeCodex(t, `
printf '%s\n' "$@" > "$BT_CODEX_TEST_DIR/args"
pwd > "$BT_CODEX_TEST_DIR/cwd"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then shift; output="$1"; fi
  shift
done
cat > "$BT_CODEX_TEST_DIR/prompt"
echo 'diagnostic noise, never the response'
printf 'SOL_RESULT\n' > "$output"
`)
	prompt := "--model other\n$(do-not-execute)\nprivate task"
	got, err := client.Generate(prompt)
	if err != nil || got != "SOL_RESULT" {
		t.Fatalf("response = %q, %v", got, err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--model\ngpt-6.1-sol\n", "--ignore-user-config", "--sandbox\nread-only", "--disable\nshell_tool", "--disable\nmulti_agent", "web_search=\"disabled\""} {
		if !strings.Contains(string(args), expected) {
			t.Errorf("missing %q in %s", expected, args)
		}
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil || string(stdin) != prompt || strings.Contains(string(args), "private task") {
		t.Fatalf("prompt escaped stdin: %q, %v", stdin, err)
	}
	cwd, err := os.ReadFile(filepath.Join(dir, "cwd"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(cwd))); !os.IsNotExist(err) {
		t.Fatalf("inference workspace was retained: %v", err)
	}
}

func TestCodexInferenceFailureAndDeadlineNeverFallback(t *testing.T) {
	for _, body := range []string{"echo unauthorized >&2; exit 1", "echo diagnostics-only; exit 0"} {
		client := &CodexClient{Bin: fakeCodex(t, body), Timeout: time.Second}
		if text, err := client.Generate("prompt"); err == nil || text != "" {
			t.Fatalf("failed request acknowledged: %q %v", text, err)
		}
	}
	client := &CodexClient{Bin: fakeCodex(t, "sleep 10 & wait"), Timeout: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := client.GenerateCtx(ctx, "prompt"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("child or inherited pipe outlived cancellation")
	}
}

func TestSolPolicyOverridesLegacyProviderAndFallback(t *testing.T) {
	for _, policy := range []string{"", "true", "invalid"} {
		t.Setenv("BT_LLM_SOL_ONLY", policy)
		provider, err := NewProvider(&config.Config{LLMProvider: "deepseek", FallbackModels: "ollama:other", LLMTimeout: 2})
		client, ok := provider.(*CodexClient)
		if err != nil || !ok || client.Timeout != 2*time.Second {
			t.Fatalf("policy %q selected %T: %v", policy, provider, err)
		}
	}
}

func TestLangChainSolPreservesStopWordsAndRejectsModelOverride(t *testing.T) {
	model := LangChainModel{Inner: &MockLLM{GenerateResponse: "Action: inspect\nObservation: invented"}}
	result, err := model.Call(context.Background(), "task", llms.WithStopWords([]string{"\nObservation:"}))
	if err != nil || result != "Action: inspect" {
		t.Fatalf("ReAct stop lost: %q %v", result, err)
	}
	if _, err := model.Call(context.Background(), "task", llms.WithModel("other")); err == nil {
		t.Fatal("per-call model bypassed Sol policy")
	}
}

func TestCodexLoginSolLive(t *testing.T) {
	if os.Getenv("BT_LIVE_SOL") != "1" {
		t.Skip("set BT_LIVE_SOL=1 for a single authenticated Sol inference")
	}
	t.Setenv("BT_LLM_SOL_ONLY", "true")
	client := NewCodexClient(45 * time.Second)
	result, err := client.Generate("Reply with exactly BT_SOL61_OK. Do not use tools.")
	if err != nil || result != "BT_SOL61_OK" {
		t.Fatalf("live Sol inference: %q %v", result, err)
	}
}

func TestSolOnlyRejectsDirectLegacyHTTPBeforeNetwork(t *testing.T) {
	t.Setenv("BT_LLM_SOL_ONLY", "true")
	// Port 1 is not a fixture dependency: policy must reject before dialing it.
	models := []LLM{
		&Client{}, // rejection must happen before even accessing a transport
		NewDeepSeekClient(DeepSeekConfig{BaseURL: "http://127.0.0.1:1", Model: "legacy"}),
		NewOpenAICompatClient(OpenAICompatConfig{BaseURL: "http://127.0.0.1:1", Model: "legacy"}),
		NewACPClient(ACPConfig{Command: "/nonexistent/legacy-model"}),
	}
	for _, model := range models {
		if _, err := model.Generate("private task"); err == nil || !strings.Contains(err.Error(), "sol-only policy") {
			t.Fatalf("%T did not reject before transport: %v", model, err)
		}
	}
}

func TestSolHealthChecksLoginWithoutInference(t *testing.T) {
	t.Setenv("BT_LLM_SOL_ONLY", "true")
	t.Setenv("BT_CODEX_BIN", fakeCodex(t, `test "$#" = 2 && test "$1" = login && test "$2" = status`))
	monitor := NewProviderHealthMonitor(&config.Config{LLMProvider: "ollama", OllamaHost: "http://127.0.0.1:1"}, 0)
	if !monitor.Probe() || !monitor.IsHealthy() {
		t.Fatalf("login readiness failed: %v", monitor.State().LastError())
	}
	t.Setenv("BT_CODEX_BIN", fakeCodex(t, "exit 1"))
	monitor = NewProviderHealthMonitor(&config.Config{}, 0)
	if monitor.Probe() {
		t.Fatal("failed login reported healthy")
	}
	t.Setenv("BT_CODEX_BIN", "codex")
	monitor = NewProviderHealthMonitor(&config.Config{}, 0)
	if monitor.Probe() || !strings.Contains(monitor.State().LastError().Error(), "absolute") {
		t.Fatal("health probe accepted a relative executable")
	}
}
