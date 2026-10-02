package llm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/tmc/langchaingo/llms"
)

// CodexClient uses the operator's existing Codex login for text inference.
// Each request has an empty working directory, no user/project configuration,
// no shell/web/apps/subagents, an explicit model and a caller-owned deadline.
// Coding delegation deliberately keeps its separate permission contract.
type CodexClient struct {
	Bin     string
	Timeout time.Duration
}

func NewCodexClient(timeout time.Duration) *CodexClient {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	bin := strings.TrimSpace(os.Getenv("BT_CODEX_BIN"))
	if bin == "" {
		bin = strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_CODEX_BIN"))
	}
	if bin == "" {
		bin = "/home/nico/.local/bin/codex"
	}
	return &CodexClient{Bin: bin, Timeout: timeout}
}

// NewConfigured loads the normal configuration without silently substituting
// a provider or test mock when configuration fails.
func NewConfigured() (LLM, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return NewProvider(cfg)
}

func (c *CodexClient) Generate(prompt string) (string, error) {
	return c.GenerateCtx(context.Background(), prompt)
}

func (c *CodexClient) validateExecutable() error {
	if c == nil || !filepath.IsAbs(c.Bin) || filepath.Clean(c.Bin) != c.Bin {
		return fmt.Errorf("sol inference requires an absolute Codex executable")
	}
	info, err := os.Stat(c.Bin)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o002 != 0 || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("sol inference: Codex executable unavailable or unsafe")
	}
	return nil
}

func (c *CodexClient) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	if err := c.validateExecutable(); err != nil {
		return "", err
	}
	if ctx == nil {
		return "", fmt.Errorf("sol inference: nil context")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "bt-sol-inference-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	args := []string{"exec", "--model", config.SolModel, "--ignore-user-config",
		"--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "--color", "never",
		"--disable", "shell_tool", "--disable", "multi_agent", "--disable", "apps",
		"--disable", "hooks", "--disable", "remote_plugin",
		"-c", "web_search=\"disabled\"", "-c", "model_reasoning_effort=\"high\"",
		"-c", "approval_policy=\"never\"",
		"--output-last-message", filepath.Join(dir, "response.txt"), "-"}
	cmd := exec.CommandContext(ctx, c.Bin, args...) // #nosec G204 G702 -- validated absolute operator executable; prompt is stdin, never shell/CLI options.
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	reliability.BindCommandCancellation(cmd)
	diagnostics, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("sol inference failed: %w: %s", err, reliability.TruncateForError(diagnostics))
	}
	response, err := root.ReadFile("response.txt")
	if err != nil {
		return "", fmt.Errorf("sol inference returned no final response: %w", err)
	}
	result := strings.TrimSpace(string(response))
	if result == "" {
		return "", fmt.Errorf("sol inference returned an empty final response")
	}
	return result, nil
}

func (c *CodexClient) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.GenerateCtx(ctx, prompt)
}

func (c *CodexClient) AnalyzeComplexity(task string) string {
	result, err := c.GenerateWithTimeout("Classify this task as one word: low, medium, or high.\n"+task, 30*time.Second)
	if err == nil {
		switch strings.ToLower(strings.TrimSpace(result)) {
		case "low", "medium", "high":
			return strings.ToLower(strings.TrimSpace(result))
		}
	}
	return "medium" // classification heuristic only, never another model
}

func (c *CodexClient) GeneratePlan(task, complexity string) string {
	result, err := c.Generate(fmt.Sprintf("Create a step-by-step execution plan for this %s-complexity task:\n%s", complexity, task))
	if err != nil {
		return "LLM error: " + err.Error()
	}
	return result
}

func (c *CodexClient) Reflect(task, outcome, plan string) (string, string) {
	result, err := c.Generate(fmt.Sprintf("Task: %s\nPlan: %s\nOutcome: %s\nRespond with WENT_WELL: and TO_IMPROVE: sections.", task, plan, outcome))
	if err != nil {
		return "", "LLM error: " + err.Error()
	}
	return extractSection(result, "WENT_WELL:"), extractSection(result, "TO_IMPROVE:")
}

// LangChainModel preserves the existing text ReAct loop: the application
// executes BT tools, while Codex only produces the next textual decision.
type LangChainModel struct{ Inner LLM }

func (m LangChainModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	var opts llms.CallOptions
	for _, option := range options {
		option(&opts)
	}
	if opts.Model != "" && opts.Model != config.SolModel {
		return "", fmt.Errorf("sol-only policy rejects model %q", opts.Model)
	}
	if len(opts.Tools) != 0 || len(opts.Functions) != 0 {
		return "", fmt.Errorf("codex text inference does not accept native tool schemas; use the existing text ReAct executor")
	}
	if opts.MaxTokens > 0 {
		prompt = fmt.Sprintf("Keep the answer within %d tokens.\n%s", opts.MaxTokens, prompt)
	}
	result, err := m.Inner.GenerateCtx(ctx, prompt)
	if err != nil {
		return "", err
	}
	for _, stop := range opts.StopWords {
		if stop != "" {
			if index := strings.Index(result, stop); index >= 0 {
				result = result[:index]
			}
		}
	}
	return result, nil
}

func (m LangChainModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	var prompt strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&prompt, "%s:\n", message.Role)
		for _, part := range message.Parts {
			text, ok := part.(llms.TextContent)
			if !ok {
				return nil, fmt.Errorf("codex text inference received unsupported content %T", part)
			}
			prompt.WriteString(text.Text)
			prompt.WriteByte('\n')
		}
	}
	result, err := m.Call(ctx, prompt.String(), options...)
	if err != nil {
		return nil, err
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: result}}}, nil
}
