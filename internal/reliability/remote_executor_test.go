package reliability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteExecutor_Execute_Success(t *testing.T) {
	expected := &AgentResult{
		Agent:        "hermes-monitor",
		Task:         "Check system health",
		Output:       "All systems operational",
		Duration:     1500 * time.Millisecond,
		Success:      true,
		QualityScore: 0.95,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/agents/execute" {
			t.Errorf("expected /api/agents/execute, got %s", r.URL.Path)
		}

		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["agent"] != "hermes-monitor" {
			t.Errorf("expected agent hermes-monitor, got %q", req["agent"])
		}
		if req["task"] != "Check system health" {
			t.Errorf("expected task, got %q", req["task"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "test-remote",
		BaseURL: server.URL,
	})

	result, err := exec.Execute("hermes-monitor", "Check system health")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.Agent != expected.Agent {
		t.Errorf("agent: got %q, want %q", result.Agent, expected.Agent)
	}
	if result.Task != expected.Task {
		t.Errorf("task: got %q, want %q", result.Task, expected.Task)
	}
	if result.Output != expected.Output {
		t.Errorf("output: got %q, want %q", result.Output, expected.Output)
	}
	if result.Success != expected.Success {
		t.Errorf("success: got %v, want %v", result.Success, expected.Success)
	}
	if result.QualityScore != expected.QualityScore {
		t.Errorf("quality: got %f, want %f", result.QualityScore, expected.QualityScore)
	}
}

func TestRemoteExecutor_Execute_FailureResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&AgentResult{
			Agent:   "bad-agent",
			Task:    "bad task",
			Success: false,
			Error:   "agent not found",
		})
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "test-remote",
		BaseURL: server.URL,
	})

	result, err := exec.Execute("bad-agent", "bad task")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.Success {
		t.Error("expected failure result")
	}
	if result.Error != "agent not found" {
		t.Errorf("error: got %q, want %q", result.Error, "agent not found")
	}
}

func TestRemoteExecutor_Execute_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "test-remote",
		BaseURL: server.URL,
	})

	_, err := exec.Execute("any-agent", "any task")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected status 500 in error, got: %v", err)
	}
}

func TestRemoteExecutor_Execute_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "test-remote",
		BaseURL: server.URL,
	})

	_, err := exec.Execute("agent", "task")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal result") {
		t.Errorf("expected unmarshal error, got: %v", err)
	}
}

func TestRemoteExecutor_Health_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("expected /api/health, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "test-remote",
		BaseURL: server.URL,
	})

	if err := exec.Health(); err != nil {
		t.Errorf("Health() should pass, got: %v", err)
	}
}

func TestRemoteExecutor_Health_Down(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "test-remote",
		BaseURL: server.URL,
	})

	err := exec.Health()
	if err == nil {
		t.Fatal("expected Health() to fail with 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503 in error, got: %v", err)
	}
}

func TestRemoteExecutor_Health_Unreachable(t *testing.T) {
	// Use a port that shouldn't have a listener
	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "dead-node",
		BaseURL: "http://127.0.0.1:19999",
		Timeout: 100 * time.Millisecond,
	})

	err := exec.Health()
	if err == nil {
		t.Fatal("expected Health() to fail for unreachable host")
	}
}

func TestRemoteExecutor_String(t *testing.T) {
	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "node-1",
		BaseURL: "http://100.123.73.66:9800",
	})

	s := exec.String()
	if !strings.Contains(s, "node-1") {
		t.Errorf("String() should contain name, got: %s", s)
	}
	if !strings.Contains(s, "100.123.73.66:9800") {
		t.Errorf("String() should contain URL, got: %s", s)
	}
}

func TestRemoteExecutor_WithAPIKey(t *testing.T) {
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "auth-node",
		BaseURL: server.URL,
		APIKey:  "secret-token-123",
	})

	exec.Health()

	if receivedKey != "secret-token-123" {
		t.Errorf("expected API key 'secret-token-123', got %q", receivedKey)
	}
}

func TestRemoteExecutor_WithoutAPIKey(t *testing.T) {
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "no-auth-node",
		BaseURL: server.URL,
		// No APIKey set
	})

	exec.Health()

	if receivedKey != "" {
		t.Errorf("expected no API key header, got %q", receivedKey)
	}
}

func TestRemoteExecutor_CustomTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Short timeout that should still succeed (50ms < 1s)
	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "timeout-node",
		BaseURL: server.URL,
		Timeout: 1 * time.Second,
	})

	if err := exec.Health(); err != nil {
		t.Errorf("Health() failed: %v", err)
	}
}

func TestRemoteExecutor_DefaultTimeout(t *testing.T) {
	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "default-timeout",
		BaseURL: "http://localhost:9800",
	})

	if exec.client.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", exec.client.Timeout)
	}
}

func TestRemoteExecutor_ImplementsAgentExecutor(t *testing.T) {
	var _ AgentExecutor = (*RemoteExecutor)(nil)
}

func TestAgentRouter_WithRemoteExecutor(t *testing.T) {
	// Create a mock remote that always succeeds
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/agents/execute":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&AgentResult{
				Agent:        "test-agent",
				Task:         "hello",
				Output:       "from remote",
				Success:      true,
				QualityScore: 0.9,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer remoteServer.Close()

	remote := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "remote-node-1",
		BaseURL: remoteServer.URL,
	})

	// Local executor as fallback
	local := NewLocalExecutor("local", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{
			Agent:   agent,
			Task:    task,
			Output:  "from local",
			Success: true,
		}, nil
	})

	router := NewAgentRouter(remote)
	router.SetLocal(local)

	result, err := router.Execute("test-agent", "hello")
	if err != nil {
		t.Fatalf("router.Execute(): %v", err)
	}
	if result.Output != "from remote" {
		t.Errorf("expected 'from remote', got %q", result.Output)
	}

	// Verify router reports 1 healthy executor
	healthy := router.HealthyExecutors()
	if len(healthy) != 1 {
		t.Errorf("expected 1 healthy executor, got %d", len(healthy))
	}
	if healthy[0].String() != remote.String() {
		t.Errorf("expected remote executor, got %s", healthy[0].String())
	}
}

func TestAgentRouter_FallsBackToLocal_WhenRemoteUnhealthy(t *testing.T) {
	// Remote is immediately closed (unreachable)
	remote := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "dead-remote",
		BaseURL: "http://127.0.0.1:19999",
		Timeout: 50 * time.Millisecond,
	})

	local := NewLocalExecutor("local-fallback", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{
			Agent:   agent,
			Task:    task,
			Output:  "from local fallback",
			Success: true,
		}, nil
	})

	router := NewAgentRouter(remote)
	router.SetLocal(local)

	result, err := router.Execute("test-agent", "fallback test")
	if err != nil {
		t.Fatalf("router.Execute() should succeed via fallback: %v", err)
	}
	if result.Output != "from local fallback" {
		t.Errorf("expected local fallback, got %q", result.Output)
	}
}
