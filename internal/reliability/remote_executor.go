package reliability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RemoteExecutor implements AgentExecutor by delegating agent tasks to a
// remote bt-dashboard or bt-agent HTTP endpoint. This enables horizontal
// scaling — the AgentRouter can distribute tasks across multiple nodes.
type RemoteExecutor struct {
	name    string
	baseURL string
	apiKey  string
	timeout time.Duration
	client  *http.Client
}

// RemoteExecutorConfig configures a RemoteExecutor.
type RemoteExecutorConfig struct {
	Name    string
	BaseURL string // e.g., "http://100.123.73.66:9800"
	APIKey  string // optional, sent as X-API-Key header
	Timeout time.Duration
	Pool    *ConnPool // optional shared connection pool; if nil, a private pool is created
}

// NewRemoteExecutor creates a remote executor that delegates to a
// bt-dashboard instance at the given base URL.
// If cfg.Pool is set, the executor shares that connection pool (ideal when
// multiple executors target the same host). Otherwise, a private pool is created.
func NewRemoteExecutor(cfg RemoteExecutorConfig) *RemoteExecutor {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	re := &RemoteExecutor{
		name:    cfg.Name,
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		timeout: timeout,
	}
	if cfg.Pool != nil {
		re.client = cfg.Pool.HTTPClient()
	} else {
		re.client = &http.Client{Timeout: timeout}
	}
	return re
}

// Execute sends the agent task to the remote dashboard's execution endpoint.
// POST {baseURL}/api/agents/execute with JSON body {"agent":"...", "task":"..."}
func (re *RemoteExecutor) Execute(ctx context.Context, agent, task string) (*AgentResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body := map[string]string{
		"agent": agent,
		"task":  task,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Derive the per-call timeout from the caller's context so an outer job
	// deadline caps the remote call; re.timeout still bounds a call whose
	// caller context has no (or a later) deadline.
	ctx, cancel := context.WithTimeout(ctx, re.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, re.baseURL+"/api/agents/execute", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if re.apiKey != "" {
		req.Header.Set("X-API-Key", re.apiKey)
	}

	// Execution POSTs must not be repeated by HTTP redirect handling. Copy
	// the client so health checks/shared connection-pool users keep their policy.
	client := *re.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, &ExecutionUncertainError{Err: fmt.Errorf("remote executor %q: %w", re.name, err)}
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ExecutionUncertainError{Err: fmt.Errorf("read response: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		diagnostic := fmt.Errorf("remote executor %q: status %d: %s", re.name, resp.StatusCode, string(respData))
		if resp.Header.Get(ExecutionAdmissionHeader) == "false" {
			return nil, diagnostic
		}
		return nil, &ExecutionUncertainError{Err: diagnostic}
	}

	var result AgentResult
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, &ExecutionUncertainError{Err: fmt.Errorf("unmarshal result: %w (body: %s)", err, string(respData))}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(respData, &fields)
	for _, name := range []string{"agent", "task", "output", "duration", "success"} {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, &ExecutionUncertainError{Err: fmt.Errorf("remote executor %q: missing result field %s", re.name, name)}
		}
	}
	if result.Agent != agent {
		return nil, &ExecutionUncertainError{Err: fmt.Errorf("remote executor %q: result agent does not match request", re.name)}
	}
	// Empty task explicitly requests the dashboard's configured default; its
	// computed task need not equal empty. Bind every supplied task exactly.
	if task != "" && result.Task != task {
		return nil, &ExecutionUncertainError{Err: fmt.Errorf("remote executor %q: result task does not match request", re.name)}
	}
	switch result.ErrorKind {
	case ExecutionPersistenceKind:
		if result.Error != "" {
			diagnostic := &ExecutionPersistenceError{Err: errors.New(result.Error)}
			if IsStoppedOutcome(result.Outcome) {
				return &result, &ExecutionStoppedError{Outcome: result.Outcome, Err: diagnostic}
			}
			if IsHealthyOutcome(result.Outcome) || (result.Outcome == "" && result.Success) {
				return &result, diagnostic
			}
		}
	case ExecutionUncertainKind:
		if result.Error != "" {
			return &result, &ExecutionUncertainError{Err: errors.New(result.Error)}
		}
	case ExecutionStoppedKind:
		if result.Error != "" && !result.Success && IsStoppedOutcome(result.Outcome) {
			return &result, &ExecutionStoppedError{Outcome: result.Outcome, Err: errors.New(result.Error)}
		}
	case "":
		return &result, nil // Older peers retain their raw outcome compatibility.
	}
	return &result, &ExecutionUncertainError{Err: fmt.Errorf("remote executor %q: invalid execution error kind/detail", re.name)}
}

// Health checks if the remote dashboard is reachable.
// GET {baseURL}/api/health
func (re *RemoteExecutor) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), re.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, re.baseURL+"/api/health", nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	if re.apiKey != "" {
		req.Header.Set("X-API-Key", re.apiKey)
	}

	resp, err := re.client.Do(req)
	if err != nil {
		return fmt.Errorf("remote executor %q unhealthy: %w", re.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote executor %q unhealthy: status %d", re.name, resp.StatusCode)
	}
	return nil
}

// String returns the executor identifier.
func (re *RemoteExecutor) String() string {
	return fmt.Sprintf("RemoteExecutor(%s @ %s)", re.name, re.baseURL)
}
