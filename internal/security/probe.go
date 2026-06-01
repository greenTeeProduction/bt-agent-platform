package security

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeStatus is the outcome of one security posture check.
type ProbeStatus string

const (
	// ProbePass means the target satisfied the security check.
	ProbePass ProbeStatus = "pass"
	// ProbeFail means the target responded but violated the expected security posture.
	ProbeFail ProbeStatus = "fail"
	// ProbeError means the check could not complete due to transport or protocol errors.
	ProbeError ProbeStatus = "error"
)

// ProbeCheck records the result of a single dashboard security posture check.
type ProbeCheck struct {
	Name     string      `json:"name"`
	Status   ProbeStatus `json:"status"`
	Expected string      `json:"expected"`
	Actual   string      `json:"actual"`
	Detail   string      `json:"detail,omitempty"`
}

// SecurityProbeReport is a machine-readable lightweight penetration/smoke-test report.
type SecurityProbeReport struct {
	Target    string        `json:"target"`
	CheckedAt time.Time     `json:"checked_at"`
	Passed    bool          `json:"passed"`
	Checks    []ProbeCheck  `json:"checks"`
	Duration  time.Duration `json:"duration"`
}

// Summary returns a compact human-readable PASS/TOTAL string for operator reports.
func (r SecurityProbeReport) Summary() string {
	passed := 0
	for _, c := range r.Checks {
		if c.Status == ProbePass {
			passed++
		}
	}
	return fmt.Sprintf("%d/%d security probes passed", passed, len(r.Checks))
}

// ProbeDashboard runs a production-safety security smoke test against a dashboard base URL.
// It is intentionally dependency-free and safe to run from CI, cron, or an operator shell.
// The probe validates externally observable controls: hardening headers on /api/health,
// CORS preflight handling, and rejection of mutating requests that omit JSON Content-Type.
func ProbeDashboard(ctx context.Context, baseURL, apiKey string, client *http.Client) (SecurityProbeReport, error) {
	start := time.Now()
	baseURL = strings.TrimRight(baseURL, "/")
	report := SecurityProbeReport{Target: baseURL, CheckedAt: start.UTC(), Passed: true}

	if baseURL == "" {
		report.Passed = false
		report.Checks = append(report.Checks, ProbeCheck{
			Name:     "target_url",
			Status:   ProbeError,
			Expected: "non-empty dashboard base URL",
			Actual:   "empty",
			Detail:   "baseURL is required",
		})
		report.Duration = time.Since(start)
		return report, fmt.Errorf("baseURL is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/health", nil)
	if err != nil {
		return finishProbe(report, start), err
	}
	if apiKey != "" {
		getReq.Header.Set("X-API-Key", apiKey)
	}
	getResp, err := client.Do(getReq)
	if err != nil {
		report.Checks = append(report.Checks, ProbeCheck{Name: "health_reachable", Status: ProbeError, Expected: "GET /api/health returns HTTP response", Actual: err.Error()})
		return finishProbe(report, start), err
	}
	io.Copy(io.Discard, getResp.Body)
	getResp.Body.Close()

	report.Checks = append(report.Checks, statusCheck("health_reachable", "2xx/3xx/4xx HTTP response", getResp.StatusCode < 500, fmt.Sprintf("status %d", getResp.StatusCode)))
	report.Checks = append(report.Checks, headerEquals(getResp.Header, "X-Content-Type-Options", "nosniff"))
	report.Checks = append(report.Checks, headerPresent(getResp.Header, "Content-Security-Policy"))
	report.Checks = append(report.Checks, headerPresent(getResp.Header, "X-Frame-Options"))
	report.Checks = append(report.Checks, headerPresent(getResp.Header, "Cache-Control"))

	optionsReq, err := http.NewRequestWithContext(ctx, http.MethodOptions, baseURL+"/api/health", nil)
	if err != nil {
		return finishProbe(report, start), err
	}
	optionsResp, err := client.Do(optionsReq)
	if err != nil {
		report.Checks = append(report.Checks, ProbeCheck{Name: "cors_preflight", Status: ProbeError, Expected: "OPTIONS /api/health returns response", Actual: err.Error()})
	} else {
		io.Copy(io.Discard, optionsResp.Body)
		optionsResp.Body.Close()
		ok := optionsResp.StatusCode == http.StatusNoContent || optionsResp.Header.Get("Access-Control-Allow-Methods") != ""
		report.Checks = append(report.Checks, statusCheck("cors_preflight", "204 No Content or Access-Control-Allow-Methods header", ok, fmt.Sprintf("status %d methods=%q", optionsResp.StatusCode, optionsResp.Header.Get("Access-Control-Allow-Methods"))))
	}

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/tasks/approve?id=security-probe", strings.NewReader(`{"probe":true}`))
	if err != nil {
		return finishProbe(report, start), err
	}
	if apiKey != "" {
		postReq.Header.Set("X-API-Key", apiKey)
	}
	postResp, err := client.Do(postReq)
	if err != nil {
		report.Checks = append(report.Checks, ProbeCheck{Name: "mutating_without_json_rejected", Status: ProbeError, Expected: "POST without Content-Type is rejected", Actual: err.Error()})
	} else {
		io.Copy(io.Discard, postResp.Body)
		postResp.Body.Close()
		ok := postResp.StatusCode >= 400 && postResp.StatusCode < 500
		report.Checks = append(report.Checks, statusCheck("mutating_without_json_rejected", "4xx rejection", ok, fmt.Sprintf("status %d", postResp.StatusCode)))
	}

	return finishProbe(report, start), nil
}

func finishProbe(report SecurityProbeReport, start time.Time) SecurityProbeReport {
	report.Duration = time.Since(start)
	report.Passed = true
	for _, c := range report.Checks {
		if c.Status != ProbePass {
			report.Passed = false
			break
		}
	}
	return report
}

func statusCheck(name, expected string, ok bool, actual string) ProbeCheck {
	if ok {
		return ProbeCheck{Name: name, Status: ProbePass, Expected: expected, Actual: actual}
	}
	return ProbeCheck{Name: name, Status: ProbeFail, Expected: expected, Actual: actual}
}

func headerEquals(h http.Header, name, expected string) ProbeCheck {
	actual := h.Get(name)
	if actual == expected {
		return ProbeCheck{Name: "header_" + strings.ToLower(name), Status: ProbePass, Expected: expected, Actual: actual}
	}
	return ProbeCheck{Name: "header_" + strings.ToLower(name), Status: ProbeFail, Expected: expected, Actual: actual}
}

func headerPresent(h http.Header, name string) ProbeCheck {
	actual := h.Get(name)
	if actual != "" {
		return ProbeCheck{Name: "header_" + strings.ToLower(name), Status: ProbePass, Expected: "present", Actual: actual}
	}
	return ProbeCheck{Name: "header_" + strings.ToLower(name), Status: ProbeFail, Expected: "present", Actual: "missing"}
}
