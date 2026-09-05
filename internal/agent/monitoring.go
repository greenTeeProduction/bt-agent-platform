// Package monitoring provides alert evaluation for the BT platform.
// It reads metrics JSON from /api/metrics and evaluates production alert rules.
package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

// ─── Types ──────────────────────────────────────────────────────────────────

// Severity levels for alerts.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Alert represents a single evaluated alert rule.
type Alert struct {
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Component   string `json:"component"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Firing      bool   `json:"firing"`
	Value       string `json:"value,omitempty"`
}

// MetricsJSON mirrors the /api/metrics JSON response from the dashboard.
type MetricsJSON struct {
	HTTPRequestsTotal uint64        `json:"http_requests_total"`
	HTTPErrorsTotal   uint64        `json:"http_errors_total"`
	TotalRequests     uint64        `json:"total_requests"`
	TotalErrors       uint64        `json:"total_errors"`
	Agents            []AgentMetric `json:"agents"`
}

// AgentMetric mirrors per-agent metrics from /api/metrics.
type AgentMetric struct {
	Name          string `json:"name"`
	SuccessCount  uint64 `json:"success_count"`
	ErrorCount    uint64 `json:"error_count"`
	TotalCount    uint64 `json:"total_count"`
	AvgDurationMs string `json:"avg_duration_ms"`
	SuccessRate   string `json:"success_rate"`
	LastRun       string `json:"last_run"` // RFC3339
}

// AlertReport is the result of evaluating all alert rules.
type AlertReport struct {
	EvaluatedAt string  `json:"evaluated_at"`
	TotalRules  int     `json:"total_rules"`
	FiringCount int     `json:"firing_count"`
	Alerts      []Alert `json:"alerts"`
	AllClear    bool    `json:"all_clear"`
}

// ─── Thresholds ─────────────────────────────────────────────────────────────

const (
	// Agent error rate thresholds (lifetime ratio). Requires min_samples to avoid
	// false positives on brand-new agents with 1 failure out of 2 runs.
	agentErrorRateWarning  = 0.10 // 10%
	agentErrorRateCritical = 0.50 // 50%
	agentMinSamples        = 10   // minimum tasks before error rate alerts fire

	// Agent no-activity threshold.
	agentNoActivityDuration = 10 * time.Minute

	// Agent slow execution threshold (average ms). Tuned for Jetson ARM64 where
	// Ollama qwen3.6:35b calls take 2-4 minutes. 600s gives headroom.
	agentSlowExecutionMs = 600_000 // 10 minutes

	// HTTP error rate threshold.
	httpErrorRateThreshold = 0.05 // 5%

	// Global error rate threshold. Tuned from 5→10 to reduce noise on Jetson
	// where transient Ollama timeouts during high load are normal.
	globalErrorSpikeThreshold = 10 // raw count threshold in evaluation window

	// No request / no activity thresholds.
	noRequestDuration      = 10 * time.Minute
	lowActivitySuppressDur = 1 * time.Hour
)

// ─── Alert Evaluation ───────────────────────────────────────────────────────

// EvaluateAlerts runs all alert rules against the given metrics.
func EvaluateAlerts(metrics MetricsJSON) AlertReport {
	now := time.Now().UTC()
	alerts := make([]Alert, 0)

	// ── Agent-level alerts ───────────────────────────────────────────
	for _, a := range metrics.Agents {
		agentAlerts := evaluateAgentAlerts(a, now)
		alerts = append(alerts, agentAlerts...)
	}

	// ── HTTP / Dashboard alerts ──────────────────────────────────────
	alerts = append(alerts, evaluateHTTPAlerts(metrics)...)

	// ── Global alerts ────────────────────────────────────────────────
	alerts = append(alerts, evaluateGlobalAlerts(metrics, now)...)

	firingCount := 0
	for _, a := range alerts {
		if a.Firing {
			firingCount++
		}
	}

	return AlertReport{
		EvaluatedAt: now.Format(time.RFC3339),
		TotalRules:  len(alerts),
		FiringCount: firingCount,
		Alerts:      alerts,
		AllClear:    firingCount == 0,
	}
}

func evaluateAgentAlerts(a AgentMetric, now time.Time) []Alert {
	var alerts []Alert

	errorRate := 0.0
	if a.TotalCount > 0 {
		errorRate = float64(a.ErrorCount) / float64(a.TotalCount)
	}

	// AgentHighErrorRate (>10% lifetime, requires min_samples to avoid false positives)
	if errorRate > agentErrorRateWarning && a.TotalCount >= agentMinSamples {
		alerts = append(alerts, Alert{
			Name:      "BTAgentHighErrorRate",
			Severity:  SeverityWarning,
			Component: "bt-agent",
			Summary:   fmt.Sprintf("Agent %s has high error rate", a.Name),
			Description: fmt.Sprintf(
				"Agent %s error rate is %.1f%% (threshold: 10%%) over %d total tasks.",
				a.Name, errorRate*100, a.TotalCount,
			),
			Firing: true,
			Value:  fmt.Sprintf("%.1f%%", errorRate*100),
		})
	}

	// AgentCriticalErrorRate (>50% lifetime, requires min_samples)
	if errorRate > agentErrorRateCritical && a.TotalCount >= agentMinSamples {
		alerts = append(alerts, Alert{
			Name:      "BTAgentCriticalErrorRate",
			Severity:  SeverityCritical,
			Component: "bt-agent",
			Summary:   fmt.Sprintf("Agent %s has critical error rate", a.Name),
			Description: fmt.Sprintf(
				"Agent %s error rate is %.1f%% (threshold: 50%%) over %d total tasks. Immediate investigation required.",
				a.Name, errorRate*100, a.TotalCount,
			),
			Firing: true,
			Value:  fmt.Sprintf("%.1f%%", errorRate*100),
		})
	}

	// AgentNoActivity (last run > 10 min ago, if agent has history)
	if a.TotalCount > 0 && a.LastRun != "" {
		lastRun, err := time.Parse(time.RFC3339, a.LastRun)
		if err == nil && now.Sub(lastRun) > agentNoActivityDuration {
			alerts = append(alerts, Alert{
				Name:      "BTAgentNoActivity",
				Severity:  SeverityWarning,
				Component: "bt-agent",
				Summary:   fmt.Sprintf("Agent %s has no recent activity", a.Name),
				Description: fmt.Sprintf(
					"Agent %s last ran at %s (%s ago). Check if the agent is stuck or the scheduler has stalled.",
					a.Name, a.LastRun, now.Sub(lastRun).Round(time.Second),
				),
				Firing: true,
				Value:  now.Sub(lastRun).Round(time.Second).String(),
			})
		}
	}

	// AgentSlowExecution (avg duration > 5 min)
	if a.AvgDurationMs != "" && a.AvgDurationMs != "0" {
		var avgMs float64
		if _, err := fmt.Sscanf(a.AvgDurationMs, "%f", &avgMs); err == nil && avgMs > agentSlowExecutionMs {
			alerts = append(alerts, Alert{
				Name:      "BTAgentSlowExecution",
				Severity:  SeverityWarning,
				Component: "bt-agent",
				Summary:   fmt.Sprintf("Agent %s has slow execution", a.Name),
				Description: fmt.Sprintf(
					"Agent %s average execution time is %.0fms (threshold: 300000ms / 5 minutes). May indicate Ollama contention or CPU saturation.",
					a.Name, avgMs,
				),
				Firing: true,
				Value:  fmt.Sprintf("%.0fms", avgMs),
			})
		}
	}

	return alerts
}

func evaluateHTTPAlerts(metrics MetricsJSON) []Alert {
	var alerts []Alert

	httpErrorRate := 0.0
	if metrics.HTTPRequestsTotal > 0 {
		httpErrorRate = float64(metrics.HTTPErrorsTotal) / float64(metrics.HTTPRequestsTotal)
	}

	// HTTP error rate > 5%
	if httpErrorRate > httpErrorRateThreshold && metrics.HTTPRequestsTotal > 0 {
		alerts = append(alerts, Alert{
			Name:      "BTDashboardHighHTTPErrorRate",
			Severity:  SeverityWarning,
			Component: "dashboard",
			Summary:   "Dashboard has high HTTP error rate",
			Description: fmt.Sprintf(
				"Dashboard HTTP error rate is %.1f%% (threshold: 5%%) over %d total requests.",
				httpErrorRate*100, metrics.HTTPRequestsTotal,
			),
			Firing: true,
			Value:  fmt.Sprintf("%.1f%%", httpErrorRate*100),
		})
	}

	// Dashboard has received no requests (info severity during idle periods)
	if metrics.HTTPRequestsTotal == 0 {
		alerts = append(alerts, Alert{
			Name:        "BTDashboardNoRequests",
			Severity:    SeverityInfo,
			Component:   "dashboard",
			Summary:     "Dashboard has received no requests",
			Description: fmt.Sprintf("Dashboard has received no HTTP requests for at least %s. This may be normal during low-usage periods.", noRequestDuration),
			Firing:      true,
		})
	}

	return alerts
}

func evaluateGlobalAlerts(metrics MetricsJSON, _ time.Time) []Alert {
	var alerts []Alert

	// No platform activity: no requests AND no agents have recent activity
	if metrics.TotalRequests == 0 && len(metrics.Agents) == 0 {
		alerts = append(alerts, Alert{
			Name:        "BTGlobalNoActivity",
			Severity:    SeverityWarning,
			Component:   "platform",
			Summary:     "No platform activity detected",
			Description: "The platform has received no task requests and has no registered agents. Check if the Hermes gateway and MCP servers are running.",
			Firing:      true,
		})
	}

	// Global error spike: total errors > threshold
	if metrics.TotalErrors > globalErrorSpikeThreshold {
		alerts = append(alerts, Alert{
			Name:      "BTGlobalErrorSpike",
			Severity:  SeverityCritical,
			Component: "platform",
			Summary:   "Global error spike detected",
			Description: fmt.Sprintf(
				"Platform-wide error count is %d (threshold: %d). Multiple agents may be failing.",
				metrics.TotalErrors, globalErrorSpikeThreshold,
			),
			Firing: true,
			Value:  fmt.Sprintf("%d errors", metrics.TotalErrors),
		})
	}

	// Low activity suppression hint: platform is idle, non-critical alerts are noise
	if metrics.TotalRequests == 0 && metrics.HTTPRequestsTotal == 0 {
		alerts = append(alerts, Alert{
			Name:        "BTAlertSuppressionHint",
			Severity:    SeverityInfo,
			Component:   "platform",
			Summary:     "Low platform activity — consider suppressing alerts",
			Description: fmt.Sprintf("Platform request rate is near zero for at least %s. Non-critical alert noise may be unnecessary during idle periods.", lowActivitySuppressDur),
			Firing:      true,
		})
	}

	return alerts
}

// ─── JSON Helpers ────────────────────────────────────────────────────────────

// ParseMetricsJSON parses raw JSON bytes into MetricsJSON.
func ParseMetricsJSON(data []byte) (MetricsJSON, error) {
	var m MetricsJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("parse metrics: %w", err)
	}
	return m, nil
}

// EvaluateFromJSON is a convenience function that parses metrics JSON and evaluates alerts.
func EvaluateFromJSON(data []byte) (AlertReport, error) {
	m, err := ParseMetricsJSON(data)
	if err != nil {
		return AlertReport{}, err
	}
	return EvaluateAlerts(m), nil
}
