package monitoring

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvaluateAlerts_AllClear(t *testing.T) {
	metrics := MetricsJSON{
		HTTPRequestsTotal: 100,
		HTTPErrorsTotal:   2, // 2% — below 5% threshold
		TotalRequests:     50,
		TotalErrors:       0,
		Agents: []AgentMetric{
			{
				Name:           "system-monitor",
				SuccessCount:   48,
				ErrorCount:     2,
				TotalCount:     50,
				AvgDurationMs:  "1500",
				SuccessRate:    "96.0%",
				LastRun:        time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	if report.FiringCount != 0 {
		t.Errorf("expected 0 firing alerts, got %d", report.FiringCount)
		for _, a := range report.Alerts {
			if a.Firing {
				t.Logf("  firing: %s — %s", a.Name, a.Description)
			}
		}
	}
	if !report.AllClear {
		t.Error("expected AllClear=true")
	}
}

func TestEvaluateAlerts_AgentHighErrorRate(t *testing.T) {
	metrics := MetricsJSON{
		Agents: []AgentMetric{
			{
				Name:         "code-reviewer",
				SuccessCount: 3,
				ErrorCount:   7,
				TotalCount:   10, // 70% error rate
				LastRun:      time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	// Should fire both warning (70% > 10%) and critical (70% > 50%)
	hasWarning := false
	hasCritical := false
	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTAgentHighErrorRate" {
			hasWarning = true
		}
		if a.Firing && a.Name == "BTAgentCriticalErrorRate" {
			hasCritical = true
		}
	}

	if !hasWarning {
		t.Error("expected BTAgentHighErrorRate to fire for 70% error rate")
	}
	if !hasCritical {
		t.Error("expected BTAgentCriticalErrorRate to fire for 70% error rate")
	}
}

func TestEvaluateAlerts_AgentNoActivity(t *testing.T) {
	// Agent that last ran 15 minutes ago
	lastRun := time.Now().UTC().Add(-15 * time.Minute).Format(time.RFC3339)

	metrics := MetricsJSON{
		Agents: []AgentMetric{
			{
				Name:      "daily-researcher",
				TotalCount: 10,
				LastRun:   lastRun,
			},
		},
	}

	report := EvaluateAlerts(metrics)

	hasNoActivity := false
	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTAgentNoActivity" {
			hasNoActivity = true
		}
	}

	if !hasNoActivity {
		t.Error("expected BTAgentNoActivity to fire for agent idle 15 min")
	}
}

func TestEvaluateAlerts_AgentRecentActivity(t *testing.T) {
	// Agent that ran 1 minute ago — should NOT fire
	lastRun := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)

	metrics := MetricsJSON{
		Agents: []AgentMetric{
			{
				Name:      "daily-researcher",
				TotalCount: 10,
				LastRun:   lastRun,
			},
		},
	}

	report := EvaluateAlerts(metrics)

	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTAgentNoActivity" {
			t.Error("BTAgentNoActivity should NOT fire for agent idle only 1 min")
		}
	}
}

func TestEvaluateAlerts_AgentSlowExecution(t *testing.T) {
	metrics := MetricsJSON{
		Agents: []AgentMetric{
			{
				Name:          "deep-researcher",
				TotalCount:    5,
				AvgDurationMs: "450000", // 7.5 minutes — above 5 min threshold
				LastRun:       time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	hasSlow := false
	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTAgentSlowExecution" {
			hasSlow = true
		}
	}

	if !hasSlow {
		t.Error("expected BTAgentSlowExecution to fire for 450s avg duration")
	}
}

func TestEvaluateAlerts_AgentFastExecution(t *testing.T) {
	metrics := MetricsJSON{
		Agents: []AgentMetric{
			{
				Name:          "quick-agent",
				TotalCount:    5,
				AvgDurationMs: "5000", // 5 seconds — below 5 min threshold
				LastRun:       time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTAgentSlowExecution" {
			t.Error("BTAgentSlowExecution should NOT fire for 5s avg duration")
		}
	}
}

func TestEvaluateAlerts_HTTPErrorRate(t *testing.T) {
	metrics := MetricsJSON{
		HTTPRequestsTotal: 100,
		HTTPErrorsTotal:   12, // 12% — above 5% threshold
		Agents: []AgentMetric{
			{
				Name:         "test-agent",
				SuccessCount: 50,
				TotalCount:   50,
				LastRun:      time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	hasHTTPError := false
	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTDashboardHighHTTPErrorRate" {
			hasHTTPError = true
		}
	}

	if !hasHTTPError {
		t.Error("expected BTDashboardHighHTTPErrorRate to fire for 12% error rate")
	}
}

func TestEvaluateAlerts_HTTPErrorRateLow(t *testing.T) {
	metrics := MetricsJSON{
		HTTPRequestsTotal: 100,
		HTTPErrorsTotal:   3, // 3% — below 5% threshold
	}

	report := EvaluateAlerts(metrics)

	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTDashboardHighHTTPErrorRate" {
			t.Error("BTDashboardHighHTTPErrorRate should NOT fire for 3% error rate")
		}
	}
}

func TestEvaluateAlerts_GlobalErrorSpike(t *testing.T) {
	metrics := MetricsJSON{
		TotalErrors: 12, // above 5 threshold
		Agents: []AgentMetric{
			{
				Name:         "test-agent",
				SuccessCount: 50,
				TotalCount:   50,
				LastRun:      time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	hasSpike := false
	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTGlobalErrorSpike" {
			hasSpike = true
		}
	}

	if !hasSpike {
		t.Error("expected BTGlobalErrorSpike to fire for 12 total errors")
	}
}

func TestEvaluateAlerts_GlobalNoActivity(t *testing.T) {
	metrics := MetricsJSON{
		TotalRequests: 0,
		TotalErrors:   0,
		Agents:        []AgentMetric{}, // no agents
	}

	report := EvaluateAlerts(metrics)

	hasNoActivity := false
	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTGlobalNoActivity" {
			hasNoActivity = true
		}
	}

	if !hasNoActivity {
		t.Error("expected BTGlobalNoActivity to fire for empty platform")
	}
}

