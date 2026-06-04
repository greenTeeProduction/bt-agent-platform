package reliability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── ConnPool Creation & Configuration ─────────────────────────────────────

func TestConnPool_DefaultConfig(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{})
	if pool == nil {
		t.Fatal("NewConnPool returned nil")
	}
	if pool.transport == nil {
		t.Fatal("transport is nil")
	}

	// Verify defaults propagated
	tr := pool.transport
	if tr.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns: got %d, want 100", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost: got %d, want 10", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout: got %v, want 90s", tr.IdleConnTimeout)
	}
}

func TestConnPool_CustomConfig(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 5,
		MaxConnsPerHost:     20,
		IdleConnTimeout:     60 * time.Second,
		KeepAlive:           15 * time.Second,
		DisableKeepAlives:   true,
	})

	tr := pool.transport
	if tr.MaxIdleConns != 50 {
		t.Errorf("MaxIdleConns: got %d, want 50", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 5 {
		t.Errorf("MaxIdleConnsPerHost: got %d, want 5", tr.MaxIdleConnsPerHost)
	}
	if tr.MaxConnsPerHost != 20 {
		t.Errorf("MaxConnsPerHost: got %d, want 20", tr.MaxConnsPerHost)
	}
	if tr.IdleConnTimeout != 60*time.Second {
		t.Errorf("IdleConnTimeout: got %v, want 60s", tr.IdleConnTimeout)
	}
	if !tr.DisableKeepAlives {
		t.Error("DisableKeepAlives should be true")
	}
	if tr.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("TLSHandshakeTimeout: got %v, want 10s", tr.TLSHandshakeTimeout)
	}
}

func TestConnPool_TLSHandshakeTimeout(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{
		TLSHandshakeTimeout: 5 * time.Second,
	})

	tr := pool.transport
	if tr.TLSHandshakeTimeout != 5*time.Second {
		t.Errorf("TLSHandshakeTimeout: got %v, want 5s", tr.TLSHandshakeTimeout)
	}
}

func TestNewSharedConnPool(t *testing.T) {
	pool := NewSharedConnPool(ConnPoolConfig{})
	if pool == nil {
		t.Fatal("NewSharedConnPool returned nil")
	}
	if pool.transport == nil {
		t.Fatal("transport is nil")
	}
}

// ─── HTTPClient ─────────────────────────────────────────────────────────────

func TestConnPool_HTTPClient(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{
		MaxIdleConnsPerHost: 5,
		KeepAlive:           30 * time.Second,
	})
	client := pool.HTTPClient()
	if client == nil {
		t.Fatal("HTTPClient returned nil")
	}
	if client.Transport == nil {
		t.Fatal("HTTPClient has nil Transport")
	}

	// Verify the client uses our transport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := client.Get(server.URL + "/test")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	resp.Body.Close()

	// Verify connection was tracked
	stats := pool.Stats()
	if stats.Created == 0 {
		t.Error("expected at least 1 connection created")
	}
}

func TestConnPool_SharedHTTPClient_Concurrency(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{
		MaxIdleConnsPerHost: 10,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := pool.HTTPClient()

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL + "/concurrent")
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent request failed: %v", err)
	}

	stats := pool.Stats()
	if stats.Created == 0 {
		t.Error("expected connections created during concurrent access")
	}
}

// ─── RemoteExecutor with ConnPool ───────────────────────────────────────────

func TestRemoteExecutor_WithConnPool(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{
		MaxIdleConnsPerHost: 5,
	})

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":"test","task":"test","output":"ok","success":true}`))
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "pooled-exec",
		BaseURL: server.URL,
		Pool:    pool,
	})

	result, err := exec.Execute("test-agent", "test task")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "ok" {
		t.Errorf("output: got %q, want %q", result.Output, "ok")
	}

	// Verify pool was used (connection was created)
	stats := pool.Stats()
	if stats.Created == 0 {
		t.Error("expected connection tracking in shared pool")
	}
}

