package reliability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeMultiNodeDashboard_PassesTwoHealthyNodes(t *testing.T) {
	n1 := newScalabilityProbeServer(t, "node-a", true, true)
	defer n1.Close()
	n2 := newScalabilityProbeServer(t, "node-b", true, true)
	defer n2.Close()

	report, err := ProbeMultiNodeDashboard(context.Background(), MultiNodeProbeConfig{
		Nodes:           []string{n1.URL, n2.URL},
		RequiredHealthy: 2,
		Client:          n1.Client(),
	})
	if err != nil {
		t.Fatalf("probe returned error: %v", err)
	}
	if !report.Passed || report.HealthyNodes != 2 || len(report.Nodes) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Nodes[0].ScalabilityStatus == nil || report.Nodes[1].ScalabilityStatus == nil {
		t.Fatalf("expected scalability snapshots for both nodes: %+v", report.Nodes)
	}
	if !strings.Contains(report.Summary(), "PASS") {
		t.Fatalf("expected PASS summary, got %q", report.Summary())
	}
}

func TestProbeMultiNodeDashboard_DetectsUnhealthyNode(t *testing.T) {
	healthy := newScalabilityProbeServer(t, "healthy", true, true)
	defer healthy.Close()
	unhealthy := newScalabilityProbeServer(t, "unhealthy", false, true)
	defer unhealthy.Close()

	report, err := ProbeMultiNodeDashboard(context.Background(), MultiNodeProbeConfig{
		Nodes:           []string{healthy.URL, unhealthy.URL},
		RequiredHealthy: 2,
		Client:          healthy.Client(),
	})
	if err != nil {
		t.Fatalf("probe transport should succeed: %v", err)
	}
	if report.Passed || report.HealthyNodes != 1 {
		t.Fatalf("expected failed one-healthy report, got %+v", report)
	}
	if report.Nodes[1].Error == "" || !strings.Contains(report.Nodes[1].Error, "health") {
		t.Fatalf("expected health diagnostic for bad node, got %+v", report.Nodes[1])
	}
}

func TestProbeMultiNodeDashboard_ExecuteSmoke(t *testing.T) {
	n1 := newScalabilityProbeServer(t, "node-a", true, true)
	defer n1.Close()
	n2 := newScalabilityProbeServer(t, "node-b", true, true)
	defer n2.Close()

	report, err := ProbeMultiNodeDashboard(context.Background(), MultiNodeProbeConfig{
		Nodes:           []string{n1.URL, n2.URL},
		RequiredHealthy: 2,
		Execute:         true,
		Agent:           "scalability-smoke",
		Task:            "check distributed execution smoke path",
		Client:          n1.Client(),
	})
	if err != nil {
		t.Fatalf("probe returned error: %v", err)
	}
	if !report.Passed || !report.ExecuteEnabled {
		t.Fatalf("expected execute-enabled pass, got %+v", report)
	}
	for _, node := range report.Nodes {
		if !node.ExecuteOK || node.ExecuteResult == nil || node.ExecuteResult.Agent != "scalability-smoke" {
			t.Fatalf("expected execute success on %s, got %+v", node.URL, node)
		}
	}
}

func TestProbeMultiNodeDashboard_Validation(t *testing.T) {
	if report, err := ProbeMultiNodeDashboard(context.Background(), MultiNodeProbeConfig{Nodes: []string{"http://one"}}); err == nil || report.Passed {
		t.Fatalf("expected single-node validation failure, got report=%+v err=%v", report, err)
	}
	if report, err := ProbeMultiNodeDashboard(context.Background(), MultiNodeProbeConfig{Nodes: []string{"http://one", "http://two"}, Execute: true}); err == nil || report.Passed {
		t.Fatalf("expected execute validation failure, got report=%+v err=%v", report, err)
	}
}

func newScalabilityProbeServer(t *testing.T, nodeName string, healthy, scalability bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			http.Error(w, `{"status":"down"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/scalability", func(w http.ResponseWriter, r *http.Request) {
		if !scalability {
			http.Error(w, `{"error":"missing"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(NewScalabilityStatus(nil, nil, 3, 100, 2, 2, nil, 0, nil))
	})
	mux.HandleFunc("/api/agents/execute", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Agent string `json:"agent"`
			Task  string `json:"task"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&AgentResult{
			Agent:        req.Agent,
			Task:         req.Task,
			Output:       nodeName,
			Duration:     time.Millisecond,
			Success:      true,
			QualityScore: 1,
		})
	})
	return httptest.NewServer(mux)
}
