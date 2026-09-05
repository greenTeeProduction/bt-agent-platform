package agent

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// ─── Helpers ────────────────────────────────────────────────────────────────

func alertNames(alerts []Alert) []string {
	names := make([]string, 0, len(alerts))
	for _, a := range alerts {
		names = append(names, a.Name)
	}
	return names
}

func assertAlertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("alert names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("alert names = %v, want %v", got, want)
		}
	}
}

// checkReport pins the invariants that hold across every EvaluateAlerts call:
// every emitted Alert has Firing:true, so TotalRules/FiringCount both equal
// len(want), AllClear is their negation, and EvaluatedAt is RFC3339.
func checkReport(t *testing.T, report AlertReport, want []string) {
	t.Helper()
	assertAlertNames(t, alertNames(report.Alerts), want)
	if report.TotalRules != len(want) {
		t.Errorf("TotalRules = %d, want %d", report.TotalRules, len(want))
	}
	if report.FiringCount != len(want) {
		t.Errorf("FiringCount = %d, want %d", report.FiringCount, len(want))
	}
	if report.AllClear != (len(want) == 0) {
		t.Errorf("AllClear = %v, want %v", report.AllClear, len(want) == 0)
	}
	if _, err := time.Parse(time.RFC3339, report.EvaluatedAt); err != nil {
		t.Errorf("EvaluatedAt = %q is not RFC3339: %v", report.EvaluatedAt, err)
	}
}

// ─── Platform-level alerts (HTTP + global) ─────────────────────────────────

func TestEvaluateAlerts_PlatformLevel(t *testing.T) {
	cases := []struct {
		name    string
		metrics MetricsJSON
		want    []string
	}{
		{
			name:    "fully idle platform fires no-requests, no-activity, and suppression hint",
			metrics: MetricsJSON{},
			want:    []string{"BTDashboardNoRequests", "BTGlobalNoActivity", "BTAlertSuppressionHint"},
		},
		{
			name: "healthy active platform with one healthy agent is all-clear",
			metrics: MetricsJSON{
				TotalRequests:     100,
				HTTPRequestsTotal: 100,
				HTTPErrorsTotal:   0,
				TotalErrors:       0,
				Agents: []AgentMetric{
					{Name: "researcher", TotalCount: 20, ErrorCount: 0, AvgDurationMs: "1000", LastRun: time.Now().UTC().Format(time.RFC3339)},
				},
			},
			want: []string{},
		},
		{
			// NOTE: total_requests (agent-task volume) and http_requests_total
			// (dashboard HTTP volume) are tracked independently. BTDashboardNoRequests
			// keys only off http_requests_total, so it fires here even though
			// total_requests is nonzero.
			name: "BTDashboardNoRequests fires whenever http_requests_total is zero, independent of total_requests",
			metrics: MetricsJSON{
				TotalRequests:     100,
				HTTPRequestsTotal: 0,
			},
			want: []string{"BTDashboardNoRequests"},
		},
		{
			name: "global error spike fires above the raw-count threshold",
			metrics: MetricsJSON{
				TotalRequests:     100,
				HTTPRequestsTotal: 100,
				TotalErrors:       11,
			},
			want: []string{"BTGlobalErrorSpike"},
		},
		{
			name: "global error spike does not fire exactly at the threshold boundary",
			metrics: MetricsJSON{
				TotalRequests:     100,
				HTTPRequestsTotal: 100,
				TotalErrors:       10,
			},
			want: []string{},
		},
		{
			name: "HTTP error rate fires above 5 percent",
			metrics: MetricsJSON{
				TotalRequests:     1000,
				HTTPRequestsTotal: 1000,
				HTTPErrorsTotal:   60,
			},
			want: []string{"BTDashboardHighHTTPErrorRate"},
		},
		{
			name: "HTTP error rate does not fire below 5 percent",
			metrics: MetricsJSON{
				TotalRequests:     1000,
				HTTPRequestsTotal: 1000,
				HTTPErrorsTotal:   40,
			},
			want: []string{},
		},
		{
			name: "BTGlobalNoActivity requires both zero requests and zero agents",
			metrics: MetricsJSON{
				TotalRequests:     0,
				HTTPRequestsTotal: 100,
			},
			want: []string{"BTGlobalNoActivity"},
		},
		{
			name: "BTGlobalNoActivity does not fire when agents are present, even with zero requests",
			metrics: MetricsJSON{
				TotalRequests:     0,
				HTTPRequestsTotal: 100,
				Agents:            []AgentMetric{{Name: "quiet-agent", TotalCount: 5}},
			},
			want: []string{},
		},
		{
			name: "BTGlobalNoActivity does not fire when requests are present, even with zero agents",
			metrics: MetricsJSON{
				TotalRequests:     5,
				HTTPRequestsTotal: 100,
			},
			want: []string{},
		},
		{
			name: "suppression hint fires alongside no-requests when agents are present but both totals are zero",
			metrics: MetricsJSON{
				TotalRequests:     0,
				HTTPRequestsTotal: 0,
				Agents:            []AgentMetric{{Name: "quiet-agent", TotalCount: 5}},
			},
			want: []string{"BTDashboardNoRequests", "BTAlertSuppressionHint"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := EvaluateAlerts(tc.metrics)
			checkReport(t, report, tc.want)
		})
	}
}

