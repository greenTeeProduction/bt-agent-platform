package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newProbeNodeServer returns a fake bt-dashboard node that answers the three
// probe endpoints. Its /api/agents/execute echoes the node name in the result
// Output, so callers can tell which backend actually handled a dispatch.
func newProbeNodeServer(t *testing.T, nodeName string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/scalability", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":1}`))
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
		// Output carries the node identity so the probe can prove that a routed
		// dispatch stream fanned out across distinct backends.
		resp := map[string]any{
			"agent":         req.Agent,
			"task":          req.Task,
			"output":        nodeName,
			"success":       true,
			"quality_score": 1,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux)
}

// TestRun_MultiNodeExecute_DrivesDistributedDispatch is the milestone 4/5
// regression: the scalability probe must not merely poke each node's execute
// endpoint independently — it must drive the real horizontal-scaling substrate
// (RemoteExecutor behind an AgentRouter) and emit evidence that a routed task
// stream was distributed across more than one node.
//
// Until the probe is wired to the executor path, the emitted report contains no
// distributed_dispatch section, so this test fails for the intended reason.
func TestRun_MultiNodeExecute_DrivesDistributedDispatch(t *testing.T) {
	n1 := newProbeNodeServer(t, "node-a")
	defer n1.Close()
	n2 := newProbeNodeServer(t, "node-b")
	defer n2.Close()

	var stdout, stderr strings.Builder
	code := run([]string{
		"--nodes", n1.URL + "," + n2.URL,
		"--required-healthy", "2",
		"--execute",
		"--json",
	}, &stdout, &stderr, &http.Client{})

	if code != 0 {
		t.Fatalf("probe exit=%d, stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	var report map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, stdout.String())
	}

	ddRaw, ok := report["distributed_dispatch"]
	if !ok {
		t.Fatalf("report has no distributed_dispatch section — probe never drove the RemoteExecutor+AgentRouter path; report=%s", stdout.String())
	}
	dd, ok := ddRaw.(map[string]any)
	if !ok {
		t.Fatalf("distributed_dispatch must be a JSON object, got %T", ddRaw)
	}

	dispatches, _ := dd["dispatch_count"].(float64)
	if int(dispatches) < 2 {
		t.Fatalf("expected the router to issue at least 2 dispatches, got %v; dd=%v", dd["dispatch_count"], dd)
	}

	distinct, _ := dd["distinct_nodes"].(float64)
	if int(distinct) < 2 {
		t.Fatalf("expected the routed task stream to reach >=2 distinct nodes (distributed dispatch), got %v; dd=%v", dd["distinct_nodes"], dd)
	}

	if okv, _ := dd["ok"].(bool); !okv {
		t.Fatalf("expected distributed_dispatch ok=true, got %v; dd=%v", dd["ok"], dd)
	}
}