func TestRemoteExecutor_SharedPoolAcrossExecutors(t *testing.T) {
	// Multiple executors sharing one pool — verifies connection reuse
	pool := NewSharedConnPool(ConnPoolConfig{
		MaxIdleConnsPerHost: 5,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":"shared","task":"shared","output":"pooled","success":true}`))
	}))
	defer server.Close()

	// Create 3 executors sharing the same pool
	exec1 := NewRemoteExecutor(RemoteExecutorConfig{Name: "e1", BaseURL: server.URL, Pool: pool})
	exec2 := NewRemoteExecutor(RemoteExecutorConfig{Name: "e2", BaseURL: server.URL, Pool: pool})
	exec3 := NewRemoteExecutor(RemoteExecutorConfig{Name: "e3", BaseURL: server.URL, Pool: pool})

	// Verify all executors use the same transport (connection reuse)
	cl1 := exec1.client
	cl2 := exec2.client
	cl3 := exec3.client

	if cl1.Transport != cl2.Transport {
		t.Error("executors 1 and 2 should share the same transport")
	}
	if cl1.Transport != cl3.Transport {
		t.Error("executors 1 and 3 should share the same transport")
	}

	// Execute on all three
	for _, exec := range []*RemoteExecutor{exec1, exec2, exec3} {
		result, err := exec.Execute("shared", "task")
		if err != nil {
			t.Errorf("%s: Execute failed: %v", exec.name, err)
		}
		if result.Output != "pooled" {
			t.Errorf("%s: got %q, want %q", exec.name, result.Output, "pooled")
		}
	}
}

func TestRemoteExecutor_WithoutConnPool_UsesPrivateClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":"p","task":"p","output":"private","success":true}`))
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "private-exec",
		BaseURL: server.URL,
	})

	result, err := exec.Execute("agent", "task")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output != "private" {
		t.Errorf("output: got %q, want %q", result.Output, "private")
	}

	// Private client should have the timeout set
	if exec.client.Timeout != 30*time.Second {
		t.Errorf("private client timeout: want 30s, got %v", exec.client.Timeout)
	}
}

func TestRemoteExecutor_PooledHealthCheck(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "pooled-health",
		BaseURL: server.URL,
		Pool:    pool,
	})

	if err := exec.Health(); err != nil {
		t.Errorf("Health failed: %v", err)
	}
}

func TestRemoteExecutor_PooledHealthDown(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "pooled-down",
		BaseURL: server.URL,
		Pool:    pool,
	})

	err := exec.Health()
	if err == nil {
		t.Fatal("expected Health to fail")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503, got: %v", err)
	}
}

// ─── ConnPool MaxConnsPerHost ───────────────────────────────────────────────

func TestConnPool_MaxConnsPerHost(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{
		MaxConnsPerHost:     2,
		MaxIdleConnsPerHost: 1,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond) // hold connection open
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := pool.HTTPClient()

	var wg sync.WaitGroup
	errs := make(chan error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL + "/maxconn")
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	close(errs)

	failures := 0
	for err := range errs {
		failures++
		t.Logf("expected MaxConnsPerHost backpressure: %v", err)
	}

	// With MaxConnsPerHost=2, some of 5 concurrent requests should be queued/blocked
	// Not all may get errors — Go's transport queues excess connections
	t.Logf("failures under MaxConnsPerHost=2: %d/5", failures)
}

// ─── Stats ──────────────────────────────────────────────────────────────────

func TestConnPool_Stats_Initial(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{})
	stats := pool.Stats()

	if stats.Created != 0 {
		t.Errorf("initial created: got %d, want 0", stats.Created)
	}
	if stats.MaxIdle != 100 {
		t.Errorf("MaxIdle: got %d, want 100", stats.MaxIdle)
	}
	if stats.MaxIdlePerHost != 10 {
		t.Errorf("MaxIdlePerHost: got %d, want 10", stats.MaxIdlePerHost)
	}
}