// ─── Agent-level alerts ─────────────────────────────────────────────────────

func TestEvaluateAlerts_AgentLevel(t *testing.T) {
	now := time.Now().UTC()
	freshLastRun := now.Format(time.RFC3339)
	staleLastRun := now.Add(-1 * time.Hour).Format(time.RFC3339)
	veryStaleLastRun := now.Add(-24 * time.Hour).Format(time.RFC3339)

	cases := []struct {
		name  string
		agent AgentMetric
		want  []string
	}{
		{
			name:  "below min-samples suppresses high error rate even at 80 percent",
			agent: AgentMetric{Name: "a1", TotalCount: 5, ErrorCount: 4},
			want:  []string{},
		},
		{
			name:  "warning fires at 15 percent with sufficient samples, critical does not",
			agent: AgentMetric{Name: "a2", TotalCount: 20, ErrorCount: 3},
			want:  []string{"BTAgentHighErrorRate"},
		},
		{
			// Non-obvious: critical does NOT suppress warning — both fire together.
			name:  "warning and critical fire simultaneously above 50 percent",
			agent: AgentMetric{Name: "a3", TotalCount: 20, ErrorCount: 15},
			want:  []string{"BTAgentHighErrorRate", "BTAgentCriticalErrorRate"},
		},
		{
			name:  "exactly at the warning threshold does not fire (strict greater-than)",
			agent: AgentMetric{Name: "a4", TotalCount: 100, ErrorCount: 10},
			want:  []string{},
		},
		{
			name:  "no-activity fires when last run is older than 10 minutes",
			agent: AgentMetric{Name: "a5", TotalCount: 1, LastRun: staleLastRun},
			want:  []string{"BTAgentNoActivity"},
		},
		{
			name:  "no-activity does not fire for a freshly active agent",
			agent: AgentMetric{Name: "a6", TotalCount: 1, LastRun: freshLastRun},
			want:  []string{},
		},
		{
			name:  "no-activity check is silently skipped on an unparseable LastRun",
			agent: AgentMetric{Name: "a7", TotalCount: 1, LastRun: "not-a-timestamp"},
			want:  []string{},
		},
		{
			name:  "no-activity check is skipped entirely when TotalCount is zero",
			agent: AgentMetric{Name: "a8", TotalCount: 0, LastRun: veryStaleLastRun},
			want:  []string{},
		},
		{
			name:  "slow execution fires above the 600000ms threshold",
			agent: AgentMetric{Name: "a9", TotalCount: 1, AvgDurationMs: "700000"},
			want:  []string{"BTAgentSlowExecution"},
		},
		{
			name:  "slow execution does not fire below the threshold",
			agent: AgentMetric{Name: "a10", TotalCount: 1, AvgDurationMs: "300000"},
			want:  []string{},
		},
		{
			name:  "slow execution check is silently skipped on non-numeric AvgDurationMs",
			agent: AgentMetric{Name: "a11", TotalCount: 1, AvgDurationMs: "n/a"},
			want:  []string{},
		},
		{
			name:  "slow execution check is explicitly skipped when AvgDurationMs is the literal string zero",
			agent: AgentMetric{Name: "a12", TotalCount: 1, AvgDurationMs: "0"},
			want:  []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A quiet platform wrapper keeps HTTP/global alerts silent so only
			// this agent's alerts can appear in the report.
			quiet := MetricsJSON{
				TotalRequests:     1000,
				HTTPRequestsTotal: 1000,
				HTTPErrorsTotal:   0,
				TotalErrors:       0,
				Agents:            []AgentMetric{tc.agent},
			}
			report := EvaluateAlerts(quiet)
			checkReport(t, report, tc.want)
		})
	}
}

