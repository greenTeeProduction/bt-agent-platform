package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// LiveModel is the owner's explicit benchmark-only Ollama exception. It never
// changes the process-wide Sol policy or substitutes synthetic model output.
// A slow/unavailable local model switches this client to Sol for subsequent calls.
type LiveModel struct {
	mu                   sync.Mutex
	host, model, backend string
	timeout              time.Duration
	sol                  *llm.CodexClient
	calls                int
	fallbacks            int
	lastError            string
	errors               int
}

type ModelEvidence struct {
	Backend   string `json:"backend"`
	Model     string `json:"model"`
	Calls     int    `json:"calls"`
	Fallbacks int    `json:"fallbacks"`
	LastError string `json:"last_error,omitempty"`
	Errors    int    `json:"errors"`
}

func newLiveModel() (*LiveModel, error) {
	backend := strings.TrimSpace(os.Getenv("BT_BENCHMARK_BACKEND"))
	if backend == "" {
		backend = "ollama"
	}
	if backend != "ollama" && backend != "sol" {
		return nil, fmt.Errorf("benchmark backend must be ollama or sol, got %q", backend)
	}
	host := strings.TrimRight(os.Getenv("BT_BENCHMARK_OLLAMA_URL"), "/")
	if host == "" {
		host = "http://127.0.0.1:11434"
	}
	model := strings.TrimSpace(os.Getenv("BT_BENCHMARK_MODEL"))
	if model == "" {
		model = "qwen2.5:1.5b"
	}
	timeout := 15 * time.Second
	if raw := os.Getenv("BT_BENCHMARK_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("invalid BT_BENCHMARK_TIMEOUT %q", raw)
		}
		timeout = value
	}
	return &LiveModel{host: host, model: model, backend: backend, timeout: timeout, sol: llm.NewCodexClient(90 * time.Second)}, nil
}

func (m *LiveModel) Evidence() ModelEvidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	model := m.model
	if m.backend == "sol" {
		model = config.SolModel
	}
	return ModelEvidence{Backend: m.backend, Model: model, Calls: m.calls, Fallbacks: m.fallbacks, LastError: m.lastError, Errors: m.errors}
}

func (m *LiveModel) Generate(prompt string) (string, error) {
	return m.GenerateCtx(context.Background(), prompt)
}
func (m *LiveModel) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return m.generate(ctx, prompt, 256)
}
func (m *LiveModel) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.generate(ctx, prompt, 256)
}
func (m *LiveModel) GenerateWithMaxTokens(prompt string, maxTokens int) (string, error) {
	return m.generate(context.Background(), prompt, maxTokens)
}

func (m *LiveModel) generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	m.mu.Lock()
	backend := m.backend
	m.mu.Unlock()
	maxTokens = min(max(maxTokens, 1), 512)
	var output string
	var err error
	if backend == "ollama" {
		callCtx, cancel := context.WithTimeout(ctx, m.timeout)
		output, err = m.ollama(callCtx, prompt, maxTokens)
		cancel()
		if err != nil && ctx.Err() == nil {
			m.mu.Lock()
			m.backend = "sol"
			m.fallbacks++
			m.mu.Unlock()
			backend = "sol"
		}
	}
	if backend == "sol" {
		output, err = m.sol.GenerateCtx(ctx, prompt)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if err != nil {
		m.errors++
		m.lastError = err.Error()
	}
	return output, err
}

func (m *LiveModel) ollama(ctx context.Context, prompt string, maxTokens int) (string, error) {
	data, err := json.Marshal(map[string]any{
		"model": m.model, "prompt": prompt, "stream": false, "keep_alive": "15m",
		"options": map[string]any{"temperature": 0, "seed": 42, "num_ctx": 4096, "num_predict": maxTokens},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.host+"/api/generate", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("benchmark Ollama HTTP %d", resp.StatusCode)
	}
	var result struct {
		Response string `json:"response"`
		Error    string `json:"error"`
		Done     bool   `json:"done"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("benchmark Ollama: %s", result.Error)
	}
	if !result.Done || strings.TrimSpace(result.Response) == "" {
		return "", fmt.Errorf("benchmark Ollama returned incomplete or empty output")
	}
	return strings.TrimSpace(result.Response), nil
}

func (m *LiveModel) AnalyzeComplexity(task string) string {
	output, err := m.generate(context.Background(), "Classify task complexity. Reply only low, medium, or high. Task: "+task, 8)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}
func (m *LiveModel) GeneratePlan(task, complexity string) string {
	output, err := m.Generate("Complete this benchmark task using only the supplied facts. Be concise and report missing evidence honestly. Do not claim external actions were executed. Task: " + task + "\nComplexity: " + complexity)
	if err != nil {
		return ""
	}
	return output
}
func (m *LiveModel) Reflect(task, outcome, plan string) (string, string) {
	output, err := m.Generate("Review the result against the task. State verified strengths and missing evidence. Task: " + task + "\nOutcome: " + outcome + "\nResult: " + plan)
	if err != nil {
		return "", err.Error()
	}
	return output, ""
}
