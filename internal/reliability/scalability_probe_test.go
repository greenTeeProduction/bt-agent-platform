package reliability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

// badNodeHost is a reserved-TLD host (RFC 2606) used only as a lookup key by
// panicOnHostRoundTripper; requests to it never touch the network because the
// RoundTripper panics before any dial is attempted.
const badNodeHost = "bad-node.invalid"

// panicOnHostRoundTripper simulates a misbehaving peer node: any request
// whose Host matches badNodeHost panics instead of returning a response,
// while every other request is forwarded to base untouched.
type panicOnHostRoundTripper struct {
	base http.RoundTripper
}

func (rt panicOnHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == badNodeHost {
		panic("scalability_probe_test: simulated peer probe panic")
	}
	return rt.base.RoundTrip(req)
}

// scalabilityProbePanicSubprocessEnv triggers the subprocess body of
// TestProbeMultiNodeDashboard_PanickingPeerNodeDoesNotAbortOthers. An
// unrecovered panic inside the `go func(s *NodeProbeStatus) {...}` fan-out
// goroutine in ProbeMultiNodeDashboard crashes the entire process — it
// cannot be caught by the parent test's own recover() — so this test
// re-execs itself and asserts the child survives instead of crashing. This
// mirrors the pattern in internal/engine/reactive_parallel_test.go and
// internal/llm/health_test.go.
const scalabilityProbePanicSubprocessEnv = "BT_SCALABILITY_PROBE_PANIC_SUBPROCESS"