func TestEvaluateAlerts_NoFalsePositives(t *testing.T) {
	// Normal, healthy platform
	metrics := MetricsJSON{
		HTTPRequestsTotal: 500,
		HTTPErrorsTotal:   10,  // 2%
		TotalRequests:     200,
		TotalErrors:       3,   // below spike threshold of 5
		Agents: []AgentMetric{
			{
				Name:           "system-monitor",
				SuccessCount:   95,
				ErrorCount:     5,
				TotalCount:     100, // 5% error rate — below 10% warning
				AvgDurationMs:  "2500",
				SuccessRate:    "95.0%",
				LastRun:        time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339),
			},
			{
				Name:           "code-reviewer",
				SuccessCount:   8,
				ErrorCount:     2,
				TotalCount:     10, // 20% — above 10% warning, below 50% critical
				AvgDurationMs:  "120000", // 2 min — below 5 min
				SuccessRate:    "80.0%",
				LastRun:        time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	// code-reviewer at 20% should fire warning but not critical
	hasWarning := false
	hasCritical := false
	for _, a := range report.Alerts {
		if a.Firing && a.Name == "BTAgentHighErrorRate" && a.Summary == "Agent code-reviewer has high error rate" {
			hasWarning = true
		}
		if a.Firing && a.Name == "BTAgentCriticalErrorRate" {
			hasCritical = true
		}
	}

	if !hasWarning {
		t.Error("expected BTAgentHighErrorRate for code-reviewer at 20%")
	}
	if hasCritical {
		t.Error("BTAgentCriticalErrorRate should NOT fire for 20% error rate")
	}

	// system-monitor at 5% should NOT fire anything
	for _, a := range report.Alerts {
		if a.Firing && a.Component == "bt-agent" && a.Summary == "Agent system-monitor has high error rate" {
			t.Error("system-monitor at 5%% should NOT fire any agent alerts")
		}
	}
}

func TestParseMetricsJSON(t *testing.T) {
	raw := `{
		"http_requests_total": 42,
		"http_errors_total": 3,
		"total_requests": 10,
		"total_errors": 1,
		"agents": [
			{
				"name": "test-agent",
				"success_count": 8,
				"error_count": 2,
				"total_count": 10,
				"avg_duration_ms": "1500",
				"success_rate": "80.0%",
				"last_run": "2026-05-28T10:00:00Z"
			}
		]
	}`

	m, err := ParseMetricsJSON([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.HTTPRequestsTotal != 42 {
		t.Errorf("HTTPRequestsTotal = %d, want 42", m.HTTPRequestsTotal)
	}
	if len(m.Agents) != 1 {
		t.Errorf("len(Agents) = %d, want 1", len(m.Agents))
	}
	if m.Agents[0].LastRun != "2026-05-28T10:00:00Z" {
		t.Errorf("LastRun = %s, want 2026-05-28T10:00:00Z", m.Agents[0].LastRun)
	}
}

func TestEvaluateFromJSON(t *testing.T) {
	raw := `{"http_requests_total": 0, "http_errors_total": 0, "total_requests": 0, "total_errors": 0, "agents": []}`

	report, err := EvaluateFromJSON([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalRules == 0 {
		t.Error("expected at least 1 rule evaluated (BTGlobalNoActivity)")
	}
}

func TestAlertReport_JSON(t *testing.T) {
	metrics := MetricsJSON{
		HTTPRequestsTotal: 10,
		Agents: []AgentMetric{
			{
				Name:         "test-agent",
				SuccessCount: 5,
				TotalCount:   10,
				LastRun:      time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	report := EvaluateAlerts(metrics)

	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if len(b) < 10 {
		t.Error("json output too short")
	}

	var decoded AlertReport
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if decoded.EvaluatedAt == "" {
		t.Error("EvaluatedAt should not be empty")
	}
}