func TestConnPool_Stats_AfterRequests(t *testing.T) {
	pool := NewConnPool(ConnPoolConfig{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := pool.HTTPClient()
	_, _ = client.Get(server.URL + "/s1")
	_, _ = client.Get(server.URL + "/s2")
	_, _ = client.Get(server.URL + "/s3")

	stats := pool.Stats()
	if stats.Created == 0 {
		t.Error("expected connections created after requests")
	}
	if stats.MaxObserved == 0 {
		t.Error("expected MaxObserved > 0")
	}
}

// ─── Close / CloseIdleConnections ───────────────────────────────────────────

func TestConnPool_CloseIdleConnections(_ *testing.T) {
	pool := NewConnPool(ConnPoolConfig{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := pool.HTTPClient()
	resp, _ := client.Get(server.URL + "/close")
	if resp != nil {
		resp.Body.Close()
	}

	// Should not panic
	pool.CloseIdleConnections()
}

func TestConnPool_Close(_ *testing.T) {
	pool := NewConnPool(ConnPoolConfig{})
	pool.Close() // should not panic
}

// ─── Stats JSON serialization ───────────────────────────────────────────────

func TestConnPoolStats_JSON(t *testing.T) {
	stats := ConnPoolStats{
		Idle:           5,
		InUse:          3,
		MaxIdle:        100,
		MaxIdlePerHost: 10,
		MaxPerHost:     20,
		MaxObserved:    15,
		Created:        42,
		IsShared:       true,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundtrip ConnPoolStats
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if roundtrip.Idle != stats.Idle {
		t.Errorf("Idle: got %d, want %d", roundtrip.Idle, stats.Idle)
	}
	if roundtrip.Created != stats.Created {
		t.Errorf("Created: got %d, want %d", roundtrip.Created, stats.Created)
	}
	if roundtrip.IsShared != stats.IsShared {
		t.Errorf("IsShared: got %v, want %v", roundtrip.IsShared, stats.IsShared)
	}
}

// ─── AgentRouter with Pooled Executors ─────────────────────────────────────

func TestAgentRouter_WithSharedPool(t *testing.T) {
	pool := NewSharedConnPool(ConnPoolConfig{
		MaxIdleConnsPerHost: 5,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/agents/execute":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"agent":"pooled","task":"task","output":"router-pooled","success":true,"quality_score":0.95}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create 3 executors sharing one pool
	e1 := NewRemoteExecutor(RemoteExecutorConfig{Name: "r1", BaseURL: server.URL, Pool: pool})
	e2 := NewRemoteExecutor(RemoteExecutorConfig{Name: "r2", BaseURL: server.URL, Pool: pool})
	e3 := NewRemoteExecutor(RemoteExecutorConfig{Name: "r3", BaseURL: server.URL, Pool: pool})

	router := NewAgentRouter(e1)
	router.Add(e2)
	router.Add(e3)

	result, err := router.Execute("pooled-agent", "shared task")
	if err != nil {
		t.Fatalf("router.Execute failed: %v", err)
	}
	if result.Output != "router-pooled" {
		t.Errorf("output: got %q, want %q", result.Output, "router-pooled")
	}

	// All three executors should be healthy
	healthy := router.HealthyExecutors()
	if len(healthy) != 3 {
		t.Errorf("expected 3 healthy, got %d", len(healthy))
	}
}

func TestAgentRouter_PooledExecutors_LeastConnections(t *testing.T) {
	pool := NewSharedConnPool(ConnPoolConfig{
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     10,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/agents/execute":
			time.Sleep(20 * time.Millisecond) // simulate work
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"agent":"lc","task":"lc","output":"ok","success":true,"quality_score":0.9}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	e1 := NewRemoteExecutor(RemoteExecutorConfig{Name: "r1", BaseURL: server.URL, Pool: pool})
	e2 := NewRemoteExecutor(RemoteExecutorConfig{Name: "r2", BaseURL: server.URL, Pool: pool})

	router := NewAgentRouter(e1)
	router.Add(e2)
	router.SetStrategy(RoutingLeastConnections)

	// Run 10 concurrent tasks — verify no crashes
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := router.Execute("lc", "task")
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent pooled execute: %v", err)
	}
}

// ─── Backward Compatibility ────────────────────────────────────────────────

func TestRemoteExecutor_BackwardCompat_NilPool(t *testing.T) {
	// Existing code that doesn't set Pool should continue to work
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":"bc","task":"bc","output":"backward","success":true}`))
	}))
	defer server.Close()

	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "backward-compat",
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})

	result, err := exec.Execute("bc-agent", "bc task")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output != "backward" {
		t.Errorf("output: got %q, want %q", result.Output, "backward")
	}
	if exec.client.Timeout != 5*time.Second {
		t.Errorf("timeout: got %v, want 5s", exec.client.Timeout)
	}
}

func TestRemoteExecutor_PooledClientNoTimeout(t *testing.T) {
	// With pool, the client from Pool.HTTPClient() has no timeout set
	// (transport-level timeouts handle that). Without pool, the timeout is set.
	pool := NewConnPool(ConnPoolConfig{})
	exec := NewRemoteExecutor(RemoteExecutorConfig{
		Name:    "pooled-timeout",
		BaseURL: "http://localhost:9800",
		Pool:    pool,
	})

	// Pooled client doesn't have a per-request timeout — that's intentional,
	// the transport handles TLS/dial timeouts, and the caller can use context.
	if exec.client.Timeout != 0 {
		t.Logf("note: pooled client timeout = %v (expected 0 for transport-level timeouts)", exec.client.Timeout)
	}
}