// ─── JSON helpers ────────────────────────────────────────────────────────────

func TestParseMetricsJSON(t *testing.T) {
	t.Run("valid JSON decodes every field, including nested agents", func(t *testing.T) {
		input := []byte(`{
			"http_requests_total": 500,
			"http_errors_total": 10,
			"total_requests": 500,
			"total_errors": 2,
			"agents": [
				{
					"name": "researcher",
					"success_count": 18,
					"error_count": 2,
					"total_count": 20,
					"avg_duration_ms": "1234",
					"success_rate": "90.0%",
					"last_run": "2026-01-01T00:00:00Z"
				}
			]
		}`)

		want := MetricsJSON{
			HTTPRequestsTotal: 500,
			HTTPErrorsTotal:   10,
			TotalRequests:     500,
			TotalErrors:       2,
			Agents: []AgentMetric{
				{
					Name:          "researcher",
					SuccessCount:  18,
					ErrorCount:    2,
					TotalCount:    20,
					AvgDurationMs: "1234",
					SuccessRate:   "90.0%",
					LastRun:       "2026-01-01T00:00:00Z",
				},
			},
		}

		got, err := ParseMetricsJSON(input)
		if err != nil {
			t.Fatalf("ParseMetricsJSON() error = %v, want nil", err)
		}
		if got.HTTPRequestsTotal != want.HTTPRequestsTotal ||
			got.HTTPErrorsTotal != want.HTTPErrorsTotal ||
			got.TotalRequests != want.TotalRequests ||
			got.TotalErrors != want.TotalErrors {
			t.Fatalf("ParseMetricsJSON() top-level fields = %+v, want %+v", got, want)
		}
		if len(got.Agents) != 1 || got.Agents[0] != want.Agents[0] {
			t.Fatalf("ParseMetricsJSON() agents = %+v, want %+v", got.Agents, want.Agents)
		}
	})

	t.Run("invalid JSON returns a wrapped error and the zero value", func(t *testing.T) {
		got, err := ParseMetricsJSON([]byte(`{not valid json`))
		if err == nil {
			t.Fatal("ParseMetricsJSON() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "parse metrics:") {
			t.Errorf("ParseMetricsJSON() error = %q, want prefix %q", err.Error(), "parse metrics:")
		}
		if !reflect.DeepEqual(got, MetricsJSON{}) {
			t.Errorf("ParseMetricsJSON() result = %+v, want zero value", got)
		}
	})
}

func TestEvaluateFromJSON(t *testing.T) {
	t.Run("valid JSON parses then evaluates to an all-clear report", func(t *testing.T) {
		input := []byte(`{
			"http_requests_total": 100,
			"http_errors_total": 0,
			"total_requests": 100,
			"total_errors": 0,
			"agents": [
				{"name": "researcher", "success_count": 20, "error_count": 0, "total_count": 20, "avg_duration_ms": "1000", "success_rate": "100.0%", "last_run": "` + time.Now().UTC().Format(time.RFC3339) + `"}
			]
		}`)

		report, err := EvaluateFromJSON(input)
		if err != nil {
			t.Fatalf("EvaluateFromJSON() error = %v, want nil", err)
		}
		checkReport(t, report, []string{})
	})

	t.Run("invalid JSON propagates the parse error and returns the zero AlertReport", func(t *testing.T) {
		report, err := EvaluateFromJSON([]byte(`{{{`))
		if err == nil {
			t.Fatal("EvaluateFromJSON() error = nil, want non-nil")
		}
		if !reflect.DeepEqual(report, AlertReport{}) {
			t.Errorf("EvaluateFromJSON() result = %+v, want zero value", report)
		}
	})
}
