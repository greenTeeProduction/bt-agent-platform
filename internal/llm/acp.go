package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// ACPConfig holds Agent Client Protocol process settings.
type ACPConfig struct {
	Command string
	Args    []string
	CWD     string
	Timeout time.Duration
}

// ACPClient implements LLM by delegating prompts to an ACP-compatible agent
// process such as `hermes acp --accept-hooks`.
type ACPClient struct {
	cfg ACPConfig
	seq atomic.Int64
}

// NewACPClient creates an ACP-backed LLM client.
func NewACPClient(cfg ACPConfig) *ACPClient {
	if cfg.Command == "" {
		cfg.Command = "hermes"
	}
	if len(cfg.Args) == 0 {
		cfg.Args = []string{"acp", "--accept-hooks"}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 300 * time.Second
	}
	if cfg.CWD == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.CWD = cwd
		} else {
			cfg.CWD = "."
		}
	}
	return &ACPClient{cfg: cfg}
}

// Generate implements LLM.Generate using a fresh ACP session per prompt.
func (c *ACPClient) Generate(prompt string) (string, error) {
	return c.GenerateCtx(context.Background(), prompt)
}

// GenerateCtx generates with caller-provided cancellation.
func (c *ACPClient) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("acp client is nil")
	}
	if c.cfg.Command == "" {
		return "", fmt.Errorf("acp command must not be empty")
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
	cmd.Dir = c.cfg.CWD
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("acp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("acp stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start acp command %q: %w", c.cfg.Command, err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	messages := make(chan map[string]any, 16)
	scanErr := make(chan error, 1)
	go scanJSONLines(stdout, messages, scanErr)

	request := func(method string, params map[string]any, textParts *[]string) (map[string]any, error) {
		id := c.seq.Add(1)
		msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
		if err := writeACPMessage(stdin, msg); err != nil {
			return nil, err
		}
		for {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("timed out waiting for ACP response to %s: %w; stderr: %s", method, ctx.Err(), strings.TrimSpace(stderr.String()))
			case err := <-scanErr:
				if err != nil {
					return nil, fmt.Errorf("read acp response: %w; stderr: %s", err, strings.TrimSpace(stderr.String()))
				}
				return nil, fmt.Errorf("acp process exited before %s response; stderr: %s", method, strings.TrimSpace(stderr.String()))
			case incoming := <-messages:
				if incoming == nil {
					continue
				}
				if handled := c.handleServerMessage(stdin, incoming, textParts); handled {
					continue
				}
				if sameJSONID(incoming["id"], id) {
					if errObj, ok := incoming["error"].(map[string]any); ok {
						return nil, fmt.Errorf("acp %s error: %v", method, errObj["message"])
					}
					if result, ok := incoming["result"].(map[string]any); ok {
						return result, nil
					}
					return map[string]any{}, nil
				}
			}
		}
	}

	if _, err := request("initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{"fs": map[string]any{
			"readTextFile":  true,
			"writeTextFile": false,
		}},
		"clientInfo": map[string]any{"name": "go-bt-evolve", "title": "Go BT Framework", "version": "0.0.0"},
	}, nil); err != nil {
		return "", err
	}

	session, err := request("session/new", map[string]any{"cwd": c.cfg.CWD, "mcpServers": []any{}}, nil)
	if err != nil {
		return "", err
	}
	sessionID, _ := session["sessionId"].(string)
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("ACP session/new did not return sessionId")
	}

	var textParts []string
	_, err = request("session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]string{{"type": "text", "text": prompt}},
	}, &textParts)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.Join(textParts, "")), nil
}

// GenerateWithTimeout generates with a per-operation timeout override.
func (c *ACPClient) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.GenerateCtx(ctx, prompt)
}

// AnalyzeComplexity estimates task complexity without an extra ACP round trip.
func (c *ACPClient) AnalyzeComplexity(task string) string {
	if len(task) < 50 {
		return "low"
	}
	if len(task) < 200 {
		return "medium"
	}
	return "high"
}

// GeneratePlan creates an execution plan for a task via ACP.
func (c *ACPClient) GeneratePlan(task, complexity string) string {
	prompt := fmt.Sprintf("Create a step-by-step execution plan for this %s-complexity task.\nTask: %s\nPlan:", complexity, task)
	result, err := c.GenerateWithTimeout(prompt, 120*time.Second)
	if err != nil {
		return fmt.Sprintf("1. Analyze: %s\n2. Execute: %s\n3. Verify result", task, task)
	}
	return result
}

// Reflect generates a reflection on a completed task via ACP.
func (c *ACPClient) Reflect(task, outcome, plan string) (wentWell string, toImprove string) {
	prompt := fmt.Sprintf("Task: %s\nPlan: %s\nOutcome: %s\n\nAnalyze what went well and what could be improved. Respond in exactly this format:\nWENT_WELL: <text>\nTO_IMPROVE: <text>", task, plan, outcome)
	result, err := c.GenerateWithTimeout(prompt, 120*time.Second)
	if err != nil {
		return "task completed", "better error handling"
	}
	wentWell = extractSection(result, "WENT_WELL:")
	toImprove = extractSection(result, "TO_IMPROVE:")
	if wentWell == "" {
		wentWell = "task completed"
	}
	if toImprove == "" {
		toImprove = "better error handling"
	}
	return
}

func scanJSONLines(r io.Reader, out chan<- map[string]any, errc chan<- error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			errc <- err
			return
		}
		out <- msg
	}
	errC := scanner.Err()
	errc <- errC
}

func writeACPMessage(w io.Writer, msg map[string]any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal acp request: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write acp request: %w", err)
	}
	return nil
}

func sameJSONID(got any, want int64) bool {
	switch v := got.(type) {
	case float64:
		return int64(v) == want
	case int64:
		return v == want
	case int:
		return int64(v) == want
	case string:
		return v == fmt.Sprintf("%d", want)
	default:
		return false
	}
}

func (c *ACPClient) handleServerMessage(stdin io.Writer, msg map[string]any, textParts *[]string) bool {
	method, ok := msg["method"].(string)
	if !ok || method == "" {
		return false
	}
	if method == "session/update" {
		if textParts == nil {
			return true
		}
		params, _ := msg["params"].(map[string]any)
		update, _ := params["update"].(map[string]any)
		kind, _ := update["sessionUpdate"].(string)
		if kind != "agent_message_chunk" {
			return true
		}
		content, _ := update["content"].(map[string]any)
		if text, _ := content["text"].(string); text != "" {
			*textParts = append(*textParts, text)
		}
		return true
	}

	id := msg["id"]
	var response map[string]any
	switch method {
	case "session/request_permission":
		response = map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"outcome": map[string]any{"outcome": "cancelled", "message": "ACP permission denied by go-bt-evolve client"}}}
	case "fs/read_text_file":
		response = map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"content": ""}}
	case "fs/write_text_file":
		response = map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32603, "message": "fs/write_text_file is disabled by go-bt-evolve ACP client"}}
	default:
		response = map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "ACP client method not supported"}}
	}
	_ = writeACPMessage(stdin, response)
	return true
}