// TestProbeMultiNodeDashboard_PanickingPeerNodeDoesNotAbortOthers is the
// regression test for the per-node probe fan-out goroutine in
// ProbeMultiNodeDashboard (scalability_probe.go:113) lacking panic recovery:
// a panic probing one misbehaving peer node must not abort the WaitGroup or
// crash the process, and the other node's probe must still complete.
func TestProbeMultiNodeDashboard_PanickingPeerNodeDoesNotAbortOthers(t *testing.T) {
	if os.Getenv(scalabilityProbePanicSubprocessEnv) == "1" {
		good := newScalabilityProbeServer(t, "good-node", true, true)
		defer good.Close()

		client := &http.Client{Transport: panicOnHostRoundTripper{base: http.DefaultTransport}}

		report, err := ProbeMultiNodeDashboard(context.Background(), MultiNodeProbeConfig{
			Nodes:  []string{good.URL, "http://" + badNodeHost},
			Client: client,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "unexpected validation error: %v\n", err)
			os.Exit(3)
		}
		if len(report.Nodes) != 2 {
			fmt.Fprintf(os.Stderr, "expected both nodes present in report, got %+v\n", report.Nodes)
			os.Exit(3)
		}
		if report.HealthyNodes != 1 || !report.Nodes[0].Healthy || !report.Nodes[0].ScalabilityOK {
			fmt.Fprintf(os.Stderr, "expected good node still probed successfully despite peer panic, got %+v\n", report)
			os.Exit(3)
		}
		if report.Nodes[1].Healthy {
			fmt.Fprintf(os.Stderr, "expected panicking node to not be marked healthy, got %+v\n", report.Nodes[1])
			os.Exit(3)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestProbeMultiNodeDashboard_PanickingPeerNodeDoesNotAbortOthers")
	cmd.Env = append(os.Environ(), scalabilityProbePanicSubprocessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ProbeMultiNodeDashboard: a panic probing one peer node crashed the process "+
			"(or aborted the WaitGroup for the other node) instead of being recovered via "+
			"reliability.SafeGo so the healthy peer's probe still completes; exit error=%v output=%s", err, out)
	}
}

func newScalabilityProbeServer(t *testing.T, nodeName string, healthy, scalability bool) *httptest.Server {
	t.Helper()
	return newScalabilityProbeServerWithExecute(t, nodeName, healthy, scalability, true)
}

func newScalabilityProbeServerWithExecute(t *testing.T, nodeName string, healthy, scalability, executeOk bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		if !healthy {
			http.Error(w, `{"status":"down"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/scalability", func(w http.ResponseWriter, _ *http.Request) {
		if !scalability {
			http.Error(w, `{"error":"missing"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(NewScalabilityStatus(nil, nil, 3, 100, 2, 2, nil, 0, nil))
	})
	mux.HandleFunc("/api/agents/execute", func(w http.ResponseWriter, r *http.Request) {
		if !executeOk {
			http.Error(w, `{"error":"agent not found"}`, http.StatusNotFound)
			return
		}
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

func TestProbeSingleNodeDashboard_Passes(t *testing.T) {
	srv := newScalabilityProbeServer(t, "single-node", true, true)
	defer srv.Close()

	report := ProbeSingleNodeDashboard(context.Background(), SingleNodeProbeConfig{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	})
	if !report.Passed || !report.Healthy || !report.ScalabilityOK {
		t.Fatalf("expected pass, got %+v", report)
	}
	if report.HealthStatusCode != 200 {
		t.Fatalf("expected 200 health, got %d", report.HealthStatusCode)
	}
	if report.ScalabilityStatus == nil {
		t.Fatal("expected scalability status snapshot")
	}
	if !strings.Contains(report.Summary(), "PASS") {
		t.Fatalf("expected PASS summary, got %q", report.Summary())
	}
}

func TestProbeSingleNodeDashboard_Unhealthy(t *testing.T) {
	srv := newScalabilityProbeServer(t, "single-node", false, true)
	defer srv.Close()

	report := ProbeSingleNodeDashboard(context.Background(), SingleNodeProbeConfig{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	})
	if report.Passed || report.Healthy {
		t.Fatalf("expected failure, got %+v", report)
	}
	if !strings.Contains(report.Summary(), "FAIL") {
		t.Fatalf("expected FAIL summary, got %q", report.Summary())
	}
}

func TestProbeSingleNodeDashboard_WithExecute(t *testing.T) {
	srv := newScalabilityProbeServer(t, "single-node", true, true)
	defer srv.Close()

	report := ProbeSingleNodeDashboard(context.Background(), SingleNodeProbeConfig{
		BaseURL: srv.URL,
		Execute: true,
		Agent:   "smoke-test",
		Task:    "verify execution",
		Client:  srv.Client(),
	})
	if !report.Passed || !report.ExecuteOK {
		t.Fatalf("expected execute pass, got %+v", report)
	}
	if report.ExecuteResult == nil || report.ExecuteResult.Agent != "smoke-test" {
		t.Fatalf("expected smoke-test agent result, got %+v", report.ExecuteResult)
	}
}

func TestProbeSingleNodeDashboard_EmptyURL(t *testing.T) {
	report := ProbeSingleNodeDashboard(context.Background(), SingleNodeProbeConfig{
		BaseURL: "",
	})
	if report.Passed || report.Error == "" {
		t.Fatalf("expected failure with error, got %+v", report)
	}
}

func TestProbeSingleNodeDashboard_NilContext(t *testing.T) {
	srv := newScalabilityProbeServer(t, "single-node", true, true)
	defer srv.Close()

	report := ProbeSingleNodeDashboard(context.TODO(), SingleNodeProbeConfig{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	})
	if !report.Passed {
		t.Fatalf("expected pass with nil context, got %+v", report)
	}
}

func TestProbeSingleNodeDashboard_ExecuteFailNoAgent(t *testing.T) {
	srv := newScalabilityProbeServer(t, "single-node", true, true)
	defer srv.Close()

	report := ProbeSingleNodeDashboard(context.Background(), SingleNodeProbeConfig{
		BaseURL: srv.URL,
		Execute: true,
		Agent:   "",
		Task:    "some task",
		Client:  srv.Client(),
	})
	if report.Passed {
		t.Fatalf("expected fail on empty agent, got %+v", report)
	}
}

func TestProbeSingleNodeDashboard_ExecuteServerError(t *testing.T) {
	srv := newScalabilityProbeServerWithExecute(t, "single-node", true, true, false)
	defer srv.Close()

	report := ProbeSingleNodeDashboard(context.Background(), SingleNodeProbeConfig{
		BaseURL: srv.URL,
		Execute: true,
		Agent:   "test-agent",
		Task:    "test task",
		Client:  srv.Client(),
	})
	if report.Passed || report.ExecuteOK {
		t.Fatalf("expected execute failure on server error, got %+v", report)
	}
}
