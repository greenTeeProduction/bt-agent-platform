package reliability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// NodeProbeStatus captures production-readiness checks for one dashboard node.
type NodeProbeStatus struct {
	Name              string             `json:"name"`
	URL               string             `json:"url"`
	Healthy           bool               `json:"healthy"`
	HealthStatusCode  int                `json:"health_status_code,omitempty"`
	ScalabilityOK     bool               `json:"scalability_ok"`
	ScalabilityStatus *ScalabilityStatus `json:"scalability_status,omitempty"`
	ExecuteOK         bool               `json:"execute_ok,omitempty"`
	ExecuteResult     *AgentResult       `json:"execute_result,omitempty"`
	LatencyMs         int64              `json:"latency_ms,omitempty"` // total probe duration for this node
	Error             string             `json:"error,omitempty"`
}

// MultiNodeProbeReport is a machine-readable scalability validation artifact.
type MultiNodeProbeReport struct {
	CheckedAt       time.Time         `json:"checked_at"`
	Passed          bool              `json:"passed"`
	NodeCount       int               `json:"node_count"`
	HealthyNodes    int               `json:"healthy_nodes"`
	RequiredHealthy int               `json:"required_healthy"`
	ExecuteEnabled  bool              `json:"execute_enabled"`
	Agent           string            `json:"agent,omitempty"`
	Task            string            `json:"task,omitempty"`
	Nodes           []NodeProbeStatus `json:"nodes"`
}

// Summary returns a compact human-readable probe result.
func (r MultiNodeProbeReport) Summary() string {
	status := "FAIL"
	if r.Passed {
		status = "PASS"
	}
	return fmt.Sprintf("%s healthy=%d/%d required=%d execute=%t", status, r.HealthyNodes, r.NodeCount, r.RequiredHealthy, r.ExecuteEnabled)
}

// MultiNodeProbeConfig configures a production-like scalability probe.
type MultiNodeProbeConfig struct {
	Nodes           []string
	APIKey          string
	RequiredHealthy int
	Execute         bool
	Agent           string
	Task            string
	Client          *http.Client
}

// ProbeMultiNodeDashboard validates that multiple dashboard nodes are reachable,
// expose scalability telemetry, and optionally accept remote agent execution.
// It is intentionally side-effect-light by default: Execute must be true before
// the probe POSTs /api/agents/execute on each node.
func ProbeMultiNodeDashboard(ctx context.Context, cfg MultiNodeProbeConfig) (MultiNodeProbeReport, error) {
	start := time.Now().UTC()
	report := MultiNodeProbeReport{
		CheckedAt:       start,
		NodeCount:       len(cfg.Nodes),
		RequiredHealthy: cfg.RequiredHealthy,
		ExecuteEnabled:  cfg.Execute,
		Agent:           cfg.Agent,
		Task:            cfg.Task,
		Passed:          true,
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.RequiredHealthy <= 0 {
		cfg.RequiredHealthy = len(cfg.Nodes)
		report.RequiredHealthy = cfg.RequiredHealthy
	}
	if len(cfg.Nodes) < 2 {
		report.Passed = false
		return report, fmt.Errorf("multi-node probe requires at least 2 nodes, got %d", len(cfg.Nodes))
	}
	if cfg.Execute && (strings.TrimSpace(cfg.Agent) == "" || strings.TrimSpace(cfg.Task) == "") {
		report.Passed = false
		return report, fmt.Errorf("execute probe requires non-empty agent and task")
	}

	for i, raw := range cfg.Nodes {
		base := strings.TrimRight(strings.TrimSpace(raw), "/")
		status := NodeProbeStatus{Name: fmt.Sprintf("node-%d", i+1), URL: base}
		if base == "" {
			status.Error = "empty node URL"
			report.Nodes = append(report.Nodes, status)
			report.Passed = false
			continue
		}
		report.Nodes = append(report.Nodes, status)
	}

	// Probe all nodes concurrently for production-grade latency behavior.
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(report.Nodes))
	for i := range report.Nodes {
		go func(s *NodeProbeStatus) {
			defer wg.Done()
			probeNode(ctx, cfg, s)
			mu.Lock()
			if s.Healthy && s.ScalabilityOK && (!cfg.Execute || s.ExecuteOK) {
				report.HealthyNodes++
			}
			if !(s.Healthy && s.ScalabilityOK && (!cfg.Execute || s.ExecuteOK)) {
				report.Passed = false
			}
			mu.Unlock()
		}(&report.Nodes[i])
	}
	wg.Wait()
	if report.HealthyNodes < cfg.RequiredHealthy {
		report.Passed = false
	}
	return report, nil
}

func probeNode(ctx context.Context, cfg MultiNodeProbeConfig, status *NodeProbeStatus) {
	start := time.Now()
	healthCode, err := getJSON(ctx, cfg.Client, status.URL+"/api/health", cfg.APIKey, nil)
	status.HealthStatusCode = healthCode
	if err != nil {
		status.Error = appendErr(status.Error, "health: "+err.Error())
		status.LatencyMs = time.Since(start).Milliseconds()
		return
	}
	if healthCode < 200 || healthCode >= 300 {
		status.Error = appendErr(status.Error, fmt.Sprintf("health: status %d", healthCode))
		status.LatencyMs = time.Since(start).Milliseconds()
		return
	}
	status.Healthy = true

	var sc ScalabilityStatus
	scCode, err := getJSON(ctx, cfg.Client, status.URL+"/api/scalability", cfg.APIKey, &sc)
	if err != nil {
		status.Error = appendErr(status.Error, "scalability: "+err.Error())
		status.LatencyMs = time.Since(start).Milliseconds()
		return
	}
	if scCode < 200 || scCode >= 300 {
		status.Error = appendErr(status.Error, fmt.Sprintf("scalability: status %d", scCode))
		status.LatencyMs = time.Since(start).Milliseconds()
		return
	}
	status.ScalabilityOK = true
	status.ScalabilityStatus = &sc

	if cfg.Execute {
		result, err := executeProbeTask(ctx, cfg.Client, status.URL, cfg.APIKey, cfg.Agent, cfg.Task)
		if err != nil {
			status.Error = appendErr(status.Error, "execute: "+err.Error())
			status.LatencyMs = time.Since(start).Milliseconds()
			return
		}
		status.ExecuteOK = result.Success
		status.ExecuteResult = result
		if !result.Success && result.Error != "" {
			status.Error = appendErr(status.Error, "execute result: "+result.Error)
		}
	}
	status.LatencyMs = time.Since(start).Milliseconds()
}

func getJSON(ctx context.Context, client *http.Client, url, apiKey string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if dst != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil && err != io.EOF {
			return resp.StatusCode, err
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}

func executeProbeTask(ctx context.Context, client *http.Client, baseURL, apiKey, agent, task string) (*AgentResult, error) {
	body, err := json.Marshal(map[string]string{"agent": agent, "task": task})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/agents/execute", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result AgentResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SingleNodeProbeReport is a machine-readable scalability validation artifact for a single dashboard node.
type SingleNodeProbeReport struct {
	CheckedAt         time.Time          `json:"checked_at"`
	Passed            bool               `json:"passed"`
	BaseURL           string             `json:"base_url"`
	HealthStatusCode  int                `json:"health_status_code,omitempty"`
	Healthy           bool               `json:"healthy"`
	ScalabilityOK     bool               `json:"scalability_ok"`
	ScalabilityStatus *ScalabilityStatus `json:"scalability_status,omitempty"`
	ExecuteOK         bool               `json:"execute_ok,omitempty"`
	ExecuteResult     *AgentResult       `json:"execute_result,omitempty"`
	LatencyMs         int64              `json:"latency_ms,omitempty"`
	Error             string             `json:"error,omitempty"`
}

// Summary returns a compact human-readable probe result for single-node reports.
func (r SingleNodeProbeReport) Summary() string {
	status := "FAIL"
	if r.Passed {
		status = "PASS"
	}
	return fmt.Sprintf("%s node=%s healthy=%t scalability=%t execute=%t",
		status, r.BaseURL, r.Healthy, r.ScalabilityOK, r.ExecuteOK)
}

// SingleNodeProbeConfig configures a production-relevant scalability probe for a single dashboard node.
type SingleNodeProbeConfig struct {
	BaseURL string
	APIKey  string
	Execute bool
	Agent   string
	Task    string
	Client  *http.Client
}

// ProbeSingleNodeDashboard validates that a single dashboard node is reachable,
// exposes scalability telemetry, and optionally accepts remote agent execution.
// Produces a structured JSON artifact for production validation evidence.
func ProbeSingleNodeDashboard(ctx context.Context, cfg SingleNodeProbeConfig) SingleNodeProbeReport {
	start := time.Now()
	report := SingleNodeProbeReport{
		CheckedAt: start,
		BaseURL:   cfg.BaseURL,
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		report.Error = "empty node URL"
		return report
	}

	// 1. Health check
	healthCode, err := getJSON(ctx, cfg.Client, base+"/api/health", cfg.APIKey, nil)
	report.HealthStatusCode = healthCode
	if err != nil {
		report.Error = appendErr(report.Error, "health: "+err.Error())
		return report
	}
	if healthCode < 200 || healthCode >= 300 {
		report.Error = appendErr(report.Error, fmt.Sprintf("health: status %d", healthCode))
		return report
	}
	report.Healthy = true

	// 2. Scalability status
	var sc ScalabilityStatus
	scCode, err := getJSON(ctx, cfg.Client, base+"/api/scalability", cfg.APIKey, &sc)
	if err != nil {
		report.Error = appendErr(report.Error, "scalability: "+err.Error())
		return report
	}
	if scCode < 200 || scCode >= 300 {
		report.Error = appendErr(report.Error, fmt.Sprintf("scalability: status %d", scCode))
		return report
	}
	report.ScalabilityOK = true
	report.ScalabilityStatus = &sc

	// 3. Optional execute
	if cfg.Execute {
		if strings.TrimSpace(cfg.Agent) == "" || strings.TrimSpace(cfg.Task) == "" {
			report.Error = appendErr(report.Error, "execute probe requires non-empty agent and task")
			return report
		}
		result, err := executeProbeTask(ctx, cfg.Client, base, cfg.APIKey, cfg.Agent, cfg.Task)
		if err != nil {
			report.Error = appendErr(report.Error, "execute: "+err.Error())
			return report
		}
		report.ExecuteOK = result.Success
		report.ExecuteResult = result
		if !result.Success && result.Error != "" {
			report.Error = appendErr(report.Error, "execute result: "+result.Error)
		}
	}

	report.Passed = report.Healthy && report.ScalabilityOK && (!cfg.Execute || report.ExecuteOK)
	report.LatencyMs = time.Since(start).Milliseconds()
	return report
}

func appendErr(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}
